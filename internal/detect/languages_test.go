package detect

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Synthetic languages: the walk only needs ids and extensions.
const (
	langAlpha config.Language = "alpha"
	langBeta  config.Language = "beta"
	langGamma config.Language = "gamma"
	langDelta config.Language = "delta"
)

func testSpecs() []config.LanguageSpec {
	return []config.LanguageSpec{
		{ID: langAlpha, Extensions: []string{".alpha"}},
		{ID: langBeta, Extensions: []string{".beta"}},
		{ID: langGamma, Extensions: []string{".gamma", ".gs"}},
		{ID: langDelta, Extensions: []string{".delta", ".dl"}},
	}
}

func fixture(name string) string {
	return filepath.Join("testdata", name)
}

func TestLanguagesPerFixture(t *testing.T) {
	tests := map[string]struct {
		root string
		want map[config.Language]int
	}{
		"one language, node_modules skipped": {
			root: "alpha-only",
			want: map[config.Language]int{langAlpha: 3},
		},
		"two languages, second extension counted": {
			root: "beta-gamma",
			want: map[config.Language]int{langBeta: 3, langGamma: 2},
		},
		"extensions match case-insensitively": {
			root: "delta",
			want: map[config.Language]int{langDelta: 4},
		},
		"mixed tree": {
			root: "mixed",
			want: map[config.Language]int{langAlpha: 1, langDelta: 1},
		},
		"empty tree": {
			root: "empty",
			want: map[config.Language]int{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			d, err := Languages(context.Background(), fixture(tt.root), testSpecs())
			require.NoError(t, err)
			assert.Equal(t, tt.want, d.Counts)
			assert.False(t, d.Truncated)
			assert.GreaterOrEqual(t, d.Elapsed, time.Duration(0))
		})
	}
}

func TestLanguagesOnlyCountsTheGivenSpecs(t *testing.T) {
	d, err := Languages(context.Background(), fixture("mixed"), testSpecs()[:1])
	require.NoError(t, err)
	assert.Equal(t, map[config.Language]int{langAlpha: 1}, d.Counts, "delta is not in the specs")
}

func TestLanguagesOrder(t *testing.T) {
	d := Detected{Counts: map[config.Language]int{
		langDelta: 1,
		langAlpha: 2,
	}}
	assert.Equal(t, []config.Language{langAlpha, langDelta}, d.Languages(testSpecs()), "specs order")
	assert.Empty(t, Detected{}.Languages(testSpecs()))
}

func TestLanguagesExpiredContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err := Languages(ctx, fixture("alpha-only"), testSpecs())
	require.NoError(t, err, "a cut-short scan is not a failure")
	assert.True(t, d.Truncated)
	assert.Empty(t, d.Counts)
}

func TestLanguagesDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	d, err := Languages(ctx, fixture("mixed"), testSpecs())
	require.NoError(t, err)
	assert.True(t, d.Truncated)
}

func TestLanguagesMissingRoot(t *testing.T) {
	_, err := Languages(context.Background(), fixture("does-not-exist"), testSpecs())
	assert.Error(t, err)
}

func TestSkipDir(t *testing.T) {
	for _, name := range []string{".git", "node_modules", "vendor", "build", "dist", "target", "out"} {
		assert.True(t, SkipDir(name), name)
	}
	assert.False(t, SkipDir("src"))
}
