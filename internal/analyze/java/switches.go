package java

import (
	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// switchArms charges the arms of a switch, in the arrow form and in the
// old fallthrough form.
type switchArms struct {
	g *grammar
	l *ledger
}

// countRule charges an arrow-form arm, `case 1, 2 -> ...`, unless it is the
// `default` arm (FR-10).
func (s *switchArms) countRule(n *ts.Node) {
	if s.testsAValue(n) {
		s.l.charge(config.MetricCodeBranch, n)
	}
}

// countArm charges an old-style switch arm, which the grammar spreads over
// more than one node: `case 1: case 2: stmt;` parses as two
// switch_block_statement_groups, the first holding only its label and the
// second holding its own label plus the statements. So an arm is the run of
// label-only groups that fall through, plus the group that carries the code,
// and it is charged once, on the group that ends it, from the first label of
// the run. That makes `case 1: case 2: stmt` one branch, like the arrow
// form's `case 1, 2 ->`, and `default: case 1: stmt` one too, because the
// arm still tests a value. An arm of `default` alone is none (FR-10).
func (s *switchArms) countArm(n *ts.Node) {
	if s.isLabelOnly(n) {
		return
	}
	first, tested := n, s.testsAValue(n)
	for g := s.prevGroup(n); g != nil && s.isLabelOnly(g); g = s.prevGroup(g) {
		first, tested = g, tested || s.testsAValue(g)
	}
	if !tested {
		return
	}
	start, end := treesitter.SpanOf(first), treesitter.SpanOf(n)
	s.l.chargeSpan(config.MetricCodeBranch, treesitter.Span{
		Line: start.Line, Col: start.Col, EndLine: end.EndLine, EndCol: end.EndCol,
	})
}

// prevGroup returns the arm before n inside the switch block, skipping the
// comments a reader may have written between the labels of a fallthrough.
func (s *switchArms) prevGroup(n *ts.Node) *ts.Node {
	for g := n.PrevNamedSibling(); g != nil; g = g.PrevNamedSibling() {
		if !s.isComment(g) {
			return g
		}
	}
	return nil
}

// testsAValue reports whether a switch arm's label names a value to match,
// rather than being the `default` every unmatched value falls into. The
// keyword is an anonymous child of the switch_label.
func (s *switchArms) testsAValue(n *ts.Node) bool {
	label := s.g.childOfKind(n, kindSwitchLabel)
	return label != nil && !hasToken(label, s.g.tokens.defaultKeyword)
}

// isLabelOnly reports whether a switch group holds labels and nothing else,
// which is how the grammar writes a `case` that falls through into the arm
// below it.
func (s *switchArms) isLabelOnly(n *ts.Node) bool {
	if s.g.kindOf(n) != kindSwitchBlockStatementGroup {
		return false
	}
	for _, child := range treesitter.NamedChildren(n) {
		statement := child
		if s.isStatement(&statement) {
			return false
		}
	}
	return true
}

// isStatement reports whether a child of a switch group is code the arm runs,
// rather than one of its labels or a comment written between them.
func (s *switchArms) isStatement(n *ts.Node) bool {
	return s.g.kindOf(n) != kindSwitchLabel && !s.isComment(n)
}

// isComment reports whether n is a comment of either form.
func (s *switchArms) isComment(n *ts.Node) bool {
	k := s.g.kindOf(n)
	return k == kindLineComment || k == kindBlockComment
}
