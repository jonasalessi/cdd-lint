// Package initcmd is the UI-agnostic core of cdd init: it turns interview
// answers into a validated configuration and writes it atomically. Every
// default and coupling rule of the command lives here, so both the prompt
// flow and the flags stay thin.
package initcmd

import (
	"time"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Answers collects everything the interview or the flags decide. The zero
// value of a field means "use the default": Build fills it in.
type Answers struct {
	// Languages to configure; at least one is required.
	Languages []config.Language
	// ProjectType is greenfield or legacy; "" means greenfield.
	ProjectType string
	// LegacyMode is the enforcement mode; "" means the default for
	// ProjectType.
	LegacyMode string
	// Limit is the ICP limit for every language; 0 means
	// config.DefaultLimit(ProjectType).
	Limit int
	// Metrics are the selected metric ids applied to every language; nil
	// means config.DefaultSelection().
	Metrics []config.MetricID
	// MetricsByLanguage is the per-language selection made by the
	// interview. A language missing from the map falls back to Metrics or
	// the default selection.
	MetricsByLanguage map[config.Language][]config.MetricID
	// Weights overrides the default weight of selected metrics; a missing
	// id keeps config.DefaultWeight.
	Weights map[config.MetricID]float64
	// Packages are the internal package prefixes.
	Packages []string
	// PackagesByLanguage is the per-language prefix list made by the
	// interview or detection; Build merges it with Packages.
	PackagesByLanguage map[config.Language][]string
	// DefaultExcludes adds each language's spec DefaultExcludes when true.
	DefaultExcludes bool
	// Timeout is the analysis budget; 0 means config.DefaultTimeout().
	Timeout time.Duration
}

// SeedMetrics returns the metric ids the interview pre-checks for spec: the
// global Metrics list when given, otherwise config.DefaultSelection(),
// filtered to what the language's analyzer can count, order preserved.
func SeedMetrics(a Answers, spec config.LanguageSpec) []config.MetricID {
	seed := a.Metrics
	if len(seed) == 0 {
		seed = config.DefaultSelection()
	}
	var out []config.MetricID
	for _, id := range seed {
		if spec.IsApplicable(id) {
			out = append(out, id)
		}
	}
	return out
}

// SeedPackages returns the prefixes the interview pre-fills for lang: the
// language's own detected or given list when one exists, otherwise the flat
// Packages list.
func SeedPackages(a Answers, lang config.Language) []string {
	if a.PackagesByLanguage != nil {
		return a.PackagesByLanguage[lang]
	}
	return a.Packages
}
