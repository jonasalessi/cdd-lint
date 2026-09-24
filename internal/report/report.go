// Package report renders the outcome of an analysis run in the formats
// reporter.format allows: console, json, xml and markdown. Every format
// carries the same document — the model in schema.go — so switching format
// changes the shape of the output, never its content.
package report

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// outputFileMode is the permission of a report written to a file. A report
// is meant to be read by humans and by CI, never kept secret.
const outputFileMode = 0o644

// metricsNone stands in for the metric list of a unit that scored on no
// metric at all; every format spells that the same way.
const metricsNone = "none"

// The values of the document's filter field: which units the report lists.
// They are not part of the configuration vocabulary, they only tell a reader
// why a unit is missing from the document.
const (
	filterViolations = "violations"
	filterAll        = "all"
)

// Options selects what a report lists.
type Options struct {
	// All lists every unit; the default lists only units above their limit.
	All bool
	// Explain lists every counted construct of each listed unit with its
	// position and score, for editors and plugins. It details whatever All
	// listed; the two filters are independent.
	Explain bool
}

// filterName spells the filter opts stands for.
func filterName(opts Options) string {
	if opts.All {
		return filterAll
	}
	return filterViolations
}

// listedUnits are the units of one file a report lists: every unit when
// opts.All is set, otherwise only the ones above their limit. Every format
// lists the same units because every format renders the document this
// filter produced.
func listedUnits(f analyze.FileReport, opts Options) []analyze.UnitReport {
	if opts.All {
		return f.Units
	}
	out := make([]analyze.UnitReport, 0, len(f.Units))
	for _, u := range f.Units {
		if u.Exceeds {
			out = append(out, u)
		}
	}
	return out
}

// Write renders res to w in the given format. An unknown format is an error
// naming it.
func Write(w io.Writer, format string, res analyze.RunResult, opts Options) error {
	doc := newReport(res, opts)
	switch format {
	case config.FormatConsole:
		return renderConsole(w, doc)
	case config.FormatJSON:
		return renderJSON(w, doc)
	case config.FormatXML:
		return renderXML(w, doc)
	case config.FormatMarkdown:
		return renderMarkdown(w, doc)
	default:
		return fmt.Errorf("unknown reporter format %q", format)
	}
}

// Emit renders res as r asks and returns the path it wrote. A nil
// r.OutputFile means stdout and the returned path is empty; otherwise the
// file is created or truncated, its parent directory must already exist,
// and the caller can print the returned path as a receipt.
func Emit(stdout io.Writer, r config.Reporter, res analyze.RunResult, opts Options) (string, error) {
	if r.OutputFile == nil {
		return "", Write(stdout, r.Format, res, opts)
	}
	var buf bytes.Buffer
	if err := Write(&buf, r.Format, res, opts); err != nil {
		return "", err
	}
	path, err := outputPath(res.Root, *r.OutputFile)
	if err != nil {
		return "", err
	}
	if err := writeFile(res.Root, path, buf.Bytes()); err != nil {
		return "", err
	}
	return path, nil
}

// outputPath resolves the configured file against root. The configuration
// may come from a repository the user did not write, so the report never
// lands outside the project: an absolute path or a way out through ".." is
// refused before anything is touched.
func outputPath(root, configured string) (string, error) {
	if filepath.IsAbs(configured) {
		return "", fmt.Errorf("outputFile %s: must be relative to the configuration, inside the project", configured)
	}
	path := filepath.Join(root, configured)
	if !within(root, path) {
		return "", fmt.Errorf("outputFile %s: leaves the project directory", configured)
	}
	return path, nil
}

// within reports whether path is root or lies under it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeFile creates or truncates path. The report is rendered before the
// file is touched, so a format error never leaves an empty file behind. A
// symlink in the parent directory or as the target itself could lead out of
// root, so both are refused.
func writeFile(root, path string, data []byte) error {
	if err := checkOutputDir(root, filepath.Dir(path)); err != nil {
		return err
	}
	if err := checkOutputTarget(path); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, outputFileMode); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

// checkOutputDir requires dir to exist, be a directory and still lie under
// root once every symlink on both sides is resolved.
func checkOutputDir(root, dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("output directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("output directory %s: not a directory", dir)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("output directory %s: %w", dir, err)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("output directory %s: %w", dir, err)
	}
	if !within(realRoot, realDir) {
		return fmt.Errorf("output directory %s: a symlink leaves the project directory", dir)
	}
	return nil
}

// checkOutputTarget refuses an existing target that is a symlink; a missing
// or regular file is fine.
func checkOutputTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("output file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("output file %s: is a symlink", path)
	}
	return nil
}

// formatNumber renders an ICP value with as few digits as possible: 2 reads
// "2" and 2.5 reads "2.5". Every format uses it, so a total is spelled the
// same way everywhere.
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatElapsed renders a duration in milliseconds the way Go spells
// durations, e.g. "1.234s".
func formatElapsed(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).String()
}

// metricText spells one metric for a human: "code_branch 4×0.5=2", or the
// bare count when the metric is enabled but never occurred, since no weight
// was applied to it.
func metricText(m Metric) string {
	if m.Count == 0 {
		return m.ID + " 0"
	}
	weight := m.Score / float64(m.Count)
	return fmt.Sprintf("%s %d×%s=%s", m.ID, m.Count, formatNumber(weight), formatNumber(m.Score))
}

// occurrenceText spells one counted construct: the editor range it covers,
// the metric it was counted under and what it added to the unit, e.g.
// "12:3-14:4 code_branch +1". The console and markdown reports both open
// the line their own way and share this tail.
func occurrenceText(o Occurrence) string {
	return fmt.Sprintf("%d:%d-%d:%d %s +%s", o.Line, o.Col, o.EndLine, o.EndCol, o.Metric, formatNumber(o.Score))
}

// printer writes a report linearly and remembers the first failure, so a
// renderer checks for an error once instead of after every line.
type printer struct {
	w   io.Writer
	err error
}

// printf writes one formatted chunk unless an earlier write already failed.
func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}
