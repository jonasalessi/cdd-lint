package kotlin

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

// TestFieldsResolve (TC-P2) pins the fields the analyzer navigates by, and
// the ones it must not rely on because the grammar lacks them.
func TestFieldsResolve(t *testing.T) {
	g := sharedGrammar()
	for _, name := range []string{
		fieldCondition, fieldName, fieldType, fieldOperator, fieldLeft, fieldRight, fieldArgument,
	} {
		require.NotZero(t, g.lang.FieldIdForName(name), "unknown field %q", name)
	}
	for _, name := range []string{"alternative", "consequence", "body", "receiver", "delegate"} {
		require.Zero(t, g.lang.FieldIdForName(name),
			"the grammar gained field %q; the analyzer reads that position by index today", name)
	}
	require.Equal(t, fields{
		condition: g.lang.FieldIdForName(fieldCondition),
		name:      g.lang.FieldIdForName(fieldName),
		kindType:  g.lang.FieldIdForName(fieldType),
		operator:  g.lang.FieldIdForName(fieldOperator),
		left:      g.lang.FieldIdForName(fieldLeft),
		right:     g.lang.FieldIdForName(fieldRight),
		argument:  g.lang.FieldIdForName(fieldArgument),
	}, g.fields)
}

// TestTokensResolve (TC-P3): the anonymous tokens resolve through the
// unnamed lookup only.
func TestTokensResolve(t *testing.T) {
	g := sharedGrammar()
	for _, name := range []string{
		tokenSafeCall, tokenStar, tokenElse, tokenVal, tokenVar, tokenInterface, tokenEnum, tokenAssign,
	} {
		require.NotZero(t, g.lang.IdForNodeKind(name, false), "anonymous token %q is unknown", name)
		require.Zero(t, g.lang.IdForNodeKind(name, true), "%q must stay an anonymous token", name)
	}
	require.NotZero(t, g.tokens.safeCall)
	require.NotZero(t, g.tokens.star)
	require.NotZero(t, g.tokens.elseKeyword)
	require.NotZero(t, g.tokens.val)
	require.NotZero(t, g.tokens.variable)
	require.NotZero(t, g.tokens.interfaceKeyword)
	require.NotZero(t, g.tokens.enum)
	require.NotZero(t, g.tokens.assign)
}

