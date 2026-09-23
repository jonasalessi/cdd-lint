// Package kotlin analyzes Kotlin source code for Intrinsic Complexity Points
// and supplies its language configuration and the package-declaration based
// prefix detection it shares with Java.
package kotlin

import (
	"context"
	"maps"
	"slices"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/jvm"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// extKotlin is the one extension the analyzer reads. Kotlin scripts (.kts)
// are build files and one-off tooling, not the code a limit is set for, so
// neither the analyzer nor the package detection opens them.
const extKotlin = ".kt"

// extensions is the roster the spec and the package detection share.
var extensions = [...]string{extKotlin}

// Spec returns the Kotlin language spec. Every call returns fresh slices and
// a fresh map, so a caller that edits them edits its own copy.
func Spec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              "kotlin",
		DisplayName:     "Kotlin",
		Extensions:      slices.Clone(extensions[:]),
		DefaultExcludes: []string{"**/src/test/**", "**/build/**", "**/target/**"},
		Descriptions: maps.Clone(map[config.MetricID]string{
			config.MetricCodeBranch:     "if/when, loops, safe calls (?.)",
			config.MetricCondition:      "&&, || and ?: clauses",
			config.MetricStdlibCoupling: "kotlin.* and JDK types",
			config.MetricInheritance:    ": Base() / : Iface, per level",
			config.MetricLambda:         "lambdas and function refs",
		}),
		PackageExample: "com.acme.app",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`},
		DetectPackages: detectPackages,
	}
}

// detectPackages reduces the package declarations of the Kotlin sources
// under root to their shortest telling prefixes.
func detectPackages(ctx context.Context, root string) ([]string, error) {
	return jvm.Prefixes(ctx, root, extensions[:])
}
