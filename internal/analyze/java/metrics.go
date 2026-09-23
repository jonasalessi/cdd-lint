package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// The operators that make a Boolean clause. Java's short-circuit pair is the
// whole list: `&`, `|` and `^` are arithmetic on bits, and the Boolean form
// `a & b` cannot be told from the bitwise one without type resolution.
const (
	opAnd = "&&"
	opOr  = "||"
	opNot = "!"
)

// counter accumulates the raw ICP counts of one unit while its subtree is
// walked. It counts every metric, enabled or not: the pipeline drops the
// ones the configuration disables.
//
// Every count is charged through charge or chargeSpan, which record where it
// came from, so a unit's Counts is always the sum of its Occurrences' Count,
// metric by metric.
type counter struct {
	g      *grammar
	src    []byte
	counts map[config.MetricID]int
	// occurrences locate every charge, in the order it was made. The walk is
	// a pre-order traversal, so they come out in source order except for the
	// coupling charges, which are added last; measure sorts them.
	occurrences []analyze.Occurrence
	// consumed holds the logical binary expressions already folded into an
	// enclosing clause chain, so a nested `&&` is never counted twice.
	consumed map[uintptr]bool
	// refs are the names the unit mentions, used to attribute the file's
	// imports to the units that actually reference them. This grammar spells
	// values `identifier` and types `type_identifier`, so both kinds land
	// here.
	refs map[string]struct{}
}

// newCounter returns a counter for one unit's subtree.
func newCounter(g *grammar, src []byte) *counter {
	return &counter{
		g:        g,
		src:      src,
		counts:   zeroCounts(),
		consumed: map[uintptr]bool{},
		refs:     map[string]struct{}{},
	}
}

// zeroCounts returns a map holding every metric at zero. A unit always
// carries a key for every metric, enabled or not: the pipeline drops the
// ones the configuration disables.
func zeroCounts() map[config.MetricID]int {
	counts := make(map[config.MetricID]int, len(config.Metrics()))
	for _, m := range config.Metrics() {
		counts[m] = 0
	}
	return counts
}

// charge adds one point of metric to the unit and records n's range as where
// it comes from. Every Java construct is worth one point: the language has no
// form that folds two decisions into one node.
func (c *counter) charge(metric config.MetricID, n *ts.Node) {
	c.chargeSpan(metric, treesitter.SpanOf(n))
}

// chargeSpan is charge for a range that is not a node the caller still holds,
// which is how an `else` branch, a fallthrough arm or a coupling charge is
// located.
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
// construct nested inside it, members, nested types and local classes
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
func (c *counter) countControlFlow(k kind, n *ts.Node) bool {
	switch k {
	case kindIfStatement:
		c.charge(config.MetricCodeBranch, n)
		c.countElse(n)
	case kindSwitchBlockStatementGroup:
		c.countSwitchArm(n)
	case kindSwitchRule:
		if c.testsAValue(n) {
			c.charge(config.MetricCodeBranch, n)
		}
	case kindEnhancedForStatement:
		c.charge(config.MetricCodeBranch, n)
		c.countLoopBinding(n)
	case kindTernaryExpression, kindForStatement, kindWhileStatement, kindDoStatement:
		c.charge(config.MetricCodeBranch, n)
	case kindBinaryExpression:
		c.countCondition(n)
	case kindTryStatement, kindTryWithResourcesStatement:
		c.countTryBody(n)
	case kindCatchClause, kindFinallyClause:
		c.charge(config.MetricExceptionHandling, n)
	default:
		return false
	}
	return true
}

