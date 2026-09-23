package kotlin

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// acmePrefix is the configured internal prefix of the coupling fixtures.
const acmePrefix = "com.acme"

// fixtureNames returns every Kotlin fixture under testdata, named the way
// analyzeFixture wants them, so a fixture added later is covered by the
// invariants without anyone remembering to list it.
func fixtureNames(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("testdata", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.EqualFold(filepath.Ext(p), extKotlin) {
			rel, relErr := filepath.Rel("testdata", p)
			if relErr != nil {
				return relErr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	return out
}

// occurrenceAt is one expected occurrence, spelled the way an editor would
// highlight it: 1-based start, end just past the construct.
type occurrenceAt struct {
	metric                     config.MetricID
	line, col, endLine, endCol int
	count                      int
}

// occurrence builds the analyze.Occurrence the expectation describes.
func (o occurrenceAt) occurrence() analyze.Occurrence {
	return analyze.Occurrence{
		Metric:  o.metric,
		Line:    o.line,
		Col:     o.col,
		EndLine: o.endLine,
		EndCol:  o.endCol,
		Count:   o.count,
	}
}

// occurrences builds the analyze.Occurrence list the expectations describe.
func occurrences(want []occurrenceAt) []analyze.Occurrence {
	out := make([]analyze.Occurrence, 0, len(want))
	for _, w := range want {
		out = append(out, w.occurrence())
	}
	return out
}

// TestBranchOccurrences pins where the branch and condition charges point,
// inline so the ranges do not move with a fixture comment (TC-B3, TC-B4,
// TC-B7, TC-B14).
func TestBranchOccurrences(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []occurrenceAt
	}{
		{
			name: "a when charges each conditioned entry and not the else",
			src:  "fun d(x: Int) = when (x) {\n    1 -> \"one\"\n    2, 3 -> \"few\"\n    else -> \"many\"\n}\n",
			want: []occurrenceAt{
				{config.MetricCodeBranch, 2, 5, 2, 15, 1},
				{config.MetricCodeBranch, 3, 5, 3, 18, 1},
			},
		},
		{
			name: "an if / else if / else chain charges three ifs and the final else",
			src:  "fun c(a: Boolean, b: Boolean, c: Boolean): Int =\n    if (a) 1 else if (b) 2 else if (c) 3 else 4\n",
			want: []occurrenceAt{
				{config.MetricCodeBranch, 2, 5, 2, 48, 1},
				{config.MetricCodeBranch, 2, 19, 2, 48, 1},
				{config.MetricCodeBranch, 2, 33, 2, 48, 1},
				{config.MetricCodeBranch, 2, 42, 2, 48, 1},
			},
		},
		{
			name: "the if sorts before the clauses of its own condition",
			src:  "fun g(a: Boolean, b: Boolean) = if (a && b) x else y\n",
			want: []occurrenceAt{
				{config.MetricCodeBranch, 1, 33, 1, 53, 1},
				{config.MetricCondition, 1, 37, 1, 38, 1},
				{config.MetricCondition, 1, 42, 1, 43, 1},
				{config.MetricCodeBranch, 1, 47, 1, 53, 1},
			},
		},
		{
			name: "a safe call charges its token and elvis its clauses",
			src:  "fun s(v: String?): Int = v?.trim()?.length ?: 0\n",
			want: []occurrenceAt{
				{config.MetricCondition, 1, 26, 1, 43, 1},
				{config.MetricCodeBranch, 1, 27, 1, 29, 1},
				{config.MetricCodeBranch, 1, 35, 1, 37, 1},
				{config.MetricCondition, 1, 47, 1, 48, 1},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeSource(t, c.src)
			require.Len(t, res.Units, 1)
			require.Equal(t, occurrences(c.want), res.Units[0].Occurrences)
		})
	}
}

// TestOccurrencesAccountForEveryCount (TC-X1) is the invariant the
// occurrence list exists to keep: for every unit of every fixture, and for
// every metric, the occurrences add up to exactly the raw count.
func TestOccurrencesAccountForEveryCount(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			res := analyzeFixture(t, name, acmePrefix)
			for _, u := range res.Units {
				summed := map[config.MetricID]int{}
				for _, o := range u.Occurrences {
					summed[o.Metric] += o.Count
				}
				for _, m := range config.Metrics() {
					require.Equal(t, u.Counts[m], summed[m], "unit %q, metric %q", u.Name, m)
				}
				require.Len(t, u.Counts, len(config.Metrics())) // TC-X4
			}
		})
	}
}

