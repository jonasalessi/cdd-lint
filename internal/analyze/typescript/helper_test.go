package typescript

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// newTestAnalyzer returns an analyzer that is closed when the test ends.
func newTestAnalyzer(t *testing.T, prefixes ...string) analyze.Analyzer {
	t.Helper()
	a := NewAnalyzer(analyze.Options{InternalPrefixes: prefixes})
	closer, ok := a.(io.Closer)
	require.True(t, ok, "the analyzer must implement io.Closer")
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return a
}

// readFixture returns the content of a file under testdata.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return src
}

// analyzeFixture analyzes a file under testdata under its own name.
func analyzeFixture(t *testing.T, name string, prefixes ...string) analyze.FileResult {
	t.Helper()
	a := newTestAnalyzer(t, prefixes...)
	res, err := a.Analyze(context.Background(), name, readFixture(t, name))
	require.NoError(t, err)
	return res
}

// unitNamed returns the unit with the given name, failing when it is absent.
func unitNamed(t *testing.T, res analyze.FileResult, name string) analyze.Unit {
	t.Helper()
	for _, u := range res.Units {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("no unit named %q in %v", name, unitNames(res))
	return analyze.Unit{}
}

// unitNames lists the names of the units of a result, for failure messages.
func unitNames(res analyze.FileResult) []string {
	out := make([]string, 0, len(res.Units))
	for _, u := range res.Units {
		out = append(out, u.Name)
	}
	return out
}

// requireCount asserts one raw count of a unit and that the unit carries
// every metric, enabled or not.
func requireCount(t *testing.T, u analyze.Unit, metric config.MetricID, want int) {
	t.Helper()
	require.Len(t, u.Counts, len(config.Metrics()), "every metric must be counted")
	require.Equal(t, want, u.Counts[metric], "unit %q, metric %q", u.Name, metric)
}
