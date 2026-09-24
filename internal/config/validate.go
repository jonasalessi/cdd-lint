package config

import (
	"fmt"
	"slices"
	"strings"
)

// Rule ids. Each one is a line of the schema contract:
//
//	V1  version is 1
//	V2  project_type is greenfield or legacy
//	V3  metrics has at least one language and every language key is known
//	V4  every metric id is known and applicable to its language
//	V5  every weight is > 0
//	V6  every limit is >= 1 (error); inside the band of project_type (warning)
//	V7  every pattern is a valid RE2 regex; include/exclude entries are not
//	    empty and "regex:" entries compile
//	V8  every pattern list opens with ".*"; the ".*" weights count at least
//	    MinMetrics metrics
//	V9  legacy_mode is known; greenfield needs strict_all; measure_only needs
//	    block_on_ci false; boy_scout is not supported yet (warning)
//	V10 reporter.format is known
//	V11 metrics and icp-limits configure the same languages
//	V12 timeout is >= 0
const (
	RuleVersion      = "V1"
	RuleProjectType  = "V2"
	RuleLanguages    = "V3"
	RuleMetricIDs    = "V4"
	RuleWeights      = "V5"
	RuleLimits       = "V6"
	RulePatterns     = "V7"
	RuleDefaultEntry = "V8"
	RuleLegacyMode   = "V9"
	RuleReporter     = "V10"
	RuleLanguageSets = "V11"
	RuleTimeout      = "V12"
)

// Validate checks cfg against rules V1 to V12 and returns every finding, so
// a user sees all problems in one run. specs are the known languages; a nil
// cfg is one V1 error.
func Validate(cfg *Config, specs []LanguageSpec) Issues {
	if cfg == nil {
		return Issues{{Rule: RuleVersion, Severity: SeverityError, Message: "no configuration"}}
	}
	v := &validator{specs: specs}
	settingRules{v}.check(cfg)
	languageRules{v}.check(cfg)
	metricRules{v}.check(cfg)
	return v.issues
}

// validator collects the issues the rule groups report and answers what
// the known specs say about a language and its metrics.
type validator struct {
	specs  []LanguageSpec
	issues Issues
}

// isLanguage reports whether lang is one of the known specs.
func (v *validator) isLanguage(lang Language) bool {
	_, ok := FindSpec(v.specs, lang)
	return ok
}

// isApplicable reports whether id can be counted for lang. An unknown
// language, already reported by V3, applies every known metric.
func (v *validator) isApplicable(lang Language, id MetricID) bool {
	spec, ok := FindSpec(v.specs, lang)
	if !ok {
		return IsMetric(id)
	}
	return spec.IsApplicable(id)
}

func (v *validator) errorf(rule, format string, args ...any) {
	v.issues = append(v.issues, Issue{Rule: rule, Severity: SeverityError, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(rule, format string, args ...any) {
	v.issues = append(v.issues, Issue{Rule: rule, Severity: SeverityWarning, Message: fmt.Sprintf(format, args...)})
}

// sortedLanguages returns the keys of m in specs order, with unknown
// languages last in lexical order, so messages are stable.
func sortedLanguages[V any](m map[Language]V, specs []LanguageSpec) []Language {
	var out []Language
	for _, spec := range specs {
		if _, ok := m[spec.ID]; ok {
			out = append(out, spec.ID)
		}
	}
	var unknown []string
	for lang := range m {
		if _, ok := FindSpec(specs, lang); !ok {
			unknown = append(unknown, string(lang))
		}
	}
	slices.Sort(unknown)
	for _, s := range unknown {
		out = append(out, Language(s))
	}
	return out
}

func join[T ~string](list []T) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
