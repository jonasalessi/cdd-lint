package java

import (
	"context"
	"io"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// TestGrammarResolvesEveryKind (TC-P1) makes sure every node kind the
// analyzer looks for exists in the grammar, so a grammar bump that renames
// one fails here rather than silently counting nothing.
func TestGrammarResolvesEveryKind(t *testing.T) {
	g := sharedGrammar()
	for k := kindOther + 1; k < kindCount; k++ {
		name := kindNames[k]
		require.NotEmpty(t, name, "kind %d has no grammar name", k)
		id := g.lang.IdForNodeKind(name, true)
		require.NotZero(t, id, "node kind %q is unknown to the grammar", name)
		require.Equal(t, k, g.byID[id])
	}
}

// TestFieldsResolve (TC-P2) pins the fields the analyzer navigates by. Unlike
// Kotlin, this grammar names every position the rules need, so a field that
// disappears must fail here instead of turning into a silent zero.
func TestFieldsResolve(t *testing.T) {
	g := sharedGrammar()
	for _, name := range []string{
		fieldName, fieldAlternative, fieldConsequence, fieldCondition, fieldOperator,
		fieldLeft, fieldRight, fieldOperand, fieldBody, fieldType,
		fieldParameters, fieldDeclarator, fieldValue, fieldScope,
	} {
		require.NotZero(t, g.lang.FieldIdForName(name), "unknown field %q", name)
	}
	require.Equal(t, newFields(g.lang), g.fields)
}

// TestTokensResolve (TC-P3): the anonymous tokens resolve through the
// unnamed lookup only.
func TestTokensResolve(t *testing.T) {
	g := sharedGrammar()
	for _, name := range []string{tokenElse, tokenDefault} {
		require.NotZero(t, g.lang.IdForNodeKind(name, false), "anonymous token %q is unknown", name)
		require.Zero(t, g.lang.IdForNodeKind(name, true), "%q must stay an anonymous token", name)
	}
	require.NotZero(t, g.tokens.elseKeyword)
	require.NotZero(t, g.tokens.defaultKeyword)
}

// TestTokensAreDirectChildren (TC-P3) pins where the tokens sit: `else` under
// the if statement that owns the alternative, `default` under a switch label.
func TestTokensAreDirectChildren(t *testing.T) {
	g := sharedGrammar()
	tree := parseWith(t, g, []byte("class A { void f(boolean a, int x) {\n"+
		"if (a) {} else {}\nswitch (x) { default: g(); }\n} }\n"))
	defer tree.Close()

	root := tree.RootNode()
	cursor := root.Walk()
	defer cursor.Close()
	var ifStatement, label ts.Node
	treesitter.Walk(cursor, root, func(n *ts.Node) bool {
		switch g.kindOf(n) {
		case kindIfStatement:
			ifStatement = *n
		case kindSwitchLabel:
			label = *n
		}
		return true
	})
	require.NotNil(t, findToken(&ifStatement, g.tokens.elseKeyword))
	require.True(t, hasToken(&label, g.tokens.defaultKeyword))
	require.False(t, hasToken(&ifStatement, g.tokens.defaultKeyword))
}

// TestKindOfOutOfRange (TC-P4) guards the symbol-id lookup against a node
// from another grammar.
func TestKindOfOutOfRange(t *testing.T) {
	g := newGrammar(sharedGrammar().lang)
	g.byID = g.byID[:1]
	tree := parseWith(t, g, []byte("class A {}\n"))
	defer tree.Close()
	require.Equal(t, kindOther, g.kindOf(tree.RootNode()))
}

// parseWith parses src with one grammar, for the tests that need a tree
// without going through the analyzer.
func parseWith(t *testing.T, g *grammar, src []byte) *ts.Tree {
	t.Helper()
	p := ts.NewParser()
	t.Cleanup(p.Close)
	require.NoError(t, p.SetLanguage(g.lang))
	tree := p.Parse(src, nil)
	require.NotNil(t, tree)
	return tree
}

// TestSyntaxError (TC-P5) pins FR-5: no units, one warning naming the first
// error position and no file path, which the pipeline already carries.
func TestSyntaxError(t *testing.T) {
	res := analyzeFixture(t, "broken.java")
	require.Empty(t, res.Units)
	require.Len(t, res.Warnings, 1)
	require.Regexp(t, regexp.MustCompile(`^`+treesitter.SyntaxError+` at \d+:\d+$`), res.Warnings[0])

	g := sharedGrammar()
	tree := parseWith(t, g, readFixture(t, "broken.java"))
	defer tree.Close()
	errNode := treesitter.FirstErrorNode(tree.RootNode())
	require.NotNil(t, errNode)
	line, col := treesitter.Position(errNode)
	require.Equal(t, treesitter.SyntaxError+" at 1:14", res.Warnings[0])
	require.Equal(t, 1, line)
	require.Equal(t, 14, col)
}

// TestSyntaxWarningFallback (TC-P6): no Java source is known that flags the
// root without producing an ERROR or MISSING node, so the 1:1 fallback is
// asserted through the helper the analyzer calls.
func TestSyntaxWarningFallback(t *testing.T) {
	g := sharedGrammar()
	tree := parseWith(t, g, []byte("class A {}\n"))
	defer tree.Close()
	root := tree.RootNode()
	require.False(t, root.HasError())
	require.Nil(t, treesitter.FirstErrorNode(root))
	require.Equal(t, treesitter.SyntaxError+" at 1:1", treesitter.SyntaxWarning(root))
}

// TestEmptyAndHeaderOnlyFiles (TC-P7): nothing to measure is not a warning.
func TestEmptyAndHeaderOnlyFiles(t *testing.T) {
	for _, name := range []string{"empty.java", "header_only.java"} {
		res := analyzeFixture(t, name)
		require.Empty(t, res.Units, name)
		require.Empty(t, res.Warnings, name)
	}
}

// TestModuleInfo (TC-P15): a module declaration parses, holds no unit and
// charges no coupling.
func TestModuleInfo(t *testing.T) {
	res := analyzeSource(t, "module com.acme {\n    requires java.base;\n}\n", "com.acme")
	require.Empty(t, res.Units)
}

// TestUnsupportedExtension (TC-P8) refuses to guess: a Kotlin file or a
// path without an extension is an error naming the path.
func TestUnsupportedExtension(t *testing.T) {
	a := newTestAnalyzer(t)
	for _, p := range []string{"src/Main.kt", "a.jav", "noext"} {
		res, err := a.Analyze(context.Background(), p, []byte("class A {}\n"))
		require.ErrorContains(t, err, p)
		require.Empty(t, res.Units)
	}
	_, err := a.Analyze(context.Background(), "a.kt", []byte("class A {}\n"))
	require.ErrorContains(t, err, `".kt"`)
}

// TestUppercaseExtension (TC-P9): the extension test is case-insensitive.
func TestUppercaseExtension(t *testing.T) {
	a := newTestAnalyzer(t)
	for _, p := range []string{"A.JAVA", "A.Java"} {
		res, err := a.Analyze(context.Background(), p, []byte("class A {}\n"))
		require.NoError(t, err, p)
		require.Empty(t, res.Warnings, p)
	}
}

// TestReanalyzeDoesNotLeak (TC-P10) analyzes the same file many times
// through one analyzer. The binding installs no finalizers, so a parser or
// tree that is never closed leaks on the C heap; running the loop under
// -race and watching it stay correct is the cheap guard against that.
func TestReanalyzeDoesNotLeak(t *testing.T) {
	a := newTestAnalyzer(t)
	src := readFixture(t, "header_only.java")
	for range 200 {
		res, err := a.Analyze(context.Background(), "header_only.java", src)
		require.NoError(t, err)
		require.Empty(t, res.Warnings)
	}
}

// TestCloseIsIdempotent (TC-P11) covers the pipeline closing an analyzer
// twice, and using one after it was closed.
func TestCloseIsIdempotent(t *testing.T) {
	a := NewAnalyzer(analyze.Options{})
	closer, ok := a.(io.Closer)
	require.True(t, ok)
	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close())
	_, err := a.Analyze(context.Background(), "A.java", []byte("class A {}\n"))
	require.ErrorContains(t, err, "closed")
}

