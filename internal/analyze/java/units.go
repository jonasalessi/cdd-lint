package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// The Kind values a Java unit can carry. Unlike Kotlin, this grammar has one
// node kind per declaration keyword, so the kind is read from the node and
// never from a modifier.
const (
	unitClass      = "class"
	unitInterface  = "interface"
	unitEnum       = "enum"
	unitRecord     = "record"
	unitAnnotation = "annotation"
	unitMethod     = "method"
)

// unitDecl is one extracted unit: the node whose subtree the metrics are
// counted over, plus the metadata the pipeline reports. The node is only
// valid while the tree that produced it is open.
type unitDecl struct {
	node ts.Node
	name string
	kind string
	line int
	col  int
}

// units returns the file's units in source order (FR-3).
//
// A unit is a top-level type declaration: a direct child of program that is
// a class, interface, enum, record or annotation type, plus the top-level
// method of a compact source file, which has no type to bill to. There is
// no visibility filter: a package-private class still carries complexity
// someone maintains. Nothing nested inside a unit is a unit; its complexity
// is charged to the unit that contains it. The package declaration, the
// imports, a module declaration and the top-level statements of a compact
// source file are not units, so their complexity is invisible.
//
// Line and Col point at the first token after the modifiers and
// annotations: `class` for `public class A`, `@interface` for an annotation
// type, the return type for a compact file's method.
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

// declaredUnit maps a top-level declaration onto a unit, reporting false for
// the nodes that are not units.
func declaredUnit(g *grammar, n *ts.Node, src []byte) (unitDecl, bool) {
	unit, ok := unitKind(g.kindOf(n))
	if !ok {
		return unitDecl{}, false
	}
	line, col := treesitter.Position(declarationStart(g, n))
	return unitDecl{
		node: *n,
		name: treesitter.Text(n.ChildByFieldId(g.fields.name), src),
		kind: unit,
		line: line,
		col:  col,
	}, true
}

// unitKind returns the reported kind of a declaration node, false when that
// kind of node is not a unit.
func unitKind(k kind) (string, bool) {
	switch k {
	case kindClassDeclaration:
		return unitClass, true
	case kindInterfaceDeclaration:
		return unitInterface, true
	case kindEnumDeclaration:
		return unitEnum, true
	case kindRecordDeclaration:
		return unitRecord, true
	case kindAnnotationTypeDeclaration:
		return unitAnnotation, true
	case kindMethodDeclaration:
		return unitMethod, true
	default:
		return "", false
	}
}

// declarationStart returns the first child of n that is not its modifiers,
// which is the keyword the declaration is reported at. Annotations live
// inside the modifiers node, so `@Deprecated class H` is reported at
// `class` too. A declaration with no such child, which the grammar never
// produces, is reported at itself.
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
