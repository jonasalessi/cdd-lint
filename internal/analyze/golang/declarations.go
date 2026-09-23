package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// declarations charges the metrics a declaration carries — embedding, locals
// and func literals. A type switch marks its own guard here: ast.Inspect
// visits the statement before the `v := x.(type)` it holds, so the short
// declaration rule never sees it and the guard stays 0, like a Java pattern
// variable.
type declarations struct {
	l    *ledger
	vars *variables
}

// newDeclarations returns the declaration rules charging l.
func newDeclarations(l *ledger) *declarations {
	return &declarations{l: l, vars: newVariables(l)}
}

// count charges n when it is a declaration construct.
func (d *declarations) count(n ast.Node) {
	switch n := n.(type) {
	case *ast.StructType:
		d.countFields(n)
	case *ast.InterfaceType:
		d.countInterfaceEmbedding(n)
	case *ast.ValueSpec:
		d.vars.countNames(n.Names)
	case *ast.TypeSwitchStmt:
		d.vars.guards[n.Assign] = true
	case *ast.AssignStmt:
		d.vars.countDefine(n)
	case *ast.RangeStmt:
		d.vars.countRange(n)
	case *ast.FuncLit:
		d.countFuncLit(n)
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
func (d *declarations) countFuncLit(n *ast.FuncLit) {
	d.l.charge(config.MetricLambda, n)
}

// countFields charges the members of a struct type, the unit's own and every
// anonymous one written inside its body. A field with no name embeds another
// type, which is one inheritance point; a named field is one local_variable
// per name, the per-declarator rule TypeScript and Java already follow.
func (d *declarations) countFields(n *ast.StructType) {
	for _, field := range n.Fields.List {
		if field.Names == nil {
			d.l.charge(config.MetricInheritance, field.Type)
			continue
		}
		d.vars.countNames(field.Names)
	}
}

// countInterfaceEmbedding charges one inheritance point per embedded
// interface. An element with names is a method signature and costs nothing,
// and a type term — `~string`, or a `A | B` union of them — is a constraint
// on what may instantiate a parameter, not a supertype a reader must follow.
func (d *declarations) countInterfaceEmbedding(n *ast.InterfaceType) {
	for _, element := range n.Methods.List {
		if element.Names == nil && !isTypeTerm(element.Type) {
			d.l.charge(config.MetricInheritance, element.Type)
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
