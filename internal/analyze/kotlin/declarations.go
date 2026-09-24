package kotlin

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// declarations charges the inheritance, local-variable and lambda metrics,
// and records the identifiers the unit mentions.
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
type declarations struct {
	g   *grammar
	src []byte
	l   *ledger
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

// newDeclarations returns the declaration rules charging l for the unit
// rooted at d.
func newDeclarations(g *grammar, src []byte, l *ledger, d *unitDecl) *declarations {
	decls := &declarations{g: g, src: src, l: l, refs: map[string]struct{}{}}
	if d.kind == unitProperty {
		decls.skipDeclaration = d.node.Id()
	}
	if d.body != nil {
		decls.skipLambda = d.body.Id()
	}
	return decls
}

// count charges n when k is a declaration construct.
func (d *declarations) count(k kind, n *ts.Node) {
	switch k {
	case kindDelegationSpecifier:
		d.l.charge(config.MetricInheritance, d.specifierType(n))
	case kindPropertyDeclaration:
		if n.Id() != d.skipDeclaration && !d.isShape(n) {
			d.l.charge(config.MetricLocalVariable, n)
		}
	case kindClassParameter:
		if hasToken(n, d.g.tokens.val) || hasToken(n, d.g.tokens.variable) {
			d.l.charge(config.MetricLocalVariable, n)
		}
	case kindLambdaLiteral, kindAnonymousFunction, kindCallableReference:
		if n.Id() != d.skipLambda {
			d.l.charge(config.MetricLambda, n)
		}
	case kindIdentifier:
		d.refs[n.Utf8Text(d.src)] = struct{}{}
	}
}

// specifierType returns the user_type a delegation specifier names, which
// is where its inheritance occurrence points so that `: A, B` is two
// occurrences a reader can tell apart. The type sits directly under the
// specifier for `: Iface`, and one level down for `: Base()` and
// `: Iface by d`, whose specifier wraps a constructor_invocation or an
// explicit_delegation. A specifier of another shape is charged whole.
func (d *declarations) specifierType(n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		outer := child
		if d.g.kindOf(&outer) == kindUserType {
			return &outer
		}
		if inner := d.g.childOfKind(&outer, kindUserType); inner != nil {
			return inner
		}
	}
	return n
}

// isShape reports whether a property declaration is an interface member
// with no value of its own: no initializer, no delegate, no accessor body.
func (d *declarations) isShape(n *ts.Node) bool {
	body := n.Parent()
	if body == nil || d.g.kindOf(body) != kindClassBody {
		return false
	}
	owner := body.Parent()
	if owner == nil || d.g.kindOf(owner) != kindClassDeclaration || !hasToken(owner, d.g.tokens.interfaceKeyword) {
		return false
	}
	if hasToken(n, d.g.tokens.assign) {
		return false
	}
	_, hasValue := propertyBody(d.g, n)
	return !hasValue
}
