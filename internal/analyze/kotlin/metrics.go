package kotlin

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// The operators that make a Boolean clause. Elvis joins the list because
// `x ?: y` is the same short-circuit choice TypeScript's `??` is, and that
// one is a condition.
const (
	opAnd   = "&&"
	opOr    = "||"
	opElvis = "?:"
	opNot   = "!"
)

// counter accumulates the raw ICP counts of one unit while its subtree is
// walked. It counts every metric, enabled or not: the pipeline drops the
// ones the configuration disables.
//
// Every count is charged through charge or chargeSpan, which record where
// it came from, so a unit's Counts is always the sum of its Occurrences'
// Count, metric by metric.
type counter struct {
	g      *grammar
	src    []byte
	counts map[config.MetricID]int
	// occurrences locate every charge, in the order it was made. The walk
	// is a pre-order traversal, so they come out in source order except for
	// the coupling charges, which are added last; measure sorts them.
	occurrences []analyze.Occurrence
	// consumed holds the logical binary expressions already folded into an
	// enclosing clause chain, so a nested `&&` is never counted twice.
	consumed map[uintptr]bool
	// refs are the identifiers the unit mentions, used to attribute the
	// file's imports to the units that actually reference them. Types are
	// `user_type > identifier` in this grammar, so one node kind covers
	// values, types and annotations.
	refs map[string]struct{}
	// skipDeclaration is a property unit's own declaration, which is not
	// one of its local variables; skipLambda is the unit's own body, which
	// is not one of its lambdas (FR-11).
	skipDeclaration uintptr
	skipLambda      uintptr
}

// newCounter returns a counter for the unit rooted at d.
func newCounter(g *grammar, src []byte, d *unitDecl) *counter {
	c := &counter{
		g:        g,
		src:      src,
		counts:   zeroCounts(),
		consumed: map[uintptr]bool{},
		refs:     map[string]struct{}{},
	}
	if d.kind == unitProperty {
		c.skipDeclaration = d.node.Id()
	}
	if d.body != nil {
		c.skipLambda = d.body.Id()
	}
	return c
}

// zeroCounts returns a map holding every metric at zero.
func zeroCounts() map[config.MetricID]int {
	counts := make(map[config.MetricID]int, len(config.Metrics()))
	for _, m := range config.Metrics() {
		counts[m] = 0
	}
	return counts
}

// charge adds one point of metric to the unit and records n's range as
// where it comes from. Every Kotlin construct is worth one point: the
// language has no `&&=` sugar that would fold two clauses into one node.
func (c *counter) charge(metric config.MetricID, n *ts.Node) {
	c.chargeSpan(metric, treesitter.SpanOf(n))
}

// chargeSpan is charge for a range that is not a node the caller still
// holds, which is how an `else` branch or a coupling charge is located.
func (c *counter) chargeSpan(metric config.MetricID, s treesitter.Span) {
	c.counts[metric]++
	c.occurrences = append(c.occurrences, analyze.Occurrence{
		Metric:  metric,
		Line:    s.Line,
		Col:     s.Col,
		EndLine: s.EndLine,
		EndCol:  s.EndCol,
		Count:   1,
	})
}

// sortedOccurrences returns the unit's occurrences in source order.
func (c *counter) sortedOccurrences() []analyze.Occurrence {
	treesitter.SortOccurrences(c.occurrences)
	return c.occurrences
}

// visit is the walk callback; it always descends, because a unit owns every
// construct nested inside it, members, local functions and lambdas
// included.
func (c *counter) visit(n *ts.Node) bool {
	k := c.g.kindOf(n)
	if !c.countControlFlow(k, n) {
		c.countDeclaration(k, n)
	}
	return true
}

