package java

import (
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"
	javabind "github.com/tree-sitter/tree-sitter-java/bindings/go"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// kind is this package's dense id for the tree-sitter node kinds the
// analyzer reacts to. Grammar symbol ids are resolved once, when the grammar
// is built, so the traversal compares integers instead of paying for a
// string per node across the cgo boundary.
type kind uint8

// kindOther is the zero value: every node the analyzer does not care about.
const kindOther kind = 0

const (
	kindProgram kind = iota + 1
	kindImportDeclaration
	kindScopedIdentifier
	kindAsterisk
	kindClassDeclaration
	kindInterfaceDeclaration
	kindEnumDeclaration
	kindRecordDeclaration
	kindAnnotationTypeDeclaration
	kindMethodDeclaration
	kindModifiers
	kindIfStatement
	kindSwitchBlockStatementGroup
	kindSwitchRule
	kindSwitchLabel
	kindTernaryExpression
	kindForStatement
	kindEnhancedForStatement
	kindWhileStatement
	kindDoStatement
	kindBinaryExpression
	kindUnaryExpression
	kindParenthesizedExpression
	kindTryStatement
	kindTryWithResourcesStatement
	kindResource
	kindCatchClause
	kindFinallyClause
	kindSuperclass
	kindSuperInterfaces
	kindExtendsInterfaces
	kindTypeList
	kindObjectCreationExpression
	kindClassBody
	kindFieldDeclaration
	kindConstantDeclaration
	kindLocalVariableDeclaration
	kindVariableDeclarator
	kindFormalParameter
	kindLambdaExpression
	kindMethodReference
	kindIdentifier
	kindTypeIdentifier
	kindLineComment
	kindBlockComment
	kindCount
)

// kindNames maps each kind to its exact node name in the
// tree-sitter/tree-sitter-java v0.23.5 grammar.
var kindNames = [kindCount]string{
	kindProgram:                   "program",
	kindImportDeclaration:         "import_declaration",
	kindScopedIdentifier:          "scoped_identifier",
	kindAsterisk:                  "asterisk",
	kindClassDeclaration:          "class_declaration",
	kindInterfaceDeclaration:      "interface_declaration",
	kindEnumDeclaration:           "enum_declaration",
	kindRecordDeclaration:         "record_declaration",
	kindAnnotationTypeDeclaration: "annotation_type_declaration",
	kindMethodDeclaration:         "method_declaration",
	kindModifiers:                 "modifiers",
	kindIfStatement:               "if_statement",
	kindSwitchBlockStatementGroup: "switch_block_statement_group",
	kindSwitchRule:                "switch_rule",
	kindSwitchLabel:               "switch_label",
	kindTernaryExpression:         "ternary_expression",
	kindForStatement:              "for_statement",
	kindEnhancedForStatement:      "enhanced_for_statement",
	kindWhileStatement:            "while_statement",
	kindDoStatement:               "do_statement",
	kindBinaryExpression:          "binary_expression",
	kindUnaryExpression:           "unary_expression",
	kindParenthesizedExpression:   "parenthesized_expression",
	kindTryStatement:              "try_statement",
	kindTryWithResourcesStatement: "try_with_resources_statement",
	kindResource:                  "resource",
	kindCatchClause:               "catch_clause",
	kindFinallyClause:             "finally_clause",
	kindSuperclass:                "superclass",
	kindSuperInterfaces:           "super_interfaces",
	kindExtendsInterfaces:         "extends_interfaces",
	kindTypeList:                  "type_list",
	kindObjectCreationExpression:  "object_creation_expression",
	kindClassBody:                 "class_body",
	kindFieldDeclaration:          "field_declaration",
	kindConstantDeclaration:       "constant_declaration",
	kindLocalVariableDeclaration:  "local_variable_declaration",
	kindVariableDeclarator:        "variable_declarator",
	kindFormalParameter:           "formal_parameter",
	kindLambdaExpression:          "lambda_expression",
	kindMethodReference:           "method_reference",
	kindIdentifier:                "identifier",
	kindTypeIdentifier:            "type_identifier",
	kindLineComment:               "line_comment",
	kindBlockComment:              "block_comment",
}

// Grammar field names the analyzer navigates by. This grammar names every
// position the rules need, so nothing is reached by child index.
const (
	fieldName        = "name"
	fieldAlternative = "alternative"
	fieldConsequence = "consequence"
	fieldCondition   = "condition"
	fieldOperator    = "operator"
	fieldLeft        = "left"
	fieldRight       = "right"
	fieldOperand     = "operand"
	fieldBody        = "body"
	fieldType        = "type"
	fieldParameters  = "parameters"
	fieldDeclarator  = "declarator"
	fieldValue       = "value"
	fieldScope       = "scope"
)

// Anonymous tokens the analyzer looks for among a node's direct children.
// Neither has a named node of its own, so only
// IdForNodeKind(name, false) resolves them.
const (
	tokenElse    = "else"
	tokenDefault = "default"
)

// fields holds the numeric field ids of the grammar.
type fields struct {
	name, alternative, consequence uint16
	condition, operator            uint16
	left, right, operand           uint16
	body, kindType, parameters     uint16
	declarator, value, scope       uint16
}

// tokens holds the symbol ids of the anonymous tokens, which have no entry
// in byID because that table only holds named kinds.
type tokens struct {
	elseKeyword, defaultKeyword uint16
}

// grammar is the Java parse table plus the symbol, field and token ids
// resolved from it.
type grammar struct {
	lang   *ts.Language
	byID   []kind
	fields fields
	tokens tokens
}

// newGrammar resolves every kind, field and token the analyzer uses against
// lang.
func newGrammar(lang *ts.Language) *grammar {
	g := &grammar{lang: lang, byID: make([]kind, lang.NodeKindCount()+1)}
	for k := kindOther + 1; k < kindCount; k++ {
		if id := lang.IdForNodeKind(kindNames[k], true); id != 0 && int(id) < len(g.byID) {
			g.byID[id] = k
		}
	}
	g.fields = newFields(lang)
	g.tokens = tokens{
		elseKeyword:    lang.IdForNodeKind(tokenElse, false),
		defaultKeyword: lang.IdForNodeKind(tokenDefault, false),
	}
	return g
}

// newFields resolves the field ids the rules navigate by.
func newFields(lang *ts.Language) fields {
	return fields{
		name:        lang.FieldIdForName(fieldName),
		alternative: lang.FieldIdForName(fieldAlternative),
		consequence: lang.FieldIdForName(fieldConsequence),
		condition:   lang.FieldIdForName(fieldCondition),
		operator:    lang.FieldIdForName(fieldOperator),
		left:        lang.FieldIdForName(fieldLeft),
		right:       lang.FieldIdForName(fieldRight),
		operand:     lang.FieldIdForName(fieldOperand),
		body:        lang.FieldIdForName(fieldBody),
		kindType:    lang.FieldIdForName(fieldType),
		parameters:  lang.FieldIdForName(fieldParameters),
		declarator:  lang.FieldIdForName(fieldDeclarator),
		value:       lang.FieldIdForName(fieldValue),
		scope:       lang.FieldIdForName(fieldScope),
	}
}

// kindOf returns the analyzer's kind for n, kindOther when the node is not
// one the analyzer reacts to.
func (g *grammar) kindOf(n *ts.Node) kind {
	id := int(n.KindId())
	if id < 0 || id >= len(g.byID) {
		return kindOther
	}
	return g.byID[id]
}

// childOfKind returns the first named child of n with the given kind, nil
// when there is none.
func (g *grammar) childOfKind(n *ts.Node, k kind) *ts.Node {
	for _, child := range treesitter.NamedChildren(n) {
		candidate := child
		if g.kindOf(&candidate) == k {
			return &candidate
		}
	}
	return nil
}

// nodeOr returns n when the grammar gave one and fallback otherwise, so a
// charge always has a range to point at, even on a shape the grammar spells
// without the field the rule reads.
func nodeOr(n, fallback *ts.Node) *ts.Node {
	if n == nil {
		return fallback
	}
	return n
}

// hasToken reports whether one of n's direct children is the anonymous
// token with the given symbol id.
func hasToken(n *ts.Node, token uint16) bool {
	return findToken(n, token) != nil
}

// findToken returns the first direct child of n that is the anonymous token
// with the given symbol id, nil when there is none.
func findToken(n *ts.Node, token uint16) *ts.Node {
	for i := uint(0); i < n.ChildCount(); i++ {
		if child := n.Child(i); child != nil && !child.IsNamed() && child.KindId() == token {
			return child
		}
	}
	return nil
}

// sharedGrammar builds the parse table once for process-wide sharing. The
// table is immutable after construction and lives in the C library.
var sharedGrammar = sync.OnceValue(func() *grammar {
	return newGrammar(ts.NewLanguage(javabind.Language()))
})
