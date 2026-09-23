package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/jvm"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// modules returns the modules imported by the file, in source order (FR-6,
// FR-7). The walk reads the path, the binding and the star out of the
// grammar; jvm.Imports holds what they mean, the JDK packages included.
func modules(g *grammar, root *ts.Node, src []byte, prefixes []string) []jvm.Module {
	imports := jvm.NewImports(prefixes, jvm.IsJDK)
	for _, child := range treesitter.NamedChildren(root) {
		n := child
		if g.kindOf(&n) != kindImportDeclaration {
			continue
		}
		collectImport(g, &n, src, imports)
	}
	return imports.Modules()
}

// collectImport reports one import statement to imports. A `static` import
// needs no handling of its own: the grammar spells the keyword as an
// anonymous token and leaves the path where an ordinary import has it.
func collectImport(g *grammar, n *ts.Node, src []byte, imports *jvm.Imports) {
	name := importName(g, n)
	if name == nil {
		return
	}
	path, at := treesitter.Text(name, src), treesitter.SpanOf(n)
	if hasStar(g, n) {
		imports.Star(path, at)
		return
	}
	imports.Bind(path, importBinding(g, name, src), at)
}

// importName returns the child naming what an import brings in: a
// scoped_identifier, or a bare identifier for a single-segment import. On a
// star import it already stops before the `.*`, which the grammar hangs off
// the statement rather than off the path.
func importName(g *grammar, n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		c := child
		switch g.kindOf(&c) {
		case kindScopedIdentifier, kindIdentifier:
			return &c
		}
	}
	return nil
}

// hasStar reports whether an import ends in `.*`, which the grammar spells as
// an asterisk node beside the path, in `import a.b.*;` and in the static
// `import a.b.C.*;` alike.
func hasStar(g *grammar, n *ts.Node) bool {
	for _, child := range treesitter.NamedChildren(n) {
		c := child
		if g.kindOf(&c) == kindAsterisk {
			return true
		}
	}
	return false
}

// importBinding returns the local name a named import introduces: the last
// segment of the path, which the grammar names, so a static member import
// binds the member (`rate` of `com.acme.shared.Rates.rate`) rather than the
// class holding it. Java has no import alias.
func importBinding(g *grammar, name *ts.Node, src []byte) string {
	if last := name.ChildByFieldId(g.fields.name); last != nil {
		return treesitter.Text(last, src)
	}
	return treesitter.Text(name, src)
}

// countCoupling charges the unit for the modules it uses, one point per
// module. The charge points at the import that brings the module in, which
// is above the unit rather than inside it: it is the one place the
// dependency is written down.
func (c *counter) countCoupling(mods []jvm.Module) {
	for i := range mods {
		m := &mods[i]
		if m.UsedBy(c.decls.refs) {
			c.chargeSpan(m.Metric, m.At)
		}
	}
}
