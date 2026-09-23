package githook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hookIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "pre-commit")
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

func TestInstallCreatesTheFile(t *testing.T) { // TC-H1
	path := hookIn(t)
	block := Block("")

	require.NoError(t, Install(path, block))

	assert.Equal(t, "#!/bin/sh\n\n"+block+"\n", read(t, path))
	assert.Equal(t, os.FileMode(0o755), mode(t, path))
}

func TestInstallInsertsAfterTheShebang(t *testing.T) { // TC-H2
	path := hookIn(t)
	write(t, path, "#!/bin/bash\nnpm test\n", 0o755)

	require.NoError(t, Install(path, Block("")))

	assert.Equal(t, "#!/bin/bash\n\n"+Block("")+"\n\nnpm test\n", read(t, path))
}

func TestInstallAcceptsShellShebangs(t *testing.T) { // TC-H3
	for _, shebang := range []string{"#!/usr/bin/env zsh", "#!/bin/sh -e", "#!/usr/bin/env -S bash -e"} {
		t.Run(shebang, func(t *testing.T) {
			path := hookIn(t)
			write(t, path, shebang+"\necho hi\n", 0o755)
			require.NoError(t, Install(path, Block("")))
			assert.True(t, strings.HasPrefix(read(t, path), shebang+"\n\n"+BeginMarker))
		})
	}
}

func TestInstallWithoutShebangPutsTheBlockFirst(t *testing.T) { // TC-H4
	path := hookIn(t)
	write(t, path, "npm test\n", 0o755)

	require.NoError(t, Install(path, Block("")))

	assert.Equal(t, Block("")+"\n\nnpm test\n", read(t, path))
}

func TestInstallIsIdempotent(t *testing.T) { // TC-H5
	path := hookIn(t)
	write(t, path, "#!/bin/sh\nnpm test\n", 0o755)
	require.NoError(t, Install(path, Block("")))
	first := read(t, path)

	require.NoError(t, Install(path, Block("")))

	assert.Equal(t, first, read(t, path))
}

func TestInstallReplacesTheBlockInPlace(t *testing.T) { // TC-H6
	path := hookIn(t)
	write(t, path, "#!/bin/sh\nbefore\n"+Block("")+"\nafter\n", 0o755)

	require.NoError(t, Install(path, Block("sub/cdd.config.yaml")))

	assert.Equal(t, "#!/bin/sh\nbefore\n"+Block("sub/cdd.config.yaml")+"\nafter\n", read(t, path))
}

func TestInstallMakesTheFileExecutable(t *testing.T) { // TC-H7
	t.Run("0644 becomes 0755", func(t *testing.T) {
		path := hookIn(t)
		write(t, path, "#!/bin/sh\n", 0o644)
		require.NoError(t, Install(path, Block("")))
		assert.Equal(t, os.FileMode(0o755), mode(t, path))
	})
	t.Run("0700 stays 0700", func(t *testing.T) {
		path := hookIn(t)
		write(t, path, "#!/bin/sh\n", 0o700)
		require.NoError(t, Install(path, Block("")))
		assert.Equal(t, os.FileMode(0o700), mode(t, path))
	})
}

func TestInstallRefusesAForeignHook(t *testing.T) { // TC-H8
	path := hookIn(t)
	body := "#!/usr/bin/env python3\nprint('hi')\n"
	write(t, path, body, 0o755)

	err := Install(path, Block(""))

	require.ErrorIs(t, err, ErrForeignHook)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "#!/usr/bin/env python3")
	assert.Equal(t, body, read(t, path))
}

func TestSymlinkIsRefused(t *testing.T) { // TC-H9
	dir := t.TempDir()
	target := filepath.Join(dir, "managed")
	write(t, target, "#!/bin/sh\nmanaged\n", 0o755)
	path := filepath.Join(dir, "pre-commit")
	require.NoError(t, os.Symlink(target, path))

	assert.ErrorIs(t, Install(path, Block("")), ErrSymlink)
	_, err := Remove(path)
	assert.ErrorIs(t, err, ErrSymlink)
	assert.Equal(t, "#!/bin/sh\nmanaged\n", read(t, target))
}

func TestRemoveMissingFile(t *testing.T) { // TC-H10
	removed, err := Remove(hookIn(t))
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestRemoveWithoutBlock(t *testing.T) { // TC-H11
	path := hookIn(t)
	write(t, path, "#!/bin/sh\nnpm test\n", 0o755)

	removed, err := Remove(path)

	require.NoError(t, err)
	assert.False(t, removed)
	assert.Equal(t, "#!/bin/sh\nnpm test\n", read(t, path))
}

func TestRemoveDeletesAFileTheCommandCreated(t *testing.T) { // TC-H12
	path := hookIn(t)
	require.NoError(t, Install(path, Block("")))

	removed, err := Remove(path)

	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoFileExists(t, path)
}

func TestRemoveRestoresAForeignBody(t *testing.T) { // TC-H13
	path := hookIn(t)
	write(t, path, "#!/bin/bash\nnpm test\n", 0o700)
	require.NoError(t, Install(path, Block("")))

	removed, err := Remove(path)

	require.NoError(t, err)
	assert.True(t, removed)
	assert.Equal(t, "#!/bin/bash\nnpm test\n", read(t, path))
	assert.Equal(t, os.FileMode(0o700), mode(t, path))
}

func TestInstalled(t *testing.T) { // TC-H14
	path := hookIn(t)
	ok, err := Installed(path)
	require.NoError(t, err)
	assert.False(t, ok, "missing file")

	write(t, path, "#!/bin/sh\nnpm test\n", 0o755)
	ok, err = Installed(path)
	require.NoError(t, err)
	assert.False(t, ok, "foreign file")

	require.NoError(t, Install(path, Block("")))
	ok, err = Installed(path)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestBlock(t *testing.T) { // TC-H15
	assert.NotContains(t, Block(""), "--config")
	assert.Contains(t, Block("sub/cdd.config.yaml"), "cdd check --staged --config 'sub/cdd.config.yaml' || exit $?")
	assert.Contains(t, Block("it's/cdd.config.yaml"), `--config 'it'\''s/cdd.config.yaml'`)
	assert.True(t, strings.HasPrefix(Block(""), BeginMarker+"\n"))
	assert.True(t, strings.HasSuffix(Block(""), "\n"+EndMarker))
}

func TestFailedWriteLeavesNoTempFile(t *testing.T) { // TC-H16
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	path := filepath.Join(dir, "pre-commit")

	require.Error(t, Install(path, Block("")))

	assert.NoFileExists(t, path+".tmp")
}
