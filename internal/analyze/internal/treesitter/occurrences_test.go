package treesitter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
)

// TestSortOccurrences orders by line then column and keeps the order of
// two occurrences that start at the same place.
func TestSortOccurrences(t *testing.T) {
	occ := []analyze.Occurrence{
		{Metric: "c", Line: 3, Col: 5},
		{Metric: "a", Line: 1, Col: 9},
		{Metric: "b", Line: 3, Col: 5},
		{Metric: "d", Line: 3, Col: 1},
	}
	SortOccurrences(occ)
	var order []string
	for _, o := range occ {
		order = append(order, string(o.Metric))
	}
	require.Equal(t, []string{"a", "d", "c", "b"}, order)
}
