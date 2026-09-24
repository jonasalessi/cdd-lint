package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ledger accumulates the raw ICP counts of one unit while its subtree is
// walked. It counts every metric, enabled or not: the pipeline drops the
// ones the configuration disables.
//
// Every count is charged through charge or chargeSpan, which record where it
// came from, so a unit's Counts is always the sum of its Occurrences' Count,
// metric by metric.
type ledger struct {
	counts map[config.MetricID]int
	// occurrences locate every charge, in the order it was made. The walk is
	// a pre-order traversal, so they come out in source order except for the
	// coupling charges, which are added last; measure sorts them.
	occurrences []analyze.Occurrence
}

// newLedger returns a ledger with every metric at zero.
func newLedger() *ledger {
	return &ledger{counts: zeroCounts()}
}

// zeroCounts returns a map holding every metric at zero. A unit always
// carries a key for every metric, enabled or not: the pipeline drops the
// ones the configuration disables.
func zeroCounts() map[config.MetricID]int {
	counts := make(map[config.MetricID]int, len(config.Metrics()))
	for _, m := range config.Metrics() {
		counts[m] = 0
	}
	return counts
}

// charge adds one point of metric to the unit and records n's range as where
// it comes from. Every Java construct is worth one point: the language has no
// form that folds two decisions into one node.
func (l *ledger) charge(metric config.MetricID, n *ts.Node) {
	l.chargeSpan(metric, treesitter.SpanOf(n))
}

// chargeSpan is charge for a range that is not a node the caller still holds,
// which is how an `else` branch, a fallthrough arm or a coupling charge is
// located.
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
