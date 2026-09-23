package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initRepo creates a repository in dir with an identity, so commits work
// without a global configuration. Paths come back symlink-resolved, which
// is what git prints on macOS where the temp dir lives under /var.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.name", "cdd test")
	gitRun(t, dir, "config", "user.email", "cdd@example.com")
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return resolved
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

var ctx = context.Background()

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func TestToplevel(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	sub := filepath.Join(dir, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	t.Run("from the root", func(t *testing.T) { // TC-G1
		got, err := Toplevel(ctx, dir)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})
	t.Run("from a subdirectory", func(t *testing.T) { // TC-G2
		got, err := Toplevel(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})
}

func TestNotRepository(t *testing.T) { // TC-G3
	dir := t.TempDir()
	_, err := Toplevel(ctx, dir)
	assert.ErrorIs(t, err, ErrNotRepository)
	_, err = HooksDir(ctx, dir)
	assert.ErrorIs(t, err, ErrNotRepository)
	_, err = Staged(ctx, dir)
	assert.ErrorIs(t, err, ErrNotRepository)
}

func TestHooksDir(t *testing.T) {
	dir := initRepo(t, t.TempDir())
	sub := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	t.Run("default", func(t *testing.T) { // TC-G4
		require.NoError(t, os.RemoveAll(filepath.Join(dir, ".git", "hooks")))
		got, err := HooksDir(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, ".git", "hooks"), got)
	})
	t.Run("core.hooksPath", func(t *testing.T) { // TC-G5
		gitRun(t, dir, "config", "core.hooksPath", ".githooks")
		got, err := HooksDir(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, ".githooks"), got)
	})
}

func TestStaged(t *testing.T) {
	dir := initRepo(t, t.TempDir())

	t.Run("clean index", func(t *testing.T) { // TC-G6
		got, err := Staged(ctx, dir)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("added and modified", func(t *testing.T) { // TC-G7
		writeFile(t, dir, "a.ts", "export const a = 1\n")
		writeFile(t, dir, "sub/b.ts", "export const b = 1\n")
		gitRun(t, dir, "add", "a.ts", "sub/b.ts")
		gitRun(t, dir, "commit", "-q", "-m", "base")
		writeFile(t, dir, "a.ts", "export const a = 2\n")
		writeFile(t, dir, "sub/c.ts", "export const c = 1\n")
		gitRun(t, dir, "add", "a.ts", "sub/c.ts")
		got, err := Staged(ctx, filepath.Join(dir, "sub"))
		require.NoError(t, err)
		assert.Equal(t, []string{"a.ts", "sub/c.ts"}, got)
	})
	t.Run("deletion and rename", func(t *testing.T) { // TC-G8
		gitRun(t, dir, "commit", "-q", "-m", "more")
		gitRun(t, dir, "rm", "-q", "a.ts")
		gitRun(t, dir, "mv", "sub/b.ts", "sub/renamed.ts")
		got, err := Staged(ctx, dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"sub/renamed.ts"}, got)
	})
	t.Run("unusual names", func(t *testing.T) { // TC-G9
		gitRun(t, dir, "commit", "-q", "-m", "renamed")
		writeFile(t, dir, "with space.ts", "")
		writeFile(t, dir, "café.ts", "")
		gitRun(t, dir, "add", "with space.ts", "café.ts")
		got, err := Staged(ctx, dir)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"with space.ts", "café.ts"}, got)
	})
}

func TestMissingGitBinary(t *testing.T) { // TC-G10
	t.Setenv("PATH", t.TempDir())
	_, err := Toplevel(ctx, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git")
}
