package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// conditions charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if a > b && c < d` is 3 ICPs, "1 for the if and 1 for
// each Boolean condition". The clauses of a chain are its leaf operands, so
// `a && b` is 2 and `a && b || c` is 3, while a plain `if x > 1` joins
// nothing and adds no condition at all (FR-5).
type conditions struct {
	l *ledger
	// consumed holds the logical expressions already folded into an
	// enclosing clause chain, so a nested `&&` is never counted twice.
	consumed map[ast.Node]bool
}

// newConditions returns the condition rules charging l.
func newConditions(l *ledger) *conditions {
	return &conditions{l: l, consumed: map[ast.Node]bool{}}
}

// count charges the clauses of the chain rooted at n, unless n is a plain
// operator or a chain already folded into its parent.
func (c *conditions) count(n *ast.BinaryExpr) {
	if c.consumed[n] || !isLogical(n) {
		return
	}
	for _, clause := range c.clauses(n, nil) {
		c.l.charge(config.MetricCondition, clause)
	}
}

// clauses appends the Boolean clauses of the expression rooted at n to out
// and returns it. The clauses of a chain are its leaf operands: the walk
// flattens through nested logical operators, parentheses and `!`, so what
// lands in out is the operand itself, `a` rather than `!a`. Flattening
// through `!` follows De Morgan: `!(a || b) && x` is `!a && !b && x`, three
// clauses, not four. Every logical node it folds in is marked consumed so
// the walk does not count it again.
func (c *conditions) clauses(n ast.Expr, out []ast.Expr) []ast.Expr {
	switch e := n.(type) {
	case *ast.ParenExpr:
		return c.clauses(e.X, out)
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			return c.clauses(e.X, out)
		}
	case *ast.BinaryExpr:
		if isLogical(e) {
			c.consumed[e] = true
			return c.clauses(e.Y, c.clauses(e.X, out))
		}
	}
	return append(out, n)
}

// isLogical reports whether a binary expression joins Boolean clauses. Go's
// short-circuit pair is the whole list: `&`, `|`, `^` and `&^` are
// arithmetic on bits and never clauses.
func isLogical(n *ast.BinaryExpr) bool {
	return n.Op == token.LAND || n.Op == token.LOR
}
