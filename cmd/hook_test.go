package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/githook"
)

// hookRepo is a repository with a configuration and no hook yet.
func hookRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	writeTSFixture(t, dir)
	return dir
}

func defaultHook(dir string) string {
	return filepath.Join(dir, ".git", "hooks", "pre-commit")
}

func readHookFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestHookPrintsHelp(t *testing.T) { // TC-C1
	stdout, _, code := runCdd(t, t.TempDir(), "hook")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "cdd hook git")
}

func TestHookGitOutsideARepository(t *testing.T) { // TC-C2
	dir := t.TempDir()
	writeTSFixture(t, dir)

	_, stderr, code := runCdd(t, dir, "hook", "git")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cdd: not a git repository")
}

func TestHookGitWithoutConfiguration(t *testing.T) { // TC-C3
	dir := t.TempDir()
	initRepo(t, dir)

	_, stderr, code := runCdd(t, dir, "hook", "git")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cdd: cdd.config.yaml not found; run cdd init first")
	assert.NoFileExists(t, defaultHook(dir))
}

func TestHookGitInstalls(t *testing.T) { // TC-C4
	dir := hookRepo(t)

	stdout, stderr, code := runCdd(t, dir, "hook", "git")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, "installed pre-commit hook at .git/hooks/pre-commit\n", stdout)
	info, err := os.Stat(defaultHook(dir))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "executable")
	body := readHookFile(t, defaultHook(dir))
	assert.Contains(t, body, githook.Block(""))
	assert.NotContains(t, body, "--config")
}

func TestHookGitTwiceUpdates(t *testing.T) { // TC-C5
	dir := hookRepo(t)
	runCdd(t, dir, "hook", "git")
	first := readHookFile(t, defaultHook(dir))

	stdout, _, code := runCdd(t, dir, "hook", "git")
	assert.Equal(t, 0, code)
	assert.Equal(t, "updated pre-commit hook at .git/hooks/pre-commit\n", stdout)
	assert.Equal(t, first, readHookFile(t, defaultHook(dir)))
}

func TestHookGitHonorsHooksPath(t *testing.T) { // TC-C6
	dir := hookRepo(t)
	gitRun(t, dir, "config", "core.hooksPath", ".githooks")

	stdout, stderr, code := runCdd(t, dir, "hook", "git")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, "installed pre-commit hook at .githooks/pre-commit\n", stdout)
	assert.FileExists(t, filepath.Join(dir, ".githooks", "pre-commit"))
	assert.NoFileExists(t, defaultHook(dir))
}

func TestHookGitKeepsAnExistingShellHook(t *testing.T) { // TC-C7
	dir := hookRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultHook(dir)), 0o755))
	require.NoError(t, os.WriteFile(defaultHook(dir), []byte("#!/bin/sh\nnpm test\n"), 0o755))

	stdout, _, code := runCdd(t, dir, "hook", "git")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "installed")
	body := readHookFile(t, defaultHook(dir))
	assert.True(t, strings.HasPrefix(body, "#!/bin/sh\n\n"+githook.BeginMarker))
	assert.True(t, strings.HasSuffix(body, "\nnpm test\n"))
}

func TestHookGitRefusesAForeignHook(t *testing.T) { // TC-C8
	dir := hookRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultHook(dir)), 0o755))
	require.NoError(t, os.WriteFile(defaultHook(dir), []byte("#!/usr/bin/env node\nrun()\n"), 0o755))

	_, stderr, code := runCdd(t, dir, "hook", "git")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "pre-commit")
	assert.Contains(t, stderr, "left untouched")
	assert.Equal(t, "#!/usr/bin/env node\nrun()\n", readHookFile(t, defaultHook(dir)))
}

func TestHookGitBakesANonDefaultConfig(t *testing.T) { // TC-C9
	dir := t.TempDir()
	initRepo(t, dir)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	writeTSFixture(t, sub)

	t.Run("from the top level with --config", func(t *testing.T) {
		_, stderr, code := runCdd(t, dir, "hook", "git", "--config", "sub/cdd.config.yaml")
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Contains(t, readHookFile(t, defaultHook(dir)), "--config 'sub/cdd.config.yaml'")
	})
	t.Run("from the subdirectory with the default path", func(t *testing.T) {
		stdout, stderr, code := runCdd(t, sub, "hook", "git")
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Contains(t, readHookFile(t, defaultHook(dir)), "--config 'sub/cdd.config.yaml'")
		resolved, err := filepath.EvalSymlinks(defaultHook(dir))
		require.NoError(t, err)
		assert.Equal(t, "updated pre-commit hook at "+resolved+"\n", stdout, "outside the working directory")
	})
}

func TestHookGitConfigOutsideTheRepository(t *testing.T) { // TC-C10
	dir := hookRepo(t)
	elsewhere := t.TempDir()
	writeTSFixture(t, elsewhere)
	outside := filepath.Join(elsewhere, "cdd.config.yaml")

	_, stderr, code := runCdd(t, dir, "hook", "git", "--config", outside)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, outside)
	assert.Contains(t, stderr, "outside the repository")
}

func TestHookGitRemove(t *testing.T) { // TC-C11
	dir := hookRepo(t)
	runCdd(t, dir, "hook", "git")

	stdout, _, code := runCdd(t, dir, "hook", "git", "--remove")
	assert.Equal(t, 0, code)
	assert.Equal(t, "removed cdd hook from .git/hooks/pre-commit\n", stdout)
	assert.NoFileExists(t, defaultHook(dir))
}

func TestHookGitRemoveWithoutHook(t *testing.T) { // TC-C12
	dir := t.TempDir()
	initRepo(t, dir)

	stdout, _, code := runCdd(t, dir, "hook", "git", "--remove")
	assert.Equal(t, 0, code)
	assert.Equal(t, "no cdd hook found at .git/hooks/pre-commit\n", stdout)
}

func TestHookGitRemoveKeepsAForeignBody(t *testing.T) { // TC-C13
	dir := hookRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultHook(dir)), 0o755))
	require.NoError(t, os.WriteFile(defaultHook(dir), []byte("#!/bin/sh\nnpm test\n"), 0o755))
	runCdd(t, dir, "hook", "git")

	_, _, code := runCdd(t, dir, "hook", "git", "--remove")
	assert.Equal(t, 0, code)
	assert.Equal(t, "#!/bin/sh\nnpm test\n", readHookFile(t, defaultHook(dir)))
}

func TestHookGitRejectsArguments(t *testing.T) { // TC-C14
	_, _, code := runCdd(t, hookRepo(t), "hook", "git", "extra")
	assert.Equal(t, 1, code)
}
