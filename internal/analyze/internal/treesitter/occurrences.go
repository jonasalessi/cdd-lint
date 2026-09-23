package treesitter

import (
	"cmp"
	"slices"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
)

// SortOccurrences orders occurrences by position, in place. A walk yields
// the constructs inside a unit in source order already, but a leaf clause is
// charged before the constructs of an earlier sibling it was flattened out
// of, and coupling charges point at import statements above the unit, so
// the whole slice is ordered. The sort is stable, so two constructs starting
// at the same place keep the order the walk gave them: the `if` before the
// clauses of its own condition.
func SortOccurrences(occurrences []analyze.Occurrence) {
	slices.SortStableFunc(occurrences, func(a, b analyze.Occurrence) int {
		if a.Line != b.Line {
			return cmp.Compare(a.Line, b.Line)
		}
		return cmp.Compare(a.Col, b.Col)
	})
}
