package kotlin

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// The Kind values a Kotlin unit can carry. The grammar folds interfaces,
// enums, sealed, data and annotation classes into class_declaration; the
// keyword is read once per unit so the report never calls an interface a
// class.
const (
	unitClass     = "class"
	unitInterface = "interface"
	unitEnum      = "enum"
	unitObject    = "object"
	unitTypeAlias = "typealias"
	unitFunction  = "function"
	unitProperty  = "property"
)

// unitDecl is one extracted unit: the node whose subtree the metrics are
// counted over, plus the metadata the pipeline reports. The node is only
// valid while the tree that produced it is open.
type unitDecl struct {
	node ts.Node
	// body is the lambda or anonymous function a property unit is named
	// after. It is the unit itself, so it is never charged as one of the
	// unit's lambdas (FR-11).
	body *ts.Node
	name string
	kind string
	line int
	col  int
}

// units returns the file's units in source order (FR-4).
//
// A unit is a top-level declaration: a direct child of source_file that is
// a class, interface, enum, object, function, type alias, or a property
// whose value is code -- a lambda, an anonymous function, an accessor with
// a body, or a delegate. A top-level property holding any other value is
// not a unit. There is no visibility filter: a private function still
// carries complexity someone maintains. Nothing nested inside a unit is a
// unit; its complexity is charged to the unit that contains it.
//
// Line and Col point at the first token after the modifiers: `class` for
// `data class E`, `fun` for `private fun m`, `val` for a property.
func units(g *grammar, root *ts.Node, src []byte) []unitDecl {
	var out []unitDecl
	for _, child := range treesitter.NamedChildren(root) {
		n := child
		if d, ok := declaredUnit(g, &n, src); ok {
			out = append(out, d)
		}
	}
	return out
}

// declaredUnit maps a declaration node onto a unit, reporting false for the
// nodes that are not units.
func declaredUnit(g *grammar, n *ts.Node, src []byte) (unitDecl, bool) {
	var (
		unit string
		name *ts.Node
		body *ts.Node
	)
	switch g.kindOf(n) {
	case kindClassDeclaration:
		unit = classKind(g, n)
		name = n.ChildByFieldId(g.fields.name)
	case kindObjectDeclaration:
		unit = unitObject
		name = n.ChildByFieldId(g.fields.name)
	case kindFunctionDeclaration:
		unit = unitFunction
		name = n.ChildByFieldId(g.fields.name)
	case kindTypeAlias:
		unit = unitTypeAlias
		name = n.ChildByFieldId(g.fields.kindType)
	case kindPropertyDeclaration:
		var ok bool
		if body, ok = propertyBody(g, n); !ok {
			return unitDecl{}, false
		}
		unit = unitProperty
		name = propertyName(g, n)
	default:
		return unitDecl{}, false
	}
	line, col := treesitter.Position(declarationStart(g, n))
	return unitDecl{
		node: *n,
		body: body,
		name: treesitter.Text(name, src),
		kind: unit,
		line: line,
		col:  col,
	}, true
}

// classKind reads the keyword of a class_declaration: `interface` for
// interfaces (`sealed interface` and `fun interface` included), `enum` for
// a class carrying the enum modifier, `class` for everything else --
// sealed, data, annotation, abstract, open, inner and value classes alike.
func classKind(g *grammar, n *ts.Node) string {
	if hasToken(n, g.tokens.interfaceKeyword) {
		return unitInterface
	}
	if hasClassModifier(g, n, g.tokens.enum) {
		return unitEnum
	}
	return unitClass
}

// hasClassModifier reports whether the declaration's modifiers hold a
// class_modifier spelled with the given keyword token.
func hasClassModifier(g *grammar, n *ts.Node, token uint16) bool {
	mods := modifiers(g, n)
	if mods == nil {
		return false
	}
	for _, child := range treesitter.NamedChildren(mods) {
		m := child
		if g.kindOf(&m) == kindClassModifier && hasToken(&m, token) {
			return true
		}
	}
	return false
}

// modifiers returns the declaration's modifiers node, nil when it has
// none.
func modifiers(g *grammar, n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		m := child
		if g.kindOf(&m) == kindModifiers {
			return &m
		}
	}
	return nil
}

// declarationStart returns the first child of n that is not its modifiers,
// which is the keyword the declaration is reported at. A declaration with
// no such child, which the grammar never produces, is reported at itself.
func declarationStart(g *grammar, n *ts.Node) *ts.Node {
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if child == nil || g.kindOf(child) == kindModifiers {
			continue
		}
		return child
	}
	return n
}

// propertyBody reports whether a property declaration is a unit, and
// returns the lambda or anonymous function it initializes when that is why.
// A property is a unit when its initializer is a lambda or an anonymous
// function, when it carries a getter or setter with a body, or when it is
// delegated (`by lazy { }`).
func propertyBody(g *grammar, n *ts.Node) (body *ts.Node, ok bool) {
	for _, child := range treesitter.NamedChildren(n) {
		c := child
		switch g.kindOf(&c) {
		case kindLambdaLiteral, kindAnonymousFunction:
			return &c, true
		case kindPropertyDelegate:
			ok = true
		case kindGetter, kindSetter:
			ok = ok || hasAccessorBody(g, &c)
		}
	}
	return nil, ok
}

// hasAccessorBody reports whether a getter or setter declares a body,
// rather than just changing the default accessor's visibility.
func hasAccessorBody(g *grammar, accessor *ts.Node) bool {
	for _, child := range treesitter.NamedChildren(accessor) {
		c := child
		if g.kindOf(&c) == kindFunctionBody {
			return true
		}
	}
	return false
}

// propertyName returns the identifier of the property's variable
// declaration.
func propertyName(g *grammar, n *ts.Node) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		c := child
		if g.kindOf(&c) != kindVariableDeclaration {
			continue
		}
		return treesitter.FirstNamedChild(&c)
	}
	return nil
}
