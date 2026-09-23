package prompt

import (
	"context"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Synthetic languages. alpha cannot count exceptions or inheritance and has
// its own branch wording; beta and gamma count everything and share their
// exclude globs; delta has no display name and no package example.
const (
	langAlpha config.Language = "alpha"
	langBeta  config.Language = "beta"
	langGamma config.Language = "gamma"
	langDelta config.Language = "delta"
)

func noPackages(context.Context, string) ([]string, error) { return nil, nil }

func alphaSpec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              langAlpha,
		DisplayName:     "Alpha",
		Extensions:      []string{".alpha"},
		NotApplicable:   []config.MetricID{config.MetricExceptionHandling, config.MetricInheritance},
		DefaultExcludes: []string{"**/*_test.alpha", "vendor/**"},
		Descriptions:    map[config.MetricID]string{config.MetricCodeBranch: "if/else, switch/select, for"},
		PackageExample:  "example.com/acme/api",
		DetectPackages:  noPackages,
	}
}

func betaSpec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              langBeta,
		DisplayName:     "Beta",
		Extensions:      []string{".beta"},
		DefaultExcludes: []string{"**/src/test/**", "**/build/**"},
		PackageExample:  "com.acme.app",
		DetectPackages:  noPackages,
	}
}

func gammaSpec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              langGamma,
		DisplayName:     "Gamma",
		Extensions:      []string{".gamma"},
		DefaultExcludes: []string{"**/src/test/**", "**/build/**"},
		PackageExample:  "com.acme.app",
		DetectPackages:  noPackages,
	}
}

func deltaSpec() config.LanguageSpec {
	return config.LanguageSpec{ID: langDelta, Extensions: []string{".delta"}, DetectPackages: noPackages}
}

func testSpecs() []config.LanguageSpec {
	return []config.LanguageSpec{alphaSpec(), betaSpec(), gammaSpec(), deltaSpec()}
}
