package analyze

import (
	"fmt"
	"maps"
	"regexp"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// weights are the metric weights one language configures, pattern by
// pattern, in the document order of cdd.config.yaml. Every pattern whose
// regex matches a file applies; a later entry merges its weights over the
// earlier ones, metric by metric.
type weights struct {
	entries []weightEntry
}

// weightEntry is one compiled "pattern: weights" pair.
type weightEntry struct {
	match   *regexp.Regexp
	weights map[config.MetricID]float64
}

// newWeights compiles the pattern list config.Config.Metrics holds for one
// language. A pattern that is not a valid RE2 regex is an error naming it;
// config.Validate reports the same problem earlier and with more context.
func newWeights(patterns config.PatternWeights) (weights, error) {
	entries := make([]weightEntry, 0, len(patterns))
	for _, p := range patterns {
		match, err := regexp.Compile(p.Pattern)
		if err != nil {
			return weights{}, fmt.Errorf("pattern %q: %w", p.Pattern, err)
		}
		entries = append(entries, weightEntry{match: match, weights: p.Weights})
	}
	return weights{entries: entries}, nil
}

// For returns the weights that apply to path, which is slash-separated and
// relative to the configuration file. A metric absent from the result is
// disabled for that file. The map is fresh, so the caller may keep it.
func (w weights) For(path string) map[config.MetricID]float64 {
	merged := map[config.MetricID]float64{}
	for _, e := range w.entries {
		if !e.match.MatchString(path) {
			continue
		}
		maps.Copy(merged, e.weights)
	}
	return merged
}

// limits are the ICP limits one language configures, pattern by pattern, in
// document order. The last matching entry wins; limits do not merge.
type limits struct {
	entries []limitEntry
}

// limitEntry is one compiled "pattern: limit" pair.
type limitEntry struct {
	match *regexp.Regexp
	limit int
}

// newLimits compiles the pattern list config.Config.ICPLimits holds for one
// language. A pattern that is not a valid RE2 regex is an error naming it.
func newLimits(patterns config.PatternLimits) (limits, error) {
	entries := make([]limitEntry, 0, len(patterns))
	for _, p := range patterns {
		match, err := regexp.Compile(p.Pattern)
		if err != nil {
			return limits{}, fmt.Errorf("pattern %q: %w", p.Pattern, err)
		}
		entries = append(entries, limitEntry{match: match, limit: p.Limit})
	}
	return limits{entries: entries}, nil
}

// For returns the limit of the last pattern matching path, or 0 when none
// does. A validated configuration always opens with config.PatternAll, so 0
// only comes back from a hand-built list.
func (l limits) For(path string) int {
	limit := 0
	for _, e := range l.entries {
		if e.match.MatchString(path) {
			limit = e.limit
		}
	}
	return limit
}

// resolver answers, for one language, how a file is weighed and limited. It
// is built once per run and only read afterwards, so every worker shares it.
type resolver struct {
	weights weights
	limits  limits
}

// newResolver compiles the metrics and icp-limits pattern lists cfg holds
// for lang.
func newResolver(cfg *config.Config, lang config.Language) (*resolver, error) {
	weights, err := newWeights(cfg.Metrics[lang])
	if err != nil {
		return nil, fmt.Errorf("metrics.%s: %w", lang, err)
	}
	limits, err := newLimits(cfg.ICPLimits[lang])
	if err != nil {
		return nil, fmt.Errorf("icp-limits.%s: %w", lang, err)
	}
	return &resolver{weights: weights, limits: limits}, nil
}

// Resolve scores the units of the file at path against the weights and the
// limit that path resolves to.
func (r *resolver) Resolve(path string, units []Unit) []UnitReport {
	if len(units) == 0 {
		return nil
	}
	weights := r.weights.For(path)
	limit := r.limits.For(path)
	out := make([]UnitReport, len(units))
	for i, u := range units {
		out[i] = score(u, weights, limit)
	}
	return out
}

// score applies weights and limit to one unit: a metric absent from weights
// is dropped from the report, the remaining counts are multiplied by their
// weight, and the sum is the unit's ICP total. Metrics are summed in
// config.Metrics order, so the total does not depend on map iteration. The
// unit's occurrences go through the same filter, so what the report locates
// is exactly what it counts.
func score(unit Unit, weights map[config.MetricID]float64, limit int) UnitReport {
	counts := make(map[config.MetricID]int, len(unit.Counts))
	scores := make(map[config.MetricID]float64, len(unit.Counts))
	total := 0.0
	for _, id := range config.Metrics() {
		count, counted := unit.Counts[id]
		weight, enabled := weights[id]
		if !counted || !enabled {
			continue
		}
		counts[id] = count
		scores[id] = float64(count) * weight
		total += scores[id]
	}
	return UnitReport{
		Name:    unit.Name,
		Kind:    unit.Kind,
		Line:    unit.Line,
		Col:     unit.Col,
		Counts:  counts,
		Scores:  scores,
		Total:   total,
		Limit:   limit,
		Exceeds: total > float64(limit),

		Occurrences: weighOccurrences(unit.Occurrences, weights),
	}
}

// weighOccurrences keeps the occurrences of the enabled metrics, in the
// source order the analyzer produced them, and scores each one by its
// metric's weight. An occurrence of a disabled metric is dropped, exactly
// as that metric's count is.
func weighOccurrences(occurrences []Occurrence, weights map[config.MetricID]float64) []OccurrenceReport {
	if len(occurrences) == 0 {
		return nil
	}
	out := make([]OccurrenceReport, 0, len(occurrences))
	for _, o := range occurrences {
		weight, enabled := weights[o.Metric]
		if !enabled {
			continue
		}
		out = append(out, OccurrenceReport{Occurrence: o, Score: float64(o.Count) * weight})
	}
	return out
}
