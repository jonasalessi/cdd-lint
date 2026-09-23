package analyze

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

func TestNewWeightsRejectsAnInvalidPattern(t *testing.T) {
	_, err := newWeights(config.PatternWeights{{Pattern: "("}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `pattern "("`)
}

func TestNewLimitsRejectsAnInvalidPattern(t *testing.T) {
	_, err := newLimits(config.PatternLimits{{Pattern: "[", Limit: 10}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `pattern "["`)
}

func TestWeightsForMergesEveryMatchingPatternInOrder(t *testing.T) {
	weights, err := newWeights(config.PatternWeights{
		{Pattern: config.PatternAll, Weights: map[config.MetricID]float64{
			config.MetricCodeBranch:       1,
			config.MetricCondition:        1,
			config.MetricInternalCoupling: 1,
		}},
		{Pattern: ".*/dto/.*", Weights: map[config.MetricID]float64{
			config.MetricInternalCoupling: 0.5,
		}},
		{Pattern: `.*Dto\.ts`, Weights: map[config.MetricID]float64{
			config.MetricCondition: 2,
		}},
	})
	require.NoError(t, err)

	tests := map[string]map[config.MetricID]float64{
		"src/app/service.ts": {
			config.MetricCodeBranch:       1,
			config.MetricCondition:        1,
			config.MetricInternalCoupling: 1,
		},
		"src/dto/order.ts": {
			config.MetricCodeBranch:       1,
			config.MetricCondition:        1,
			config.MetricInternalCoupling: 0.5,
		},
		"src/dto/OrderDto.ts": {
			config.MetricCodeBranch:       1,
			config.MetricCondition:        2,
			config.MetricInternalCoupling: 0.5,
		},
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			assert.Equal(t, want, weights.For(path))
		})
	}
}

func TestWeightsForReturnsAFreshMap(t *testing.T) {
	weights, err := newWeights(config.PatternWeights{
		{Pattern: config.PatternAll, Weights: map[config.MetricID]float64{config.MetricCodeBranch: 1}},
	})
	require.NoError(t, err)

	first := weights.For("a.ts")
	first[config.MetricCondition] = 99
	assert.NotContains(t, weights.For("a.ts"), config.MetricCondition)
}

func TestWeightsForWithoutAnyMatchDisablesEveryMetric(t *testing.T) {
	weights, err := newWeights(config.PatternWeights{
		{Pattern: `^src/`, Weights: map[config.MetricID]float64{config.MetricCodeBranch: 1}},
	})
	require.NoError(t, err)
	assert.Empty(t, weights.For("lib/a.ts"))
}

func TestLimitsForTakesTheLastMatch(t *testing.T) {
	limits, err := newLimits(config.PatternLimits{
		{Pattern: config.PatternAll, Limit: 10},
		{Pattern: ".*/adapters/.*", Limit: 8},
		{Pattern: ".*/adapters/legacy/.*", Limit: 25},
	})
	require.NoError(t, err)

	assert.Equal(t, 10, limits.For("src/app/service.ts"))
	assert.Equal(t, 8, limits.For("src/adapters/http.ts"))
	assert.Equal(t, 25, limits.For("src/adapters/legacy/soap.ts"))
}

func TestLimitsForWithoutAnyMatchIsZero(t *testing.T) {
	limits, err := newLimits(nil)
	require.NoError(t, err)
	assert.Equal(t, 0, limits.For("a.ts"))
}

func TestScoreMultipliesOnlyTheEnabledMetrics(t *testing.T) {
	unit := Unit{
		Name: "Order",
		Kind: kindClass,
		Line: 3,
		Col:  1,
		Counts: map[config.MetricID]int{
			config.MetricCodeBranch:       2,
			config.MetricCondition:        3,
			config.MetricExternalCoupling: 5,
			config.MetricLambda:           4,
		},
	}
	weights := map[config.MetricID]float64{
		config.MetricCodeBranch:       1,
		config.MetricCondition:        1,
		config.MetricExternalCoupling: 0.5,
	}

	got := score(unit, weights, 10)

	assert.Equal(t, "Order", got.Name)
	assert.Equal(t, kindClass, got.Kind)
	assert.Equal(t, 3, got.Line)
	assert.Equal(t, 1, got.Col)
	assert.Equal(t, map[config.MetricID]int{
		config.MetricCodeBranch:       2,
		config.MetricCondition:        3,
		config.MetricExternalCoupling: 5,
	}, got.Counts, "lambda is disabled and never reported")
	assert.Equal(t, map[config.MetricID]float64{
		config.MetricCodeBranch:       2,
		config.MetricCondition:        3,
		config.MetricExternalCoupling: 2.5,
	}, got.Scores)
	assert.InDelta(t, 7.5, got.Total, 0)
	assert.Equal(t, 10, got.Limit)
	assert.False(t, got.Exceeds)
}

