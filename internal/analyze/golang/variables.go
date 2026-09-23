package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// variables charges the local-variable metric for the names a declaration
// introduces, wherever the declaration sits.
type variables struct {
	l *ledger
	// guards holds the `v := x.(type)` of every type switch met so far,
	// which declares nothing a reader holds in mind and is skipped.
	guards map[ast.Node]bool
}

// newVariables returns the variable rules charging l.
func newVariables(l *ledger) *variables {
	return &variables{l: l, guards: map[ast.Node]bool{}}
}

// countNames charges one local_variable per declared name, which is how a
// `var`, a `const` and a struct field are all counted: `var x, y = 1, 2` is
// two variables to hold in mind, not one declaration. The blank identifier
// names nothing a reader can read back and is 0.
func (v *variables) countNames(names []*ast.Ident) {
	for _, name := range names {
		if !isBlank(name) {
			v.l.charge(config.MetricLocalVariable, name)
		}
	}
}

// countDefine charges the names a short variable declaration introduces. A
// `:=` may also assign to a name already in scope — the `z` of
// `z, err := split(y)` — which the resolver marks by leaving the object's
// Decl on the statement that first declared it, so only new names count.
func (v *variables) countDefine(n *ast.AssignStmt) {
	if n.Tok != token.DEFINE || v.guards[n] {
		return
	}
	for _, lhs := range n.Lhs {
		if name, ok := lhs.(*ast.Ident); ok && declares(name, n) {
			v.l.charge(config.MetricLocalVariable, name)
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
func (v *variables) countRange(n *ast.RangeStmt) {
	if n.Tok != token.DEFINE {
		return
	}
	for _, binding := range []ast.Expr{n.Key, n.Value} {
		if name, ok := binding.(*ast.Ident); ok && !isBlank(name) {
			v.l.charge(config.MetricLocalVariable, name)
		}
	}
}

// isBlank reports whether an identifier is the blank one, which declares no
// name a reader can read back.
func isBlank(n *ast.Ident) bool {
	return n.Name == "_"
}
