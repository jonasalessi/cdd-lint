package typescript

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// The Kind values a TypeScript unit can carry.
const (
	unitClass     = "class"
	unitInterface = "interface"
	unitEnum      = "enum"
	unitType      = "type"
	unitFunction  = "function"
)

// defaultName is the name of an anonymous `export default` unit.
const defaultName = "default"

// unitDecl is one extracted unit: the node whose subtree the metrics are
// counted over, plus the metadata the pipeline reports. The node is only
// valid while the tree that produced it is open.
type unitDecl struct {
	// node is the metric root. It is the declaration itself, except for
	// `export const f = () => {}`, where it is the declarator, so that the
	// arrow's whole body counts towards the unit.
	node ts.Node
	// body is the arrow function or function expression a declarator unit
	// is named after. It is the unit itself, so it is never charged as one
	// of the unit's lambdas.
	body *ts.Node
	name string
	kind string
	line int
	col  int
}

// units returns the file's units in source order (FR-1).
//
// A unit is a top-level declaration: a direct child of `program`, or the
// declaration of an `export_statement` that is a direct child of `program`.
// Ambient declarations (`declare …`), declaration-only signatures (call
// overloads, `declare function`), non-exported arrow or function-expression
// constants and anything nested inside another declaration are not units:
// their complexity is charged to the unit that contains them.
//
// Line and Col point at the start of the declaration node, which is the
// `class`, `interface`, `enum`, `type` or `function` keyword, or the
// declarator's identifier for an arrow constant. A leading `export` is not
// part of the position.
func units(g *grammar, root *ts.Node, src []byte) []unitDecl {
	var out []unitDecl
	for _, child := range treesitter.NamedChildren(root) {
		n := child
		if g.kindOf(&n) == kindExportStatement {
			out = append(out, exportedUnits(g, &n, src)...)
			continue
		}
		if d, ok := declaredUnit(g, &n, src); ok {
			out = append(out, d)
		}
	}
	return out
}

// exportedUnits returns the units an `export_statement` contributes: the
// declaration it exports, the function-valued constants it declares, or the
// anonymous class or function it default-exports. Re-exports
// (`export … from "x"`) and value exports (`export default expr`)
// contribute nothing.
func exportedUnits(g *grammar, exp *ts.Node, src []byte) []unitDecl {
	if decl := exp.ChildByFieldId(g.fields.declaration); decl != nil {
		if g.kindOf(decl) == kindLexicalDeclaration {
			return functionConstUnits(g, decl, src)
		}
		if d, ok := declaredUnit(g, decl, src); ok {
			return []unitDecl{d}
		}
		return nil
	}
	if value := exp.ChildByFieldId(g.fields.value); value != nil {
		return defaultUnit(g, value)
	}
	return nil
}

// declaredUnit maps a declaration node onto a unit, reporting false for the
// declaration kinds that are not units.
func declaredUnit(g *grammar, n *ts.Node, src []byte) (unitDecl, bool) {
	var unit string
	switch g.kindOf(n) {
	case kindClassDeclaration, kindAbstractClassDeclaration:
		unit = unitClass
	case kindInterfaceDeclaration:
		unit = unitInterface
	case kindEnumDeclaration:
		unit = unitEnum
	case kindTypeAliasDeclaration:
		unit = unitType
	case kindFunctionDeclaration, kindGeneratorFunctionDeclaration:
		unit = unitFunction
	default:
		return unitDecl{}, false
	}
	line, col := treesitter.Position(n)
	return unitDecl{
		node: *n,
		name: treesitter.Text(n.ChildByFieldId(g.fields.name), src),
		kind: unit,
		line: line,
		col:  col,
	}, true
}

// functionConstUnits returns one unit per declarator of an exported
// `const`/`let` whose initializer is an arrow function or a function
// expression. A declarator holding anything else is a value, not a unit.
func functionConstUnits(g *grammar, lexical *ts.Node, src []byte) []unitDecl {
	var out []unitDecl
	for _, child := range treesitter.NamedChildren(lexical) {
		declarator := child
		if g.kindOf(&declarator) != kindVariableDeclarator {
			continue
		}
		value := declarator.ChildByFieldId(g.fields.value)
		if value == nil || !isFunctionValue(g, value) {
			continue
		}
		line, col := treesitter.Position(&declarator)
		out = append(out, unitDecl{
			node: declarator,
			body: value,
			name: treesitter.Text(declarator.ChildByFieldId(g.fields.name), src),
			kind: unitFunction,
			line: line,
			col:  col,
		})
	}
	return out
}

// defaultUnit returns the unit of an anonymous `export default class {}` or
// `export default function () {}`, named "default".
func defaultUnit(g *grammar, value *ts.Node) []unitDecl {
	var unit string
	switch g.kindOf(value) {
	case kindClass:
		unit = unitClass
	case kindFunctionExpression, kindGeneratorFunction, kindArrowFunction:
		unit = unitFunction
	default:
		return nil
	}
	line, col := treesitter.Position(value)
	return []unitDecl{{node: *value, body: value, name: defaultName, kind: unit, line: line, col: col}}
}

// isFunctionValue reports whether n is an initializer that turns its
// declarator into a unit.
func isFunctionValue(g *grammar, n *ts.Node) bool {
	switch g.kindOf(n) {
	case kindArrowFunction, kindFunctionExpression:
		return true
	default:
		return false
	}
}