// countDeclaration charges the inheritance, local-variable and lambda
// metrics, and records the identifiers the unit mentions.
//
// Inheritance is charged per supertype, so `extends Base implements A, B`
// is three occurrences a reader can tell apart, and it is charged wherever
// the declaration sits: a nested type's heritage is the enclosing unit's,
// like everything else nested in it.
//
// A local variable is one declarator, so `int a, b;` is two, and a field,
// an interface constant and a block-local are the same declaration to a
// reader following a name. A resource, a `for (T x : xs)` binding and a
// record component each declare a name the body then reads, so they count
// too. A parameter does not: it names a value the caller already had.
//
// A lambda is a lambda expression or a method reference, and the grammar
// spells every form of the second one the same way, so `String::valueOf`,
// `ArrayList::new`, `this::m` and `super::m` all count. An anonymous class
// is inheritance instead: it names the type it implements, which a lambda
// never does. Nothing is exempted from the count, because a Java unit is a
// type or a method and never a property whose own body is a lambda, so
// this counter needs neither of Kotlin's skipLambda and skipDeclaration
// escapes.
func (c *counter) countDeclaration(k kind, n *ts.Node) {
	switch k {
	case kindSuperclass:
		c.charge(config.MetricInheritance, nodeOr(treesitter.FirstNamedChild(n), n))
	case kindSuperInterfaces, kindExtendsInterfaces:
		c.countListedInterfaces(n)
	case kindObjectCreationExpression:
		c.countAnonymousClass(n)
	case kindVariableDeclarator:
		c.countDeclarator(n)
	case kindResource:
		c.countResource(n)
	case kindFormalParameter:
		c.countRecordComponent(n)
	case kindLambdaExpression, kindMethodReference:
		c.charge(config.MetricLambda, n)
	case kindIdentifier, kindTypeIdentifier:
		c.refs[n.Utf8Text(c.src)] = struct{}{}
	}
}

// countListedInterfaces charges one point per type an `implements` or an
// interface's `extends` clause lists, each on its own type so that
// `implements A, B` is two occurrences rather than one wide range. Only
// these two clauses hold a type_list the analyzer charges: a `permits`
// clause lists the same way but narrows who may extend the type instead of
// making it depend on anything, and stays 0.
func (c *counter) countListedInterfaces(n *ts.Node) {
	list := c.typeList(n)
	if list == nil {
		return
	}
	for _, child := range treesitter.NamedChildren(list) {
		listed := child
		c.charge(config.MetricInheritance, &listed)
	}
}

// typeList returns the type_list a heritage clause holds, nil when the
// clause names none.
func (c *counter) typeList(n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		list := child
		if c.g.kindOf(&list) == kindTypeList {
			return &list
		}
	}
	return nil
}

// countAnonymousClass charges the supertype an anonymous class implements or
// extends -- an object creation that brings a class body of its own. It
// implements that type as much as a named class does, and a reader must
// follow the supertype to know what the body is for, so it is inheritance
// and not a lambda. The occurrence points at the type, the one name in the
// expression that says what was implemented.
func (c *counter) countAnonymousClass(n *ts.Node) {
	if !c.hasClassBody(n) {
		return
	}
	c.charge(config.MetricInheritance, nodeOr(n.ChildByFieldId(c.g.fields.kindType), n))
}

// hasClassBody reports whether an object creation declares a body, which is
// what makes it an anonymous class rather than a plain `new`.
func (c *counter) hasClassBody(n *ts.Node) bool {
	for _, child := range treesitter.NamedChildren(n) {
		body := child
		if c.g.kindOf(&body) == kindClassBody {
			return true
		}
	}
	return false
}

// countDeclarator charges one declared variable, on the declarator rather
// than on the declaration, so `int a, b;` is two occurrences a reader can
// tell apart. Every other declarator the grammar produces -- an annotation
// element's default, for one -- declares no variable and is skipped.
func (c *counter) countDeclarator(n *ts.Node) {
	if c.declaresVariables(n.Parent()) {
		c.charge(config.MetricLocalVariable, n)
	}
}

