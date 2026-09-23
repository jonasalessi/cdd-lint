package report

import (
	"encoding/xml"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Report is the document every format renders and the schema of the json
// and xml outputs. It is the run seen from outside: where it ran, what it
// measured, what it could not measure, and how long it took.
type Report struct {
	XMLName xml.Name `json:"-" xml:"report"`
	// Root is the directory every File.Path is relative to.
	Root string `json:"root" xml:"root,attr"`
	// Blocked is true when the finalized result must fail the command.
	Blocked bool `json:"blocked" xml:"blocked,attr"`
	// Filter says which units the document lists: "violations" for the
	// units above their limit alone, "all" for every measured unit. The
	// summary always counts the whole run, so the two disagree by design.
	Filter string `json:"filter" xml:"filter,attr"`
	// Explain is true when every listed unit carries its counted
	// constructs, so a reader can tell a unit that counted nothing from a
	// document that was simply not asked for the detail.
	Explain bool `json:"explain" xml:"explain,attr"`
	// Partial is true when the timeout elapsed before every file was
	// analyzed: Files then holds only what was finished in time.
	Partial bool `json:"partial" xml:"partial,attr"`
	// ElapsedMS is the wall-clock time of the run in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms" xml:"elapsed_ms,attr"`
	// Files are the analyzed files in the order the run produced them.
	Files []File `json:"files" xml:"files>file"`
	// Warnings concern the run rather than one file.
	Warnings []string `json:"warnings,omitempty" xml:"warnings>warning,omitempty"`
	// Summary counts what the reader wants first.
	Summary Summary `json:"summary" xml:"summary"`
}

// File is one analyzed file. A file with warnings and no units was skipped.
type File struct {
	// Path is slash-separated and relative to Report.Root.
	Path string `json:"path" xml:"path,attr"`
	// Language is the canonical language id the analyzer was chosen by.
	Language string `json:"language" xml:"language,attr"`
	// Units are the measured units in source order.
	Units []Unit `json:"units" xml:"units>unit"`
	// Warnings are the analyzer's findings for this file.
	Warnings []string `json:"warnings,omitempty" xml:"warnings>warning,omitempty"`
}

// Unit is one measured code unit after weights and limits were applied.
type Unit struct {
	// Name identifies the unit for the reader.
	Name string `json:"name" xml:"name,attr"`
	// Kind says what the unit is, e.g. a class, a function or a file.
	Kind string `json:"kind" xml:"kind,attr"`
	// Line and Col locate the unit's declaration, 1-based.
	Line int `json:"line" xml:"line,attr"`
	Col  int `json:"col"  xml:"col,attr"`
	// Total is the sum of the metric scores: the unit's ICPs.
	Total float64 `json:"total" xml:"total,attr"`
	// Limit is the ICP limit resolved for the file.
	Limit int `json:"limit" xml:"limit,attr"`
	// Exceeds is Total > Limit.
	Exceeds bool `json:"exceeds" xml:"exceeds,attr"`
	// Metrics are the metrics enabled for the file, in the canonical order
	// of config.Metrics(). A metric disabled for the file is absent, a
	// metric that never occurred has count and score zero.
	Metrics []Metric `json:"metrics" xml:"metric"`
	// Occurrences are the unit's counted constructs in source order, and
	// nil when the document was built without Options.Explain — a nil list
	// is left out of the output, an empty one is rendered, so an explained
	// unit that counted nothing still says so.
	Occurrences *[]Occurrence `json:"occurrences,omitempty" xml:"occurrences>occurrence,omitempty"`
}

// constructs are the unit's counted constructs, and none at all when the
// document was built without Options.Explain.
func (u Unit) constructs() []Occurrence {
	if u.Occurrences == nil {
		return nil
	}
	return *u.Occurrences
}

// Occurrence is one counted construct of a unit: where it is and what it
// added to the unit's total. Positions are 1-based and EndLine and EndCol
// point just past the construct, like an editor range, so a plugin can turn
// one into an inline hint without reparsing the file.
type Occurrence struct {
	// Unit repeats the name of the unit the construct belongs to, so a
	// reader can flatten every occurrence of a file into a single list.
	Unit string `json:"unit" xml:"unit,attr"`
	// Metric is the canonical id of the metric the construct was counted
	// under.
	Metric string `json:"metric" xml:"metric,attr"`
	// Line and Col locate the construct's first character.
	Line int `json:"line" xml:"line,attr"`
	Col  int `json:"col"  xml:"col,attr"`
	// EndLine and EndCol point just past its last character.
	EndLine int `json:"end_line" xml:"end_line,attr"`
	EndCol  int `json:"end_col"  xml:"end_col,attr"`
	// Count is how many points the construct adds before weighting: 1 for
	// most, 2 for a clause pair.
	Count int `json:"count" xml:"count,attr"`
	// Score is Count multiplied by the metric's weight: what the construct
	// contributed to Unit.Total.
	Score float64 `json:"score" xml:"score,attr"`
}

// Metric is one metric of one unit. Score is Count multiplied by the weight
// the configuration gives the metric for that file, so the weight itself is
// Score divided by Count whenever Count is not zero.
type Metric struct {
	// ID is the canonical metric id.
	ID string `json:"metric" xml:"id,attr"`
	// Count is the raw number of occurrences, before any weight.
	Count int `json:"count" xml:"count,attr"`
	// Score is the weighed contribution of the metric to Unit.Total.
	Score float64 `json:"score" xml:"score,attr"`
}

// Summary counts the whole run.
type Summary struct {
	// Units is every measured unit.
	Units int `json:"units" xml:"units,attr"`
	// Violations is the units above their limit.
	Violations int `json:"violations" xml:"violations,attr"`
}

// newReport turns a run into the document every format renders. Only the
// units opts lists reach the document; a file left without a listed unit and
// without a warning has nothing to say and is dropped. The summary is read
// from the run itself, so filtering never changes what was measured.
func newReport(res analyze.RunResult, opts Options) Report {
	files := make([]File, 0, len(res.Files))
	for _, f := range res.Files {
		units := listedUnits(f, opts)
		if len(units) == 0 && len(f.Warnings) == 0 {
			continue
		}
		files = append(files, newFile(f, units, opts))
	}
	return Report{
		Root:      res.Root,
		Blocked:   res.Blocked,
		Filter:    filterName(opts),
		Explain:   opts.Explain,
		Partial:   res.Partial,
		ElapsedMS: res.Elapsed.Milliseconds(),
		Files:     files,
		Warnings:  res.Warnings,
		Summary:   Summary{Units: res.UnitCount(), Violations: res.Violations()},
	}
}

// newFile turns one weighed file into its document entry, listing the units
// the filter kept.
func newFile(f analyze.FileReport, listed []analyze.UnitReport, opts Options) File {
	units := make([]Unit, 0, len(listed))
	for _, u := range listed {
		units = append(units, newUnit(u, opts))
	}
	return File{
		Path:     f.Path,
		Language: string(f.Language),
		Units:    units,
		Warnings: f.Warnings,
	}
}

// newUnit turns one weighed unit into its document entry, its metrics
// ordered by config.Metrics() so every format lists them the same way.
func newUnit(u analyze.UnitReport, opts Options) Unit {
	metrics := make([]Metric, 0, len(u.Counts))
	for _, id := range config.Metrics() {
		count, ok := u.Counts[id]
		if !ok {
			continue
		}
		metrics = append(metrics, Metric{ID: string(id), Count: count, Score: u.Scores[id]})
	}
	return Unit{
		Name:        u.Name,
		Kind:        u.Kind,
		Line:        u.Line,
		Col:         u.Col,
		Total:       u.Total,
		Limit:       u.Limit,
		Exceeds:     u.Exceeds,
		Metrics:     metrics,
		Occurrences: newOccurrences(u, opts),
	}
}

// newOccurrences lists the constructs opts asks for: nil unless Explain is
// set, so a document that was not asked to explain carries no occurrence
// field at all, and an empty list for an explained unit that counted
// nothing.
func newOccurrences(u analyze.UnitReport, opts Options) *[]Occurrence {
	if !opts.Explain {
		return nil
	}
	out := make([]Occurrence, 0, len(u.Occurrences))
	for _, o := range u.Occurrences {
		out = append(out, newOccurrence(u.Name, o))
	}
	return &out
}

// newOccurrence turns one weighed construct into its document entry,
// repeating the name of the unit it belongs to.
func newOccurrence(unit string, o analyze.OccurrenceReport) Occurrence {
	return Occurrence{
		Unit:    unit,
		Metric:  string(o.Metric),
		Line:    o.Line,
		Col:     o.Col,
		EndLine: o.EndLine,
		EndCol:  o.EndCol,
		Count:   o.Count,
		Score:   o.Score,
	}
}
