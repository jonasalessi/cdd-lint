package kotlin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// TestSpec pins the whole spec at once (TC-S1): a drift fails with a diff
// naming the field. Go funcs are not comparable, so DetectPackages is checked
// for presence and cleared on the copy.
func TestSpec(t *testing.T) {
	got := Spec()
	require.NotNil(t, got.DetectPackages)
	got.DetectPackages = nil

	want := config.LanguageSpec{
		ID:              "kotlin",
		DisplayName:     "Kotlin",
		Extensions:      []string{".kt"},
		NotApplicable:   nil,
		DefaultExcludes: []string{"**/src/test/**", "**/build/**", "**/target/**"},
		Descriptions: map[config.MetricID]string{
			config.MetricCodeBranch:     "if/when, loops, safe calls (?.)",
			config.MetricCondition:      "&&, || and ?: clauses",
			config.MetricStdlibCoupling: "kotlin.* and JDK types",
			config.MetricInheritance:    ": Base() / : Iface, per level",
			config.MetricLambda:         "lambdas and function refs",
		},
		PackageExample: "com.acme.app",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`},
	}
	assert.Equal(t, want, got)
}

// TestSpecIsSafeToMutate (TC-S2): Spec hands out copies, so a caller's edit
// never leaks into the next call.
func TestSpecIsSafeToMutate(t *testing.T) {
	first := Spec()
	first.Extensions[0] = ".java"
	first.DefaultExcludes[0] = "**/nothing/**"
	first.Descriptions[config.MetricCodeBranch] = "changed"

	second := Spec()
	assert.Equal(t, []string{".kt"}, second.Extensions)
	assert.Equal(t, "**/src/test/**", second.DefaultExcludes[0])
	assert.Equal(t, "if/when, loops, safe calls (?.)", second.Descriptions[config.MetricCodeBranch])
}

// TestDetectPackagesSkipsJavaFiles (TC-S3): the java source under
// com.acme.other is not read, and the build script never declared a package.
func TestDetectPackagesSkipsJavaFiles(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), filepath.Join("testdata", "project"))
	require.NoError(t, err)
	assert.Equal(t, []string{"com.acme.billing", "com.acme.shared"}, got)
}

// TestDetectPackagesSkipsScripts (TC-S4): a .kts file is not read even when
// it declares a package.
func TestDetectPackagesSkipsScripts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.kts"), []byte("package a.b\n"), 0o644))
	got, err := Spec().DetectPackages(t.Context(), dir)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDetectPackagesEmptyProject(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, got)
}
