package golang

import (
	"go/ast"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// controlFlow charges the branch and condition metrics.
//
// An `if` is one branch and its `else` another, unless that `else` opens
// another `if`, which charges itself (FR-6). A `case` of a switch or of a
// type switch and a `case` of a select are one branch each however many
// values they list, and the `default` arm is none. Each loop is one branch:
// the reader still has to decide whether to go round again, in all four
// shapes of `for` and over a `range`. A `return`, a `break`, a `continue`, a
// `goto`, a `fallthrough`, a label, a `go`, a `defer`, a type assertion,
// `panic` and `recover` are not branches, and Go has no handler construct,
// so exception_handling is never charged.
type controlFlow struct {
	l          *ledger
	conditions *conditions
}

// newControlFlow returns the control-flow rules charging l.
func newControlFlow(l *ledger) *controlFlow {
	return &controlFlow{l: l, conditions: newConditions(l)}
}

// count charges n when it is a control-flow construct.
func (f *controlFlow) count(n ast.Node) {
	switch n := n.(type) {
	case *ast.IfStmt:
		f.l.charge(config.MetricCodeBranch, n)
		f.countElse(n)
	case *ast.CaseClause:
		f.countArm(n, n.List != nil)
	case *ast.CommClause:
		f.countArm(n, n.Comm != nil)
	case *ast.ForStmt, *ast.RangeStmt:
		f.l.charge(config.MetricCodeBranch, n)
	case *ast.BinaryExpr:
		f.conditions.count(n)
	}
}

// countArm charges one arm of a switch, a type switch or a select when the
// arm tests something. A `default` tests nothing and is 0: it is where every
// value not matched above falls, not a decision of its own.
func (f *controlFlow) countArm(n ast.Node, tests bool) {
	if tests {
		f.l.charge(config.MetricCodeBranch, n)
	}
}

// countElse charges the alternative of an `if` only when it is a block: an
// `else if` is an IfStmt that charges itself, so `if / else if / else` is 3
// and not 4 (FR-6). The occurrence spans the block, since go/ast keeps no
// position for the `else` keyword.
func (f *controlFlow) countElse(n *ast.IfStmt) {
	if block, ok := n.Else.(*ast.BlockStmt); ok {
		f.l.charge(config.MetricCodeBranch, block)
	}
}
