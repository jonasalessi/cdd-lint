package typescript

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// fixtureNames returns every TypeScript fixture under testdata, named the
// way analyzeFixture wants them, so a fixture added later is covered by the
// invariant below without anyone remembering to list it.
func fixtureNames(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir("testdata", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case extTS, extMTS, extCTS, extTSX:
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

// requireOccurrence asserts a unit locates the given construct exactly once.
func requireOccurrence(t *testing.T, u analyze.Unit, want occurrenceAt) {
	t.Helper()
	found := 0
	for _, got := range u.Occurrences {
		if got == want.occurrence() {
			found++
		}
	}
	assert.Equal(t, 1, found, "unit %q must locate %+v exactly once; it has %+v",
		u.Name, want.occurrence(), u.Occurrences)
}

// TestDocExampleOccurrences pins the worked example of docs/cdd.md down to
// its ranges: `if (a > b && c < d)` is the `if` statement plus one
// occurrence on each Boolean clause, and nothing else.
func TestDocExampleOccurrences(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.ts")
	want := []occurrenceAt{
		{config.MetricCodeBranch, 6, 3, 8, 4, 1},
		{config.MetricCondition, 6, 7, 6, 12, 1},
		{config.MetricCondition, 6, 16, 6, 21, 1},
	}
	got := make([]analyze.Occurrence, 0, len(want))
	for _, w := range want {
		got = append(got, w.occurrence())
	}
	assert.Equal(t, got, unitNamed(t, res, "docCondition").Occurrences)
}

// TestOccurrenceRanges pins the node each metric points at, one case per
// rule the analyzer follows.
func TestOccurrenceRanges(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		unit    string
		want    occurrenceAt
	}{
		{
			name: "an else clause, from the keyword to the end of its block",
			// `  } else {` on line 24 of cdd_examples.ts.
			fixture: "cdd_examples.ts", unit: "docIfElse",
			want: occurrenceAt{config.MetricCodeBranch, 24, 5, 26, 4, 1},
		},
		{
			name: "an else if charges the chained if, not the else",
			// `  } else if (v === 1) {` on line 6: the `if` sits at column 10.
			fixture: "branches.ts", unit: "branches",
			want: occurrenceAt{config.MetricCodeBranch, 6, 10, 12, 4, 1},
		},
		{
			name: "an optional member access charges its `?.` token",
			// `  const shallow = o?.a;` on line 50: `?.` spans columns 20-21.
			fixture: "branches.ts", unit: "branches",
			want: occurrenceAt{config.MetricCodeBranch, 50, 20, 50, 22, 1},
		},
		{
			name: "an optional call charges the anonymous `?.` token",
			// `  const a = f?.();` on line 64: `?.` spans columns 14-15.
			fixture: "branches.ts", unit: "optionalCalls",
			want: occurrenceAt{config.MetricCodeBranch, 64, 14, 64, 16, 1},
		},
		{
			name: "a logical augmented assignment is one occurrence worth two",
			// `  y ||= b;` on line 18 of conditions.ts.
			fixture: "conditions.ts", unit: "conditions",
			want: occurrenceAt{config.MetricCondition, 18, 3, 18, 10, 2},
		},
		{
			name: "the right-hand side of `y ||= b && c` adds to the same count",
			// `  y ||= b && c;` on line 21: one occurrence, three clauses.
			fixture: "conditions.ts", unit: "conditions",
			want: occurrenceAt{config.MetricCondition, 21, 3, 21, 15, 3},
		},
		{
			name: "a try charges its block, not the whole statement",
			// `  try {` on line 5 of exceptions.ts: the block opens at column 7.
			fixture: "exceptions.ts", unit: "exceptions",
			want: occurrenceAt{config.MetricExceptionHandling, 5, 7, 8, 4, 1},
		},
		{
			name:    "a catch clause charges itself",
			fixture: "exceptions.ts", unit: "exceptions",
			want: occurrenceAt{config.MetricExceptionHandling, 8, 5, 11, 4, 1},
		},
		{
			name:    "a finally clause charges itself",
			fixture: "exceptions.ts", unit: "exceptions",
			want: occurrenceAt{config.MetricExceptionHandling, 11, 5, 14, 4, 1},
		},
		{
			name: "extends charges the parent type, not its type arguments",
			// `export class Both extends Base<Generic> implements First, Second {}`:
			// Base spans columns 27-30, `<Generic>` is left out.
			fixture: "inheritance.ts", unit: "Both",
			want: occurrenceAt{config.MetricInheritance, 13, 27, 13, 31, 1},
		},
		{
			name:    "implements charges each listed contract on its own",
			fixture: "inheritance.ts", unit: "Both",
			want: occurrenceAt{config.MetricInheritance, 13, 59, 13, 65, 1},
		},
		{
			name: "a for…of binding is charged where it is bound",
			// `  for (const item of input) {` on line 16: `item` at column 14.
			fixture: "locals.ts", unit: "locals",
			want: occurrenceAt{config.MetricLocalVariable, 16, 14, 16, 18, 1},
		},
		{
			name: "a lambda charges the arrow function itself",
			// `  return xs.map((n) => double(n) + triple(n));` on line 10.
			fixture: "lambdas.ts", unit: "lambdas",
			want: occurrenceAt{config.MetricLambda, 10, 17, 10, 45, 1},
		},
		{
			name: "internal coupling points at the import statement",
			// `import { Repo } from "./repo";` on line 7, the first of the two
			// statements naming that module.
			fixture: "coupling.ts", unit: "UsesInternal",
			want: occurrenceAt{config.MetricInternalCoupling, 7, 1, 7, 31, 1},
		},
		{
			name: "stdlib coupling points at the import statement",
			// `import { readFile } from "node:fs/promises";` on line 11.
			fixture: "coupling.ts", unit: "usesExternal",
			want: occurrenceAt{config.MetricStdlibCoupling, 11, 1, 11, 45, 1},
		},
		{
			name: "external coupling points at the import statement",
			// `import * as lodash from "lodash/fp";` on line 12.
			fixture: "coupling.ts", unit: "usesExternal",
			want: occurrenceAt{config.MetricExternalCoupling, 12, 1, 12, 37, 1},
		},
		{
			name: "a side-effect import is charged to a unit that names nothing",
			// `import "./polyfill";` on line 15, charged to every unit.
			fixture: "coupling.ts", unit: "Untouched",
			want: occurrenceAt{config.MetricInternalCoupling, 15, 1, 15, 21, 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeFixture(t, c.fixture, appPrefix)
			requireOccurrence(t, unitNamed(t, res, c.unit), c.want)
		})
	}
}

// TestOccurrencesAccountForEveryCount is the invariant the occurrence list
// exists to keep: for every unit of every fixture, and for every metric, the
// occurrences add up to exactly the raw count. A construct counted without
// being located, or located twice, fails here.
func TestOccurrencesAccountForEveryCount(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			res := analyzeFixture(t, name, appPrefix)
			for _, u := range res.Units {
				summed := map[config.MetricID]int{}
				for _, o := range u.Occurrences {
					summed[o.Metric] += o.Count
				}
				for _, m := range config.Metrics() {
					assert.Equal(t, u.Counts[m], summed[m], "unit %q, metric %q", u.Name, m)
				}
			}
		})
	}
}

// TestOccurrencesAreWellFormed checks that every located construct carries a
// range an editor can use -- 1-based, non-empty, end after start -- and that
// a unit's occurrences come out in source order.
func TestOccurrencesAreWellFormed(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			res := analyzeFixture(t, name, appPrefix)
			for _, u := range res.Units {
				requireWellFormed(t, u)
			}
		})
	}
}

// requireWellFormed asserts one unit's occurrences are usable ranges in
// source order.
func requireWellFormed(t *testing.T, u analyze.Unit) {
	t.Helper()
	for i, o := range u.Occurrences {
		where := u.Name + " occurrence " + string(o.Metric)
		assert.Positive(t, o.Line, where)
		assert.Positive(t, o.Col, where)
		assert.Positive(t, o.Count, where)
		assert.GreaterOrEqual(t, o.EndLine, o.Line, where)
		if o.EndLine == o.Line {
			assert.Greater(t, o.EndCol, o.Col, where)
		}
		if i == 0 {
			continue
		}
		prev := u.Occurrences[i-1]
		assert.True(t, prev.Line < o.Line || (prev.Line == o.Line && prev.Col <= o.Col),
			"%s: %d:%d comes after %d:%d", where, o.Line, o.Col, prev.Line, prev.Col)
	}
}
