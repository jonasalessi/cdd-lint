package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"
)

// counter measures one unit. It owns the ledger every rule charges and hands
// each node of the unit's subtree to the control-flow rules first and the
// declaration rules second; a node belongs to one family or the other.
type counter struct {
	*ledger
	g     *grammar
	flow  *controlFlow
	decls *declarations
}

// newCounter returns a counter for the unit rooted at d.
func newCounter(g *grammar, src []byte, d *unitDecl) *counter {
	l := newLedger()
	return &counter{
		ledger: l,
		g:      g,
		flow:   newControlFlow(g, src, l),
		decls:  newDeclarations(g, src, l, d),
	}
}

// visit is the walk callback; it always descends, because a unit owns every
// construct nested inside it, methods and callbacks included.
func (c *counter) visit(n *ts.Node) bool {
	k := c.g.kindOf(n)
	if !c.flow.count(k, n) {
		c.decls.count(k, n)
	}
	return true
}
