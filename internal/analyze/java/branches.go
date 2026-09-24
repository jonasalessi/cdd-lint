package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// controlFlow charges the branch, condition and exception metrics.
//
// An `if` is one branch and its `else` another, unless that `else` opens
// another `if`, which charges itself (FR-9). A switch arm is one branch
// however many labels it lists, and the `default` arm is none (FR-10). A
// ternary is a one-line `if` and costs the same. Each loop statement is one
// branch: the reader still has to decide whether to go round again. `try`,
// `return`, `break`, `continue`, `throw`, `yield`, `assert` and `instanceof`
// are not branches.
//
// A guarded block is one point of handling, a `catch` another and a
// `finally` a third, so the `try / catch / finally` of docs/cdd.md is 3.
// A multi-catch is one clause and one point: the reader follows one
// recovery path however many types lead into it. `throw` and a `throws`
// clause are 0 -- they hand the problem on rather than handling it.
type controlFlow struct {
	g          *grammar
	l          *ledger
	switches   *switchArms
	conditions *conditions
}

// newControlFlow returns the control-flow rules charging l.
func newControlFlow(g *grammar, l *ledger) *controlFlow {
	return &controlFlow{
		g:          g,
		l:          l,
		switches:   &switchArms{g: g, l: l},
		conditions: newConditions(g, l),
	}
}

// count charges n when k is a control-flow construct, reporting whether it
// was one.
func (f *controlFlow) count(k kind, n *ts.Node) bool {
	switch k {
	case kindIfStatement:
		f.l.charge(config.MetricCodeBranch, n)
		f.countElse(n)
	case kindSwitchBlockStatementGroup:
		f.switches.countArm(n)
	case kindSwitchRule:
		f.switches.countRule(n)
	case kindEnhancedForStatement:
		f.l.charge(config.MetricCodeBranch, n)
		f.countLoopBinding(n)
	case kindTernaryExpression, kindForStatement, kindWhileStatement, kindDoStatement:
		f.l.charge(config.MetricCodeBranch, n)
	case kindBinaryExpression:
		f.conditions.count(n)
	case kindTryStatement, kindTryWithResourcesStatement:
		f.countTryBody(n)
	case kindCatchClause, kindFinallyClause:
		f.l.charge(config.MetricExceptionHandling, n)
	default:
		return false
	}
	return true
}

// countElse charges the `else` branch of an if_statement, unless that branch
// is itself an `if`: that one already charged itself, so `if / else if /
// else` is 3 and not 4 (FR-9).
func (f *controlFlow) countElse(n *ts.Node) {
	alternative := n.ChildByFieldId(f.g.fields.alternative)
	if alternative == nil || f.g.kindOf(alternative) == kindIfStatement {
		return
	}
	f.l.chargeSpan(config.MetricCodeBranch, f.elseSpan(n, alternative))
}

// elseSpan is the range an `else` charge points at: from the keyword to the
// end of the branch, so a reader sees the branch rather than the whole
// statement, which starts back at the `if`. The keyword is an anonymous
// child of the if_statement; without it the branch alone is the best range.
func (f *controlFlow) elseSpan(n, alternative *ts.Node) treesitter.Span {
	branch := treesitter.SpanOf(alternative)
	keyword := findToken(n, f.g.tokens.elseKeyword)
	if keyword == nil {
		return branch
	}
	start := treesitter.SpanOf(keyword)
	return treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: branch.EndLine, EndCol: branch.EndCol,
	}
}

// countLoopBinding charges the binding of an enhanced `for` as one
// method-level temporary variable, which is what docs/cdd.md counts and what
// TypeScript charges for a `for…of` binding. The charge points at the
// binding, the closest the grammar has to the declaration the loop never
// gets.
func (f *controlFlow) countLoopBinding(n *ts.Node) {
	if binding := n.ChildByFieldId(f.g.fields.name); binding != nil {
		f.l.charge(config.MetricLocalVariable, binding)
	}
}

// countTryBody charges the block a `try` guards, plain or with resources.
// The occurrence sits on that block rather than on the whole statement,
// whose range would swallow the catch and finally clauses charged beside it.
func (f *controlFlow) countTryBody(n *ts.Node) {
	f.l.charge(config.MetricExceptionHandling, nodeOr(n.ChildByFieldId(f.g.fields.body), n))
}
