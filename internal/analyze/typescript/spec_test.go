package typescript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

func TestSpecID(t *testing.T) {
	assert.Equal(t, "typescript", string(Spec().ID))
	assert.Len(t, Spec().NotApplicable, 0, "every metric applies to typescript")
}

func TestSpecExtensionsAndDefaultExcludes(t *testing.T) {
	wantExtensions := []string{".ts", ".tsx", ".mts", ".cts"}
	wantExcludes := []string{
		"**/*.test.ts", "**/*.spec.ts",
		"**/*.test.tsx", "**/*.spec.tsx",
		"**/*.test.mts", "**/*.spec.mts",
		"**/*.test.cts", "**/*.spec.cts",
		"**/*.d.ts", "**/*.d.mts", "**/*.d.cts",
		"**/node_modules/**", "**/dist/**",
	}

	spec := Spec()
	assert.Equal(t, wantExtensions, spec.Extensions)
	assert.Equal(t, wantExcludes, spec.DefaultExcludes)

	spec.Extensions[0] = ".js"
	assert.Equal(t, wantExtensions, Spec().Extensions, "callers cannot mutate the extension roster")
}

// TestSpecCouplingDescriptions pins the wording that tells a reader which
// imports land in which coupling metric.
func TestSpecCouplingDescriptions(t *testing.T) {
	descriptions := Spec().Descriptions
	assert.Equal(t, "references to project modules", descriptions[config.MetricInternalCoupling])
	assert.Equal(t, "framework / node_modules types", descriptions[config.MetricExternalCoupling])
	assert.Equal(t, "Node.js built-in modules (node:fs, path)", descriptions[config.MetricStdlibCoupling])
}

func TestDetectPackagesReadsTSConfigWithComments(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), filepath.Join("testdata", "aliases"))
	require.NoError(t, err)
	assert.Equal(t, []string{"@app/", "@lib/"}, got)
}

func TestDetectPackagesOfABrokenTSConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{not json"), 0o644))
	got, err := Spec().DetectPackages(t.Context(), dir)
	require.NoError(t, err, "a tsconfig that does not parse is not an error")
	assert.Nil(t, got)
}

func TestDetectPackagesWithoutTSConfig(t *testing.T) {
	got, err := Spec().DetectPackages(t.Context(), t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDetectPackagesCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := Spec().DetectPackages(ctx, filepath.Join("testdata", "aliases"))

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
}

func TestStripJSONC(t *testing.T) {
	in := "{\n// line comment\n\"a\": \"http://x//y\", /* block */\n\"b\": [1, 2,],\n}"
	assert.JSONEq(t, `{"a": "http://x//y", "b": [1, 2]}`, string(stripJSONC([]byte(in))))
}

func TestStripJSONCKeepsEscapedQuotes(t *testing.T) {
	in := `{"a": "say \"//hi\"" /* after */}`
	assert.JSONEq(t, `{"a": "say \"//hi\""}`, string(stripJSONC([]byte(in))),
		"an escaped quote does not end the string, so the slashes stay")
}
