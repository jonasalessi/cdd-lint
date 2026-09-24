package cmd

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// initRepo turns dir into a repository with an identity, so commits work
// without a global configuration.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.name", "cdd test")
	gitRun(t, dir, "config", "user.email", "cdd@example.com")
}

// gitRun runs one git command in dir and fails the test when git does.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitTry(dir, args...)
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return out
}

// gitTry runs one git command in dir and returns its combined output.
func gitTry(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// stage adds names to the index.
func stage(t *testing.T, dir string, names ...string) {
	t.Helper()
	gitRun(t, dir, append([]string{"add", "--"}, names...)...)
}
