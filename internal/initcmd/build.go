package initcmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ErrTooFewMetrics reports a language left with fewer applicable metrics
// than docs/cdd.md requires, so an interactive caller can re-ask the metric
// question naming the language.
type ErrTooFewMetrics struct {
	Language config.Language
	Have     int
	Need     int
}

func (e ErrTooFewMetrics) Error() string {
	return fmt.Sprintf("%s is left with %d applicable metrics, at least %d are required", e.Language, e.Have, e.Need)
}

// Build turns a into a complete configuration for the languages in specs.
// The second return value carries non-blocking findings, e.g. a limit
// outside the recommended band. The result always passes config.Validate
// with zero errors; anything else is a bug and comes back as an internal
// error.
func Build(a Answers, specs []config.LanguageSpec) (*config.Config, []string, error) {
	a, warnings, err := normalize(a, specs)
	if err != nil {
		return nil, nil, err
	}
	metrics, err := metricsByLanguage(a, specs)
	if err != nil {
		return nil, nil, err
	}
	cfg := assemble(a, metrics, specs)
	if issues := config.Validate(cfg, specs).Errors(); len(issues) > 0 {
		return nil, nil, fmt.Errorf("internal: built config is invalid: %v", issues)
	}
	return cfg, warnings, nil
}

// normalize fills every defaulted field and validates the answers against
// the vocabulary. The rules are FR-3 to FR-8 of the init spec.
func normalize(a Answers, specs []config.LanguageSpec) (Answers, []string, error) {
	var warnings []string
	langs, err := normalizeLanguages(a.Languages, specs)
	if err != nil {
		return a, nil, err
	}
	a.Languages = langs
	if a.ProjectType == "" {
		a.ProjectType = config.ProjectGreenfield
	}
	if !config.IsProjectType(a.ProjectType) {
		return a, nil, fmt.Errorf("project type %q is not one of %s", a.ProjectType, join(config.ProjectTypes()))
	}
	mode, modeWarnings, err := normalizeMode(a.ProjectType, a.LegacyMode)
	if err != nil {
		return a, nil, err
	}
	a.LegacyMode = mode
	warnings = append(warnings, modeWarnings...)
	limit, limitWarnings, err := normalizeLimit(a.ProjectType, a.Limit)
	if err != nil {
		return a, nil, err
	}
	a.Limit = limit
	warnings = append(warnings, limitWarnings...)
	byLang, err := resolveMetrics(a)
	if err != nil {
		return a, nil, err
	}
	a.MetricsByLanguage = byLang
	selected, counted := selectionUnion(byLang, specs)
	if err := checkWeights(selected, counted, a.Weights); err != nil {
		return a, nil, err
	}
	if a.Timeout == 0 {
		a.Timeout = config.DefaultTimeout()
	}
	if a.Timeout < 0 {
		return a, nil, fmt.Errorf("timeout %s must not be negative", a.Timeout)
	}
	a.Packages = resolvePackages(a, specs)
	return a, warnings, nil
}

// resolvePackages merges the flat prefix list with the per-language lists of
// the selected languages, deduplicated in specs order.
func resolvePackages(a Answers, specs []config.LanguageSpec) []string {
	merged := append([]string(nil), a.Packages...)
	for _, spec := range specs {
		if slices.Contains(a.Languages, spec.ID) {
			merged = append(merged, a.PackagesByLanguage[spec.ID]...)
		}
	}
	return cleanList(merged)
}

// normalizeLanguages requires at least one known language and drops
// duplicates, keeping the given order.
func normalizeLanguages(langs []config.Language, specs []config.LanguageSpec) ([]config.Language, error) {
	known := config.LanguageIDs(specs)
	if len(langs) == 0 {
		return nil, fmt.Errorf("at least one language is required; known ids: %s", join(known))
	}
	var out []config.Language
	seen := map[config.Language]bool{}
	for _, lang := range langs {
		if !slices.Contains(known, lang) {
			return nil, fmt.Errorf("language %q is not one of %s", lang, join(known))
		}
		if !seen[lang] {
			seen[lang] = true
			out = append(out, lang)
		}
	}
	return out, nil
}

