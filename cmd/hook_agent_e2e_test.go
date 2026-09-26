package cmd

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installedCommand reads the command "cdd hook claude" wrote, so the test
// runs exactly what Claude Code would.
func installedCommand(t *testing.T, dir string) string {
	t.Helper()
	var doc struct {
		Hooks struct {
			PostToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PostToolUse"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal([]byte(readSettings(t, claudeSettings(dir))), &doc))
	require.Len(t, doc.Hooks.PostToolUse, 1)
	require.Len(t, doc.Hooks.PostToolUse[0].Hooks, 1)
	return doc.Hooks.PostToolUse[0].Hooks[0].Command
}

// runHookCommand runs command the way Claude Code does: through sh from
// the project directory, with the event on stdin and the binary on PATH.
// It returns the two streams and the exit code.
func runHookCommand(t *testing.T, env e2eEnv, command, event string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), env.sh, "-c", command)
	cmd.Dir = env.dir
	cmd.Env = append(envWithout("PATH"), "PATH="+env.path)
	cmd.Stdin = strings.NewReader(event)
	var outBuf, errBuf strings.Builder
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "sh -c %q: %s", command, errBuf.String())
	return outBuf.String(), errBuf.String(), exitErr.ExitCode()
}

func TestHookClaudeEndToEnd(t *testing.T) { // TC-E1
	if testing.Short() {
		t.Skip("builds the binary")
	}
	shPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not on PATH")
	}
	bin := buildCdd(t)
	dir := t.TempDir()
	env := e2eEnv{
		dir: dir, cdd: filepath.Join(bin, "cdd"), sh: shPath,
		path: bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	writeTSFixture(t, dir)

	out, code := env.run(t, "cdd", "hook", "claude")
	require.Equal(t, 0, code, out)
	command := installedCommand(t, dir)

	t.Run("a clean edit is silent", func(t *testing.T) {
		writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
		stdout, stderr, code := runHookCommand(t, env, command, claudeEvent(t, dir, "src/greeter.ts"))
		assert.Equal(t, 0, code, stderr)
		assert.Empty(t, stdout)
		assert.Empty(t, stderr)
	})
	t.Run("an over-limit edit is fed back", func(t *testing.T) {
		writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
		stdout, stderr, code := runHookCommand(t, env, command, claudeEvent(t, dir, "src/order-service.ts"))
		assert.Equal(t, 2, code)
		assert.Empty(t, stdout)
		assert.Contains(t, stderr, "cdd check: FAIL")
		assert.Contains(t, stderr, "class OrderService")
	})
	t.Run("an unclaimed edit is silent", func(t *testing.T) {
		writeFixtureFile(t, dir, "README.md", "# fixture\n")
		stdout, stderr, code := runHookCommand(t, env, command, claudeEvent(t, dir, "README.md"))
		assert.Equal(t, 0, code, stderr)
		assert.Empty(t, stdout)
		assert.Empty(t, stderr)
	})
	t.Run("--remove deletes the file", func(t *testing.T) {
		out, code := env.run(t, "cdd", "hook", "claude", "--remove")
		require.Equal(t, 0, code, out)
		assert.NoFileExists(t, claudeSettings(dir))
	})
}
