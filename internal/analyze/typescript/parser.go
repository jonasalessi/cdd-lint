package typescript

import (
	"fmt"
	"path"
	"strings"
	"sync"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsbind "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Source extensions. The plain grammar and the TSX grammar are mutually
// exclusive: JSX syntax does not parse with the plain grammar, and a legacy
// `<Type>expr` cast does not parse with the TSX one, so the extension picks
// the grammar and there is no fallback.
const (
	extTS  = ".ts"
	extTSX = ".tsx"
	extMTS = ".mts"
	extCTS = ".cts"
)

var typeScriptExtensions = [...]string{extTS, extTSX, extMTS, extCTS}

// kind is this package's dense id for the tree-sitter node kinds the
// analyzer reacts to. Grammar symbol ids are resolved once, when the
// analyzer is built, so the traversal compares integers instead of paying
// for a string per node across the cgo boundary.
type kind uint8

// kindOther is the zero value: every node the analyzer does not care about.
const kindOther kind = 0

const (
	kindProgram kind = iota + 1
	kindExportStatement
	kindClassDeclaration
	kindAbstractClassDeclaration
	kindClass
	kindInterfaceDeclaration
	kindEnumDeclaration
	kindTypeAliasDeclaration
	kindFunctionDeclaration
	kindGeneratorFunctionDeclaration
	kindGeneratorFunction
	kindFunctionExpression
	kindArrowFunction
	kindLexicalDeclaration
	kindVariableDeclarator
	kindIfStatement
	kindElseClause
	kindSwitchCase
	kindTernaryExpression
	kindForStatement
	kindForInStatement
	kindWhileStatement
	kindDoStatement
	kindBinaryExpression
	kindAugmentedAssignmentExpression
	kindUnaryExpression
	kindParenthesizedExpression
	kindTryStatement
	kindCatchClause
	kindFinallyClause
	kindExtendsClause
	kindImplementsClause
	kindExtendsTypeClause
	kindPublicFieldDefinition
	kindImportStatement
	kindImportClause
	kindImportRequireClause
	kindNamedImports
	kindImportSpecifier
	kindNamespaceImport
	kindIdentifier
	kindTypeIdentifier
	kindShorthandPropertyIdentifier
	kindCount
)

// kindNames maps each kind to its exact node name in the
// tree-sitter-typescript v0.23.2 grammar; both grammars share the names.
var kindNames = [kindCount]string{
	kindProgram:                       "program",
	kindExportStatement:               "export_statement",
	kindClassDeclaration:              "class_declaration",
	kindAbstractClassDeclaration:      "abstract_class_declaration",
	kindClass:                         "class",
	kindInterfaceDeclaration:          "interface_declaration",
	kindEnumDeclaration:               "enum_declaration",
	kindTypeAliasDeclaration:          "type_alias_declaration",
	kindFunctionDeclaration:           "function_declaration",
	kindGeneratorFunctionDeclaration:  "generator_function_declaration",
	kindGeneratorFunction:             "generator_function",
	kindFunctionExpression:            "function_expression",
	kindArrowFunction:                 "arrow_function",
	kindLexicalDeclaration:            "lexical_declaration",
	kindVariableDeclarator:            "variable_declarator",
	kindIfStatement:                   "if_statement",
	kindElseClause:                    "else_clause",
	kindSwitchCase:                    "switch_case",
	kindTernaryExpression:             "ternary_expression",
	kindForStatement:                  "for_statement",
	kindForInStatement:                "for_in_statement",
	kindWhileStatement:                "while_statement",
	kindDoStatement:                   "do_statement",
	kindBinaryExpression:              "binary_expression",
	kindAugmentedAssignmentExpression: "augmented_assignment_expression",
	kindUnaryExpression:               "unary_expression",
	kindParenthesizedExpression:       "parenthesized_expression",
	kindTryStatement:                  "try_statement",
	kindCatchClause:                   "catch_clause",
	kindFinallyClause:                 "finally_clause",
	kindExtendsClause:                 "extends_clause",
	kindImplementsClause:              "implements_clause",
	kindExtendsTypeClause:             "extends_type_clause",
	kindPublicFieldDefinition:         "public_field_definition",
	kindImportStatement:               "import_statement",
	kindImportClause:                  "import_clause",
	kindImportRequireClause:           "import_require_clause",
	kindNamedImports:                  "named_imports",
	kindImportSpecifier:               "import_specifier",
	kindNamespaceImport:               "namespace_import",
	kindIdentifier:                    "identifier",
	kindTypeIdentifier:                "type_identifier",
	kindShorthandPropertyIdentifier:   "shorthand_property_identifier",
}

// Grammar field names the analyzer navigates by.
const (
	fieldOperator    = "operator"
	fieldLeft        = "left"
	fieldRight       = "right"
	fieldArgument    = "argument"
	fieldName        = "name"
	fieldAlias       = "alias"
	fieldValue       = "value"
	fieldDeclaration = "declaration"
	fieldSource      = "source"
	fieldType        = "type"
	// fieldBody holds the statement_block a `try` guards, which is where an
	// exception-handling occurrence points: the try_statement's own range
	// would swallow the catch and finally clauses charged beside it.
	fieldBody = "body"
	// fieldKind holds the `let`, `const` or `var` token of a `for…in` or
	// `for…of` loop. The token is anonymous, so it never shows up in an
	// s-expression, but the field is there and reaches it.
	fieldKind = "kind"
)

// fields holds the numeric field ids of one grammar.
type fields struct {
	operator, left, right, argument uint16
	name, alias, value              uint16
	declaration, source, kind       uint16
	body                            uint16
}

// grammar is one parse table plus the symbol and field ids resolved from it.
type grammar struct {
	lang   *ts.Language
	byID   []kind
	fields fields
}

// newGrammar resolves every kind and field the analyzer uses against lang.
func newGrammar(lang *ts.Language) *grammar {
	g := &grammar{lang: lang, byID: make([]kind, lang.NodeKindCount()+1)}
	for k := kindOther + 1; k < kindCount; k++ {
		if id := lang.IdForNodeKind(kindNames[k], true); id != 0 && int(id) < len(g.byID) {
			g.byID[id] = k
		}
	}
	g.fields = fields{
		operator:    lang.FieldIdForName(fieldOperator),
		left:        lang.FieldIdForName(fieldLeft),
		right:       lang.FieldIdForName(fieldRight),
		argument:    lang.FieldIdForName(fieldArgument),
		name:        lang.FieldIdForName(fieldName),
		alias:       lang.FieldIdForName(fieldAlias),
		value:       lang.FieldIdForName(fieldValue),
		declaration: lang.FieldIdForName(fieldDeclaration),
		source:      lang.FieldIdForName(fieldSource),
		kind:        lang.FieldIdForName(fieldKind),
		body:        lang.FieldIdForName(fieldBody),
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

// grammars holds the two parse tables an analyzer switches between.
type grammars struct {
	plain *grammar
	tsx   *grammar
}

// newGrammars builds both parse tables for process-wide sharing. The tables
// are immutable after construction and live in the C library.
func newGrammars() *grammars {
	return &grammars{
		plain: newGrammar(ts.NewLanguage(tsbind.LanguageTypescript())),
		tsx:   newGrammar(ts.NewLanguage(tsbind.LanguageTSX())),
	}
}

var sharedGrammars = sync.OnceValue(newGrammars)

// forPath returns the grammar that parses p, chosen by extension: .ts, .mts
// and .cts use the plain grammar, .tsx uses the TSX one. Any other
// extension is an error, never a silent guess.
func (gs *grammars) forPath(p string) (*grammar, error) {
	switch strings.ToLower(path.Ext(p)) {
	case extTS, extMTS, extCTS:
		return gs.plain, nil
	case extTSX:
		return gs.tsx, nil
	default:
		return nil, fmt.Errorf("%s: unsupported TypeScript extension %q", p, path.Ext(p))
	}
}