// normalizeMode resolves the enforcement mode. An unknown mode is an error
// whatever the project type, so a typo never passes for the value it was
// meant to be. Greenfield then always gets strict_all (another mode is
// ignored with a warning), legacy defaults to strict_on_new_only, and
// boy_scout warns that its baseline store does not exist yet.
func normalizeMode(projectType, mode string) (string, []string, error) {
	if mode != "" && !config.IsLegacyMode(mode) {
		return "", nil, fmt.Errorf("legacy mode %q is not one of %s", mode, join(config.LegacyModes()))
	}
	if projectType == config.ProjectGreenfield {
		if mode != "" && mode != config.ModeStrictAll {
			warning := fmt.Sprintf("legacy mode %q only applies to %s projects; using %s",
				mode, config.ProjectLegacy, config.ModeStrictAll)
			return config.ModeStrictAll, []string{warning}, nil
		}
		return config.ModeStrictAll, nil, nil
	}
	if mode == "" {
		mode = config.ModeStrictOnNewOnly
	}
	if mode == config.ModeBoyScout {
		warning := fmt.Sprintf("legacy mode %s needs the baseline store, which is not supported yet", mode)
		return mode, []string{warning}, nil
	}
	return mode, nil, nil
}

// normalizeLimit resolves the ICP limit: 0 becomes the default for the
// project type, below 1 is an error, and a value outside the recommended
// band is accepted with a warning.
func normalizeLimit(projectType string, limit int) (int, []string, error) {
	if limit == 0 {
		limit = config.DefaultLimit(projectType)
	}
	if limit < 1 {
		return 0, nil, fmt.Errorf("limit %d must be at least 1", limit)
	}
	lo, hi := config.LimitBand(projectType)
	if limit < lo || limit > hi {
		warning := fmt.Sprintf("limit %d is outside the %s band %d-%d", limit, projectType, lo, hi)
		return limit, []string{warning}, nil
	}
	return limit, nil, nil
}

// resolveMetrics settles the metric selection of every language: its
// MetricsByLanguage entry when non-empty, else the global Metrics list, else
// the default selection. Ids must be known; duplicates are dropped. The
// minimum count is judged later, after applicability filtering.
func resolveMetrics(a Answers) (map[config.Language][]config.MetricID, error) {
	out := make(map[config.Language][]config.MetricID, len(a.Languages))
	for _, lang := range a.Languages {
		sel := a.MetricsByLanguage[lang]
		if len(sel) == 0 {
			sel = a.Metrics
		}
		if len(sel) == 0 {
			sel = config.DefaultSelection()
		}
		clean, err := normalizeSelection(sel)
		if err != nil {
			return nil, err
		}
		out[lang] = clean
	}
	return out, nil
}

