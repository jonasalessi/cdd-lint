package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// counter accumulates the raw ICP counts of one unit while the subtrees it
// owns are walked. It counts every metric, enabled or not: the pipeline
// drops the ones the configuration disables (FR-4).
//
// Every count is charged through charge or chargeSpan, which record where it
// came from, so a unit's Counts is always the sum of its Occurrences' Count,
// metric by metric.
type counter struct {
	fset   *token.FileSet
	counts map[config.MetricID]int
	// occurrences locate every charge, in the order it was made. ast.Inspect
	// walks in pre-order, so they come out in source order except for the
	// leaf clauses flattened out of a chain; sortedOccurrences orders them.
	occurrences []analyze.Occurrence
	// consumed holds the logical expressions already folded into an
	// enclosing clause chain, so a nested `&&` is never counted twice.
	consumed map[ast.Node]bool
	// refs are the package names the unit qualifies something by, used to
	// attribute the file's imports to the units that actually reference
	// them (FR-8).
	refs map[string]struct{}
}

// newCounter returns a counter for the subtrees of one unit.
func newCounter(fset *token.FileSet) *counter {
	return &counter{
		fset:     fset,
		counts:   zeroCounts(),
		consumed: map[ast.Node]bool{},
		refs:     map[string]struct{}{},
	}
}

// zeroCounts returns a map holding every metric at zero. A unit always
// carries a key for every metric, enabled or not: the pipeline drops the
// ones the configuration disables (FR-4).
func zeroCounts() map[config.MetricID]int {
	counts := make(map[config.MetricID]int, len(config.Metrics()))
	for _, m := range config.Metrics() {
		counts[m] = 0
	}
	return counts
}

// measureUnit counts the ICPs of one unit over every subtree billed to it —
// the type declaration plus its methods, or the function itself — attributes
// the file's imports to it, and reports the unit the pipeline consumes
// (FR-4, FR-8).
func measureUnit(fset *token.FileSet, d unitDecl, mods []module) analyze.Unit {
	c := newCounter(fset)
	for _, n := range d.nodes {
		ast.Inspect(n, c.visit)
	}
	c.countCoupling(mods)
	return analyze.Unit{
		Name:        d.name,
		Kind:        d.kind,
		Line:        d.line,
		Col:         d.col,
		Counts:      c.counts,
		Occurrences: c.sortedOccurrences(),
	}
}

// charge adds one point of metric to the unit and records n's range as where
// it comes from. Every Go construct is worth one point: the language has no
// form that folds two decisions into one node.
func (c *counter) charge(metric config.MetricID, n ast.Node) {
	c.chargeSpan(metric, c.span(n))
}

// chargeSpan is charge for a range the caller computed itself, which is how
// a charge that points at part of a node is located.
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

// span returns n's range the way analyze.Occurrence carries it.
func (c *counter) span(n ast.Node) treesitter.Span {
	return spanOf(c.fset, n)
}

// spanOf returns n's range the way analyze.Occurrence carries it. go/token
// positions are 1-based with byte columns and End is already exclusive, so
// they are the contract treesitter.SpanOf produces and spans compare across
// languages. The imports need it without a counter, which is why it stands
// on its own.
func spanOf(fset *token.FileSet, n ast.Node) treesitter.Span {
	start, end := fset.Position(n.Pos()), fset.Position(n.End())
	return treesitter.Span{
		Line:    start.Line,
		Col:     start.Column,
		EndLine: end.Line,
		EndCol:  end.Column,
	}
}

// sortedOccurrences returns the unit's occurrences in source order.
func (c *counter) sortedOccurrences() []analyze.Occurrence {
	treesitter.SortOccurrences(c.occurrences)
	return c.occurrences
}

// visit is the ast.Inspect callback; it always descends, because a unit owns
// every construct nested inside it, func literals and local types included.
// Both halves see every node, because one node can be both: a `range` loop is
// a branch and declares the names it iterates with.
func (c *counter) visit(n ast.Node) bool {
	c.countControlFlow(n)
	c.countDeclaration(n)
	c.collectRef(n)
	return true
}