// countControlFlow charges the branch, condition and exception metrics,
// reporting whether k was one of theirs.
//
// An `if` is one branch and its `else` another, unless that `else` opens
// another `if`, which charges itself (FR-10). A `when` entry is one branch
// however many values its condition lists; the `else` entry is none. Every
// `?.` short-circuits and is one branch; `!!` asserts and is none. `try`,
// `return`, `break`, `continue` and `throw` are not branches.
func (c *counter) countControlFlow(k kind, n *ts.Node) bool {
	switch k {
	case kindIfExpression:
		c.charge(config.MetricCodeBranch, n)
		c.countElse(n)
	case kindWhenEntry:
		if n.ChildByFieldId(c.g.fields.condition) != nil {
			c.charge(config.MetricCodeBranch, n)
		}
	case kindForStatement:
		c.charge(config.MetricCodeBranch, n)
		c.countLoopBinding(n)
	case kindWhileStatement, kindDoWhileStatement:
		c.charge(config.MetricCodeBranch, n)
	case kindNavigationExpression:
		c.countSafeCall(n)
	case kindBinaryExpression:
		c.countCondition(n)
	case kindTryExpression:
		c.countTryBody(n)
	case kindCatchBlock, kindFinallyBlock:
		c.charge(config.MetricExceptionHandling, n)
	default:
		return false
	}
	return true
}

// countDeclaration charges the inheritance, local-variable and lambda
// metrics, and records the identifiers the unit mentions.
//
// A local variable is one property declaration wherever it sits -- a local
// in a block, a member in a class body, a member of a companion -- so
// `val (a, b) = p` is one, like `const {a, b} = x` in TypeScript. A
// constructor parameter is one only when `val` or `var` turns it into a
// property; a plain parameter declares nothing the body did not already
// receive. An enum entry is a constant, not a variable. A property of an
// interface with no initializer, no delegate and no accessor body describes
// a shape rather than declaring a variable, like an interface's property
// signature in TypeScript, and does not count; the same property in an
// abstract class does, because the class may still hold it.
//
// A lambda is a lambda literal, trailing or not, an anonymous function or
// a callable reference. Scope functions (`let`, `apply`, `run`, …) are not
// exempted: telling them apart from any other receiver's `apply` needs
// type resolution, and `lambda` is opt-in for the teams that weigh it. A
// property unit's own body is the unit, not one of its lambdas (FR-11).
// `String::trim` parses as a navigation expression in this grammar and is
// not counted.
func (c *counter) countDeclaration(k kind, n *ts.Node) {
	switch k {
	case kindDelegationSpecifier:
		c.charge(config.MetricInheritance, c.specifierType(n))
	case kindPropertyDeclaration:
		if n.Id() != c.skipDeclaration && !c.isShape(n) {
			c.charge(config.MetricLocalVariable, n)
		}
	case kindClassParameter:
		if hasToken(n, c.g.tokens.val) || hasToken(n, c.g.tokens.variable) {
			c.charge(config.MetricLocalVariable, n)
		}
	case kindLambdaLiteral, kindAnonymousFunction, kindCallableReference:
		if n.Id() != c.skipLambda {
			c.charge(config.MetricLambda, n)
		}
	case kindIdentifier:
		c.refs[n.Utf8Text(c.src)] = struct{}{}
	}
}

// specifierType returns the user_type a delegation specifier names, which
// is where its inheritance occurrence points so that `: A, B` is two
// occurrences a reader can tell apart. The type sits directly under the
// specifier for `: Iface`, and one level down for `: Base()` and
// `: Iface by d`, whose specifier wraps a constructor_invocation or an
// explicit_delegation. A specifier of another shape is charged whole.
func (c *counter) specifierType(n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		outer := child
		if c.g.kindOf(&outer) == kindUserType {
			return &outer
		}
		for _, grandchild := range treesitter.NamedChildren(&outer) {
			inner := grandchild
			if c.g.kindOf(&inner) == kindUserType {
				return &inner
			}
		}
	}
	return n
}

// isShape reports whether a property declaration is an interface member
// with no value of its own: no initializer, no delegate, no accessor body.
func (c *counter) isShape(n *ts.Node) bool {
	body := n.Parent()
	if body == nil || c.g.kindOf(body) != kindClassBody {
		return false
	}
	owner := body.Parent()
	if owner == nil || c.g.kindOf(owner) != kindClassDeclaration || !hasToken(owner, c.g.tokens.interfaceKeyword) {
		return false
	}
	if hasToken(n, c.g.tokens.assign) {
		return false
	}
	_, hasValue := propertyBody(c.g, n)
	return !hasValue
}

