package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// render is Write into a buffer with the default filter: violations only.
func render(t *testing.T, format string, res analyze.RunResult) string {
	t.Helper()
	return renderWith(t, format, res, Options{})
}

// renderAll is Write into a buffer listing every unit, the way --all does.
func renderAll(t *testing.T, format string, res analyze.RunResult) string {
	t.Helper()
	return renderWith(t, format, res, Options{All: true})
}

func renderWith(t *testing.T, format string, res analyze.RunResult, opts Options) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, format, res, opts))
	return buf.String()
}

func TestWriteGolden(t *testing.T) {
	for _, format := range config.ReporterFormats() {
		for suffix, tc := range goldenCases() {
			t.Run(format+suffix, func(t *testing.T) {
				got := renderWith(t, format, tc.res, tc.opts)
				path := goldenPath(format, suffix)
				if updateGolden() {
					require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
				}
				want, err := os.ReadFile(path)
				require.NoError(t, err, "run go test ./internal/report -update to create it")
				assert.Equal(t, string(want), got)
			})
		}
	}
}

func TestWriteEveryFormatIsSupported(t *testing.T) {
	for _, format := range config.ReporterFormats() {
		assert.NotEmpty(t, render(t, format, fullRun()), format)
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	err := Write(&bytes.Buffer{}, "yaml", fullRun(), Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"yaml"`)
}

// errWriter fails every write, so a renderer must surface the failure.
type errWriter struct{}

var errWrite = errors.New("no room left")

func (errWriter) Write([]byte) (int, error) { return 0, errWrite }

func TestWriteReportsWriteErrors(t *testing.T) {
	for _, format := range config.ReporterFormats() {
		opts := Options{All: true, Explain: true}
		assert.ErrorIs(t, Write(errWriter{}, format, fullRun(), opts), errWrite, format)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	data, err := os.ReadFile(goldenPath(config.FormatJSON, ""))
	require.NoError(t, err)

	var doc Report
	require.NoError(t, json.Unmarshal(data, &doc))

	assert.Equal(t, "/projects/shop", doc.Root)
	assert.True(t, doc.Blocked)
	assert.Equal(t, filterViolations, doc.Filter)
	assert.False(t, doc.Partial)
	assert.Equal(t, int64(1234), doc.ElapsedMS)
	assert.Equal(t, Summary{Units: 3, Violations: 1}, doc.Summary, "the summary counts the run, not the listing")
	assert.Empty(t, doc.Warnings)

	require.Len(t, doc.Files, 1, "src/money.ts has no violation and no warning")
	first := doc.Files[0]
	assert.Equal(t, "src/checkout.ts", first.Path)
	assert.Equal(t, string(fixtureLanguage), first.Language)
	assert.Len(t, first.Warnings, 1)

	require.Len(t, first.Units, 1)
	over := first.Units[0]
	assert.Equal(t, "CheckoutService", over.Name)
	assert.True(t, over.Exceeds)
	assert.InDelta(t, 14.5, over.Total, 0)
	assert.Equal(t, 10, over.Limit)
	assert.Equal(t, []Metric{
		{ID: string(config.MetricCodeBranch), Count: 6, Score: 6},
		{ID: string(config.MetricCondition), Count: 3, Score: 3},
		{ID: string(config.MetricExternalCoupling), Count: 11, Score: 5.5},
	}, over.Metrics)
}

// renderExplain is Write into a buffer detailing every listed unit, the way
// --all --explain does.
func renderExplain(t *testing.T, format string, res analyze.RunResult) string {
	t.Helper()
	return renderWith(t, format, res, Options{All: true, Explain: true})
}

func TestExplainIsOffByDefault(t *testing.T) {
	doc := newReport(fullRun(), Options{All: true})
	assert.False(t, doc.Explain)
	for _, f := range doc.Files {
		for _, u := range f.Units {
			assert.Nil(t, u.Occurrences, u.Name)
			assert.Empty(t, u.constructs(), u.Name)
		}
	}
}

func TestExplainListsTheConstructsOfEveryListedUnit(t *testing.T) {
	doc := newReport(fullRun(), Options{All: true, Explain: true})
	assert.True(t, doc.Explain)

	greet := doc.Files[0].Units[0]
	require.NotNil(t, greet.Occurrences)
	assert.Equal(t, []Occurrence{{
		Unit: "greet", Metric: string(config.MetricCodeBranch),
		Line: 2, Col: 3, EndLine: 4, EndCol: 4, Count: 1, Score: 1,
	}}, greet.constructs())

	checkout := doc.Files[0].Units[1].constructs()
	require.Len(t, checkout, 4)
	assert.Equal(t, Occurrence{
		Unit: "CheckoutService", Metric: string(config.MetricExternalCoupling),
		Line: 1, Col: 1, EndLine: 1, EndCol: 30, Count: 1, Score: 0.5,
	}, checkout[0], "the coupling sits on the import, before the unit itself")
}

func TestExplainRepeatsTheOwningUnitOnEveryConstruct(t *testing.T) {
	doc := newReport(fullRun(), Options{All: true, Explain: true})
	for _, f := range doc.Files {
		for _, u := range f.Units {
			for _, o := range u.constructs() {
				assert.Equal(t, u.Name, o.Unit, "a plugin flattens a file on this field")
			}
		}
	}
}

func TestExplainIsIndependentOfTheUnitFilter(t *testing.T) {
	doc := newReport(fullRun(), Options{Explain: true})
	require.Len(t, doc.Files, 1)
	require.Len(t, doc.Files[0].Units, 1, "explain details what --all listed, it never lists more")
	assert.Equal(t, "CheckoutService", doc.Files[0].Units[0].Name)
	assert.Len(t, doc.Files[0].Units[0].constructs(), 4)
	assert.Equal(t, filterViolations, doc.Filter)
}

func TestExplainFieldIsRendered(t *testing.T) {
	assert.Contains(t, renderAll(t, config.FormatJSON, fullRun()), `"explain": false`)
	assert.Contains(t, renderExplain(t, config.FormatJSON, fullRun()), `"explain": true`)
	assert.Contains(t, renderAll(t, config.FormatXML, fullRun()), `explain="false"`)
	assert.Contains(t, renderExplain(t, config.FormatXML, fullRun()), `explain="true"`)
}

func TestJSONOmitsOccurrencesUnlessExplained(t *testing.T) {
	assert.NotContains(t, renderAll(t, config.FormatJSON, fullRun()), "occurrences")
	assert.NotContains(t, renderAll(t, config.FormatXML, fullRun()), "occurrences")
}

func TestJSONExplainsAnEmptyUnitAsAnEmptyList(t *testing.T) {
	out := renderExplain(t, config.FormatJSON, fullRun())
	assert.Contains(t, out, `"occurrences": []`, "formatMoney counted nothing we located")
	assert.NotContains(t, out, `"occurrences": null`)
}

func TestJSONRoundTripsOccurrences(t *testing.T) {
	var doc Report
	require.NoError(t, json.Unmarshal([]byte(renderExplain(t, config.FormatJSON, fullRun())), &doc))
	assert.True(t, doc.Explain)

	require.Len(t, doc.Files, 2)
	checkout := doc.Files[0].Units[1]
	assert.Equal(t, "CheckoutService", checkout.Name)
	require.NotNil(t, checkout.Occurrences)
	assert.Equal(t, []Occurrence{
		{Unit: "CheckoutService", Metric: string(config.MetricExternalCoupling),
			Line: 1, Col: 1, EndLine: 1, EndCol: 30, Count: 1, Score: 0.5},
		{Unit: "CheckoutService", Metric: string(config.MetricCodeBranch),
			Line: 12, Col: 3, EndLine: 14, EndCol: 4, Count: 1, Score: 1},
		{Unit: "CheckoutService", Metric: string(config.MetricCondition),
			Line: 12, Col: 7, EndLine: 12, EndCol: 15, Count: 1, Score: 1},
		{Unit: "CheckoutService", Metric: string(config.MetricCondition),
			Line: 12, Col: 19, EndLine: 12, EndCol: 28, Count: 1, Score: 1},
	}, checkout.constructs())

	money := doc.Files[1].Units[0]
	require.NotNil(t, money.Occurrences, "an explained unit says it counted nothing")
	assert.Empty(t, money.constructs())
}

func TestConsoleExplainsUnderTheMetricsLine(t *testing.T) {
	out := renderExplain(t, config.FormatConsole, fullRun())
	assert.Contains(t, out,
		"violation: src/checkout.ts:12:3 class CheckoutService icp=14.5 limit=10 over=4.5\n"+
			"  metrics: code_branch=6 external_coupling=11x0.5 condition=3\n"+
			"  icp: 1:1-1:30 external_coupling +0.5\n"+
			"  icp: 12:3-14:4 code_branch +1\n"+
			"  icp: 12:7-12:15 condition +1\n"+
			"  icp: 12:19-12:28 condition +1\n")
	assert.Contains(t, out,
		"unit: src/money.ts:3:1 function formatMoney icp=2 limit=10\n"+
			"  metrics: code_branch=1 condition=1\n\n",
		"a unit without a located construct gains no line")
}

func TestConsoleWithoutExplainIsUnchanged(t *testing.T) {
	for _, opts := range []Options{{}, {All: true}} {
		out := renderWith(t, config.FormatConsole, fullRun(), opts)
		assert.NotContains(t, out, "  icp: ")
	}
}

func TestMarkdownExplainsUnderTheMetricsBullet(t *testing.T) {
	out := renderExplain(t, config.FormatMarkdown, fullRun())
	assert.Contains(t, out,
		"- `greet` — code_branch 2×1=2, external_coupling 1×0.5=0.5\n"+
			"  - 2:3-4:4 code_branch +1\n")
	assert.Contains(t, out, "  - 12:19-12:28 condition +1\n")
	assert.NotContains(t, renderAll(t, config.FormatMarkdown, fullRun()), "  - 2:3-4:4")
}

func TestXMLExplainsUnderEachUnit(t *testing.T) {
	out := renderExplain(t, config.FormatXML, fullRun())
	assert.Contains(t, out, `<occurrence unit="greet" metric="code_branch" `+
		`line="2" col="3" end_line="4" end_col="4" count="1" score="1">`)
	assert.Contains(t, out, "<occurrences></occurrences>", "formatMoney counted nothing we located")
}

func TestOccurrenceText(t *testing.T) {
	o := Occurrence{Metric: string(config.MetricCondition), Line: 12, Col: 7, EndLine: 12, EndCol: 15, Score: 1}
	assert.Equal(t, "12:7-12:15 condition +1", occurrenceText(o))
	o.Score = 0.5
	assert.Equal(t, "12:7-12:15 condition +0.5", occurrenceText(o))
	o.Score = 2
	assert.Equal(t, "12:7-12:15 condition +2", occurrenceText(o))
}

func TestFilterListsOnlyViolationsByDefault(t *testing.T) {
	doc := newReport(fullRun(), Options{})
	require.Len(t, doc.Files, 1)
	require.Len(t, doc.Files[0].Units, 1)
	assert.Equal(t, "CheckoutService", doc.Files[0].Units[0].Name)
	assert.Equal(t, filterViolations, doc.Filter)
}

func TestFilterAllListsEveryUnit(t *testing.T) {
	doc := newReport(fullRun(), Options{All: true})
	require.Len(t, doc.Files, 2)
	assert.Len(t, doc.Files[0].Units, 2)
	assert.Len(t, doc.Files[1].Units, 1)
	assert.Equal(t, filterAll, doc.Filter)
}

func TestFilterKeepsAFileForItsWarningAlone(t *testing.T) {
	res := analyze.RunResult{
		Root: "/projects/shop",
		Files: []analyze.FileReport{
			{Path: "src/clean.ts", Language: fixtureLanguage, Units: []analyze.UnitReport{greetUnit()}},
			{Path: "src/broken.ts", Language: fixtureLanguage, Warnings: []string{"syntax error at 1:1"}},
		},
		Elapsed: fixtureElapsed,
	}
	doc := newReport(res, Options{})
	require.Len(t, doc.Files, 1, "the clean file says nothing, the broken one warns")
	assert.Equal(t, "src/broken.ts", doc.Files[0].Path)
	assert.Empty(t, doc.Files[0].Units)
}

func TestFilterLeavesTheSummaryAlone(t *testing.T) {
	violations := newReport(fullRun(), Options{}).Summary
	assert.Equal(t, newReport(fullRun(), Options{All: true}).Summary, violations)
	assert.Equal(t, Summary{Units: 3, Violations: 1}, violations)
}

func TestFilterFieldIsRendered(t *testing.T) {
	assert.Contains(t, render(t, config.FormatJSON, fullRun()), `"filter": "violations"`)
	assert.Contains(t, renderAll(t, config.FormatJSON, fullRun()), `"filter": "all"`)
	assert.Contains(t, render(t, config.FormatXML, fullRun()), `filter="violations"`)
	assert.Contains(t, renderAll(t, config.FormatXML, fullRun()), `filter="all"`)
}

func TestJSONPartialHasEmptyFileList(t *testing.T) {
	out := render(t, config.FormatJSON, partialRun())
	assert.Contains(t, out, `"files": []`)
	assert.Contains(t, out, `"partial": true`)
	assert.NotContains(t, out, `"warnings"`)
	assert.True(t, strings.HasSuffix(out, "}\n"))
}

func TestMetricsFollowVocabularyOrder(t *testing.T) {
	doc := newReport(fullRun(), Options{All: true})
	var ids []string
	for _, m := range doc.Files[0].Units[0].Metrics {
		ids = append(ids, m.ID)
	}
	assert.Equal(t, []string{
		string(config.MetricCodeBranch),
		string(config.MetricCondition),
		string(config.MetricExternalCoupling),
	}, ids, "canonical order, disabled metrics absent")
}

func TestConsoleShapesTheRun(t *testing.T) {
	out := render(t, config.FormatConsole, fullRun())
	assert.True(t, strings.HasPrefix(out,
		"cdd check: FAIL violations=1 units=3 blocked=true root=/projects/shop elapsed=1.234s\n"))
	assert.Contains(t, out,
		"\nviolation: src/checkout.ts:12:3 class CheckoutService icp=14.5 limit=10 over=4.5\n"+
			"  metrics: code_branch=6 external_coupling=11x0.5 condition=3\n")
	assert.NotContains(t, out, "greet", "a unit within its limit is not listed")
	assert.True(t, strings.HasSuffix(out,
		"\nwarning: src/checkout.ts: unsupported syntax at 42:7, unit skipped\n"))
	assert.NotContains(t, out, "partial=")
}

func TestConsoleAllListsEveryUnit(t *testing.T) {
	out := renderAll(t, config.FormatConsole, fullRun())
	assert.Contains(t, out,
		"\nunit: src/checkout.ts:1:1 function greet icp=2.5 limit=10\n"+
			"  metrics: code_branch=2 external_coupling=1x0.5\n")
	assert.Contains(t, out,
		"\nunit: src/money.ts:3:1 function formatMoney icp=2 limit=10\n"+
			"  metrics: code_branch=1 condition=1\n")
	assert.Less(t, strings.Index(out, "violation:"), strings.Index(out, "unit:"),
		"the violations come first")
}

func TestConsolePassAndPartial(t *testing.T) {
	out := render(t, config.FormatConsole, partialRun())
	assert.Equal(t,
		"cdd check: PASS violations=0 units=0 blocked=false root=/projects/shop elapsed=1.234s partial=true\n",
		out,
	)
}

func TestReportsRenderTheSharedBlockingOutcome(t *testing.T) {
	tests := map[string]struct {
		res            analyze.RunResult
		consoleStatus  string
		markdownPhrase string
	}{
		"pass": {
			res:            analyze.RunResult{Root: "/projects/shop"},
			consoleStatus:  "PASS",
			markdownPhrase: "Violations do not block this run.",
		},
		"warn": {
			res: analyze.RunResult{
				Root:  "/projects/shop",
				Files: []analyze.FileReport{{Units: []analyze.UnitReport{overUnit("warn", 11, 1)}}},
			},
			consoleStatus:  "WARN",
			markdownPhrase: "Violations do not block this run.",
		},
		"fail": {
			res: analyze.RunResult{
				Root:    "/projects/shop",
				Blocked: true,
				Files:   []analyze.FileReport{{Units: []analyze.UnitReport{overUnit("fail", 11, 1)}}},
			},
			consoleStatus:  "FAIL",
			markdownPhrase: "Violations block this run.",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			console := render(t, config.FormatConsole, tt.res)
			assert.Contains(t, console, "cdd check: "+tt.consoleStatus)
			assert.Contains(t, console, "blocked="+strconv.FormatBool(tt.res.Blocked))

			assert.Contains(t, render(t, config.FormatJSON, tt.res), `"blocked": `+strconv.FormatBool(tt.res.Blocked))
			assert.Contains(t, render(t, config.FormatXML, tt.res), `blocked="`+strconv.FormatBool(tt.res.Blocked)+`"`)
			assert.Contains(t, render(t, config.FormatMarkdown, tt.res), tt.markdownPhrase)
		})
	}
}

func TestConsoleOrdersViolationsByHowFarOver(t *testing.T) {
	res := analyze.RunResult{
		Root: "/projects/shop",
		Files: []analyze.FileReport{
			{Path: "src/a.ts", Language: fixtureLanguage, Units: []analyze.UnitReport{
				overUnit("small", 11, 3), overUnit("tied", 12, 9),
			}},
			{Path: "src/b.ts", Language: fixtureLanguage, Units: []analyze.UnitReport{
				overUnit("worst", 20, 1), overUnit("alsoTied", 12, 4),
			}},
		},
	}
	assert.Equal(t, []string{"worst", "tied", "alsoTied", "small"}, consoleNames(render(t, config.FormatConsole, res)),
		"widest gap first, then path, then line")
}

// consoleNames reads the unit names out of a console report, in the order
// the records appear.
func consoleNames(out string) []string {
	var names []string
	for line := range strings.SplitSeq(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 4 && strings.HasSuffix(fields[0], ":") {
			names = append(names, fields[3])
		}
	}
	return names
}

// overUnit is a unit of total ICPs over a limit of 10, declared at line.
func overUnit(name string, total float64, line int) analyze.UnitReport {
	return analyze.UnitReport{
		Name: name, Kind: "function", Line: line, Col: 1,
		Counts: map[config.MetricID]int{config.MetricCondition: int(total)},
		Scores: map[config.MetricID]float64{config.MetricCondition: total},
		Total:  total, Limit: 10, Exceeds: true,
	}
}

func TestConsoleMetrics(t *testing.T) {
	branch, condition := string(config.MetricCodeBranch), string(config.MetricCondition)
	assert.Equal(t, metricsNone, consoleMetrics(nil))
	assert.Equal(t, metricsNone, consoleMetrics([]Metric{{ID: branch}}), "a metric never seen is left out")
	assert.Equal(t, branch+"=4", consoleMetrics([]Metric{{ID: branch, Count: 4, Score: 4}}))
	assert.Equal(t, branch+"=3x0.5", consoleMetrics([]Metric{{ID: branch, Count: 3, Score: 1.5}}))
	assert.Equal(t, condition+"=2 "+branch+"=4x0.25",
		consoleMetrics([]Metric{{ID: branch, Count: 4, Score: 1}, {ID: condition, Count: 2, Score: 2}}),
		"the heaviest metric first")
	assert.Equal(t, branch+"=2 "+condition+"=2",
		consoleMetrics([]Metric{{ID: branch, Count: 2, Score: 2}, {ID: condition, Count: 2, Score: 2}}),
		"an equal score keeps the canonical order")
}

func TestOverLimit(t *testing.T) {
	assert.InDelta(t, 4.5, overLimit(Unit{Total: 14.5, Limit: 10}), 0)
	assert.InDelta(t, -1.0, overLimit(Unit{Total: 9, Limit: 10}), 0)
}

func TestMarkdownShapesTheRun(t *testing.T) {
	out := renderAll(t, config.FormatMarkdown, fullRun())
	assert.True(t, strings.HasPrefix(out, "# cdd check\n"))
	assert.Contains(t, out, "| Unit | Kind | Location | ICPs | Limit | Status |\n")
	assert.Contains(t, out, "| `greet` | function | 1:1 | 2.5 | 10 | ok |\n")
	assert.Contains(t, out, "| **`CheckoutService`** | class | 12:3 | **14.5** | 10 | **over limit** |\n")
	assert.Contains(t, out, "- unsupported syntax at 42:7, unit skipped\n")
	assert.Contains(t, out, "\n## Summary\n")
	assert.True(t, strings.HasSuffix(out, "3 units analyzed, 1 over limit, elapsed 1.234s.\n"))
}

func TestMarkdownSkippedFile(t *testing.T) {
	res := analyze.RunResult{
		Root: "/projects/shop",
		Files: []analyze.FileReport{{
			Path:     "src/broken.ts",
			Language: fixtureLanguage,
			Warnings: []string{"parse error at 1:1"},
		}},
		Elapsed: fixtureElapsed,
	}
	out := render(t, config.FormatMarkdown, res)
	assert.Contains(t, out, "No unit was measured.\n")
	assert.Contains(t, out, "- parse error at 1:1\n")
	assert.NotContains(t, out, "| Unit |")
}

func TestMarkdownListsOnlyCountedMetrics(t *testing.T) {
	unit := func(counts map[config.MetricID]int) analyze.UnitReport {
		return analyze.UnitReport{
			Name: "noop", Kind: "function", Line: 1, Col: 1, Limit: 10,
			Counts: counts,
			Scores: map[config.MetricID]float64{config.MetricCodeBranch: 0, config.MetricCondition: 2},
		}
	}
	res := func(u analyze.UnitReport) analyze.RunResult {
		return analyze.RunResult{
			Root: "/projects/shop",
			Files: []analyze.FileReport{{
				Path:     "src/empty.ts",
				Language: fixtureLanguage,
				Units:    []analyze.UnitReport{u},
			}},
		}
	}
	t.Run("a unit that scored on nothing reads none", func(t *testing.T) {
		out := renderAll(t, config.FormatMarkdown, res(unit(nil)))
		assert.Contains(t, out, "- `noop` — none\n")
	})
	t.Run("a metric that never occurred is left out", func(t *testing.T) {
		out := renderAll(t, config.FormatMarkdown, res(unit(map[config.MetricID]int{
			config.MetricCodeBranch: 0,
			config.MetricCondition:  2,
		})))
		assert.Contains(t, out, "- `noop` — condition 2×1=2\n")
		assert.NotContains(t, out, "code_branch")
	})
}

func TestXMLShapesTheRun(t *testing.T) {
	out := render(t, config.FormatXML, fullRun())
	assert.True(t, strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`))
	assert.Contains(t, out,
		`<report root="/projects/shop" blocked="true" filter="violations" `+
			`explain="false" partial="false" elapsed_ms="1234">`)
	assert.Contains(t, out, `<summary units="3" violations="1">`)
	assert.Contains(t, out, `<metric id="external_coupling" count="11" score="5.5">`)
	assert.True(t, strings.HasSuffix(out, "</report>\n"))
}

func TestEscapeCell(t *testing.T) {
	assert.Equal(t, `a\|b`, escapeCell("a|b"))
	assert.Equal(t, "plain", escapeCell("plain"))
}

func TestFormatNumber(t *testing.T) {
	assert.Equal(t, "2", formatNumber(2))
	assert.Equal(t, "2.5", formatNumber(2.5))
	assert.Equal(t, "0", formatNumber(0))
	assert.Equal(t, "0.25", formatNumber(0.25))
}

func TestFormatElapsed(t *testing.T) {
	assert.Equal(t, "1.234s", formatElapsed(1234))
	assert.Equal(t, "0s", formatElapsed(0))
	assert.Equal(t, (5 * time.Minute).String(), formatElapsed(300000))
}

func TestMetricText(t *testing.T) {
	id := string(config.MetricCodeBranch)
	assert.Equal(t, id+" 4×1=4", metricText(Metric{ID: id, Count: 4, Score: 4}))
	assert.Equal(t, id+" 3×0.5=1.5", metricText(Metric{ID: id, Count: 3, Score: 1.5}))
	assert.Equal(t, id+" 0", metricText(Metric{ID: id}))
}
