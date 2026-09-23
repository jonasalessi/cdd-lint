package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
)

// counter measures one unit. It owns the ledger every rule charges, hands
// each node of the unit's subtrees to the control-flow and declaration
// rules, and records the package names the unit qualifies something by.
type counter struct {
	*ledger
	flow  *controlFlow
	decls *declarations
	// refs are the package names the unit qualifies something by, used to
	// attribute the file's imports to the units that actually reference
	// them (FR-8).
	refs map[string]struct{}
}

// newCounter returns a counter for the subtrees of one unit.
func newCounter(fset *token.FileSet) *counter {
	l := newLedger(fset)
	return &counter{
		ledger: l,
		flow:   newControlFlow(l),
		decls:  newDeclarations(l),
		refs:   map[string]struct{}{},
	}
}

// measureUnit counts the ICPs of one unit over every subtree billed to it —
// the type declaration plus its methods, or the function itself — attributes
// the file's imports to it, and reports the unit the pipeline consumes
// (FR-4, FR-8).
func measureUnit(fset *token.FileSet, d unitDecl, mods []module) analyze.Unit {
	c := newCounter(fset)
	for _, n := range d.nodes {
		ast.Inspect(n, c.visit)
	}
	c.countCoupling(mods)
	return analyze.Unit{
		Name:        d.name,
		Kind:        d.kind,
		Line:        d.line,
		Col:         d.col,
		Counts:      c.counts,
		Occurrences: c.sortedOccurrences(),
	}
}

// visit is the ast.Inspect callback; it always descends, because a unit owns
// every construct nested inside it, func literals and local types included.
// Both halves see every node, because one node can be both: a `range` loop is
// a branch and declares the names it iterates with.
func (c *counter) visit(n ast.Node) bool {
	c.flow.count(n)
	c.decls.count(n)
	c.collectRef(n)
	return true
}

// collectRef records the package name a qualified expression reads, which is
// what attributes the file's imports to this unit (FR-8). The qualifier of
// `fmt.Sprint` is a package only when the resolver left it unresolved: a
// parameter, a local or a field named `fmt` carries an object, so it shadows
// the package and the selector is not a use of it. That is the precise rule
// Java cannot state and go/parser gives for free.
//
// It reads the deprecated Ident.Obj on purpose, for the reason declares
// states: syntactic resolution is exactly what a per-file analyzer needs.
func (c *counter) collectRef(n ast.Node) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if qualifier, ok := sel.X.(*ast.Ident); ok && qualifier.Obj == nil {
		c.refs[qualifier.Name] = struct{}{}
	}
}
