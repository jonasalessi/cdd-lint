package golang

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// extText keeps a fixture out of the toolchain's reach: broken.go.txt holds
// a source that must not compile and empty.go.txt one that carries no
// package clause. Both are analyzed under their `.go` name.
const extText = ".txt"

// brokenFixture is the only fixture that does not parse; every other one is
// expected to come back without a warning.
const brokenFixture = "broken.go" + extText

// goName is the name a fixture is analyzed under: its own, minus the
// suffix that hides it from the toolchain.
func goName(fixture string) string {
	return strings.TrimSuffix(fixture, extText)
}

// fixtureNames returns every fixture directly under testdata, so one added
// later is covered by the invariants without anyone remembering to list it.
// The nested module/ tree belongs to package detection, not to the analyzer.
func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(goName(e.Name()), extGo) {
			out = append(out, e.Name())
		}
	}
	require.NotEmpty(t, out)
	return out
}

// analyzeUnderGoName analyzes a fixture under its `.go` name, which is the
// only extension the analyzer accepts.
func analyzeUnderGoName(t *testing.T, fixture string) analyze.FileResult {
	t.Helper()
	res, err := newTestAnalyzer(t, goPrefix).Analyze(context.Background(), goName(fixture), readFixture(t, fixture))
	require.NoError(t, err)
	return res
}

// eachFixture analyzes every fixture and hands the result to check as a
// subtest named after the file.
func eachFixture(t *testing.T, check func(t *testing.T, fixture string, res analyze.FileResult)) {
	t.Helper()
	for _, fixture := range fixtureNames(t) {
		t.Run(fixture, func(t *testing.T) {
			check(t, fixture, analyzeUnderGoName(t, fixture))
		})
	}
}

// parseFixture parses a fixture the way the analyzer does, returning a nil
// file for the ones written not to parse.
func parseFixture(t *testing.T, fixture string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, goName(fixture), readFixture(t, fixture), parser.ParseComments)
	if err != nil {
		return fset, nil
	}
	return fset, file
}

// unitKey identifies a unit across the analyzer's result and the
// declarations the in-package units() helper extracts for it.
type unitKey struct {
	name string
	line int
}

// before reports whether one position comes strictly before another.
func before(line, col, otherLine, otherCol int) bool {
	return line < otherLine || (line == otherLine && col < otherCol)
}

// holds reports whether the whole occurrence lies inside the span.
func holds(s treesitter.Span, o analyze.Occurrence) bool {
	return !before(o.Line, o.Col, s.Line, s.Col) &&
		!before(s.EndLine, s.EndCol, o.EndLine, o.EndCol)
}

// declRanges re-derives, from the source alone, the declaration spans each
// unit of a fixture counts over: the TypeSpec plus every method billed to
// it, or the function itself.
func declRanges(t *testing.T, fixture string) map[unitKey][]treesitter.Span {
	t.Helper()
	fset, file := parseFixture(t, fixture)
	out := map[unitKey][]treesitter.Span{}
	if file == nil {
		return out
	}
	for _, d := range units(fset, file) {
		key := unitKey{name: d.name, line: d.line}
		for _, n := range d.nodes {
			out[key] = append(out[key], spanOf(fset, n))
		}
	}
	return out
}

// importLines is the set of lines an ImportSpec of a fixture starts on: the
// only lines a coupling occurrence may point at.
func importLines(t *testing.T, fixture string) map[int]bool {
	t.Helper()
	fset, file := parseFixture(t, fixture)
	out := map[int]bool{}
	if file == nil {
		return out
	}
	for _, spec := range file.Imports {
		out[fset.Position(spec.Pos()).Line] = true
	}
	return out
}

// TestOccurrencesAccountForEveryCount (TC-X1) is the invariant the
// occurrence list exists to keep: for every unit of every fixture, and for
// every metric, the occurrences add up to exactly the raw count.
func TestOccurrencesAccountForEveryCount(t *testing.T) {
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for _, u := range res.Units {
			charged := map[config.MetricID]int{}
			for _, o := range u.Occurrences {
				charged[o.Metric] += o.Count
			}
			for _, m := range config.Metrics() {
				require.Equal(t, u.Counts[m], charged[m], "unit %q, metric %q", u.Name, m)
			}
		}
	})
}

// TestOccurrencesAreWellFormed (TC-X2) checks that every located construct
// carries a range an editor can use: 1-based, non-empty, end after start, a
// known metric.
func TestOccurrencesAreWellFormed(t *testing.T) {
	known := map[config.MetricID]bool{}
	for _, m := range config.Metrics() {
		known[m] = true
	}
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for _, u := range res.Units {
			for _, o := range u.Occurrences {
				where := u.Name + " occurrence " + string(o.Metric)
				require.True(t, known[o.Metric], where)
				require.Positive(t, o.Line, where)
				require.Positive(t, o.Col, where)
				require.Positive(t, o.Count, where)
				require.True(t, before(o.Line, o.Col, o.EndLine, o.EndCol), where)
			}
		}
	})
}

