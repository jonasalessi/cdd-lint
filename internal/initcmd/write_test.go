package initcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

func builtConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := Build(alphaAnswers(), testSpecs())
	require.NoError(t, err)
	return cfg
}

func TestWriteCreatesLoadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cdd.config.yaml")
	require.NoError(t, Write(builtConfig(t), testSpecs(), path, false))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	loaded, err := config.Load(path)
	require.NoError(t, err)
	issues := config.Validate(loaded, testSpecs())
	assert.False(t, issues.HasErrors(), "issues: %v", issues)
	assert.NoFileExists(t, path+".tmp")
}

func TestWriteRefusesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cdd.config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	err := Write(builtConfig(t), testSpecs(), path, false)
	require.ErrorIs(t, err, ErrExists)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "old", string(data), "the existing file is untouched")
}

func TestWriteForceOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cdd.config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	require.NoError(t, Write(builtConfig(t), testSpecs(), path, true))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "version: 1")
}

func TestWriteStatErrorOtherThanMissing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular")
	require.NoError(t, os.WriteFile(file, nil, 0o644))

	err := Write(builtConfig(t), testSpecs(), filepath.Join(file, "cdd.config.yaml"), false)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrExists, "a path below a regular file is not an existing target")
}

func TestWriteScratchFileFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "cdd.config.yaml")
	assert.Error(t, Write(builtConfig(t), testSpecs(), path, false))
}

func TestWriteRenameFailureRemovesScratchFile(t *testing.T) {
	// A directory cannot be replaced by a rename, so the scratch file is
	// written and then has to be cleaned up.
	dir := t.TempDir()
	path := filepath.Join(dir, "cdd.config.yaml")
	require.NoError(t, os.Mkdir(path, 0o755))

	assert.Error(t, Write(builtConfig(t), testSpecs(), path, true))
	assert.NoFileExists(t, path+".tmp")
}

func TestWriteRenderErrorLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cdd.config.yaml")
	err := Write(nil, testSpecs(), path, false)
	require.Error(t, err)
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no file and no temp file")
}