// collectRef records the package name a qualified expression reads, which is
// what attributes the file's imports to this unit (FR-8). The qualifier of
// `fmt.Sprint` is a package only when the resolver left it unresolved: a
// parameter, a local or a field named `fmt` carries an object, so it shadows
// the package and the selector is not a use of it. That is the precise rule
// Java cannot state and go/parser gives for free.
//
// It reads the deprecated Ident.Obj on purpose, for the reason declares
// states: syntactic resolution is exactly what a per-file analyzer needs.
func (c *counter) collectRef(n ast.Node) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if qualifier, ok := sel.X.(*ast.Ident); ok && qualifier.Obj == nil {
		c.refs[qualifier.Name] = struct{}{}
	}
}

// countControlFlow charges the branch and condition metrics.
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
func (c *counter) countControlFlow(n ast.Node) {
	switch n := n.(type) {
	case *ast.IfStmt:
		c.charge(config.MetricCodeBranch, n)
		c.countElse(n)
	case *ast.CaseClause:
		c.countArm(n, n.List != nil)
	case *ast.CommClause:
		c.countArm(n, n.Comm != nil)
	case *ast.ForStmt, *ast.RangeStmt:
		c.charge(config.MetricCodeBranch, n)
	case *ast.BinaryExpr:
		c.countCondition(n)
	}
}

// countDeclaration charges the metrics a declaration carries — embedding,
// locals and func literals. A type switch marks its own guard consumed here:
// ast.Inspect visits the statement before the `v := x.(type)` it holds, so the
// short declaration rule never sees it and the guard stays 0, like a Java
// pattern variable.
func (c *counter) countDeclaration(n ast.Node) {
	switch n := n.(type) {
	case *ast.StructType:
		c.countFields(n)
	case *ast.InterfaceType:
		c.countInterfaceEmbedding(n)
	case *ast.ValueSpec:
		c.countNames(n.Names)
	case *ast.TypeSwitchStmt:
		c.consumed[n.Assign] = true
	case *ast.AssignStmt:
		c.countDefine(n)
	case *ast.RangeStmt:
		c.countRange(n)
	case *ast.FuncLit:
		c.countFuncLit(n)
	}
}

// countFuncLit charges one lambda per func literal, wherever it is written: a
// literal a reader meets inside a body is another function to hold in mind.
// The literal of `go func(){}()` and of `defer func(){}()` is charged like any
// other, while the `go` and the `defer` themselves are not branches and cost
// nothing of their own.
//
// A method value (`l.Wire`) or a method expression (`Lambdas.Wire`) is 0: with
// no type information a selector that yields a function is indistinguishable
// from a field access, and guessing from capitalisation or from the position
// of the selector would be a heuristic, not a rule.
func (c *counter) countFuncLit(n *ast.FuncLit) {
	c.charge(config.MetricLambda, n)
}

// countFields charges the members of a struct type, the unit's own and every
// anonymous one written inside its body. A field with no name embeds another
// type, which is one inheritance point; a named field is one local_variable
// per name, the per-declarator rule TypeScript and Java already follow.
func (c *counter) countFields(n *ast.StructType) {
	for _, field := range n.Fields.List {
		if field.Names == nil {
			c.charge(config.MetricInheritance, field.Type)
			continue
		}
		c.countNames(field.Names)
	}
}

// countInterfaceEmbedding charges one inheritance point per embedded
// interface. An element with names is a method signature and costs nothing,
// and a type term — `~string`, or a `A | B` union of them — is a constraint
// on what may instantiate a parameter, not a supertype a reader must follow.
func (c *counter) countInterfaceEmbedding(n *ast.InterfaceType) {
	for _, element := range n.Methods.List {
		if element.Names == nil && !isTypeTerm(element.Type) {
			c.charge(config.MetricInheritance, element.Type)
		}
	}
}

// isTypeTerm reports whether an unnamed interface element is a type term
// rather than an embedded interface: `~T` approximates a type and `A | B`
// unions terms, while `Reader` and `fmt.Stringer` name interfaces.
func isTypeTerm(n ast.Expr) bool {
	switch e := n.(type) {
	case *ast.BinaryExpr:
		return e.Op == token.OR
	case *ast.UnaryExpr:
		return e.Op == token.TILDE
	}
	return false
}

