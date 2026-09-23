package golang

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// TestSpec pins the whole spec at once (TC-S1, TC-S3): a drift fails with a
// diff naming the field. Go funcs are not comparable, so DetectPackages is
// checked for presence and cleared on the copy.
func TestSpec(t *testing.T) {
	got := Spec()
	require.NotNil(t, got.DetectPackages)
	got.DetectPackages = nil

	want := config.LanguageSpec{
		ID:              "go",
		DisplayName:     "Go",
		Extensions:      []string{".go"},
		NotApplicable:   []config.MetricID{config.MetricExceptionHandling},
		DefaultExcludes: []string{"**/*_test.go", "vendor/**"},
		Descriptions: map[config.MetricID]string{
			config.MetricCodeBranch:     "if/else, switch/select, for",
			config.MetricStdlibCoupling: "standard library packages (fmt, net/http)",
			config.MetricInheritance:    "embedded structs and interfaces",
			config.MetricLambda:         "func literals",
		},
		PackageExample: "github.com/acme/api",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`},
	}
	assert.Equal(t, want, got)
}

// TestSpecIsSafeToMutate (TC-S2): Spec hands out copies, so a caller's edit
// never leaks into the next call.
func TestSpecIsSafeToMutate(t *testing.T) {
	first := Spec()
	first.Extensions[0] = ".rs"
	first.NotApplicable[0] = config.MetricLambda
	first.DefaultExcludes[0] = "**/nothing/**"
	first.Descriptions[config.MetricLambda] = "changed"

	second := Spec()
	assert.Equal(t, []string{".go"}, second.Extensions)
	assert.Equal(t, []config.MetricID{config.MetricExceptionHandling}, second.NotApplicable)
	assert.Equal(t, "**/*_test.go", second.DefaultExcludes[0])
	assert.Equal(t, "func literals", second.Descriptions[config.MetricLambda])
}

// TestDetectPackagesReadsTheModuleLine is TC-S7: the module line of go.mod.
func TestDetectPackagesReadsTheModuleLine(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), filepath.Join("testdata", "module"))
	require.NoError(t, err)
	assert.Equal(t, []string{"example.com/goonly"}, got)
}

func TestDetectPackagesWithoutAModuleLine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.25.0\n"), 0o644))
	got, err := Spec().DetectPackages(t.Context(), dir)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDetectPackagesWithoutGoMod(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), t.TempDir())
	require.NoError(t, err, "a missing go.mod is not an error")
	assert.Nil(t, got)
}

func TestDetectPackagesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := Spec().DetectPackages(ctx, filepath.Join("testdata", "module"))

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
}
