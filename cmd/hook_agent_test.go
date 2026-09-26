package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeSettingsRel = ".claude/settings.json"

// claudeSettings is the project settings file of Claude Code under dir.
func claudeSettings(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(claudeSettingsRel))
}

func readSettings(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestHookHelpListsClaude(t *testing.T) { // TC-C1
	stdout, _, code := runCdd(t, t.TempDir(), "hook")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "claude")
	assert.Contains(t, stdout, "git")
}

func TestHookClaudeWithoutConfiguration(t *testing.T) { // TC-C2
	dir := t.TempDir()

	_, stderr, code := runCdd(t, dir, "hook", "claude")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cdd: cdd.config.yaml not found; run cdd init first")
	assert.NoFileExists(t, claudeSettings(dir))
}

func TestHookClaudeInstalls(t *testing.T) { // TC-C3
	dir := t.TempDir()
	writeTSFixture(t, dir)

	stdout, stderr, code := runCdd(t, dir, "hook", "claude")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Equal(t, "installed claude hook at "+claudeSettingsRel+"\n", stdout)
	body := readSettings(t, claudeSettings(dir))
	assert.Contains(t, body, `"command": "cdd check --agent claude"`)
	assert.Contains(t, body, `"matcher": "Edit|Write"`)
	assert.NotContains(t, body, "--config")
}

func TestHookClaudeTwiceUpdates(t *testing.T) { // TC-C4
	dir := t.TempDir()
	writeTSFixture(t, dir)
	runCdd(t, dir, "hook", "claude")
	first := readSettings(t, claudeSettings(dir))

	stdout, _, code := runCdd(t, dir, "hook", "claude")
	assert.Equal(t, 0, code)
	assert.Equal(t, "updated claude hook at "+claudeSettingsRel+"\n", stdout)
	assert.Equal(t, first, readSettings(t, claudeSettings(dir)))
}

func TestHookClaudeLocal(t *testing.T) { // TC-C5
	dir := t.TempDir()
	writeTSFixture(t, dir)

	stdout, _, code := runCdd(t, dir, "hook", "claude", "--local")
	assert.Equal(t, 0, code)
	assert.Equal(t, "installed claude hook at .claude/settings.local.json\n", stdout)
	assert.FileExists(t, filepath.Join(dir, ".claude", "settings.local.json"))
	assert.NoFileExists(t, claudeSettings(dir))
}

func TestHookClaudeBakesANonDefaultConfig(t *testing.T) { // TC-C6
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	writeTSFixture(t, sub)

	_, stderr, code := runCdd(t, dir, "--config", "sub/cdd.config.yaml", "hook", "claude")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, readSettings(t, claudeSettings(dir)),
		`"command": "cdd check --agent claude --config 'sub/cdd.config.yaml'"`)
}

func TestHookClaudeRefusesAConfigOutsideTheProject(t *testing.T) { // TC-C7
	parent := t.TempDir()
	writeTSFixture(t, parent)
	dir := filepath.Join(parent, "project")
	require.NoError(t, os.Mkdir(dir, 0o755))

	_, stderr, code := runCdd(t, dir, "--config", "../cdd.config.yaml", "hook", "claude")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "../cdd.config.yaml is outside")
	assert.NoFileExists(t, claudeSettings(dir))
}

func TestHookClaudeKeepsAndRefusesExistingFiles(t *testing.T) { // TC-C8
	t.Run("keeps other keys", func(t *testing.T) {
		dir := t.TempDir()
		writeTSFixture(t, dir)
		writeFixtureFile(t, dir, claudeSettingsRel, `{"permissions": {"allow": ["Bash(go test:*)"]}}`+"\n")

		_, _, code := runCdd(t, dir, "hook", "claude")
		assert.Equal(t, 0, code)
		body := readSettings(t, claudeSettings(dir))
		assert.Contains(t, body, `"Bash(go test:*)"`)
		assert.Contains(t, body, "cdd check --agent claude")
	})
	t.Run("refuses a file that is not JSON", func(t *testing.T) {
		dir := t.TempDir()
		writeTSFixture(t, dir)
		writeFixtureFile(t, dir, claudeSettingsRel, "not json\n")

		_, stderr, code := runCdd(t, dir, "hook", "claude")
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr, "settings.json")
		assert.Contains(t, stderr, "left untouched")
		assert.Equal(t, "not json\n", readSettings(t, claudeSettings(dir)))
	})
}

func TestHookClaudeRemove(t *testing.T) { // TC-C9
	dir := t.TempDir()
	writeTSFixture(t, dir)
	runCdd(t, dir, "hook", "claude")

	stdout, _, code := runCdd(t, dir, "hook", "claude", "--remove")
	assert.Equal(t, 0, code)
	assert.Equal(t, "removed cdd hook from "+claudeSettingsRel+"\n", stdout)
	assert.NoFileExists(t, claudeSettings(dir))

	stdout, _, code = runCdd(t, dir, "hook", "claude", "--remove")
	assert.Equal(t, 0, code)
	assert.Equal(t, "no cdd hook found at "+claudeSettingsRel+"\n", stdout)
}

func TestHookClaudeRejectsArguments(t *testing.T) { // TC-C10
	dir := t.TempDir()
	writeTSFixture(t, dir)

	_, _, code := runCdd(t, dir, "hook", "claude", "extra")
	assert.Equal(t, 1, code)
}
