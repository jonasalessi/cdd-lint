package initcmd

import (
	"context"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Synthetic languages. alpha cannot count exceptions or inheritance; beta
// and gamma count everything and share their exclude globs.
const (
	langAlpha config.Language = "alpha"
	langBeta  config.Language = "beta"
	langGamma config.Language = "gamma"
)

func noPackages(context.Context, string) ([]string, error) { return nil, nil }

func alphaSpec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              langAlpha,
		DisplayName:     "Alpha",
		Extensions:      []string{".alpha"},
		NotApplicable:   []config.MetricID{config.MetricExceptionHandling, config.MetricInheritance},
		DefaultExcludes: []string{"**/*_test.alpha", "vendor/**"},
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

func testSpecs() []config.LanguageSpec {
	return []config.LanguageSpec{alphaSpec(), betaSpec(), gammaSpec()}
}
