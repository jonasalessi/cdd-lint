package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// declarations charges the inheritance and lambda metrics, hands the
// variable declarations to the variable rules, and records the identifiers
// the unit mentions.
//
// Inheritance is charged per supertype, so `extends Base implements A, B`
// is three occurrences a reader can tell apart, and it is charged wherever
// the declaration sits: a nested type's heritage is the enclosing unit's,
// like everything else nested in it.
//
// A lambda is a lambda expression or a method reference, and the grammar
// spells every form of the second one the same way, so `String::valueOf`,
// `ArrayList::new`, `this::m` and `super::m` all count. An anonymous class
// is inheritance instead: it names the type it implements, which a lambda
// never does. Nothing is exempted from the count, because a Java unit is a
// type or a method and never a property whose own body is a lambda, so
// this counter needs neither of Kotlin's skipLambda and skipDeclaration
// escapes.
type declarations struct {
	g    *grammar
	src  []byte
	l    *ledger
	vars *variables
	// refs are the names the unit mentions, used to attribute the file's
	// imports to the units that actually reference them. This grammar spells
	// values `identifier` and types `type_identifier`, so both kinds land
	// here.
	refs map[string]struct{}
}

// newDeclarations returns the declaration rules charging l.
func newDeclarations(g *grammar, src []byte, l *ledger) *declarations {
	return &declarations{
		g:    g,
		src:  src,
		l:    l,
		vars: &variables{g: g, l: l},
		refs: map[string]struct{}{},
	}
}

// count charges n when k is a declaration construct.
func (d *declarations) count(k kind, n *ts.Node) {
	switch k {
	case kindSuperclass:
		d.l.charge(config.MetricInheritance, nodeOr(treesitter.FirstNamedChild(n), n))
	case kindSuperInterfaces, kindExtendsInterfaces:
		d.countListedInterfaces(n)
	case kindObjectCreationExpression:
		d.countAnonymousClass(n)
	case kindVariableDeclarator:
		d.vars.countDeclarator(n)
	case kindResource:
		d.vars.countResource(n)
	case kindFormalParameter:
		d.vars.countRecordComponent(n)
	case kindLambdaExpression, kindMethodReference:
		d.l.charge(config.MetricLambda, n)
	case kindIdentifier, kindTypeIdentifier:
		d.refs[n.Utf8Text(d.src)] = struct{}{}
	}
}

// countListedInterfaces charges one point per type an `implements` or an
// interface's `extends` clause lists, each on its own type so that
// `implements A, B` is two occurrences rather than one wide range. Only
// these two clauses hold a type_list the analyzer charges: a `permits`
// clause lists the same way but narrows who may extend the type instead of
// making it depend on anything, and stays 0.
func (d *declarations) countListedInterfaces(n *ts.Node) {
	list := d.g.childOfKind(n, kindTypeList)
	if list == nil {
		return
	}
	for _, child := range treesitter.NamedChildren(list) {
		listed := child
		d.l.charge(config.MetricInheritance, &listed)
	}
}

// countAnonymousClass charges the supertype an anonymous class implements or
// extends -- an object creation that brings a class body of its own. It
// implements that type as much as a named class does, and a reader must
// follow the supertype to know what the body is for, so it is inheritance
// and not a lambda. The occurrence points at the type, the one name in the
// expression that says what was implemented.
func (d *declarations) countAnonymousClass(n *ts.Node) {
	if d.g.childOfKind(n, kindClassBody) == nil {
		return
	}
	d.l.charge(config.MetricInheritance, nodeOr(n.ChildByFieldId(d.g.fields.kindType), n))
}