// normalizeSelection validates the ids against the vocabulary and drops
// duplicates, keeping the given order.
func normalizeSelection(sel []config.MetricID) ([]config.MetricID, error) {
	var out []config.MetricID
	seen := map[config.MetricID]bool{}
	for _, id := range sel {
		if !config.IsMetric(id) {
			return nil, fmt.Errorf("metric %q is not one of %s", id, join(config.Metrics()))
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// selectionUnion joins the per-language selections in canonical metric order.
// selected holds every id a language asked for; counted leaves out the ids
// metricsByLanguage drops as not applicable, so it is the set a weight
// override can still reach.
func selectionUnion(
	byLang map[config.Language][]config.MetricID,
	specs []config.LanguageSpec,
) (selected, counted []config.MetricID) {
	inSelection := map[config.MetricID]bool{}
	applicable := map[config.MetricID]bool{}
	for lang, sel := range byLang {
		spec, _ := config.FindSpec(specs, lang)
		for _, id := range sel {
			inSelection[id] = true
			if spec.IsApplicable(id) {
				applicable[id] = true
			}
		}
	}
	for _, id := range config.Metrics() {
		if inSelection[id] {
			selected = append(selected, id)
		}
		if applicable[id] {
			counted = append(counted, id)
		}
	}
	return selected, counted
}

// checkWeights rejects overrides for unknown metrics, for metrics no language
// selected, for metrics every selected language drops as not applicable, and
// any weight at or below zero. Judging the override against counted rather
// than selected is what keeps it from being written for a metric no analyzer
// will ever weigh.
func checkWeights(selected, counted []config.MetricID, weights map[config.MetricID]float64) error {
	ids := make([]string, 0, len(weights))
	for id := range weights {
		ids = append(ids, string(id))
	}
	slices.Sort(ids)
	for _, s := range ids {
		id := config.MetricID(s)
		if !config.IsMetric(id) {
			return fmt.Errorf("weight for unknown metric %q; known ids: %s", id, join(config.Metrics()))
		}
		if !slices.Contains(counted, id) {
			if slices.Contains(selected, id) {
				return fmt.Errorf("weight for metric %q, which none of the selected languages can count", id)
			}
			return fmt.Errorf("weight for metric %q, which is not selected", id)
		}
		if w := weights[id]; w <= 0 {
			return fmt.Errorf("weight %v for %s must be above 0", w, id)
		}
	}
	return nil
}

// metricsByLanguage drops the metrics a language cannot count and resolves
// each remaining weight; a language left under config.MinMetrics turns into
// ErrTooFewMetrics.
func metricsByLanguage(
	a Answers,
	specs []config.LanguageSpec,
) (map[config.Language]map[config.MetricID]float64, error) {
	out := make(map[config.Language]map[config.MetricID]float64, len(a.Languages))
	for _, lang := range a.Languages {
		spec, _ := config.FindSpec(specs, lang)
		sel := a.MetricsByLanguage[lang]
		weights := make(map[config.MetricID]float64, len(sel))
		for _, id := range sel {
			if !spec.IsApplicable(id) {
				continue
			}
			w := config.DefaultWeight(id)
			if override, ok := a.Weights[id]; ok {
				w = override
			}
			weights[id] = w
		}
		if len(weights) < config.MinMetrics {
			return nil, ErrTooFewMetrics{Language: lang, Have: len(weights), Need: config.MinMetrics}
		}
		out[lang] = weights
	}
	return out, nil
}

// assemble lays the normalized answers out as the configuration document.
func assemble(
	a Answers,
	metrics map[config.Language]map[config.MetricID]float64,
	specs []config.LanguageSpec,
) *config.Config {
	cfgMetrics := make(map[config.Language]config.PatternWeights, len(metrics))
	limits := make(map[config.Language]config.PatternLimits, len(metrics))
	for lang, weights := range metrics {
		cfgMetrics[lang] = config.PatternWeights{{Pattern: config.PatternAll, Weights: weights}}
		limits[lang] = config.PatternLimits{{Pattern: config.PatternAll, Limit: a.Limit}}
	}
	return &config.Config{
		Version:     config.SchemaVersion,
		ProjectType: a.ProjectType,
		Metrics:     cfgMetrics,
		ICPLimits:   limits,
		Enforcement: config.Enforcement{
			BlockOnCI:  a.LegacyMode != config.ModeMeasureOnly,
			LegacyMode: a.LegacyMode,
		},
		Timeout:  a.Timeout,
		Reporter: config.Reporter{Format: config.FormatConsole},
		InternalCoupling: config.InternalCoupling{
			AutoDetect: true,
			Packages:   a.Packages,
		},
		Include: []string{},
		Exclude: excludes(a, specs),
	}
}

// excludes joins the default exclude globs of every selected language,
// deduplicated in specs order. Without DefaultExcludes the list stays
// empty.
func excludes(a Answers, specs []config.LanguageSpec) []string {
	if !a.DefaultExcludes {
		return []string{}
	}
	var out []string
	seen := map[string]bool{}
	for _, spec := range specs {
		if !slices.Contains(a.Languages, spec.ID) {
			continue
		}
		for _, glob := range spec.DefaultExcludes {
			if !seen[glob] {
				seen[glob] = true
				out = append(out, glob)
			}
		}
	}
	return out
}

// cleanList trims each entry and drops empties and duplicates, keeping the
// given order.
func cleanList(items []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// join renders an id list for error messages.
func join[T ~string](list []T) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
