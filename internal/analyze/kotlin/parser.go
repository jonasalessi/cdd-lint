package kotlin

import (
	"sync"

	ktbind "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"

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
	kindSourceFile kind = iota + 1
	kindImport
	kindQualifiedIdentifier
	kindClassDeclaration
	kindObjectDeclaration
	kindFunctionDeclaration
	kindTypeAlias
	kindPropertyDeclaration
	kindModifiers
	kindClassModifier
	kindVariableDeclaration
	kindMultiVariableDeclaration
	kindPropertyDelegate
	kindGetter
	kindSetter
	kindFunctionBody
	kindClassBody
	kindClassParameter
	kindDelegationSpecifier
	kindUserType
	kindLambdaLiteral
	kindAnonymousFunction
	kindCallableReference
	kindIfExpression
	kindWhenEntry
	kindForStatement
	kindWhileStatement
	kindDoWhileStatement
	kindNavigationExpression
	kindBinaryExpression
	kindUnaryExpression
	kindParenthesizedExpression
	kindTryExpression
	kindCatchBlock
	kindFinallyBlock
	kindBlock
	kindIdentifier
	kindLineComment
	kindBlockComment
	kindCount
)

// kindNames maps each kind to its exact node name in the
// tree-sitter-grammars/tree-sitter-kotlin v1.1.0 grammar.
var kindNames = [kindCount]string{
	kindSourceFile:               "source_file",
	kindImport:                   "import",
	kindQualifiedIdentifier:      "qualified_identifier",
	kindClassDeclaration:         "class_declaration",
	kindObjectDeclaration:        "object_declaration",
	kindFunctionDeclaration:      "function_declaration",
	kindTypeAlias:                "type_alias",
	kindPropertyDeclaration:      "property_declaration",
	kindModifiers:                "modifiers",
	kindClassModifier:            "class_modifier",
	kindVariableDeclaration:      "variable_declaration",
	kindMultiVariableDeclaration: "multi_variable_declaration",
	kindPropertyDelegate:         "property_delegate",
	kindGetter:                   "getter",
	kindSetter:                   "setter",
	kindFunctionBody:             "function_body",
	kindClassBody:                "class_body",
	kindClassParameter:           "class_parameter",
	kindDelegationSpecifier:      "delegation_specifier",
	kindUserType:                 "user_type",
	kindLambdaLiteral:            "lambda_literal",
	kindAnonymousFunction:        "anonymous_function",
	kindCallableReference:        "callable_reference",
	kindIfExpression:             "if_expression",
	kindWhenEntry:                "when_entry",
	kindForStatement:             "for_statement",
	kindWhileStatement:           "while_statement",
	kindDoWhileStatement:         "do_while_statement",
	kindNavigationExpression:     "navigation_expression",
	kindBinaryExpression:         "binary_expression",
	kindUnaryExpression:          "unary_expression",
	kindParenthesizedExpression:  "parenthesized_expression",
	kindTryExpression:            "try_expression",
	kindCatchBlock:               "catch_block",
	kindFinallyBlock:             "finally_block",
	kindBlock:                    "block",
	kindIdentifier:               "identifier",
	kindLineComment:              "line_comment",
	kindBlockComment:             "block_comment",
}

// Grammar field names the analyzer navigates by. The grammar has no
// `alternative`, `consequence`, `body`, `receiver` or `delegate` field, so
// the branches of an `if`, the block of a `try` and the parent of a
// specifier are all reached by child position instead.
const (
	fieldCondition = "condition"
	fieldName      = "name"
	fieldType      = "type"
	fieldOperator  = "operator"
	fieldLeft      = "left"
	fieldRight     = "right"
	fieldArgument  = "argument"
)

// Anonymous tokens the analyzer looks for among a node's direct children.
// None of them has a named node of its own, so only
// IdForNodeKind(name, false) resolves them.
const (
	tokenSafeCall  = "?."
	tokenStar      = "*"
	tokenElse      = "else"
	tokenVal       = "val"
	tokenVar       = "var"
	tokenInterface = "interface"
	tokenEnum      = "enum"
	tokenAssign    = "="
)

// fields holds the numeric field ids of the grammar.
type fields struct {
	condition, name, kindType uint16
	operator, left, right     uint16
	argument                  uint16
}

// tokens holds the symbol ids of the anonymous tokens, which have no entry
// in byID because that table only holds named kinds.
type tokens struct {
	safeCall, star, elseKeyword uint16
	val, variable               uint16
	interfaceKeyword, enum      uint16
	assign                      uint16
}

// grammar is the Kotlin parse table plus the symbol, field and token ids
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
	g.fields = fields{
		condition: lang.FieldIdForName(fieldCondition),
		name:      lang.FieldIdForName(fieldName),
		kindType:  lang.FieldIdForName(fieldType),
		operator:  lang.FieldIdForName(fieldOperator),
		left:      lang.FieldIdForName(fieldLeft),
		right:     lang.FieldIdForName(fieldRight),
		argument:  lang.FieldIdForName(fieldArgument),
	}
	g.tokens = tokens{
		safeCall:         lang.IdForNodeKind(tokenSafeCall, false),
		star:             lang.IdForNodeKind(tokenStar, false),
		elseKeyword:      lang.IdForNodeKind(tokenElse, false),
		val:              lang.IdForNodeKind(tokenVal, false),
		variable:         lang.IdForNodeKind(tokenVar, false),
		interfaceKeyword: lang.IdForNodeKind(tokenInterface, false),
		enum:             lang.IdForNodeKind(tokenEnum, false),
		assign:           lang.IdForNodeKind(tokenAssign, false),
	}
	return g
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
// without the node the rule reads.
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
	return newGrammar(ts.NewLanguage(ktbind.Language()))
})