// TestCanceledContext (TC-P12) stops before parsing instead of running to
// completion.
func TestCanceledContext(t *testing.T) {
	a := newTestAnalyzer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := a.Analyze(ctx, "header_only.java", readFixture(t, "header_only.java"))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, res.Units)
}

// TestAnalyzersShareOnlyImmutableGrammarMetadata (TC-P13): two analyzers
// share the parse table and nothing else.
func TestAnalyzersShareOnlyImmutableGrammarMetadata(t *testing.T) {
	first, ok := NewAnalyzer(analyze.Options{}).(*analyzer)
	require.True(t, ok)
	second, ok := NewAnalyzer(analyze.Options{}).(*analyzer)
	require.True(t, ok)
	t.Cleanup(func() {
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())
	})
	require.Same(t, first.grammar, second.grammar)
	require.NotSame(t, first.parser, second.parser)
	for _, a := range []*analyzer{first, second} {
		res, err := a.Analyze(context.Background(), "A.java", []byte("class A {}\n"))
		require.NoError(t, err)
		require.Empty(t, res.Warnings)
	}
}

// TestBOMAndCRLF (TC-P14): a file written by Windows tooling parses. The
// bytes are built here rather than kept as a fixture so that git's
// line-ending normalization cannot quietly undo the CRLFs.
func TestBOMAndCRLF(t *testing.T) {
	src := []byte("\xEF\xBB\xBFpackage a.b;\r\n\r\nclass Bom {\r\n    void f(int x) {\r\n" +
		"        if (x > 0) {\r\n        }\r\n    }\r\n}\r\n")
	a := newTestAnalyzer(t)
	res, err := a.Analyze(context.Background(), "Bom.java", src)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
}
