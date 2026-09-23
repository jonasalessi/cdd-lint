package report

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// update rewrites the golden files from the fixtures below; UPDATE_GOLDEN=1
// does the same.
var update = flag.Bool("update", false, "rewrite golden files")

func updateGolden() bool {
	return *update || os.Getenv("UPDATE_GOLDEN") == "1"
}

// goldenNames maps a reporter format to the base name of its golden files;
// a partial run adds the "-partial" suffix.
var goldenNames = map[string]string{
	config.FormatConsole:  "console.txt",
	config.FormatJSON:     "report.json",
	config.FormatXML:      "report.xml",
	config.FormatMarkdown: "report.md",
}

// goldenPath locates the golden file of format for one of the fixtures.
func goldenPath(format, suffix string) string {
	name := goldenNames[format]
	ext := filepath.Ext(name)
	return filepath.Join("testdata", "golden", name[:len(name)-len(ext)]+suffix+ext)
}

// fixtureElapsed is fixed so the goldens never depend on the clock.
const fixtureElapsed = 1234 * time.Millisecond

// fixtureLanguage is the language id the fixture files carry; the report
// package never interprets it.
const fixtureLanguage = config.Language("typescript")

// goldenCase is one golden fixture: the run it renders and the options it
// renders with.
type goldenCase struct {
	res  analyze.RunResult
	opts Options
}

// goldenCases pairs every golden-file suffix with the report it renders.
func goldenCases() map[string]goldenCase {
	return map[string]goldenCase{
		"":         {res: fullRun()},
		"-all":     {res: fullRun(), opts: Options{All: true}},
		"-partial": {res: partialRun()},
		"-explain": {res: fullRun(), opts: Options{All: true, Explain: true}},
	}
}

// occurrence builds one weighed construct for the fixtures.
func occurrence(id config.MetricID, line, col, endLine, endCol, count int, score float64) analyze.OccurrenceReport {
	return analyze.OccurrenceReport{
		Occurrence: analyze.Occurrence{
			Metric: id, Line: line, Col: col, EndLine: endLine, EndCol: endCol, Count: count,
		},
		Score: score,
	}
}

// fullRun is a finished blocking run over two files: three units, one of them
// over its limit, and one file warning.
func fullRun() analyze.RunResult {
	return analyze.RunResult{
		Root:    "/projects/shop",
		Blocked: true,
		Files: []analyze.FileReport{
			{
				Path:     "src/checkout.ts",
				Language: fixtureLanguage,
				Units: []analyze.UnitReport{
					greetUnit(),
					checkoutServiceUnit(),
				},
				Warnings: []string{"unsupported syntax at 42:7, unit skipped"},
			},
			{
				Path:     "src/money.ts",
				Language: fixtureLanguage,
				Units:    []analyze.UnitReport{formatMoneyUnit()},
			},
		},
		Elapsed: fixtureElapsed,
	}
}

// partialRun is a run the timeout cut short before any file was analyzed.
func partialRun() analyze.RunResult {
	return analyze.RunResult{
		Root:    "/projects/shop",
		Partial: true,
		Elapsed: fixtureElapsed,
	}
}

// greetUnit stays within its limit and enables a metric it never uses.
func greetUnit() analyze.UnitReport {
	return analyze.UnitReport{
		Name: "greet", Kind: "function", Line: 1, Col: 1,
		Counts: map[config.MetricID]int{
			config.MetricCodeBranch:       2,
			config.MetricCondition:        0,
			config.MetricExternalCoupling: 1,
		},
		Scores: map[config.MetricID]float64{
			config.MetricCodeBranch:       2,
			config.MetricCondition:        0,
			config.MetricExternalCoupling: 0.5,
		},
		Total: 2.5, Limit: 10,
		Occurrences: []analyze.OccurrenceReport{
			occurrence(config.MetricCodeBranch, 2, 3, 4, 4, 1, 1),
		},
	}
}

// checkoutServiceUnit is the unit over its limit.
func checkoutServiceUnit() analyze.UnitReport {
	return analyze.UnitReport{
		Name: "CheckoutService", Kind: "class", Line: 12, Col: 3,
		Counts: map[config.MetricID]int{
			config.MetricCodeBranch:       6,
			config.MetricCondition:        3,
			config.MetricExternalCoupling: 11,
		},
		Scores: map[config.MetricID]float64{
			config.MetricCodeBranch:       6,
			config.MetricCondition:        3,
			config.MetricExternalCoupling: 5.5,
		},
		Total: 14.5, Limit: 10, Exceeds: true,
		// A handful of the constructs the unit counted, in source order:
		// the import that couples the file, the branch, and the two clauses
		// of the branch's condition.
		Occurrences: []analyze.OccurrenceReport{
			occurrence(config.MetricExternalCoupling, 1, 1, 1, 30, 1, 0.5),
			occurrence(config.MetricCodeBranch, 12, 3, 14, 4, 1, 1),
			occurrence(config.MetricCondition, 12, 7, 12, 15, 1, 1),
			occurrence(config.MetricCondition, 12, 19, 12, 28, 1, 1),
		},
	}
}

// formatMoneyUnit is the only unit of the second file.
func formatMoneyUnit() analyze.UnitReport {
	return analyze.UnitReport{
		Name: "formatMoney", Kind: "function", Line: 3, Col: 1,
		Counts: map[config.MetricID]int{
			config.MetricCodeBranch: 1,
			config.MetricCondition:  1,
		},
		Scores: map[config.MetricID]float64{
			config.MetricCodeBranch: 1,
			config.MetricCondition:  1,
		},
		Total: 2, Limit: 10,
	}
}
