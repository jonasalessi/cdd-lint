package golang

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// The two import names that bind nothing a reference can name: a dot import
// drops its exported names into the file scope, where they are
// indistinguishable from locals, and a blank import is a side effect.
const (
	dotImport   = "."
	blankImport = "_"
)

// module is one import path a file names, with the specs naming the same
// path merged into a single entry.
type module struct {
	// path is the quoted path the imports resolve, unquoted.
	path string
	// bindings are the local names the imports introduce: the alias when a
	// spec has one, otherwise the name the compiler assumes for the path.
	bindings []string
	// metric is the coupling metric the module is charged to.
	metric config.MetricID
	// star marks a `.` or `_` import, which binds no name the walk can
	// match and is therefore charged to every unit of the file, like Java's
	// star import and TypeScript's side-effect import.
	star bool
	// at is the range of the first spec naming the path, which is where the
	// module's coupling occurrence points. That spec sits outside every
	// unit it is charged to, as the contract on analyze.Occurrence says.
	at treesitter.Span
}

// usedBy reports whether a unit mentioning refs uses the module. CDD counts
// direct references to domain and library units per unit (docs/cdd.md
// section 2), but imports are file-level, so a module belongs to a unit when
// the unit qualifies something by one of its bindings, once per module
// however many bindings or mentions there are. A `.` or `_` import binds no
// name and belongs to every unit.
func (m *module) usedBy(refs map[string]struct{}) bool {
	if m.star {
		return true
	}
	for _, binding := range m.bindings {
		if _, ok := refs[binding]; ok {
			return true
		}
	}
	return false
}

// modules returns the modules the file imports, in source order (FR-8). A
// path imported twice under two names is one module carrying both bindings.
func modules(file *ast.File, fset *token.FileSet, prefixes []string) []module {
	collected := &imports{fset: fset, prefixes: prefixes, index: map[string]int{}}
	for _, spec := range file.Imports {
		collected.add(spec)
	}
	return collected.out
}

// imports collects the modules of one file in source order.
type imports struct {
	fset     *token.FileSet
	prefixes []string
	index    map[string]int
	out      []module
}

// add records one import spec under the module of its path. A path the
// scanner accepted but strconv cannot unquote names no module.
func (s *imports) add(spec *ast.ImportSpec) {
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return
	}
	bind(s.module(importPath, spec), spec, importPath)
}

// module returns the entry for importPath, appending it on first sight so
// that its range is that of the first spec naming it.
func (s *imports) module(importPath string, spec *ast.ImportSpec) *module {
	if i, ok := s.index[importPath]; ok {
		return &s.out[i]
	}
	s.index[importPath] = len(s.out)
	s.out = append(s.out, module{
		path:   importPath,
		metric: classify(importPath, s.prefixes),
		at:     spanOf(s.fset, spec),
	})
	return &s.out[len(s.out)-1]
}

// bind records the name one spec introduces: its alias when it has one,
// otherwise the name the compiler assumes for the path. A `.` or `_` alias
// names nothing and marks the module as charged to every unit instead.
func bind(m *module, spec *ast.ImportSpec, importPath string) {
	name := assumedName(importPath)
	if spec.Name != nil {
		name = spec.Name.Name
	}
	if name == dotImport || name == blankImport {
		m.star = true
		return
	}
	m.bindings = append(m.bindings, name)
}

// classify returns the coupling metric an import path is charged to. A
// configured project prefix wins over the standard library, so a project
// that lists a dotless path of its own among its packages keeps counting it
// as internal coupling.
func classify(importPath string, prefixes []string) config.MetricID {
	switch {
	case isInternal(importPath, prefixes):
		return config.MetricInternalCoupling
	case isStdlib(importPath):
		return config.MetricStdlibCoupling
	default:
		return config.MetricExternalCoupling
	}
}

// isInternal reports whether an import path belongs to the project: it does
// when it equals one of the configured prefixes or is a slash-delimited
// subpath of one, so "example.com/app" matches "example.com/app/shared" and
// not "example.com/apples". An empty prefix matches nothing, and the
// analyzer never guesses from the file's own package clause: a
// same-package reference needs no import and is invisible to it.
func isInternal(importPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && (importPath == prefix || strings.HasPrefix(importPath, prefix+"/")) {
			return true
		}
	}
	return false
}

// assumedName returns the local name a spec without an alias introduces. Go
// takes it from the package clause of the imported package, which a
// per-file analyzer cannot read, so this is goimports'
// ImportPathToAssumedName instead: the last element of the path, stepping
// over a "vN" major-version element, without a leading "go-" and cut at the
// first character an identifier cannot hold. It reads "gopkg.in/yaml.v3" as
// yaml, "github.com/mattn/go-isatty" as isatty, "github.com/nats-io/nats.go"
// as nats and "github.com/jackc/pgx/v5" as pgx.
//
// A package whose real clause disagrees with the assumption is charged to no
// unit rather than to the wrong one, which is the same outcome as an unused
// import.
func assumedName(importPath string) string {
	base := path.Base(importPath)
	if isMajorVersion(base) && path.Dir(importPath) != "." {
		base = path.Base(path.Dir(importPath))
	}
	base = strings.TrimPrefix(base, "go-")
	if i := strings.IndexFunc(base, notIdentifier); i >= 0 {
		base = base[:i]
	}
	return base
}

// isMajorVersion reports whether a path element is a major-version suffix —
// "v2", "v5" — which names no package and is stepped over.
func isMajorVersion(element string) bool {
	if !strings.HasPrefix(element, "v") {
		return false
	}
	_, err := strconv.Atoi(element[1:])
	return err == nil
}

// notIdentifier reports whether a character cannot appear in a Go
// identifier, which is where the assumed name is cut: the dot of "nats.go"
// and of "yaml.v3", and anything else a path element may hold.
func notIdentifier(ch rune) bool {
	switch {
	case 'a' <= ch && ch <= 'z', 'A' <= ch && ch <= 'Z', '0' <= ch && ch <= '9', ch == '_':
		return false
	case ch >= utf8.RuneSelf:
		return !unicode.IsLetter(ch) && !unicode.IsDigit(ch)
	default:
		return true
	}
}

// countCoupling charges the unit for the modules it uses, one point per
// module. The charge points at the import that brings the module in, which
// is above the unit rather than inside it: it is the one place the
// dependency is written down.
func (c *counter) countCoupling(mods []module) {
	for i := range mods {
		m := &mods[i]
		if m.usedBy(c.refs) {
			c.chargeSpan(m.metric, m.at)
		}
	}
}
