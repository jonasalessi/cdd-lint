package languages

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// analyzeDir is the directory every language lives under, relative to this
// package.
var analyzeDir = filepath.Join("..", "analyze")

// dirName maps a language id to its directory: the id itself, except that
// go is a keyword and lives in golang.
func dirName(id config.Language) string {
	if id == "go" {
		return "golang"
	}
	return string(id)
}

// TestEveryLanguageHasADirectory is one half of FR-7: every registered id
// has its directory under internal/analyze.
func TestEveryLanguageHasADirectory(t *testing.T) {
	for _, l := range All() {
		dir := filepath.Join(analyzeDir, dirName(l.Spec.ID))
		info, err := os.Stat(dir)
		require.NoError(t, err, "language %q is registered but %s does not exist", l.Spec.ID, dir)
		assert.True(t, info.IsDir(), "%s is not a directory", dir)
		assert.FileExists(t, filepath.Join(dir, "spec.go"), "language %q has no spec.go", l.Spec.ID)
	}
}

// TestEveryDirectoryIsRegistered is the other half of FR-7: every language
// directory under internal/analyze appears in All. A forgotten registry
// line fails here naming the directory.
func TestEveryDirectoryIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, l := range All() {
		registered[dirName(l.Spec.ID)] = true
	}
	entries, err := os.ReadDir(analyzeDir)
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "internal" || e.Name() == "testdata" {
			continue
		}
		assert.True(t, registered[e.Name()],
			"internal/analyze/%s is not registered in internal/languages/languages.go", e.Name())
	}
}

// TestEveryAnalyzerIsWired is FR-12: a language directory that has an
// analyzer.go must hand its constructor to the registry. A package that
// grew an analyzer without the registry line fails here naming the
// directory, instead of silently reporting "no analyzer yet".
func TestEveryAnalyzerIsWired(t *testing.T) {
	for _, l := range All() {
		dir := filepath.Join(analyzeDir, dirName(l.Spec.ID))
		_, err := os.Stat(filepath.Join(dir, "analyzer.go"))
		if os.IsNotExist(err) {
			assert.Nil(t, l.NewAnalyzer, "language %q has no analyzer.go but is wired", l.Spec.ID)
			continue
		}
		require.NoError(t, err)
		require.NotNil(t, l.NewAnalyzer, "language %q has an analyzer.go but no NewAnalyzer", l.Spec.ID)
		a := l.NewAnalyzer(analyze.Options{})
		assert.NotNil(t, a, "language %q builds a nil analyzer", l.Spec.ID)
		if closer, ok := a.(io.Closer); ok {
			assert.NoError(t, closer.Close())
		}
	}
}

func TestAllReturnsAFreshSlice(t *testing.T) {
	first := All()
	first[0].Spec.ID = "mutated"
	assert.NotEqual(t, first[0].Spec.ID, All()[0].Spec.ID)
	assert.Len(t, Specs(), len(All()))
	for i, l := range All() {
		assert.Equal(t, l.Spec.ID, Specs()[i].ID, "Specs keeps the registration order")
	}
}

// TestSpecCompleteness is FR-8: every spec carries what the prompts, the
// renderer and detection need, and no two specs collide.
func TestSpecCompleteness(t *testing.T) {
	ids := map[config.Language]bool{}
	exts := map[string]config.Language{}
	for _, l := range All() {
		s := l.Spec
		t.Run(string(s.ID), func(t *testing.T) {
			assert.NotEmpty(t, s.ID, "ID")
			assert.NotEmpty(t, s.DisplayName, "DisplayName")
			assert.NotEmpty(t, s.Extensions, "Extensions")
			assert.NotEmpty(t, s.PackageExample, "PackageExample")
			assert.NotNil(t, s.DetectPackages, "DetectPackages")
			assert.GreaterOrEqual(t, len(s.Applicable()), config.MinMetrics, "too few applicable metrics")
			for _, m := range s.Applicable() {
				assert.NotEmpty(t, s.Description(m), "no description for %s", m)
			}
			for _, m := range s.NotApplicable {
				assert.True(t, config.IsMetric(m), "NotApplicable names unknown metric %q", m)
			}
			for m := range s.Descriptions {
				assert.True(t, config.IsMetric(m), "Descriptions names unknown metric %q", m)
			}
			assert.False(t, ids[s.ID], "id %q registered twice", s.ID)
			ids[s.ID] = true
			for _, ext := range s.Extensions {
				assert.True(t, ext != "" && ext[0] == '.', "extension %q must start with a dot", ext)
				if other, dup := exts[ext]; dup {
					assert.Failf(t, "duplicate extension", "%s is claimed by both %s and %s", ext, other, s.ID)
				}
				exts[ext] = s.ID
			}
		})
	}
}