// declaresVariables reports whether a declaration's declarators name
// variables: a local, a field or an interface constant, which read the same
// way to whoever follows the name.
func (c *counter) declaresVariables(n *ts.Node) bool {
	if n == nil {
		return false
	}
	switch c.g.kindOf(n) {
	case kindLocalVariableDeclaration, kindFieldDeclaration, kindConstantDeclaration:
		return true
	default:
		return false
	}
}

// countResource charges a try-with-resources resource that declares a name.
// A resource that only names an already-declared variable, `try (existing)`,
// declares nothing and is 0.
func (c *counter) countResource(n *ts.Node) {
	if n.ChildByFieldId(c.g.fields.name) != nil {
		c.charge(config.MetricLocalVariable, n)
	}
}

// countLoopBinding charges the binding of an enhanced `for` as one
// method-level temporary variable, which is what docs/cdd.md counts and what
// TypeScript charges for a `for…of` binding. The charge points at the
// binding, the closest the grammar has to the declaration the loop never
// gets.
func (c *counter) countLoopBinding(n *ts.Node) {
	if binding := n.ChildByFieldId(c.g.fields.name); binding != nil {
		c.charge(config.MetricLocalVariable, binding)
	}
}

// countRecordComponent charges a record's component, which is a field with a
// shorter spelling: the record holds it and every method reads it by name,
// exactly the case Kotlin already charges for a `val` constructor parameter.
// Every other formal parameter -- of a method, a constructor or a lambda --
// stays 0.
func (c *counter) countRecordComponent(n *ts.Node) {
	if c.isRecordComponent(n) {
		c.charge(config.MetricLocalVariable, n)
	}
}

// isRecordComponent reports whether a formal parameter is the component list
// of a record rather than the parameters of something the record declares.
func (c *counter) isRecordComponent(n *ts.Node) bool {
	list := n.Parent()
	if list == nil {
		return false
	}
	owner := list.Parent()
	if owner == nil || c.g.kindOf(owner) != kindRecordDeclaration {
		return false
	}
	components := owner.ChildByFieldId(c.g.fields.parameters)
	return components != nil && components.Id() == list.Id()
}

// countTryBody charges the block a `try` guards, plain or with resources.
// The occurrence sits on that block rather than on the whole statement,
// whose range would swallow the catch and finally clauses charged beside it.
func (c *counter) countTryBody(n *ts.Node) {
	c.charge(config.MetricExceptionHandling, nodeOr(n.ChildByFieldId(c.g.fields.body), n))
}

// nodeOr returns n when the grammar gave one and fallback otherwise, so a
// charge always has a range to point at, even on a shape the grammar spells
// without the field the rule reads.
func nodeOr(n, fallback *ts.Node) *ts.Node {
	if n == nil {
		return fallback
	}
	return n
}

// countElse charges the `else` branch of an if_statement, unless that branch
// is itself an `if`: that one already charged itself, so `if / else if /
// else` is 3 and not 4 (FR-9).
func (c *counter) countElse(n *ts.Node) {
	alternative := n.ChildByFieldId(c.g.fields.alternative)
	if alternative == nil || c.g.kindOf(alternative) == kindIfStatement {
		return
	}
	c.chargeSpan(config.MetricCodeBranch, c.elseSpan(n, alternative))
}

// elseSpan is the range an `else` charge points at: from the keyword to the
// end of the branch, so a reader sees the branch rather than the whole
// statement, which starts back at the `if`. The keyword is an anonymous
// child of the if_statement; without it the branch alone is the best range.
func (c *counter) elseSpan(n, alternative *ts.Node) treesitter.Span {
	branch := treesitter.SpanOf(alternative)
	keyword := findToken(n, c.g.tokens.elseKeyword)
	if keyword == nil {
		return branch
	}
	start := treesitter.SpanOf(keyword)
	return treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: branch.EndLine, EndCol: branch.EndCol,
	}
}