func TestScoreExceedsOnlyAboveTheLimit(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		limit   int
		total   float64
		exceeds bool
	}{
		{name: "below", count: 9, limit: 10, total: 9, exceeds: false},
		{name: "equal", count: 10, limit: 10, total: 10, exceeds: false},
		{name: "above", count: 11, limit: 10, total: 11, exceeds: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit := Unit{Counts: map[config.MetricID]int{config.MetricCodeBranch: tt.count}}
			got := score(unit, map[config.MetricID]float64{config.MetricCodeBranch: 1}, tt.limit)
			assert.InDelta(t, tt.total, got.Total, 0)
			assert.Equal(t, tt.exceeds, got.Exceeds)
		})
	}
}

func TestScoreKeepsHalfPoints(t *testing.T) {
	unit := Unit{Counts: map[config.MetricID]int{config.MetricExternalCoupling: 5}}
	got := score(unit, map[config.MetricID]float64{config.MetricExternalCoupling: 0.5}, 10)
	assert.InDelta(t, 2.5, got.Total, 0)
}

func TestScoreWithoutCountsIsEmptyButNotNil(t *testing.T) {
	got := score(Unit{Name: "empty"}, map[config.MetricID]float64{config.MetricCodeBranch: 1}, 10)
	assert.NotNil(t, got.Counts)
	assert.NotNil(t, got.Scores)
	assert.Empty(t, got.Counts)
	assert.InDelta(t, 0.0, got.Total, 0)
}

func TestScoreIgnoresAnUnknownMetric(t *testing.T) {
	unit := Unit{Counts: map[config.MetricID]int{"made_up": 7, config.MetricCodeBranch: 1}}
	got := score(unit, map[config.MetricID]float64{"made_up": 1, config.MetricCodeBranch: 1}, 10)
	assert.Equal(t, map[config.MetricID]int{config.MetricCodeBranch: 1}, got.Counts)
	assert.InDelta(t, 1.0, got.Total, 0)
}

func TestNewResolverReportsTheSectionOfTheInvalidPattern(t *testing.T) {
	tests := map[string]*config.Config{
		"metrics.alpha": {
			Metrics:   map[config.Language]config.PatternWeights{langAlpha: {{Pattern: "("}}},
			ICPLimits: map[config.Language]config.PatternLimits{langAlpha: {{Pattern: config.PatternAll, Limit: 10}}},
		},
		"icp-limits.alpha": {
			Metrics:   map[config.Language]config.PatternWeights{langAlpha: {{Pattern: config.PatternAll}}},
			ICPLimits: map[config.Language]config.PatternLimits{langAlpha: {{Pattern: "(", Limit: 10}}},
		},
	}
	for section, cfg := range tests {
		t.Run(section, func(t *testing.T) {
			_, err := newResolver(cfg, langAlpha)
			require.Error(t, err)
			assert.Contains(t, err.Error(), section)
		})
	}
}

