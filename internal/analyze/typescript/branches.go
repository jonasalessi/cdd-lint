package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// controlFlow charges the branch, condition and exception metrics.
//
// Every `?.` is one branch, because every `?.` short-circuits. The grammar
// spells it two different ways: a member access (`a?.b`) and a subscript
// access (`a?.[0]`) carry a named optional_chain node, charged wherever it
// appears; an optional call (`a?.()`) carries the bare anonymous token
// instead, charged by countOptionalCall. The two never overlap, so
// `o?.b?.(1)?.[2]` is 3: the `?.b`, the `?.(` and the `?.[2]`.
type controlFlow struct {
	g          *grammar
	l          *ledger
	conditions *conditions
}

// newControlFlow returns the control-flow rules charging l.
func newControlFlow(g *grammar, src []byte, l *ledger) *controlFlow {
	return &controlFlow{g: g, l: l, conditions: newConditions(g, src, l)}
}

// count charges n when k is a control-flow construct, reporting whether it
// was one.
func (f *controlFlow) count(k kind, n *ts.Node) bool {
	switch k {
	case kindIfStatement, kindSwitchCase, kindTernaryExpression, kindForStatement,
		kindWhileStatement, kindDoStatement, kindOptionalChain:
		f.l.charge(config.MetricCodeBranch, n, 1)
	case kindForInStatement:
		f.l.charge(config.MetricCodeBranch, n, 1)
		f.countLoopBinding(n)
	case kindCallExpression:
		f.countOptionalCall(n)
	case kindElseClause:
		f.countElse(n)
	case kindTryStatement:
		f.countTryBody(n)
	case kindCatchClause, kindFinallyClause:
		f.l.charge(config.MetricExceptionHandling, n, 1)
	case kindBinaryExpression, kindAugmentedAssignmentExpression:
		f.conditions.count(k, n)
	default:
		return false
	}
	return true
}

// countTryBody charges the block a `try` guards. The occurrence sits on the
// statement_block rather than on the whole try_statement, whose range would
// cover the catch and finally clauses charged beside it.
func (f *controlFlow) countTryBody(n *ts.Node) {
	body := n.ChildByFieldId(f.g.fields.body)
	if body == nil {
		body = n
	}
	f.l.charge(config.MetricExceptionHandling, body, 1)
}

// countOptionalCall charges the `?.` of an optional call. The grammar gives
// it no node of its own -- call_expression holds the bare anonymous `?.`
// token between the function and the arguments -- so the token is found by
// symbol id among the direct children rather than by kind or by field.
// A call has at most one, so the scan stops at the first.
func (f *controlFlow) countOptionalCall(n *ts.Node) {
	for i := uint(0); i < n.ChildCount(); i++ {
		if child := n.Child(i); child != nil && child.KindId() == f.g.optionalCallToken {
			f.l.charge(config.MetricCodeBranch, child, 1)
			return
		}
	}
}

// countLoopBinding charges the binding of a `for…in` or `for…of` loop as one
// method-level temporary variable, which is what docs/cdd.md counts. The
// loop declares a local exactly as `for (let i = 0; …)` does, but the
// grammar gives it no declarator: the binding hangs off the `left` field
// and the `let`, `const` or `var` keyword off the optional `kind` field.
// A loop with no `kind`, `for (x of xs)`, assigns to an existing variable
// and declares nothing. The charge is one per statement even when `left` is
// a destructuring pattern, which is how `const {a, b} = x` is counted too,
// and it points at that binding, the closest the grammar has to the
// declarator the loop never gets.
func (f *controlFlow) countLoopBinding(n *ts.Node) {
	if n.ChildByFieldId(f.g.fields.kind) == nil {
		return
	}
	binding := n.ChildByFieldId(f.g.fields.left)
	if binding == nil {
		binding = n
	}
	f.l.charge(config.MetricLocalVariable, binding, 1)
}

// countElse charges an `else`, unless it is the `else` of an `else if`:
// that `if` already charged itself, so `if / else if / else` is 3 and not 4.
func (f *controlFlow) countElse(n *ts.Node) {
	if body := treesitter.FirstNamedChild(n); body != nil && f.g.kindOf(body) == kindIfStatement {
		return
	}
	f.l.charge(config.MetricCodeBranch, n, 1)
}
