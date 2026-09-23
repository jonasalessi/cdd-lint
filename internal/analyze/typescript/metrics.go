package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
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
	// file's imports to the units that actually reference them.
	refs map[string]struct{}
	// skipDeclarator is the unit's own declarator, which is not one of its
	// local variables; skipLambda is the unit's own function body, which is
	// not one of its lambdas.
	skipDeclarator uintptr
	skipLambda     uintptr
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
	if g.kindOf(&d.node) == kindVariableDeclarator {
		c.skipDeclarator = d.node.Id()
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

// charge adds count points of metric to the unit and records n's range as
// where they come from.
func (c *counter) charge(metric config.MetricID, n *ts.Node, count int) {
	c.chargeSpan(metric, treesitter.SpanOf(n), count)
}

// chargeSpan is charge for a range that is not a node the caller still
// holds, which is how a coupling charge points at an import statement far
// above the unit.
func (c *counter) chargeSpan(metric config.MetricID, s treesitter.Span, count int) {
	c.counts[metric] += count
	c.occurrences = append(c.occurrences, analyze.Occurrence{
		Metric:  metric,
		Line:    s.Line,
		Col:     s.Col,
		EndLine: s.EndLine,
		EndCol:  s.EndCol,
		Count:   count,
	})
}

// sortedOccurrences returns the unit's occurrences in source order.
func (c *counter) sortedOccurrences() []analyze.Occurrence {
	treesitter.SortOccurrences(c.occurrences)
	return c.occurrences
}

// visit is the walk callback; it always descends, because a unit owns every
// construct nested inside it, methods and callbacks included.
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
// Every `?.` is one branch, because every `?.` short-circuits. The grammar
// spells it two different ways: a member access (`a?.b`) and a subscript
// access (`a?.[0]`) carry a named optional_chain node, charged wherever it
// appears; an optional call (`a?.()`) carries the bare anonymous token
// instead, charged by countOptionalCall. The two never overlap, so
// `o?.b?.(1)?.[2]` is 3: the `?.b`, the `?.(` and the `?.[2]`.
func (c *counter) countControlFlow(k kind, n *ts.Node) bool {
	switch k {
	case kindIfStatement, kindSwitchCase, kindTernaryExpression, kindForStatement,
		kindWhileStatement, kindDoStatement, kindOptionalChain:
		c.charge(config.MetricCodeBranch, n, 1)
	case kindForInStatement:
		c.charge(config.MetricCodeBranch, n, 1)
		c.countLoopBinding(n)
	case kindCallExpression:
		c.countOptionalCall(n)
	case kindElseClause:
		c.countElse(n)
	case kindTryStatement:
		c.countTryBody(n)
	case kindCatchClause, kindFinallyClause:
		c.charge(config.MetricExceptionHandling, n, 1)
	case kindBinaryExpression, kindAugmentedAssignmentExpression:
		c.countCondition(k, n)
	default:
		return false
	}
	return true
}

// countDeclaration charges the inheritance, local-variable and lambda
// metrics, and records the identifiers the unit mentions.
//
// A local variable is one declarator, so `const {a, b} = x` is one and
// `let b = 2, c = 3` is two. A `for` initialiser holds declarators and
// counts; a `for…in` or `for…of` binding has no declarator in the grammar
// and is charged by countLoopBinding instead. Class fields count,
// `#private` ones included, since the grammar gives them all the same node;
// an interface's property signatures describe a shape rather than declare
// variables, and do not.
func (c *counter) countDeclaration(k kind, n *ts.Node) {
	switch k {
	case kindExtendsClause:
		c.chargeParents(n, fieldValue)
	case kindExtendsTypeClause:
		c.chargeParents(n, fieldType)
	case kindImplementsClause:
		c.chargeParents(n, "")
	case kindVariableDeclarator:
		if n.Id() != c.skipDeclarator {
			c.charge(config.MetricLocalVariable, n, 1)
		}
	case kindPublicFieldDefinition:
		c.charge(config.MetricLocalVariable, n, 1)
	case kindArrowFunction, kindFunctionExpression:
		if n.Id() != c.skipLambda {
			c.charge(config.MetricLambda, n, 1)
		}
	case kindIdentifier, kindTypeIdentifier, kindShorthandPropertyIdentifier:
		c.refs[n.Utf8Text(c.src)] = struct{}{}
	}
}

// chargeParents charges one inheritance ICP per parent a heritage clause
// lists, on the parent's own type node rather than on the clause, so
// `implements A, B` is two occurrences a reader can tell apart. field
// selects the children that are parents rather than type arguments:
// `extends Base<T>` holds Base in the clause's field and T outside it. An
// empty field takes every named child, which is what `implements` holds.
func (c *counter) chargeParents(n *ts.Node, field string) {
	for _, parent := range treesitter.NamedChildrenInField(n, field) {
		c.charge(config.MetricInheritance, &parent, 1)
	}
}

// countTryBody charges the block a `try` guards. The occurrence sits on the
// statement_block rather than on the whole try_statement, whose range would
// cover the catch and finally clauses charged beside it.
func (c *counter) countTryBody(n *ts.Node) {
	body := n.ChildByFieldId(c.g.fields.body)
	if body == nil {
		body = n
	}
	c.charge(config.MetricExceptionHandling, body, 1)
}

// countOptionalCall charges the `?.` of an optional call. The grammar gives
// it no node of its own -- call_expression holds the bare anonymous `?.`
// token between the function and the arguments -- so the token is found by
// symbol id among the direct children rather than by kind or by field.
// A call has at most one, so the scan stops at the first.
func (c *counter) countOptionalCall(n *ts.Node) {
	for i := uint(0); i < n.ChildCount(); i++ {
		if child := n.Child(i); child != nil && child.KindId() == c.g.optionalCallToken {
			c.charge(config.MetricCodeBranch, child, 1)
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
func (c *counter) countLoopBinding(n *ts.Node) {
	if n.ChildByFieldId(c.g.fields.kind) == nil {
		return
	}
	binding := n.ChildByFieldId(c.g.fields.left)
	if binding == nil {
		binding = n
	}
	c.charge(config.MetricLocalVariable, binding, 1)
}

// countElse charges an `else`, unless it is the `else` of an `else if`:
// that `if` already charged itself, so `if / else if / else` is 3 and not 4.
func (c *counter) countElse(n *ts.Node) {
	if body := treesitter.FirstNamedChild(n); body != nil && c.g.kindOf(body) == kindIfStatement {
		return
	}
	c.charge(config.MetricCodeBranch, n, 1)
}

// countCondition charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if (a > b && c < d)` is 3 ICPs, "1 for the if and 1
// for each Boolean condition". The clauses of a chain are its leaf
// operands, so `a && b` is 2, `a && b || c` is 3, and a plain `if (x > 1)`
// has no logical operator and adds no condition at all.
func (c *counter) countCondition(k kind, n *ts.Node) {
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
		c.charge(config.MetricCondition, &clause, 1)
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
func (c *counter) countLogicalAssign(n *ts.Node) {
	switch treesitter.Text(n.ChildByFieldId(c.g.fields.operator), c.src) {
	case opAndAssign, opOrAssign, opNullishAssign:
		right := c.clauses(n.ChildByFieldId(c.g.fields.right), nil)
		c.charge(config.MetricCondition, n, 1+len(right))
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
		if treesitter.Text(n.ChildByFieldId(c.g.fields.operator), c.src) == opNot {
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
	switch treesitter.Text(n.ChildByFieldId(c.g.fields.operator), c.src) {
	case opAnd, opOr, opNullish:
		return true
	default:
		return false
	}
}
