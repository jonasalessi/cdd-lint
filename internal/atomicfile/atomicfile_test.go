package atomicfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// entries lists the names in dir, so a test can tell a leftover temp file.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteCreatesTheFileWithItsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	require.NoError(t, Write(path, []byte("{}\n"), 0o755))

	assert.Equal(t, "{}\n", read(t, path))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	assert.Equal(t, []string{"settings.json"}, entries(t, dir))
}

func TestWriteReplacesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	require.NoError(t, Write(path, []byte("new"), 0o644))

	assert.Equal(t, "new", read(t, path))
}

// A repository can commit a symlink where the old code put its scratch
// file; the write must not go through it.
func TestWriteDoesNotFollowAPlantedTempPath(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(t.TempDir(), "canary")
	require.NoError(t, os.WriteFile(canary, []byte("canary"), 0o644))
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.Symlink(canary, path+".tmp"))

	require.NoError(t, Write(path, []byte("{}\n"), 0o644))

	assert.Equal(t, "canary", read(t, canary))
	assert.Equal(t, "{}\n", read(t, path))
}

func TestWriteReplacesASymlinkInsteadOfFollowingIt(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(t.TempDir(), "canary")
	require.NoError(t, os.WriteFile(canary, []byte("canary"), 0o644))
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.Symlink(canary, path))

	require.NoError(t, Write(path, []byte("{}\n"), 0o644))

	assert.Equal(t, "canary", read(t, canary))
	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "the link is replaced by a regular file")
}

func TestFailedRenameLeavesNoTempFile(t *testing.T) {
	// A directory cannot be replaced by a rename.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.Mkdir(path, 0o755))

	require.Error(t, Write(path, []byte("{}\n"), 0o644))

	assert.Equal(t, []string{"settings.json"}, entries(t, dir))
}

func TestWriteIntoAMissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "settings.json")
	assert.Error(t, Write(path, []byte("{}\n"), 0o644))
}
