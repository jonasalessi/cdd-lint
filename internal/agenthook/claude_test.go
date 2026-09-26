package agenthook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCommand = "cdd check --agent claude"
	// foreignSettings holds everything a project might already have next to
	// the cdd entry: other keys, another event, another PostToolUse entry
	// and a number that must survive a round trip verbatim.
	foreignSettings = `{
  "permissions": {"allow": ["Bash(go test:*)"]},
  "cleanupPeriodDays": 30,
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo bash"}]}]
  }
}
`
)

func settingsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), ".claude", "settings.json")
}

func writeSettings(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func readSettingsFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// decode parses a settings file into generic JSON for structural asserts.
func decode(t *testing.T, content string) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(content), &doc))
	return doc
}

// postToolUseOf returns the PostToolUse entries of a decoded document.
func postToolUseOf(t *testing.T, doc map[string]any) []any {
	t.Helper()
	hooks, ok := doc["hooks"].(map[string]any)
	require.True(t, ok, "hooks object")
	entries, ok := hooks["PostToolUse"].([]any)
	require.True(t, ok, "PostToolUse list")
	return entries
}

// commandOf returns the single command of one PostToolUse entry.
func commandOf(t *testing.T, entry any) string {
	t.Helper()
	m, ok := entry.(map[string]any)
	require.True(t, ok)
	hooks, ok := m["hooks"].([]any)
	require.True(t, ok)
	require.Len(t, hooks, 1)
	hook, ok := hooks[0].(map[string]any)
	require.True(t, ok)
	command, ok := hook["command"].(string)
	require.True(t, ok)
	return command
}

func TestClaudeInstallCreatesTheFile(t *testing.T) { // TC-K1
	path := settingsPath(t)

	updated, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)
	assert.False(t, updated)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	content := readSettingsFile(t, path)
	assert.True(t, strings.HasSuffix(content, "\n"))
	assert.Contains(t, content, `"matcher": "Edit|Write"`)
	entries := postToolUseOf(t, decode(t, content))
	require.Len(t, entries, 1)
	assert.Equal(t, testCommand, commandOf(t, entries[0]))
	entry, ok := entries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Edit|Write", entry["matcher"])
}

func TestClaudeInstallKeepsForeignContent(t *testing.T) { // TC-K2
	path := settingsPath(t)
	writeSettings(t, path, foreignSettings)

	_, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)

	content := readSettingsFile(t, path)
	assert.Contains(t, content, `"cleanupPeriodDays": 30`)
	doc := decode(t, content)
	assert.Contains(t, doc, "permissions")
	hooks, ok := doc["hooks"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, hooks, "Stop")
	entries := postToolUseOf(t, doc)
	require.Len(t, entries, 2)
	assert.Equal(t, "echo bash", commandOf(t, entries[0]))
	assert.Equal(t, testCommand, commandOf(t, entries[1]))
}

func TestClaudeInstallTwiceIsIdempotent(t *testing.T) { // TC-K3
	path := settingsPath(t)
	_, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)
	first := readSettingsFile(t, path)

	updated, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)
	assert.True(t, updated)
	assert.Equal(t, first, readSettingsFile(t, path))
}

func TestClaudeInstallReplacesInPlace(t *testing.T) { // TC-K4
	path := settingsPath(t)
	writeSettings(t, path, `{"hooks": {"PostToolUse": [
  {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo bash"}]},
  {"matcher": "Edit|Write", "hooks": [{"type": "command", "command": "cdd check --agent claude"}]},
  {"matcher": "Write", "hooks": [{"type": "command", "command": "echo after"}]}
]}}
`)

	withConfig := testCommand + " --config 'sub/cdd.config.yaml'"
	updated, err := claude{}.Install(path, withConfig)
	require.NoError(t, err)
	assert.True(t, updated)
	entries := postToolUseOf(t, decode(t, readSettingsFile(t, path)))
	require.Len(t, entries, 3)
	assert.Equal(t, "echo bash", commandOf(t, entries[0]))
	assert.Equal(t, withConfig, commandOf(t, entries[1]))
	assert.Equal(t, "echo after", commandOf(t, entries[2]))
}

func TestClaudeInstallRefusesANonObject(t *testing.T) { // TC-K5
	for name, content := range map[string]string{"array": "[]\n", "text": "not json\n", "null": "null\n"} {
		t.Run(name, func(t *testing.T) {
			path := settingsPath(t)
			writeSettings(t, path, content)

			_, err := claude{}.Install(path, testCommand)
			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
			assert.Equal(t, content, readSettingsFile(t, path))
		})
	}
}