// TestOccurrencesAreSorted (TC-X3): a unit's occurrences come out in source
// order, so a reader walks the file top to bottom.
func TestOccurrencesAreSorted(t *testing.T) {
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for _, u := range res.Units {
			for i := 1; i < len(u.Occurrences); i++ {
				prev, o := u.Occurrences[i-1], u.Occurrences[i]
				require.False(t, before(o.Line, o.Col, prev.Line, prev.Col),
					"%s: %d:%d comes after %d:%d", u.Name, o.Line, o.Col, prev.Line, prev.Col)
			}
		}
	})
}

// TestNonCouplingOccurrencesAreInsideTheUnit (TC-X4): everything but a
// coupling charge is written inside one of the declarations the unit counts
// over. The union, rather than a single line range, is what units.go
// requires: a method bills to its type wherever it sits, before or after the
// type's own declaration.
func TestNonCouplingOccurrencesAreInsideTheUnit(t *testing.T) {
	eachFixture(t, func(t *testing.T, fixture string, res analyze.FileResult) {
		ranges := declRanges(t, fixture)
		for _, u := range res.Units {
			spans, ok := ranges[unitKey{name: u.Name, line: u.Line}]
			require.True(t, ok, "no declaration for unit %q at line %d", u.Name, u.Line)
			for _, o := range u.Occurrences {
				if isCoupling(o.Metric) {
					continue
				}
				require.True(t, insideAny(spans, o),
					"%s occurrence %s at %d:%d is outside the unit", u.Name, o.Metric, o.Line, o.Col)
			}
		}
	})
}

// insideAny reports whether one of the spans holds the occurrence whole.
func insideAny(spans []treesitter.Span, o analyze.Occurrence) bool {
	for _, s := range spans {
		if holds(s, o) {
			return true
		}
	}
	return false
}

// TestCouplingOccurrencesSitOnImports (TC-X5): a coupling charge points at
// the import that brings the dependency in, which is always above the first
// unit of the file.
func TestCouplingOccurrencesSitOnImports(t *testing.T) {
	eachFixture(t, func(t *testing.T, fixture string, res analyze.FileResult) {
		lines := importLines(t, fixture)
		for _, u := range res.Units {
			for _, o := range u.Occurrences {
				if !isCoupling(o.Metric) {
					continue
				}
				where := u.Name + " occurrence " + string(o.Metric)
				require.Less(t, o.Line, res.Units[0].Line, "%s sits below the first unit", where)
				require.True(t, lines[o.Line], "%s points at line %d, which is no import", where, o.Line)
			}
		}
	})
}

// TestEveryUnitCarriesEveryMetric (TC-X6): a unit reports every metric,
// enabled or not. Which ones matter is the pipeline's business.
func TestEveryUnitCarriesEveryMetric(t *testing.T) {
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for _, u := range res.Units {
			require.Len(t, u.Counts, len(config.Metrics()), "unit %q", u.Name)
		}
	})
}

// TestEveryFixtureListsUnitsInSourceOrder (TC-X6): the units of a file come
// out top to bottom, each starting on a line of its own.
func TestEveryFixtureListsUnitsInSourceOrder(t *testing.T) {
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for i := 1; i < len(res.Units); i++ {
			require.Greater(t, res.Units[i].Line, res.Units[i-1].Line,
				"unit %q must start below %q", res.Units[i].Name, res.Units[i-1].Name)
		}
	})
}

// TestOnlyTheBrokenFixtureWarns (TC-X6): every fixture parses except the one
// written to fail, which yields a single warning and no units.
func TestOnlyTheBrokenFixtureWarns(t *testing.T) {
	eachFixture(t, func(t *testing.T, fixture string, res analyze.FileResult) {
		if fixture != brokenFixture {
			require.Empty(t, res.Warnings)
			return
		}
		require.Len(t, res.Warnings, 1)
		require.Empty(t, res.Units)
	})
}

// TestFixturesAreDeterministic (TC-X7): two analyzers built separately
// produce the same result for every fixture.
func TestFixturesAreDeterministic(t *testing.T) {
	for _, fixture := range fixtureNames(t) {
		first := analyzeUnderGoName(t, fixture)
		second := analyzeUnderGoName(t, fixture)
		require.True(t, reflect.DeepEqual(first, second), fixture)
	}
}

// TestConcurrentAnalyzers (TC-X8): eight goroutines, one analyzer each,
// reading every fixture at once under the race detector. The analyzer shares
// nothing mutable and each call owns its token.FileSet.
func TestConcurrentAnalyzers(t *testing.T) {
	fixtures := fixtureNames(t)
	sources := map[string][]byte{}
	for _, fixture := range fixtures {
		sources[fixture] = readFixture(t, fixture)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			a := NewAnalyzer(analyze.Options{InternalPrefixes: []string{goPrefix}})
			for _, fixture := range fixtures {
				_, err := a.Analyze(context.Background(), goName(fixture), sources[fixture])
				assert.NoError(t, err, "goroutine %d", i)
			}
		})
	}
	wg.Wait()
}
