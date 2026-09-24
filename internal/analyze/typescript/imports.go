package typescript

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// module is one module a file imports, with the import statements that name
// the same specifier merged into a single entry.
type module struct {
	specifier string
	// bindings are the local names the import introduces: the default
	// name, the named imports under their alias, and the namespace name.
	bindings []string
	// metric is the coupling metric the module is charged to.
	metric config.MetricID
	// sideEffect marks `import "x"`, which introduces no binding and is
	// therefore charged to every unit of the file.
	sideEffect bool
	// at is the range of the first import statement naming the specifier,
	// which is where the module's coupling occurrence points. That
	// statement sits outside every unit it is charged to, as the contract
	// on analyze.Occurrence says.
	at treesitter.Span
}

// modules returns the modules imported by the file, in source order (FR-6).
//
// Type-only imports with bindings count exactly like value imports: `import
// type { X }` and `import { type X }` are dependencies on the same module. An
// empty type-only clause introduces no dependency. Re-exports (`export … from
// "x"`) and dynamic `import()` are not import statements and contribute
// nothing. `import x = require("y")` is an import statement and is treated
// exactly like a default import: one binding, classified by the same rules.
func modules(g *grammar, root *ts.Node, src []byte, prefixes []string) []module {
	var out []module
	index := map[string]int{}
	for _, child := range treesitter.NamedChildren(root) {
		n := child
		if g.kindOf(&n) != kindImportStatement {
			continue
		}
		spec := specifier(g, &n, src)
		if spec == "" {
			continue
		}
		at, ok := index[spec]
		if !ok {
			at = len(out)
			index[spec] = at
			out = append(out, module{
				specifier: spec,
				metric:    classify(spec, prefixes),
				at:        treesitter.SpanOf(&n),
			})
		}
		bindings, sideEffect := importBindings(g, &n, src)
		out[at].bindings = append(out[at].bindings, bindings...)
		out[at].sideEffect = out[at].sideEffect || sideEffect
	}
	return out
}

// specifier returns the unquoted module specifier of an import statement.
// `import x = require("y")` carries no source of its own: the grammar hangs
// the string off the import_require_clause, so the clause is asked when the
// statement has nothing.
func specifier(g *grammar, n *ts.Node, src []byte) string {
	raw := treesitter.Text(n.ChildByFieldId(g.fields.source), src)
	if raw == "" {
		raw = treesitter.Text(requireSource(g, n), src)
	}
	if len(raw) < 2 {
		return ""
	}
	return raw[1 : len(raw)-1]
}

// requireSource returns the source string of the statement's
// import_require_clause, nil when the statement has no such clause.
func requireSource(g *grammar, n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		clause := child
		if g.kindOf(&clause) == kindImportRequireClause {
			return clause.ChildByFieldId(g.fields.source)
		}
	}
	return nil
}

// importBindings returns the local names an import statement introduces and
// whether it is a side-effect import, which introduces none.
func importBindings(g *grammar, n *ts.Node, src []byte) (names []string, sideEffect bool) {
	hasClause := false
	for _, child := range treesitter.NamedChildren(n) {
		clause := child
		switch g.kindOf(&clause) {
		case kindImportClause:
			hasClause = true
			names = append(names, clauseBindings(g, &clause, src)...)
		case kindImportRequireClause:
			hasClause = true
			if name := requireBinding(g, &clause, src); name != "" {
				names = append(names, name)
			}
		}
	}
	return names, !hasClause
}

// requireBinding returns the local name `import x = require("y")` binds,
// which the grammar hangs off the clause as a plain identifier child, the
// quoted source being the clause's only other named child.
func requireBinding(g *grammar, clause *ts.Node, src []byte) string {
	for _, child := range treesitter.NamedChildren(clause) {
		id := child
		if g.kindOf(&id) == kindIdentifier {
			return treesitter.Text(&id, src)
		}
	}
	return ""
}

// clauseBindings collects the default name, the namespace name and the
// named imports of one import clause. A named import is bound under its
// alias when it has one: `{ a as b }` introduces b.
func clauseBindings(g *grammar, clause *ts.Node, src []byte) []string {
	var names []string
	for _, child := range treesitter.NamedChildren(clause) {
		n := child
		switch g.kindOf(&n) {
		case kindIdentifier:
			names = append(names, treesitter.Text(&n, src))
		case kindNamespaceImport:
			if id := treesitter.FirstNamedChild(&n); id != nil {
				names = append(names, treesitter.Text(id, src))
			}
		case kindNamedImports:
			names = append(names, specifierBindings(g, &n, src)...)
		}
	}
	return names
}

// specifierBindings collects the local names of a `{ … }` import list.
func specifierBindings(g *grammar, list *ts.Node, src []byte) []string {
	var names []string
	for _, child := range treesitter.NamedChildren(list) {
		spec := child
		if g.kindOf(&spec) != kindImportSpecifier {
			continue
		}
		local := spec.ChildByFieldId(g.fields.alias)
		if local == nil {
			local = spec.ChildByFieldId(g.fields.name)
		}
		if name := treesitter.Text(local, src); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// classify returns the coupling metric a module specifier is charged to. An
// internal prefix wins over the Node.js built-ins, so a project that
// configures "path" as one of its own packages keeps counting it as
// internal.
func classify(spec string, prefixes []string) config.MetricID {
	switch {
	case isInternal(spec, prefixes):
		return config.MetricInternalCoupling
	case isBuiltin(spec):
		return config.MetricStdlibCoupling
	default:
		return config.MetricExternalCoupling
	}
}

// isInternal reports whether a module specifier points inside the project.
// Relative and root-anchored specifiers always do; a bare specifier does
// when it equals one of the configured internal prefixes or is its
// slash-delimited subpath ("@app/" matching "@app/users"); everything else,
// "node:fs" and "lodash/fp" included, does not.
func isInternal(spec string, prefixes []string) bool {
	if isRelative(spec) {
		return true
	}
	for _, prefix := range prefixes {
		base := strings.TrimSuffix(prefix, "/")
		if base != "" && (spec == base || strings.HasPrefix(spec, base+"/")) {
			return true
		}
	}
	return false
}

// isRelative reports whether spec points inside the project by path.
func isRelative(spec string) bool {
	return spec == "." || spec == ".." ||
		strings.HasPrefix(spec, "./") ||
		strings.HasPrefix(spec, "../") ||
		strings.HasPrefix(spec, "/")
}

// countCoupling charges the unit for the modules it uses. CDD counts
// "direct references to domain classes" and "external library units" per
// unit (docs/cdd.md section 2), but imports are file-level, so a module is
// charged to a unit when the unit mentions one of the module's bindings,
// once per module however many bindings or mentions there are. A
// side-effect import binds nothing and is charged to every unit of the
// file.
//
// The charge points at the import statement that brings the module in,
// which is above the unit rather than inside it: it is the one place the
// dependency is written down.
//
// The reference test is by name only: a local declaration that shadows an
// imported name makes the unit look like a user of that module.
func (c *counter) countCoupling(mods []module) {
	for i := range mods {
		m := &mods[i]
		if !m.sideEffect && !c.uses(m.bindings) {
			continue
		}
		c.chargeSpan(m.metric, m.at, 1)
	}
}

// uses reports whether the unit mentions any of the given bindings.
func (c *counter) uses(bindings []string) bool {
	for _, b := range bindings {
		if _, ok := c.decls.refs[b]; ok {
			return true
		}
	}
	return false
}
