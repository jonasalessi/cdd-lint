package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// declarations charges the inheritance, local-variable and lambda metrics,
// and records the identifiers the unit mentions.
//
// A local variable is one declarator, so `const {a, b} = x` is one and
// `let b = 2, c = 3` is two. A `for` initialiser holds declarators and
// counts; a `for…in` or `for…of` binding has no declarator in the grammar
// and is charged by the control-flow rules instead. Class fields count,
// `#private` ones included, since the grammar gives them all the same node;
// an interface's property signatures describe a shape rather than declare
// variables, and do not.
type declarations struct {
	g   *grammar
	src []byte
	l   *ledger
	// refs are the identifiers the unit mentions, used to attribute the
	// file's imports to the units that actually reference them.
	refs map[string]struct{}
	// skipDeclarator is the unit's own declarator, which is not one of its
	// local variables; skipLambda is the unit's own function body, which is
	// not one of its lambdas.
	skipDeclarator uintptr
	skipLambda     uintptr
}

// newDeclarations returns the declaration rules charging l for the unit
// rooted at d.
func newDeclarations(g *grammar, src []byte, l *ledger, d *unitDecl) *declarations {
	decls := &declarations{g: g, src: src, l: l, refs: map[string]struct{}{}}
	if g.kindOf(&d.node) == kindVariableDeclarator {
		decls.skipDeclarator = d.node.Id()
	}
	if d.body != nil {
		decls.skipLambda = d.body.Id()
	}
	return decls
}

// count charges n when k is a declaration construct.
func (d *declarations) count(k kind, n *ts.Node) {
	switch k {
	case kindExtendsClause:
		d.chargeParents(n, fieldValue)
	case kindExtendsTypeClause:
		d.chargeParents(n, fieldType)
	case kindImplementsClause:
		d.chargeParents(n, "")
	case kindVariableDeclarator:
		if n.Id() != d.skipDeclarator {
			d.l.charge(config.MetricLocalVariable, n, 1)
		}
	case kindPublicFieldDefinition:
		d.l.charge(config.MetricLocalVariable, n, 1)
	case kindArrowFunction, kindFunctionExpression:
		if n.Id() != d.skipLambda {
			d.l.charge(config.MetricLambda, n, 1)
		}
	case kindIdentifier, kindTypeIdentifier, kindShorthandPropertyIdentifier:
		d.refs[n.Utf8Text(d.src)] = struct{}{}
	}
}

// chargeParents charges one inheritance ICP per parent a heritage clause
// lists, on the parent's own type node rather than on the clause, so
// `implements A, B` is two occurrences a reader can tell apart. field
// selects the children that are parents rather than type arguments:
// `extends Base<T>` holds Base in the clause's field and T outside it. An
// empty field takes every named child, which is what `implements` holds.
func (d *declarations) chargeParents(n *ts.Node, field string) {
	for _, parent := range treesitter.NamedChildrenInField(n, field) {
		d.l.charge(config.MetricInheritance, &parent, 1)
	}
}
