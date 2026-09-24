package kotlin

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// controlFlow charges the branch, condition and exception metrics.
//
// An `if` is one branch and its `else` another, unless that `else` opens
// another `if`, which charges itself (FR-10). A `when` entry is one branch
// however many values its condition lists; the `else` entry is none. Every
// `?.` short-circuits and is one branch; `!!` asserts and is none. `try`,
// `return`, `break`, `continue` and `throw` are not branches.
type controlFlow struct {
	g          *grammar
	l          *ledger
	conditions *conditions
}

// newControlFlow returns the control-flow rules charging l.
func newControlFlow(g *grammar, l *ledger) *controlFlow {
	return &controlFlow{g: g, l: l, conditions: newConditions(g, l)}
}

// count charges n when k is a control-flow construct, reporting whether it
// was one.
func (f *controlFlow) count(k kind, n *ts.Node) bool {
	switch k {
	case kindIfExpression:
		f.l.charge(config.MetricCodeBranch, n)
		f.countElse(n)
	case kindWhenEntry:
		if n.ChildByFieldId(f.g.fields.condition) != nil {
			f.l.charge(config.MetricCodeBranch, n)
		}
	case kindForStatement:
		f.l.charge(config.MetricCodeBranch, n)
		f.countLoopBinding(n)
	case kindWhileStatement, kindDoWhileStatement:
		f.l.charge(config.MetricCodeBranch, n)
	case kindNavigationExpression:
		f.countSafeCall(n)
	case kindBinaryExpression:
		f.conditions.count(n)
	case kindTryExpression:
		f.countTryBody(n)
	case kindCatchBlock, kindFinallyBlock:
		f.l.charge(config.MetricExceptionHandling, n)
	default:
		return false
	}
	return true
}

// countTryBody charges the block a `try` guards. The occurrence sits on the
// block rather than on the whole try_expression, whose range would cover
// the catch and finally blocks charged beside it. The grammar has no body
// field, so the block is the first named child of that kind.
func (f *controlFlow) countTryBody(n *ts.Node) {
	f.l.charge(config.MetricExceptionHandling, nodeOr(f.g.childOfKind(n, kindBlock), n))
}

// countLoopBinding charges the binding of a `for` loop as one method-level
// temporary variable, which is what docs/cdd.md counts and what TypeScript
// charges for a `for…of` binding. The charge is one per statement even
// when the binding destructures, and it points at that binding, the
// closest the grammar has to the declaration the loop never gets.
func (f *controlFlow) countLoopBinding(n *ts.Node) {
	for _, child := range treesitter.NamedChildren(n) {
		binding := child
		switch f.g.kindOf(&binding) {
		case kindVariableDeclaration, kindMultiVariableDeclaration:
			f.l.charge(config.MetricLocalVariable, &binding)
			return
		}
	}
}

// countElse charges the `else` branch of an if_expression, from the keyword
// to the end of the branch, unless the branch is itself an `if`: that one
// already charged itself, so `if / else if / else` is 3 and not 4. The
// grammar has no else clause node and no alternative field, so the branch
// is the code sibling after the anonymous `else` token.
func (f *controlFlow) countElse(n *ts.Node) {
	keyword := findToken(n, f.g.tokens.elseKeyword)
	if keyword == nil {
		return
	}
	branch := f.nextCode(keyword)
	if branch == nil {
		f.l.charge(config.MetricCodeBranch, keyword)
		return
	}
	if f.g.kindOf(branch) == kindIfExpression {
		return
	}
	start, end := treesitter.SpanOf(keyword), treesitter.SpanOf(branch)
	f.l.chargeSpan(config.MetricCodeBranch, treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: end.EndLine, EndCol: end.EndCol,
	})
}

// nextCode returns the first named sibling after n that is not a comment,
// nil when there is none.
func (f *controlFlow) nextCode(n *ts.Node) *ts.Node {
	for s := n.NextNamedSibling(); s != nil; s = s.NextNamedSibling() {
		if k := f.g.kindOf(s); k != kindLineComment && k != kindBlockComment {
			return s
		}
	}
	return nil
}

// countSafeCall charges the `?.` of a navigation expression. The grammar
// gives it no node of its own -- navigation_expression holds either the
// bare `.` or the bare `?.` token between the receiver and the member -- so
// the token is found by symbol id among the direct children. A plain `.`
// is not a branch.
func (f *controlFlow) countSafeCall(n *ts.Node) {
	if tok := findToken(n, f.g.tokens.safeCall); tok != nil {
		f.l.charge(config.MetricCodeBranch, tok)
	}
}
