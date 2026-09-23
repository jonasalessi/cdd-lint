package java

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

// newCounter returns a counter for one unit's subtree.
func newCounter(g *grammar, src []byte) *counter {
	l := newLedger()
	return &counter{
		ledger: l,
		g:      g,
		flow:   newControlFlow(g, l),
		decls:  newDeclarations(g, src, l),
	}
}

// visit is the walk callback; it always descends, because a unit owns every
// construct nested inside it, members, nested types and local classes
// included.
func (c *counter) visit(n *ts.Node) bool {
	k := c.g.kindOf(n)
	if !c.flow.count(k, n) {
		c.decls.count(k, n)
	}
	return true
}