func TestClaudeInstallRefusesAWrongEventType(t *testing.T) { // TC-K6
	path := settingsPath(t)
	content := `{"hooks": {"PostToolUse": {"matcher": "Edit"}}}` + "\n"
	writeSettings(t, path, content)

	_, err := claude{}.Install(path, testCommand)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "PostToolUse is not a list")
	assert.Equal(t, content, readSettingsFile(t, path))
}

func TestClaudeRefusesASymlink(t *testing.T) { // TC-K7
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	require.NoError(t, os.WriteFile(target, []byte("{}\n"), 0o644))
	link := filepath.Join(dir, "settings.json")
	require.NoError(t, os.Symlink(target, link))

	_, err := claude{}.Install(link, testCommand)
	require.ErrorIs(t, err, ErrSymlink)
	_, err = claude{}.Remove(link)
	require.ErrorIs(t, err, ErrSymlink)
	assert.Equal(t, "{}\n", readSettingsFile(t, target))
}

func TestClaudeRemoveWithoutAnEntry(t *testing.T) { // TC-K8
	path := settingsPath(t)
	removed, err := claude{}.Remove(path)
	require.NoError(t, err)
	assert.False(t, removed)
	assert.NoFileExists(t, path)

	writeSettings(t, path, foreignSettings)
	removed, err = claude{}.Remove(path)
	require.NoError(t, err)
	assert.False(t, removed)
	assert.Equal(t, foreignSettings, readSettingsFile(t, path))
}

func TestClaudeRemoveDeletesWhatItCreated(t *testing.T) { // TC-K9
	path := settingsPath(t)
	_, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)

	removed, err := claude{}.Remove(path)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoFileExists(t, path)
}

func TestClaudeRemoveKeepsForeignContent(t *testing.T) { // TC-K10
	path := settingsPath(t)
	writeSettings(t, path, foreignSettings)
	_, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)

	removed, err := claude{}.Remove(path)
	require.NoError(t, err)
	assert.True(t, removed)
	content := readSettingsFile(t, path)
	assert.Contains(t, content, `"cleanupPeriodDays": 30`)
	assert.NotContains(t, content, testCommand)
	doc := decode(t, content)
	assert.Contains(t, doc, "permissions")
	entries := postToolUseOf(t, doc)
	require.Len(t, entries, 1)
	assert.Equal(t, "echo bash", commandOf(t, entries[0]))
}

func TestClaudeRemovePrunesAnEmptyEvent(t *testing.T) { // TC-K11
	path := settingsPath(t)
	writeSettings(t, path, `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say done"}]}]}}`)
	_, err := claude{}.Install(path, testCommand)
	require.NoError(t, err)

	removed, err := claude{}.Remove(path)
	require.NoError(t, err)
	assert.True(t, removed)
	doc := decode(t, readSettingsFile(t, path))
	hooks, ok := doc["hooks"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, hooks, "Stop")
	assert.NotContains(t, hooks, "PostToolUse")
}

func TestClaudeEditedFile(t *testing.T) { // TC-K12
	event := `{"session_id":"s","cwd":"/work","hook_event_name":"PostToolUse","tool_name":"Edit",` +
		`"tool_input":{"file_path":"/work/src/a.ts","old_string":"a","new_string":"b"},` +
		`"tool_response":{"filePath":"/work/src/a.ts"}}`
	path, err := claude{}.EditedFile([]byte(event))
	require.NoError(t, err)
	assert.Equal(t, "/work/src/a.ts", path)

	_, err = claude{}.EditedFile([]byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tool_input.file_path")

	_, err = claude{}.EditedFile([]byte("not json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude hook event")
}

func TestClaudeNames(t *testing.T) { // TC-K13
	assert.Equal(t, "claude", claude{}.ID())
	assert.Equal(t, ".claude/settings.json", claude{}.Settings(false))
	assert.Equal(t, ".claude/settings.local.json", claude{}.Settings(true))
	assert.NotEmpty(t, claude{}.Summary())
}

func TestClaudeFailedWriteLeavesNoTemp(t *testing.T) { // TC-K14
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), ".claude")
	require.NoError(t, os.Mkdir(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	path := filepath.Join(dir, "settings.json")

	_, err := claude{}.Install(path, testCommand)
	require.Error(t, err)
	assert.NoFileExists(t, path+".tmp")
}
