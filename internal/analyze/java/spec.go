// Package java is the home of the Java language: the data cdd init and the
// configuration need, and the package-declaration based prefix detection it
// shares with Kotlin.
package java

import (
	"context"
	"maps"
	"slices"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/jvm"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// extJava is the one extension the analyzer reads.
const extJava = ".java"

// extensions is the roster the spec and the package detection share.
var extensions = [...]string{extJava}

// Spec returns the Java language spec. Every call returns fresh slices and a
// fresh map, so a caller that edits them edits its own copy.
func Spec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:              "java",
		DisplayName:     "Java",
		Extensions:      slices.Clone(extensions[:]),
		DefaultExcludes: []string{"**/src/test/**", "**/build/**", "**/target/**"},
		Descriptions: maps.Clone(map[config.MetricID]string{
			config.MetricInternalCoupling: "references to project classes",
			config.MetricExternalCoupling: "framework / third-party types",
			config.MetricStdlibCoupling:   "JDK types (java.*, javax.*, jdk.*)",
		}),
		PackageExample: "com.acme.app",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`, `# ".*Dto\\.java": 20`},
		DetectPackages: detectPackages,
	}
}

// detectPackages reduces the package declarations of the Java sources under
// root to their shortest telling prefixes.
func detectPackages(ctx context.Context, root string) ([]string, error) {
	return jvm.Prefixes(ctx, root, extensions[:])
}
