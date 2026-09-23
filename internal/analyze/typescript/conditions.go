package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// The logical operators that make a Boolean clause.
const (
	opAnd           = "&&"
	opOr            = "||"
	opNullish       = "??"
	opAndAssign     = "&&="
	opOrAssign      = "||="
	opNullishAssign = "??="
	opNot           = "!"
)

// conditions charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if (a > b && c < d)` is 3 ICPs, "1 for the if and 1
// for each Boolean condition". The clauses of a chain are its leaf
// operands, so `a && b` is 2, `a && b || c` is 3, and a plain `if (x > 1)`
// has no logical operator and adds no condition at all.
type conditions struct {
	g   *grammar
	src []byte
	l   *ledger
	// consumed holds the logical binary expressions already folded into an
	// enclosing clause chain, so a nested `&&` is never counted twice.
	consumed map[uintptr]bool
}

// newConditions returns the condition rules charging l.
func newConditions(g *grammar, src []byte, l *ledger) *conditions {
	return &conditions{g: g, src: src, l: l, consumed: map[uintptr]bool{}}
}

// count charges the clauses of the chain rooted at n, a binary or an
// augmented assignment expression of kind k, unless n is a plain operator
// or a chain already folded into its parent.
func (c *conditions) count(k kind, n *ts.Node) {
	if c.consumed[n.Id()] {
		return
	}
	if k == kindAugmentedAssignmentExpression {
		c.countLogicalAssign(n)
		return
	}
	if !c.isLogical(n) {
		return
	}
	for _, clause := range c.clauses(n, nil) {
		c.l.charge(config.MetricCondition, &clause, 1)
	}
}

// countLogicalAssign charges `a &&= b`, `a ||= b` and `a ??= b`, which are
// sugar for `a = a && b`: the left-hand side is one clause and the
// right-hand side contributes its own.
//
// The whole assignment carries a single occurrence whose Count is that sum,
// rather than one occurrence per clause, because the clause the left-hand
// side stands for is not written anywhere: `y ||= b` reads `y` once, and
// only the operator says it is also a condition. So `y ||= b` is one
// construct worth 2, which is the clause pair analyze.Occurrence describes.
func (c *conditions) countLogicalAssign(n *ts.Node) {
	switch c.operator(n) {
	case opAndAssign, opOrAssign, opNullishAssign:
		right := c.clauses(n.ChildByFieldId(c.g.fields.right), nil)
		c.l.charge(config.MetricCondition, n, 1+len(right))
	}
}

// clauses appends the Boolean clauses of the expression rooted at n to out
// and returns it. The clauses of a chain are its leaf operands: the walk
// flattens through nested logical operators, parentheses and `!`, so the
// node that lands in out is the operand itself, `a` rather than `!a`.
// Flattening through `!` follows De Morgan: `!(a || b) && x` is
// `!a && !b && x`, three clauses, not four. Every logical node it folds in
// is marked consumed so the walk does not count it again.
func (c *conditions) clauses(n *ts.Node, out []ts.Node) []ts.Node {
	if n == nil {
		return out
	}
	switch c.g.kindOf(n) {
	case kindParenthesizedExpression:
		if inner := treesitter.FirstNamedChild(n); inner != nil {
			return c.clauses(inner, out)
		}
	case kindUnaryExpression:
		if c.operator(n) == opNot {
			return c.clauses(n.ChildByFieldId(c.g.fields.argument), out)
		}
	case kindBinaryExpression:
		if c.isLogical(n) {
			return c.chainClauses(n, out)
		}
	}
	return append(out, *n)
}

// chainClauses folds the logical binary expression n into out, left operand
// first, and marks it consumed.
func (c *conditions) chainClauses(n *ts.Node, out []ts.Node) []ts.Node {
	c.consumed[n.Id()] = true
	out = c.clauses(n.ChildByFieldId(c.g.fields.left), out)
	return c.clauses(n.ChildByFieldId(c.g.fields.right), out)
}

// isLogical reports whether n is a binary expression whose operator joins
// Boolean clauses.
func (c *conditions) isLogical(n *ts.Node) bool {
	switch c.operator(n) {
	case opAnd, opOr, opNullish:
		return true
	default:
		return false
	}
}

// operator returns the text of n's operator token.
func (c *conditions) operator(n *ts.Node) string {
	return treesitter.Text(n.ChildByFieldId(c.g.fields.operator), c.src)
}
