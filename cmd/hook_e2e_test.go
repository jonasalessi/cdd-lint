package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildCdd compiles the binary into a fresh directory and returns that
// directory, ready to be put on PATH.
func buildCdd(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	bin := t.TempDir()
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", filepath.Join(bin, "cdd"), ".")
	cmd.Dir = filepath.Dir(filepath.Dir(file))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// e2eEnv runs programs by absolute path with a PATH of its own, which is
// what the hook sees; exec resolves the program itself through the
// process's PATH, so the absolute path keeps the two apart.
type e2eEnv struct {
	dir, path, cdd, git, sh string
}

// run runs prog ("cdd" or "git") with args and returns its combined output
// and exit code.
func (e e2eEnv) run(t *testing.T, prog string, args ...string) (string, int) {
	t.Helper()
	bin := map[string]string{"cdd": e.cdd, "git": e.git}[prog]
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Dir = e.dir
	cmd.Env = append(envWithout("PATH"), "PATH="+e.path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s %s: %s", prog, strings.Join(args, " "), out)
	return string(out), exitErr.ExitCode()
}

// envWithout is the environment minus one variable.
func envWithout(name string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, name+"=") {
			env = append(env, kv)
		}
	}
	return env
}

func TestHookGitEndToEnd(t *testing.T) { // TC-E1, TC-E2
	if testing.Short() {
		t.Skip("builds the binary")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	bin := buildCdd(t)
	dir := t.TempDir()
	withCdd := e2eEnv{
		dir: dir, cdd: filepath.Join(bin, "cdd"), git: gitPath,
		path: bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	withoutCdd := withCdd
	withoutCdd.path = filepath.Dir(gitPath)
	initRepo(t, dir)
	writeTSFixture(t, dir)
	stage(t, dir, ".")
	gitRun(t, dir, "commit", "-q", "-m", "config")

	out, code := withCdd.run(t, "cdd", "hook", "git")
	require.Equal(t, 0, code, out)

	t.Run("a clean commit passes", func(t *testing.T) {
		writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
		writeFixtureFile(t, dir, "README.md", "# fixture\n")
		stage(t, dir, "src/greeter.ts", "README.md")
		out, code := withCdd.run(t, "git", "commit", "-q", "-m", "clean")
		assert.Equal(t, 0, code, out)
		assert.Contains(t, out, "cdd check: PASS")
	})
	t.Run("an over-limit commit is blocked", func(t *testing.T) {
		writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
		stage(t, dir, "src/order-service.ts")
		out, code := withCdd.run(t, "git", "commit", "-q", "-m", "over")
		assert.NotEqual(t, 0, code)
		assert.Contains(t, out, "cdd check: FAIL")
		assert.Contains(t, out, "class OrderService")
	})
	t.Run("a missing cdd blocks the commit", func(t *testing.T) { // TC-E2
		out, code := withoutCdd.run(t, "git", "commit", "-q", "-m", "over")
		assert.NotEqual(t, 0, code)
		assert.Contains(t, out, "cdd: not found in PATH")
	})
	t.Run("--no-verify skips the hook", func(t *testing.T) {
		out, code := withCdd.run(t, "git", "commit", "-q", "--no-verify", "-m", "over")
		assert.Equal(t, 0, code, out)
	})
	t.Run("after --remove the commit passes", func(t *testing.T) {
		out, code := withCdd.run(t, "cdd", "hook", "git", "--remove")
		require.Equal(t, 0, code, out)
		writeFixtureFile(t, dir, "src/order-service.ts", strings.ReplaceAll(overLimitSource, "OrderService", "Worse"))
		stage(t, dir, "src/order-service.ts")
		out, code = withCdd.run(t, "git", "commit", "-q", "-m", "unchecked")
		assert.Equal(t, 0, code, out)
		assert.NotContains(t, out, "cdd check")
	})
}