// TestOccurrencesAreWellFormed (TC-X2, TC-X3) checks that every located
// construct carries a range an editor can use -- 1-based, non-empty, end
// after start, a known metric -- and that a unit's occurrences come out in
// source order.
func TestOccurrencesAreWellFormed(t *testing.T) {
	known := map[config.MetricID]bool{}
	for _, m := range config.Metrics() {
		known[m] = true
	}
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			res := analyzeFixture(t, name, acmePrefix)
			for _, u := range res.Units {
				for i, o := range u.Occurrences {
					where := u.Name + " occurrence " + string(o.Metric)
					require.True(t, known[o.Metric], where)
					require.Positive(t, o.Line, where)
					require.Positive(t, o.Col, where)
					require.Positive(t, o.Count, where)
					require.True(t, o.EndLine > o.Line || (o.EndLine == o.Line && o.EndCol > o.Col), where)
					if i == 0 {
						continue
					}
					prev := u.Occurrences[i-1]
					require.True(t, prev.Line < o.Line || (prev.Line == o.Line && prev.Col <= o.Col),
						"%s: %d:%d comes after %d:%d", where, o.Line, o.Col, prev.Line, prev.Col)
				}
			}
		})
	}
}

// TestDeterminism (TC-X5): two analyzers produce the same result for every
// fixture.
func TestDeterminism(t *testing.T) {
	for _, name := range fixtureNames(t) {
		first := analyzeFixture(t, name, acmePrefix)
		second := analyzeFixture(t, name, acmePrefix)
		require.True(t, reflect.DeepEqual(first, second), name)
	}
}

// TestConcurrentAnalyzers (TC-X6): one analyzer per goroutine, all reading
// the shared grammar at once, under -race.
func TestConcurrentAnalyzers(t *testing.T) {
	names := fixtureNames(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			a := NewAnalyzer(analyze.Options{InternalPrefixes: []string{acmePrefix}})
			defer func() {
				closer, ok := a.(io.Closer)
				assert.True(t, ok)
				assert.NoError(t, closer.Close())
			}()
			for _, name := range names {
				src, err := os.ReadFile(filepath.Join("testdata", name))
				assert.NoError(t, err)
				_, err = a.Analyze(context.Background(), name, src)
				assert.NoError(t, err, "goroutine %d", i)
			}
		})
	}
	wg.Wait()
}

// TestDeclarationOccurrences pins where the exception, inheritance and
// local-variable charges point (TC-E1, TC-I1, TC-L5).
func TestDeclarationOccurrences(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []occurrenceAt
	}{
		{
			name: "a try charges its block, then each catch and finally",
			src:  "fun g() {\n    try { a() } catch (e: E) { b() } finally { c() }\n}\n",
			want: []occurrenceAt{
				{config.MetricExceptionHandling, 2, 9, 2, 16, 1},
				{config.MetricExceptionHandling, 2, 17, 2, 37, 1},
				{config.MetricExceptionHandling, 2, 38, 2, 53, 1},
			},
		},
		{
			name: "each specifier charges its type, not the whole clause",
			src:  "class L(private val repo: Repo, clock: Clock) : Base(), Auditable, Printer by ConsolePrinter()\n",
			want: []occurrenceAt{
				{config.MetricLocalVariable, 1, 9, 1, 31, 1},
				{config.MetricInheritance, 1, 49, 1, 53, 1},
				{config.MetricInheritance, 1, 57, 1, 66, 1},
				{config.MetricInheritance, 1, 68, 1, 75, 1},
			},
		},
		{
			name: "a for binding is charged where it is bound",
			src:  "fun l(xs: List<Int>, m: Map<String, Int>) {\n    for (x in xs) { }\n    for ((k, v) in m) { }\n}\n",
			want: []occurrenceAt{
				{config.MetricCodeBranch, 2, 5, 2, 22, 1},
				{config.MetricLocalVariable, 2, 10, 2, 11, 1},
				{config.MetricCodeBranch, 3, 5, 3, 26, 1},
				{config.MetricLocalVariable, 3, 10, 3, 16, 1},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeSource(t, c.src)
			require.Len(t, res.Units, 1)
			require.Equal(t, occurrences(c.want), res.Units[0].Occurrences)
		})
	}
}

// TestLambdaOccurrences pins that a lambda charges the literal itself, a
// trailing one included, and that a property unit's body is skipped.
func TestLambdaOccurrences(t *testing.T) {
	res := analyzeSource(t, "fun t(xs: List<Int>) = xs.map { it * 2 }\nval onEvent: (Int) -> Unit = { println(it) }\n")
	require.Equal(t, occurrences([]occurrenceAt{
		{config.MetricLambda, 1, 31, 1, 41, 1},
	}), unitNamed(t, res, "t").Occurrences)
	require.Empty(t, unitNamed(t, res, "onEvent").Occurrences)
}