// countSwitchArm charges an old-style switch arm, which the grammar spreads
// over more than one node: `case 1: case 2: stmt;` parses as two
// switch_block_statement_groups, the first holding only its label and the
// second holding its own label plus the statements. So an arm is the run of
// label-only groups that fall through, plus the group that carries the code,
// and it is charged once, on the group that ends it, from the first label of
// the run. That makes `case 1: case 2: stmt` one branch, like the arrow
// form's `case 1, 2 ->`, and `default: case 1: stmt` one too, because the
// arm still tests a value. An arm of `default` alone is none (FR-10).
func (c *counter) countSwitchArm(n *ts.Node) {
	if c.isLabelOnly(n) {
		return
	}
	first, tested := n, c.testsAValue(n)
	for s := c.prevGroup(n); s != nil && c.isLabelOnly(s); s = c.prevGroup(s) {
		first, tested = s, tested || c.testsAValue(s)
	}
	if !tested {
		return
	}
	start, end := treesitter.SpanOf(first), treesitter.SpanOf(n)
	c.chargeSpan(config.MetricCodeBranch, treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: end.EndLine, EndCol: end.EndCol,
	})
}

// prevGroup returns the arm before n inside the switch block, skipping the
// comments a reader may have written between the labels of a fallthrough.
func (c *counter) prevGroup(n *ts.Node) *ts.Node {
	for s := n.PrevNamedSibling(); s != nil; s = s.PrevNamedSibling() {
		if k := c.g.kindOf(s); k != kindLineComment && k != kindBlockComment {
			return s
		}
	}
	return nil
}

// testsAValue reports whether a switch arm's label names a value to match,
// rather than being the `default` every unmatched value falls into. The
// keyword is an anonymous child of the switch_label.
func (c *counter) testsAValue(n *ts.Node) bool {
	label := c.switchLabel(n)
	return label != nil && !hasToken(label, c.g.tokens.defaultKeyword)
}

// switchLabel returns the switch_label of an arm, nil when it has none.
func (c *counter) switchLabel(n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		label := child
		if c.g.kindOf(&label) == kindSwitchLabel {
			return &label
		}
	}
	return nil
}

// isLabelOnly reports whether a switch group holds labels and nothing else,
// which is how the grammar writes a `case` that falls through into the arm
// below it.
func (c *counter) isLabelOnly(n *ts.Node) bool {
	if c.g.kindOf(n) != kindSwitchBlockStatementGroup {
		return false
	}
	for _, child := range treesitter.NamedChildren(n) {
		statement := child
		if c.isStatement(&statement) {
			return false
		}
	}
	return true
}

// isStatement reports whether a child of a switch group is code the arm runs,
// rather than one of its labels or a comment written between them.
func (c *counter) isStatement(n *ts.Node) bool {
	switch c.g.kindOf(n) {
	case kindSwitchLabel, kindLineComment, kindBlockComment:
		return false
	default:
		return true
	}
}

// countCondition charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if (a > b && c < d)` is 3 ICPs, "1 for the if and 1
// for each Boolean condition". The clauses of a chain are its leaf operands,
// so `a && b` is 2, `a && b || c` is 3, and a plain `if (x > 1)` has no
// logical operator and adds no condition at all.
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
// flattens through nested logical operators, parentheses and `!`, so the node
// that lands in out is the operand itself, `a` rather than `!a`. Flattening
// through `!` follows De Morgan: `!(a || b) && x` is `!a && !b && x`, three
// clauses, not four. Every logical node it folds in is marked consumed so the
// walk does not count it again.
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
			return c.clauses(n.ChildByFieldId(c.g.fields.operand), out)
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
	case opAnd, opOr:
		return true
	default:
		return false
	}
}

// operator returns the text of n's operator token. The token is anonymous and
// short, so reading it is the one string the walk pays for per operator node.
func (c *counter) operator(n *ts.Node) string {
	op := n.ChildByFieldId(c.g.fields.operator)
	if op == nil {
		return ""
	}
	return op.Kind()
}
