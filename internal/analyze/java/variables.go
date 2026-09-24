package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// variables charges the local-variable metric.
//
// A local variable is one declarator, so `int a, b;` is two, and a field,
// an interface constant and a block-local are the same declaration to a
// reader following a name. A resource and a record component each declare
// a name the body then reads, so they count too, as does the binding of an
// enhanced `for`, which the control-flow rules charge beside the loop. A
// parameter does not: it names a value the caller already had.
type variables struct {
	g *grammar
	l *ledger
}

// countDeclarator charges one declared variable, on the declarator rather
// than on the declaration, so `int a, b;` is two occurrences a reader can
// tell apart. Every other declarator the grammar produces -- an annotation
// element's default, for one -- declares no variable and is skipped.
func (v *variables) countDeclarator(n *ts.Node) {
	if v.declaresVariables(n.Parent()) {
		v.l.charge(config.MetricLocalVariable, n)
	}
}

// declaresVariables reports whether a declaration's declarators name
// variables: a local, a field or an interface constant, which read the same
// way to whoever follows the name.
func (v *variables) declaresVariables(n *ts.Node) bool {
	if n == nil {
		return false
	}
	switch v.g.kindOf(n) {
	case kindLocalVariableDeclaration, kindFieldDeclaration, kindConstantDeclaration:
		return true
	default:
		return false
	}
}

// countResource charges a try-with-resources resource that declares a name.
// A resource that only names an already-declared variable, `try (existing)`,
// declares nothing and is 0.
func (v *variables) countResource(n *ts.Node) {
	if n.ChildByFieldId(v.g.fields.name) != nil {
		v.l.charge(config.MetricLocalVariable, n)
	}
}

// countRecordComponent charges a record's component, which is a field with a
// shorter spelling: the record holds it and every method reads it by name,
// exactly the case Kotlin already charges for a `val` constructor parameter.
// Every other formal parameter -- of a method, a constructor or a lambda --
// stays 0.
func (v *variables) countRecordComponent(n *ts.Node) {
	if v.isRecordComponent(n) {
		v.l.charge(config.MetricLocalVariable, n)
	}
}

// isRecordComponent reports whether a formal parameter is the component list
// of a record rather than the parameters of something the record declares.
func (v *variables) isRecordComponent(n *ts.Node) bool {
	list := n.Parent()
	if list == nil {
		return false
	}
	owner := list.Parent()
	if owner == nil || v.g.kindOf(owner) != kindRecordDeclaration {
		return false
	}
	components := owner.ChildByFieldId(v.g.fields.parameters)
	return components != nil && components.Id() == list.Id()
}
