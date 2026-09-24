package kotlin

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/jvm"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// stdlibPrefix is the package tree the language's own standard library
// occupies. The trailing dot is what keeps the kotlinx tree out: coroutines
// and serialization ship and version apart from the language, so coupling to
// them is coupling to a library.
const stdlibPrefix = "kotlin."

// isStdlib reports whether a qualified path names a standard-library
// package. A Kotlin file stands on two platforms at once: the language's own
// library and the JDK it runs on.
func isStdlib(path string) bool {
	return strings.HasPrefix(path, stdlibPrefix) || jvm.IsJDK(path)
}

// modules returns the modules imported by the file, in source order
// (FR-7, FR-8). The walk reads the path, the alias and the star out of the
// grammar; jvm.Imports holds what they mean, the standard library included.
func modules(g *grammar, root *ts.Node, src []byte, prefixes []string) []jvm.Module {
	imports := jvm.NewImports(prefixes, isStdlib)
	for _, child := range treesitter.NamedChildren(root) {
		n := child
		if g.kindOf(&n) != kindImport {
			continue
		}
		collectImport(g, &n, src, imports)
	}
	return imports.Modules()
}

// collectImport reports one import statement to imports.
func collectImport(g *grammar, n *ts.Node, src []byte, imports *jvm.Imports) {
	path := importPath(g, n, src)
	if path == "" {
		return
	}
	at := treesitter.SpanOf(n)
	if hasToken(n, g.tokens.star) {
		imports.Star(path, at)
		return
	}
	imports.Bind(path, importBinding(g, n, src), at)
}

// importPath returns the qualified path an import names: the text of its
// qualified_identifier, which for a star import stops before the `.*`.
func importPath(g *grammar, n *ts.Node, src []byte) string {
	for _, child := range treesitter.NamedChildren(n) {
		q := child
		if g.kindOf(&q) == kindQualifiedIdentifier {
			return treesitter.Text(&q, src)
		}
	}
	return ""
}

// importBinding returns the local name an import introduces: the alias
// after `as`, which the grammar hangs off the import as a bare identifier,
// or else the last segment of the qualified path.
func importBinding(g *grammar, n *ts.Node, src []byte) string {
	var last *ts.Node
	for _, child := range treesitter.NamedChildren(n) {
		c := child
		switch g.kindOf(&c) {
		case kindIdentifier:
			return treesitter.Text(&c, src)
		case kindQualifiedIdentifier:
			segments := treesitter.NamedChildren(&c)
			if len(segments) > 0 {
				last = &segments[len(segments)-1]
			}
		}
	}
	return treesitter.Text(last, src)
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