func TestResolverResolveScoresEveryUnitOfTheFile(t *testing.T) {
	cfg := &config.Config{
		Metrics: map[config.Language]config.PatternWeights{langAlpha: {
			{Pattern: config.PatternAll, Weights: map[config.MetricID]float64{config.MetricCodeBranch: 1}},
			{Pattern: ".*/dto/.*", Weights: map[config.MetricID]float64{config.MetricCodeBranch: 0.5}},
		}},
		ICPLimits: map[config.Language]config.PatternLimits{langAlpha: {
			{Pattern: config.PatternAll, Limit: 10},
			{Pattern: ".*/dto/.*", Limit: 20},
		}},
	}
	resolver, err := newResolver(cfg, langAlpha)
	require.NoError(t, err)

	units := []Unit{
		{Name: "A", Counts: map[config.MetricID]int{config.MetricCodeBranch: 4}},
		{Name: "B", Counts: map[config.MetricID]int{config.MetricCodeBranch: 30}},
	}

	app := resolver.Resolve("src/app/order.alpha", units)
	require.Len(t, app, 2)
	assert.InDelta(t, 4.0, app[0].Total, 0)
	assert.Equal(t, 10, app[0].Limit)
	assert.True(t, app[1].Exceeds)

	dto := resolver.Resolve("src/dto/order.alpha", units)
	require.Len(t, dto, 2)
	assert.InDelta(t, 2.0, dto[0].Total, 0)
	assert.Equal(t, 20, dto[0].Limit)
	assert.False(t, dto[1].Exceeds, "15 ICPs stay under the DTO limit of 20")
}

func TestResolverResolveWithoutUnitsIsNil(t *testing.T) {
	resolver, err := newResolver(&config.Config{}, langAlpha)
	require.NoError(t, err)
	assert.Nil(t, resolver.Resolve("a.alpha", nil))
}

// TestScoreWeighsTheOccurrencesOfTheEnabledMetrics pins the occurrence side
// of Score: an occurrence of a disabled metric is dropped, the rest keep the
// analyzer's source order, and each score is Count times the metric weight.
func TestScoreWeighsTheOccurrencesOfTheEnabledMetrics(t *testing.T) {
	branch := Occurrence{Metric: config.MetricCodeBranch, Line: 4, Col: 3, EndLine: 6, EndCol: 4, Count: 1}
	lambda := Occurrence{Metric: config.MetricLambda, Line: 5, Col: 10, EndLine: 5, EndCol: 20, Count: 1}
	imported := Occurrence{Metric: config.MetricExternalCoupling, Line: 1, Col: 1, EndLine: 1, EndCol: 25, Count: 1}
	pair := Occurrence{Metric: config.MetricCondition, Line: 4, Col: 7, EndLine: 4, EndCol: 13, Count: 2}

	tests := []struct {
		name    string
		unit    Unit
		weights map[config.MetricID]float64
		want    []OccurrenceReport
	}{
		{
			name:    "an occurrence of a disabled metric is dropped",
			unit:    Unit{Occurrences: []Occurrence{branch, lambda}},
			weights: map[config.MetricID]float64{config.MetricCodeBranch: 1},
			want:    []OccurrenceReport{{Occurrence: branch, Score: 1}},
		},
		{
			name: "source order is kept",
			unit: Unit{Occurrences: []Occurrence{imported, branch, lambda}},
			weights: map[config.MetricID]float64{
				config.MetricExternalCoupling: 1,
				config.MetricCodeBranch:       1,
				config.MetricLambda:           1,
			},
			want: []OccurrenceReport{
				{Occurrence: imported, Score: 1},
				{Occurrence: branch, Score: 1},
				{Occurrence: lambda, Score: 1},
			},
		},
		{
			name:    "a weight of one half halves the score",
			unit:    Unit{Occurrences: []Occurrence{imported}},
			weights: map[config.MetricID]float64{config.MetricExternalCoupling: 0.5},
			want:    []OccurrenceReport{{Occurrence: imported, Score: 0.5}},
		},
		{
			name:    "a count of two is weighed as a pair",
			unit:    Unit{Occurrences: []Occurrence{pair}},
			weights: map[config.MetricID]float64{config.MetricCondition: 0.5},
			want:    []OccurrenceReport{{Occurrence: pair, Score: 1}},
		},
		{
			name:    "a unit without occurrences reports none",
			unit:    Unit{},
			weights: map[config.MetricID]float64{config.MetricCodeBranch: 1},
			want:    nil,
		},
		{
			name:    "every metric disabled reports none",
			unit:    Unit{Occurrences: []Occurrence{branch, lambda}},
			weights: map[config.MetricID]float64{},
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := score(tt.unit, tt.weights, 10)
			assert.Len(t, got.Occurrences, len(tt.want))
			for i := range tt.want {
				assert.Equal(t, tt.want[i], got.Occurrences[i], "occurrence %d", i)
			}
		})
	}
}
