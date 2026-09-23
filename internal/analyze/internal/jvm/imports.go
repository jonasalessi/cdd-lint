package jvm

import (
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Module is one qualified path a file imports, with the import statements
// that name the same path merged into a single entry.
type Module struct {
	// Path is the qualified path the imports name.
	Path string
	// Bindings are the local names the imports introduce: the last segment
	// of the path, or the alias the language allows for it.
	Bindings []string
	// Metric is the coupling metric the module is charged to.
	Metric config.MetricID
	// Star marks `import a.b.*`, which binds no name the analyzer can see
	// and is therefore charged to every unit of the file, like a
	// side-effect import in TypeScript.
	Star bool
	// At is the range of the first import naming the path, which is where
	// the module's coupling occurrence points. That statement sits outside
	// every unit it is charged to, as the contract on analyze.Occurrence
	// says.
	At treesitter.Span
}

// UsedBy reports whether a unit mentioning refs uses the module. CDD counts
// "direct references to domain classes" and "external library units" per unit
// (docs/cdd.md section 2), but imports are file-level, so a module belongs to
// a unit when the unit mentions one of the module's bindings, once per module
// however many bindings or mentions there are. A star import binds nothing
// the analyzer can see and belongs to every unit of the file.
//
// The reference test is by name only: a local declaration that shadows an
// imported name makes the unit look like a user of that module.
func (m *Module) UsedBy(refs map[string]struct{}) bool {
	if m.Star {
		return true
	}
	for _, binding := range m.Bindings {
		if _, ok := refs[binding]; ok {
			return true
		}
	}
	return false
}

// Classify returns the coupling metric a qualified path is charged to: a
// project prefix wins, then the standard library, and everything else is
// external. The precedence is what keeps `internal_coupling.packages`
// meaning what it says, so a project that owns a prefix the platform also
// uses still reads as internal. A nil stdlib predicate makes nothing
// standard library, which is how a language that has not declared its
// platform packages behaves.
func Classify(path string, prefixes []string, stdlib func(string) bool) config.MetricID {
	switch {
	case IsInternal(path, prefixes):
		return config.MetricInternalCoupling
	case stdlib != nil && stdlib(path):
		return config.MetricStdlibCoupling
	default:
		return config.MetricExternalCoupling
	}
}

// IsInternal reports whether a qualified path belongs to the project: it does
// when it equals one of the configured prefixes or is its dot-delimited
// subpath ("com.acme" matching "com.acme.shared.Money"). An empty prefix
// matches nothing. The JVM languages have no relative import, and the
// analyzer never guesses from the file's own package: a same-package
// reference needs no import and is invisible to it.
func IsInternal(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if prefix != "" && (path == prefix || strings.HasPrefix(path, prefix+".")) {
			return true
		}
	}
	return false
}

// Imports collects the modules a file imports, in source order. Every import
// names one qualified path; two imports of the same path are one module with
// two bindings. The caller reads the paths, aliases and stars out of its own
// grammar and reports them here, which is everything about imports that is
// about the JVM rather than about one language.
type Imports struct {
	modules  []Module
	index    map[string]int
	prefixes []string
	stdlib   func(string) bool
}

// NewImports returns an empty collection classifying paths against the
// project prefixes and the language's standard-library predicate, which may
// be nil.
func NewImports(prefixes []string, stdlib func(string) bool) *Imports {
	return &Imports{index: map[string]int{}, prefixes: prefixes, stdlib: stdlib}
}

// Bind records a named import of path introducing binding, which an empty
// binding leaves out: the module is still known, it just binds no name.
func (s *Imports) Bind(path, binding string, at treesitter.Span) {
	m := s.module(path, at)
	if binding == "" {
		return
	}
	m.Bindings = append(m.Bindings, binding)
}

// Star records an `import a.b.*` of path.
func (s *Imports) Star(path string, at treesitter.Span) {
	s.module(path, at).Star = true
}

// Modules returns the collected modules in source order.
func (s *Imports) Modules() []Module {
	return s.modules
}

// module returns the entry for path, appending it on first sight so that its
// range is the first import naming it.
func (s *Imports) module(path string, at treesitter.Span) *Module {
	if i, ok := s.index[path]; ok {
		return &s.modules[i]
	}
	s.index[path] = len(s.modules)
	s.modules = append(s.modules, Module{
		Path:   path,
		Metric: Classify(path, s.prefixes, s.stdlib),
		At:     at,
	})
	return &s.modules[len(s.modules)-1]
}
