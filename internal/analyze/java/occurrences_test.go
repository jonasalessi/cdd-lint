package java

import (
	"context"
	"io"
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

// brokenFixture is the only fixture that does not parse; every other one is
// expected to come back without a warning.
const brokenFixture = "broken.java"

// importPrefix opens the only statement a coupling occurrence may sit on.
const importPrefix = "import "

// fixtureNames returns every Java fixture directly under testdata, so a
// fixture added later is covered by the invariants without anyone
// remembering to list it. The nested project/ tree belongs to package
// detection, not to the analyzer, and is left out.
func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), extJava) {
			out = append(out, e.Name())
		}
	}
	require.NotEmpty(t, out)
	return out
}

// eachFixture analyzes every fixture and hands the result to check as a
// subtest named after the file.
func eachFixture(t *testing.T, check func(t *testing.T, name string, res analyze.FileResult)) {
	t.Helper()
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			check(t, name, analyzeFixture(t, name, acmePrefix))
		})
	}
}

// fixtureLines returns the source lines of a fixture, so an occurrence's
// position can be read back against the text it points at.
func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	return strings.Split(string(readFixture(t, name)), "\n")
}

// isCoupling reports whether a metric is charged on an import statement,
// which is the one occurrence that sits outside the unit it belongs to.
func isCoupling(m config.MetricID) bool {
	return m == config.MetricInternalCoupling ||
		m == config.MetricExternalCoupling ||
		m == config.MetricStdlibCoupling
}

// lastLine returns the last line a unit may charge: units are top level and
// do not nest, so one ends where the next begins and the last one ends with
// the file.
func lastLine(units []analyze.Unit, i, lines int) int {
	if i+1 < len(units) {
		return units[i+1].Line - 1
	}
	return lines
}

// TestOccurrencesAccountForEveryCount (TC-X1) is the invariant the occurrence
// list exists to keep: for every unit of every fixture, and for every metric,
// the occurrences add up to exactly the raw count.
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
				require.True(t, o.EndLine > o.Line || (o.EndLine == o.Line && o.EndCol > o.Col), where)
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
				require.True(t, prev.Line < o.Line || (prev.Line == o.Line && prev.Col <= o.Col),
					"%s: %d:%d comes after %d:%d", u.Name, o.Line, o.Col, prev.Line, prev.Col)
			}
		}
	})
}

// TestNonCouplingOccurrencesAreInsideTheUnit (TC-X4): everything but a
// coupling charge is written inside the unit it is charged to.
func TestNonCouplingOccurrencesAreInsideTheUnit(t *testing.T) {
	eachFixture(t, func(t *testing.T, name string, res analyze.FileResult) {
		lines := len(fixtureLines(t, name))
		for i, u := range res.Units {
			end := lastLine(res.Units, i, lines)
			for _, o := range u.Occurrences {
				if isCoupling(o.Metric) {
					continue
				}
				where := u.Name + " occurrence " + string(o.Metric)
				require.GreaterOrEqual(t, o.Line, u.Line, "%s starts above its unit", where)
				require.LessOrEqual(t, o.EndLine, end, "%s ends past its unit", where)
			}
		}
	})
}

// TestCouplingOccurrencesSitOnImports (TC-X5): a coupling charge points at
// the import that brings the dependency in, which is always above the first
// unit of the file.
func TestCouplingOccurrencesSitOnImports(t *testing.T) {
	eachFixture(t, func(t *testing.T, name string, res analyze.FileResult) {
		lines := fixtureLines(t, name)
		for _, u := range res.Units {
			for _, o := range u.Occurrences {
				if !isCoupling(o.Metric) {
					continue
				}
				where := u.Name + " occurrence " + string(o.Metric)
				require.Less(t, o.Line, res.Units[0].Line, "%s sits below the first unit", where)
				require.True(t, strings.HasPrefix(strings.TrimSpace(lines[o.Line-1]), importPrefix),
					"%s points at %q", where, lines[o.Line-1])
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

// TestUnitsComeOutInSourceOrder (TC-X3 for units): the units of a file are
// listed top to bottom, each on a line of its own.
func TestUnitsComeOutInSourceOrder(t *testing.T) {
	eachFixture(t, func(t *testing.T, _ string, res analyze.FileResult) {
		for i := 1; i < len(res.Units); i++ {
			require.Greater(t, res.Units[i].Line, res.Units[i-1].Line,
				"unit %q must start below %q", res.Units[i].Name, res.Units[i-1].Name)
		}
	})
}

// TestOnlyTheBrokenFixtureWarns (TC-P5): every fixture parses except the one
// written to fail, which yields a single warning and no units.
func TestOnlyTheBrokenFixtureWarns(t *testing.T) {
	eachFixture(t, func(t *testing.T, name string, res analyze.FileResult) {
		if name != brokenFixture {
			require.Empty(t, res.Warnings)
			return
		}
		require.Len(t, res.Warnings, 1)
		require.Empty(t, res.Units)
	})
}

// TestDeterminism (TC-X7): two analyzers produce the same result for every
// fixture.
func TestDeterminism(t *testing.T) {
	for _, name := range fixtureNames(t) {
		first := analyzeFixture(t, name, acmePrefix)
		second := analyzeFixture(t, name, acmePrefix)
		require.True(t, reflect.DeepEqual(first, second), name)
	}
}

// TestConcurrentAnalyzers (TC-X8): one analyzer per goroutine, all reading
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
