package golang

import (
	"go/ast"
	"go/token"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ledger accumulates the raw ICP counts of one unit while the subtrees it
// owns are walked. It counts every metric, enabled or not: the pipeline
// drops the ones the configuration disables (FR-4).
//
// Every count is charged through charge or chargeSpan, which record where it
// came from, so a unit's Counts is always the sum of its Occurrences' Count,
// metric by metric.
type ledger struct {
	fset   *token.FileSet
	counts map[config.MetricID]int
	// occurrences locate every charge, in the order it was made. ast.Inspect
	// walks in pre-order, so they come out in source order except for the
	// leaf clauses flattened out of a chain; sortedOccurrences orders them.
	occurrences []analyze.Occurrence
}

// newLedger returns a ledger with every metric at zero.
func newLedger(fset *token.FileSet) *ledger {
	return &ledger{fset: fset, counts: zeroCounts()}
}

// zeroCounts returns a map holding every metric at zero. A unit always
// carries a key for every metric, enabled or not: the pipeline drops the
// ones the configuration disables (FR-4).
func zeroCounts() map[config.MetricID]int {
	counts := make(map[config.MetricID]int, len(config.Metrics()))
	for _, m := range config.Metrics() {
		counts[m] = 0
	}
	return counts
}

// charge adds one point of metric to the unit and records n's range as where
// it comes from. Every Go construct is worth one point: the language has no
// form that folds two decisions into one node.
func (l *ledger) charge(metric config.MetricID, n ast.Node) {
	l.chargeSpan(metric, spanOf(l.fset, n))
}

// chargeSpan is charge for a range the caller computed itself, which is how
// a charge that points at part of a node is located.
func (l *ledger) chargeSpan(metric config.MetricID, s treesitter.Span) {
	l.counts[metric]++
	l.occurrences = append(l.occurrences, analyze.Occurrence{
		Metric:  metric,
		Line:    s.Line,
		Col:     s.Col,
		EndLine: s.EndLine,
		EndCol:  s.EndCol,
		Count:   1,
	})
}

// sortedOccurrences returns the unit's occurrences in source order.
func (l *ledger) sortedOccurrences() []analyze.Occurrence {
	treesitter.SortOccurrences(l.occurrences)
	return l.occurrences
}

// spanOf returns n's range the way analyze.Occurrence carries it. go/token
// positions are 1-based with byte columns and End is already exclusive, so
// they are the contract treesitter.SpanOf produces and spans compare across
// languages. The imports need it without a ledger, which is why it stands
// on its own.
func spanOf(fset *token.FileSet, n ast.Node) treesitter.Span {
	start, end := fset.Position(n.Pos()), fset.Position(n.End())
	return treesitter.Span{
		Line:    start.Line,
		Col:     start.Column,
		EndLine: end.Line,
		EndCol:  end.Column,
	}
}
