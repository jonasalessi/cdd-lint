package java

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
		ID:              "java",
		DisplayName:     "Java",
		Extensions:      []string{".java"},
		NotApplicable:   nil,
		DefaultExcludes: []string{"**/src/test/**", "**/build/**", "**/target/**"},
		Descriptions: map[config.MetricID]string{
			config.MetricInternalCoupling: "references to project classes",
			config.MetricExternalCoupling: "framework / third-party types",
			config.MetricStdlibCoupling:   "JDK types (java.*, javax.*, jdk.*)",
		},
		PackageExample: "com.acme.app",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`, `# ".*Dto\\.java": 20`},
	}
	assert.Equal(t, want, got)
}

// TestSpecIsSafeToMutate (TC-S2): Spec hands out copies, so a caller's edit
// never leaks into the next call.
func TestSpecIsSafeToMutate(t *testing.T) {
	first := Spec()
	first.Extensions[0] = ".kt"
	first.DefaultExcludes[0] = "**/nothing/**"
	first.Descriptions[config.MetricInternalCoupling] = "changed"

	second := Spec()
	assert.Equal(t, []string{".java"}, second.Extensions)
	assert.Equal(t, "**/src/test/**", second.DefaultExcludes[0])
	assert.Equal(t, "references to project classes", second.Descriptions[config.MetricInternalCoupling])
}

// TestDetectPackagesSkipsKotlinFiles (TC-S3): the kotlin source under
// com.acme.other is not read.
func TestDetectPackagesSkipsKotlinFiles(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), filepath.Join("testdata", "project"))
	require.NoError(t, err)
	assert.Equal(t, []string{"com.acme.billing", "com.acme.shared"}, got)
}

// TestDetectPackagesSkipsKotlinOnlyProject (TC-S4): a project holding only a
// .kt file declares no Java package.
func TestDetectPackagesSkipsKotlinOnlyProject(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.kt"), []byte("package a.b\n"), 0o644))
	got, err := Spec().DetectPackages(t.Context(), dir)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDetectPackagesEmptyProject(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, got)
}