// countNames charges one local_variable per declared name, which is how a
// `var`, a `const` and a struct field are all counted: `var x, y = 1, 2` is
// two variables to hold in mind, not one declaration. The blank identifier
// names nothing a reader can read back and is 0.
func (c *counter) countNames(names []*ast.Ident) {
	for _, name := range names {
		if !isBlank(name) {
			c.charge(config.MetricLocalVariable, name)
		}
	}
}

// countDefine charges the names a short variable declaration introduces. A
// `:=` may also assign to a name already in scope — the `z` of
// `z, err := split(y)` — which the resolver marks by leaving the object's
// Decl on the statement that first declared it, so only new names count.
func (c *counter) countDefine(n *ast.AssignStmt) {
	if n.Tok != token.DEFINE || c.consumed[n] {
		return
	}
	for _, lhs := range n.Lhs {
		if name, ok := lhs.(*ast.Ident); ok && declares(name, n) {
			c.charge(config.MetricLocalVariable, name)
		}
	}
}

// declares reports whether stmt introduces name instead of assigning to a
// name already in scope. It reads the deprecated Ident.Obj on purpose: the
// syntactic resolution go/parser does is exactly what a per-file analyzer
// needs, and the alternative is a type checker that would have to load the
// whole package to answer the same question.
func declares(name *ast.Ident, stmt ast.Stmt) bool {
	return !isBlank(name) && name.Obj != nil && name.Obj.Decl == stmt
}

// countRange charges the names a `for … range` introduces. They are read off
// the statement rather than through the short-declaration rule: the resolver
// synthesizes the assignment, so a range binding carries no object pointing
// back at the loop. `for i = range xs` assigns to a name that already exists
// and is 0, and so is a blank binding.
func (c *counter) countRange(n *ast.RangeStmt) {
	if n.Tok != token.DEFINE {
		return
	}
	for _, binding := range []ast.Expr{n.Key, n.Value} {
		if name, ok := binding.(*ast.Ident); ok && !isBlank(name) {
			c.charge(config.MetricLocalVariable, name)
		}
	}
}

// isBlank reports whether an identifier is the blank one, which declares no
// name a reader can read back.
func isBlank(n *ast.Ident) bool {
	return n.Name == "_"
}

// countArm charges one arm of a switch, a type switch or a select when the
// arm tests something. A `default` tests nothing and is 0: it is where every
// value not matched above falls, not a decision of its own.
func (c *counter) countArm(n ast.Node, tests bool) {
	if tests {
		c.charge(config.MetricCodeBranch, n)
	}
}

// countElse charges the alternative of an `if` only when it is a block: an
// `else if` is an IfStmt that charges itself, so `if / else if / else` is 3
// and not 4 (FR-6). The occurrence spans the block, since go/ast keeps no
// position for the `else` keyword.
func (c *counter) countElse(n *ast.IfStmt) {
	if block, ok := n.Else.(*ast.BlockStmt); ok {
		c.charge(config.MetricCodeBranch, block)
	}
}

// countCondition charges one ICP per Boolean clause, which is the rule
// docs/cdd.md states: `if a > b && c < d` is 3 ICPs, "1 for the if and 1 for
// each Boolean condition". The clauses of a chain are its leaf operands, so
// `a && b` is 2 and `a && b || c` is 3, while a plain `if x > 1` joins
// nothing and adds no condition at all (FR-5).
func (c *counter) countCondition(n *ast.BinaryExpr) {
	if c.consumed[n] || !isLogical(n) {
		return
	}
	for _, clause := range c.clauses(n, nil) {
		c.charge(config.MetricCondition, clause)
	}
}

// clauses appends the Boolean clauses of the expression rooted at n to out
// and returns it. The clauses of a chain are its leaf operands: the walk
// flattens through nested logical operators, parentheses and `!`, so what
// lands in out is the operand itself, `a` rather than `!a`. Flattening
// through `!` follows De Morgan: `!(a || b) && x` is `!a && !b && x`, three
// clauses, not four. Every logical node it folds in is marked consumed so
// the walk does not count it again.
func (c *counter) clauses(n ast.Expr, out []ast.Expr) []ast.Expr {
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
