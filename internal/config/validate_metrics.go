package config

import (
	"regexp"
	"slices"
)

// metricRules checks the metrics and icp-limits sections: V4 to V8.
type metricRules struct{ v *validator }

// check runs the group's rules on cfg.
func (m metricRules) check(cfg *Config) {
	m.metrics(cfg)
	m.limits(cfg)
}

func (m *metricRules) metrics(cfg *Config) {
	for _, lang := range sortedLanguages(cfg.Metrics, m.v.specs) {
		patterns := cfg.Metrics[lang]
		m.defaultEntry("metrics", lang, len(patterns), func(i int) string { return patterns[i].Pattern })
		for i, p := range patterns {
			m.pattern("metrics", lang, p.Pattern)
			if i == 0 && p.Pattern == PatternAll && len(p.Weights) < MinMetrics {
				m.v.errorf(
					RuleDefaultEntry,
					"metrics.%s.%q: %d metrics configured, at least %d are required",
					lang,
					p.Pattern,
					len(p.Weights),
					MinMetrics,
				)
			}
			for _, id := range sortedMetrics(p.Weights) {
				switch {
				case !IsMetric(id):
					m.v.errorf(
						RuleMetricIDs,
						"metrics.%s.%q: %q is not one of %s",
						lang,
						p.Pattern,
						id,
						join(Metrics()),
					)
				case !m.v.isApplicable(lang, id):
					m.v.errorf(RuleMetricIDs, "metrics.%s.%q: %s does not apply to %s", lang, p.Pattern, id, lang)
				}
				if w := p.Weights[id]; w <= 0 {
					m.v.errorf(RuleWeights, "metrics.%s.%q.%s: weight %v must be > 0", lang, p.Pattern, id, w)
				}
			}
		}
	}
}

func (m *metricRules) limits(cfg *Config) {
	lo, hi := LimitBand(cfg.ProjectType)
	for _, lang := range sortedLanguages(cfg.ICPLimits, m.v.specs) {
		patterns := cfg.ICPLimits[lang]
		m.defaultEntry("icp-limits", lang, len(patterns), func(i int) string { return patterns[i].Pattern })
		for _, p := range patterns {
			m.pattern("icp-limits", lang, p.Pattern)
			switch {
			case p.Limit < 1:
				m.v.errorf(RuleLimits, "icp-limits.%s.%q: limit %d must be >= 1", lang, p.Pattern, p.Limit)
			case p.Limit < lo || p.Limit > hi:
				m.v.warnf(
					RuleLimits,
					"icp-limits.%s.%q: limit %d is outside the %s band %d-%d",
					lang,
					p.Pattern,
					p.Limit,
					cfg.ProjectType,
					lo,
					hi,
				)
			}
		}
	}
}

// defaultEntry checks that a pattern list exists and opens with ".*".
func (m *metricRules) defaultEntry(section string, lang Language, n int, pattern func(int) string) {
	if n == 0 {
		m.v.errorf(RuleDefaultEntry, "%s.%s: no patterns; a %q entry is required", section, lang, PatternAll)
		return
	}
	if first := pattern(0); first != PatternAll {
		m.v.errorf(RuleDefaultEntry, "%s.%s: first pattern is %q, must be %q", section, lang, first, PatternAll)
	}
}

func (m *metricRules) pattern(section string, lang Language, pattern string) {
	if _, err := regexp.Compile(pattern); err != nil {
		m.v.errorf(RulePatterns, "%s.%s.%q: invalid regex: %v", section, lang, pattern, err)
	}
}

// sortedMetrics returns the keys of m in Metrics order, unknown ids last.
func sortedMetrics(m map[MetricID]float64) []MetricID {
	var out []MetricID
	for _, id := range Metrics() {
		if _, ok := m[id]; ok {
			out = append(out, id)
		}
	}
	var unknown []string
	for id := range m {
		if !IsMetric(id) {
			unknown = append(unknown, string(id))
		}
	}
	slices.Sort(unknown)
	for _, s := range unknown {
		out = append(out, MetricID(s))
	}
	return out
}
