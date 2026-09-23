// Package analyze is the language-agnostic analysis pipeline: the contract
// every language analyzer implements, the types a run produces, and the
// walk that turns a configuration plus a source tree into weighed, limited
// results. Analyzers count; the pipeline weighs, limits and reports.
package analyze

import (
	"context"
	"time"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Analyzer counts the ICP constructs of one source file. Implementations
// never read the configuration: weights, disabled metrics, limits and
// enforcement are applied downstream.
//
// An Analyzer is used by one goroutine at a time; the pipeline builds one per
// worker through Language.NewAnalyzer. An implementation that holds native
// resources also implements io.Closer, and the pipeline closes it when the
// worker exits.
type Analyzer interface {
	// Analyze parses src, the content of the file at path, and returns the
	// raw counts of every unit it contains. path is slash-separated and
	// relative to the run root; it is only used for messages.
	Analyze(ctx context.Context, path string, src []byte) (FileResult, error)
}

// Options is the only configuration-derived knowledge an analyzer receives.
type Options struct {
	// InternalPrefixes are the import specifier prefixes that count as
	// internal coupling, on top of what the language itself treats as local
	// (relative paths in TypeScript). The pipeline resolves them from
	// internal_coupling.packages plus LanguageSpec.DetectPackages when
	// auto_detect is on.
	InternalPrefixes []string
}

// Language pairs the data side of a language with its analyzer constructor.
// A nil NewAnalyzer means the language has no analyzer yet; a run that meets
// a file of that language reports an error, never zero ICPs.
type Language struct {
	Spec        config.LanguageSpec
	NewAnalyzer func(Options) Analyzer
}

// FileResult is the outcome of analyzing one file.
type FileResult struct {
	// Units are the measured code units in source order.
	Units []Unit
	// Warnings are non-fatal findings, e.g. a syntax error that made the
	// analyzer skip the file, or a construct it could not classify.
	Warnings []string
}

// Unit is one measured code unit: a type, a function or a file, depending
// on the language.
type Unit struct {
	// Name identifies the unit for the reader, e.g. a type or function name.
	Name string
	// Kind says what the unit is, e.g. "class", "function" or "file".
	Kind string
	// Line and Col locate the unit's declaration, 1-based.
	Line, Col int
	// Counts are raw occurrences per metric, before any weight is applied.
	// They equal the sum of Occurrences' Count per metric.
	Counts map[config.MetricID]int
	// Occurrences locate every counted construct, in source order, so an
	// editor can show where each point comes from.
	Occurrences []Occurrence
}

// Occurrence is one counted construct of a unit. Positions are 1-based;
// EndLine and EndCol point just past the construct, like an editor range.
// A coupling occurrence sits on the import statement that brings the
// dependency in, even though that statement is outside the unit.
type Occurrence struct {
	Metric          config.MetricID
	Line, Col       int
	EndLine, EndCol int
	// Count is how many points the construct adds before weighting: 1 for
	// most, 2 for a clause pair such as `y ||= b`.
	Count int
}

// Request is everything a run needs.
type Request struct {
	// Root is the directory the walk starts from and every path is relative
	// to: the directory of the configuration file.
	Root string
	// Config is the loaded, validated configuration.
	Config *config.Config
	// Languages is the registry; only the languages present in
	// Config.Metrics are analyzed.
	Languages []Language
	// Paths narrows the run to these files and directories, slash-separated
	// and relative to Root. Empty means the whole tree under Root. A file
	// named here must exist, belong to a configured language and pass the
	// include/exclude patterns; a directory is walked like Root would be.
	Paths []string
}

// RunResult is the outcome of one run over a project.
type RunResult struct {
	// Root is the directory every FileReport.Path is relative to.
	Root string
	// Blocked is true when the finalized result has violations and the
	// configured enforcement blocks on them.
	Blocked bool
	// Files are the analyzed files in path order.
	Files []FileReport
	// Warnings concern the run rather than one file, e.g. an enforcement
	// mode that is reported but not enforced yet.
	Warnings []string
	// Partial is true when the timeout elapsed before every file was
	// analyzed; Files then holds what was finished in time.
	Partial bool
	// Elapsed is the wall-clock time of the run.
	Elapsed time.Duration
}

// FileReport is the weighed outcome of one file.
type FileReport struct {
	// Path is slash-separated and relative to RunResult.Root.
	Path     string
	Language config.Language
	// Units are the measured units in source order.
	Units []UnitReport
	// Warnings are the analyzer's findings for this file; a file with a
	// warning and no units was skipped.
	Warnings []string
}

// UnitReport is one unit after weights and limits have been applied.
type UnitReport struct {
	Name string
	Kind string
	// Line and Col locate the unit's declaration, 1-based.
	Line, Col int
	// Counts are the raw occurrences of the metrics enabled for the file;
	// disabled metrics are absent.
	Counts map[config.MetricID]int
	// Scores are Counts multiplied by the configured weight, same keys.
	Scores map[config.MetricID]float64
	// Total is the sum of Scores: the unit's ICPs.
	Total float64
	// Limit is the ICP limit resolved for the file.
	Limit int
	// Exceeds is Total > Limit.
	Exceeds bool
	// Occurrences are the unit's counted constructs for the enabled
	// metrics, in source order, each with its weighted score.
	Occurrences []OccurrenceReport
}

// OccurrenceReport is one Occurrence after its weight has been applied.
type OccurrenceReport struct {
	Occurrence
	// Score is Count multiplied by the metric's weight.
	Score float64
}

// Violations counts the units above their limit.
func (r RunResult) Violations() int {
	n := 0
	for _, f := range r.Files {
		for _, u := range f.Units {
			if u.Exceeds {
				n++
			}
		}
	}
	return n
}

// UnitCount counts every measured unit.
func (r RunResult) UnitCount() int {
	n := 0
	for _, f := range r.Files {
		n += len(f.Units)
	}
	return n
}