// countTryBody charges the block a `try` guards. The occurrence sits on the
// block rather than on the whole try_expression, whose range would cover
// the catch and finally blocks charged beside it. The grammar has no body
// field, so the block is the first named child of that kind.
func (c *counter) countTryBody(n *ts.Node) {
	for _, child := range treesitter.NamedChildren(n) {
		body := child
		if c.g.kindOf(&body) == kindBlock {
			c.charge(config.MetricExceptionHandling, &body)
			return
		}
	}
	c.charge(config.MetricExceptionHandling, n)
}

// countLoopBinding charges the binding of a `for` loop as one method-level
// temporary variable, which is what docs/cdd.md counts and what TypeScript
// charges for a `for…of` binding. The charge is one per statement even
// when the binding destructures, and it points at that binding, the
// closest the grammar has to the declaration the loop never gets.
func (c *counter) countLoopBinding(n *ts.Node) {
	for _, child := range treesitter.NamedChildren(n) {
		binding := child
		switch c.g.kindOf(&binding) {
		case kindVariableDeclaration, kindMultiVariableDeclaration:
			c.charge(config.MetricLocalVariable, &binding)
			return
		}
	}
}

// countElse charges the `else` branch of an if_expression, from the keyword
// to the end of the branch, unless the branch is itself an `if`: that one
// already charged itself, so `if / else if / else` is 3 and not 4. The
// grammar has no else clause node and no alternative field, so the branch
// is the code sibling after the anonymous `else` token.
func (c *counter) countElse(n *ts.Node) {
	keyword := findToken(n, c.g.tokens.elseKeyword)
	if keyword == nil {
		return
	}
	branch := c.nextCode(keyword)
	if branch == nil {
		c.charge(config.MetricCodeBranch, keyword)
		return
	}
	if c.g.kindOf(branch) == kindIfExpression {
		return
	}
	start, end := treesitter.SpanOf(keyword), treesitter.SpanOf(branch)
	c.chargeSpan(config.MetricCodeBranch, treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: end.EndLine, EndCol: end.EndCol,
	})
}

// nextCode returns the first named sibling after n that is not a comment,
// nil when there is none.
func (c *counter) nextCode(n *ts.Node) *ts.Node {
	for s := n.NextNamedSibling(); s != nil; s = s.NextNamedSibling() {
		if k := c.g.kindOf(s); k != kindLineComment && k != kindBlockComment {
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
func (c *counter) countSafeCall(n *ts.Node) {
	if tok := findToken(n, c.g.tokens.safeCall); tok != nil {
		c.charge(config.MetricCodeBranch, tok)
	}
}

// countCondition charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if (a > b && c < d)` is 3 ICPs, "1 for the if and 1
// for each Boolean condition". The clauses of a chain are its leaf
// operands, so `a && b` is 2, `a && b || c` is 3, `x ?: y` is 2, and a
// plain `if (x > 1)` has no logical operator and adds no condition at all.
func (c *counter) countCondition(n *ts.Node) {
	if c.consumed[n.Id()] || !c.isLogical(n) {
		return
	}
	for _, clause := range c.clauses(n, nil) {
		c.charge(config.MetricCondition, &clause)
	}
}

// clauses appends the Boolean clauses of the expression rooted at n to out
// and returns it. The clauses of a chain are its leaf operands: the walk
// flattens through nested logical operators, parentheses and `!`, so the
// node that lands in out is the operand itself, `a` rather than `!a`.
// Flattening through `!` follows De Morgan: `!(a || b) && x` is
// `!a && !b && x`, three clauses, not four. Every logical node it folds in
// is marked consumed so the walk does not count it again.
func (c *counter) clauses(n *ts.Node, out []ts.Node) []ts.Node {
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
			c.consumed[n.Id()] = true
			out = c.clauses(n.ChildByFieldId(c.g.fields.left), out)
			return c.clauses(n.ChildByFieldId(c.g.fields.right), out)
		}
	}
	return append(out, *n)
}

// isLogical reports whether n is a binary expression whose operator joins
// Boolean clauses.
func (c *counter) isLogical(n *ts.Node) bool {
	switch c.operator(n) {
	case opAnd, opOr, opElvis:
		return true
	default:
		return false
	}
}

// operator returns the text of n's operator token. The token is anonymous
// and short, so reading it is the one string the walk pays for per
// operator node.
func (c *counter) operator(n *ts.Node) string {
	op := n.ChildByFieldId(c.g.fields.operator)
	if op == nil {
		return ""
	}
	return op.Kind()
}