// TestKindOfOutOfRange (TC-P4) guards the symbol-id lookup against a node
// from another grammar.
func TestKindOfOutOfRange(t *testing.T) {
	g := newGrammar(sharedGrammar().lang)
	g.byID = g.byID[:1]
	tree := parseWith(t, g, []byte("class A\n"))
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

// TestSyntaxError (TC-P5) pins FR-6: no units, one warning naming the first
// error position and no file path, which the pipeline already carries.
func TestSyntaxError(t *testing.T) {
	res := analyzeFixture(t, "broken.kt")
	require.Empty(t, res.Units)
	require.Len(t, res.Warnings, 1)
	require.Regexp(t, regexp.MustCompile(`^`+treesitter.SyntaxError+` at \d+:\d+$`), res.Warnings[0])

	g := sharedGrammar()
	tree := parseWith(t, g, readFixture(t, "broken.kt"))
	defer tree.Close()
	errNode := treesitter.FirstErrorNode(tree.RootNode())
	require.NotNil(t, errNode)
	line, col := treesitter.Position(errNode)
	require.Equal(t, treesitter.SyntaxError+" at 1:12", res.Warnings[0])
	require.Equal(t, 1, line)
	require.Equal(t, 12, col)
}

// TestSyntaxErrorWithoutAnErrorNode (TC-P6): the grammar flags a member on
// the brace line followed by a bodiless function before a same-line brace
// without producing an ERROR or MISSING node, so the warning falls back to
// 1:1. Re-check this source on a grammar bump.
func TestSyntaxErrorWithoutAnErrorNode(t *testing.T) {
	src := "interface I { val x: Int\n fun g() }\n"
	g := sharedGrammar()
	tree := parseWith(t, g, []byte(src))
	defer tree.Close()
	require.True(t, tree.RootNode().HasError())
	require.Nil(t, treesitter.FirstErrorNode(tree.RootNode()),
		"the grammar now produces an error node for this layout; move the case to TestSyntaxError")

	a := newTestAnalyzer(t)
	res, err := a.Analyze(context.Background(), "i.kt", []byte(src))
	require.NoError(t, err)
	require.Empty(t, res.Units)
	require.Equal(t, []string{treesitter.SyntaxError + " at 1:1"}, res.Warnings)
}

// TestUnsupportedExtension (TC-P7) refuses to guess: a script or a Java file
// is an error naming the path and the extension.
func TestUnsupportedExtension(t *testing.T) {
	a := newTestAnalyzer(t)
	for _, p := range []string{"build.gradle.kts", "src/Main.java", "noext"} {
		res, err := a.Analyze(context.Background(), p, []byte("class A\n"))
		require.ErrorContains(t, err, p)
		require.Empty(t, res.Units)
	}
	_, err := a.Analyze(context.Background(), "a.kts", []byte("class A\n"))
	require.ErrorContains(t, err, `".kts"`)
}

// TestReanalyzeDoesNotLeak (TC-P8) analyzes the same file many times
// through one analyzer. The binding installs no finalizers, so a parser,
// tree or cursor that is never closed leaks on the C heap; running the loop
// under -race and watching it stay correct is the cheap guard against that.
func TestReanalyzeDoesNotLeak(t *testing.T) {
	a := newTestAnalyzer(t)
	src := readFixture(t, "cdd_examples.kt")
	for range 200 {
		res, err := a.Analyze(context.Background(), "cdd_examples.kt", src)
		require.NoError(t, err)
		require.Empty(t, res.Warnings)
	}
}

// TestCloseIsIdempotent (TC-P9) covers the pipeline closing an analyzer
// twice, and using one after it was closed.
func TestCloseIsIdempotent(t *testing.T) {
	a := NewAnalyzer(analyze.Options{})
	closer, ok := a.(io.Closer)
	require.True(t, ok)
	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close())
	_, err := a.Analyze(context.Background(), "a.kt", []byte("class A\n"))
	require.ErrorContains(t, err, "closed")
}

// TestCanceledContext (TC-P10) stops before parsing instead of running to
// completion.
func TestCanceledContext(t *testing.T) {
	a := newTestAnalyzer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := a.Analyze(ctx, "units.kt", readFixture(t, "units.kt"))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, res.Units)
}

// TestAnalyzersShareOnlyImmutableGrammarMetadata (TC-P11): two analyzers
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
		res, err := a.Analyze(context.Background(), "unit.kt", []byte("class Unit\n"))
		require.NoError(t, err)
		require.Empty(t, res.Warnings)
	}
}

// TestEmptyAndHeaderOnlyFiles (TC-P12): nothing to measure is not a
// warning.
func TestEmptyAndHeaderOnlyFiles(t *testing.T) {
	for _, name := range []string{"empty.kt", "header_only.kt"} {
		res := analyzeFixture(t, name)
		require.Empty(t, res.Units, name)
		require.Empty(t, res.Warnings, name)
	}
}

// TestBOMAndCRLF (TC-P13): a file written by Windows tooling parses, and
// its lines are counted the same way an editor counts them. The bytes are
// built here rather than kept as a fixture so that git's line-ending
// normalization cannot quietly undo the CRLFs.
func TestBOMAndCRLF(t *testing.T) {
	src := []byte(
		"\xEF\xBB\xBFpackage a.b\r\n\r\nclass Bom {\r\n    fun f(x: Int) {\r\n        if (x > 0) {\r\n        }\r\n    }\r\n}\r\n",
	)
	a := newTestAnalyzer(t)
	res, err := a.Analyze(context.Background(), "Bom.kt", src)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
}
