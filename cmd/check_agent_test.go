package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentFixture is a TypeScript project with one clean file, no repository
// needed: the agent hook works from the working directory alone.
func agentFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	return dir
}

// claudeEvent is the smallest PostToolUse document Claude Code sends,
// naming the edited file by its absolute path.
func claudeEvent(t *testing.T, dir, rel string) string {
	t.Helper()
	doc := map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": filepath.Join(dir, filepath.FromSlash(rel))},
	}
	data, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(data)
}

func TestCheckAgentRejectsAnUnknownAgent(t *testing.T) { // TC-A1
	dir := agentFixture(t)

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/greeter.ts"), "check", "--agent", "nope")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, `--agent: "nope" is not one of claude`)
	assert.Empty(t, stdout)
}

func TestCheckAgentRefusesPathsAndStaged(t *testing.T) { // TC-A2
	dir := agentFixture(t)

	_, stderr, code := runCdd(t, dir, "check", "--agent", "claude", "src/greeter.ts")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "--agent takes no paths")

	_, stderr, code = runCdd(t, dir, "check", "--agent", "claude", "--staged")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "--agent and --staged are exclusive")
}

func TestCheckAgentCleanFileIsSilent(t *testing.T) { // TC-A3
	dir := agentFixture(t)

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/greeter.ts"), "check", "--agent", "claude")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestCheckAgentOverLimitFeedsBack(t *testing.T) { // TC-A4
	dir := agentFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/order-service.ts"), "check", "--agent", "claude")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stderr, violationLabel+" src/order-service.ts:1:8 class OrderService")
}

func TestCheckAgentIgnoresEnforcement(t *testing.T) { // TC-A5
	dir := agentFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	legacyProject(t, dir, "false", "measure_only")

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/order-service.ts"), "check", "--agent", "claude")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "violations=1")
	assert.Contains(t, stderr, "OrderService")
}

func TestCheckAgentUnclaimedFileIsSilent(t *testing.T) { // TC-A6
	dir := agentFixture(t)
	writeFixtureFile(t, dir, "README.md", "# docs\n")

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "README.md"), "check", "--agent", "claude")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestCheckAgentFileOutsideTheProjectIsSilent(t *testing.T) { // TC-A7
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	writeTSFixture(t, sub)
	writeFixtureFile(t, dir, "order-service.ts", overLimitSource)

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "order-service.ts"),
		"--config", "sub/cdd.config.yaml", "check", "--agent", "claude")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestCheckAgentRejectsABadEvent(t *testing.T) { // TC-A8
	dir := agentFixture(t)

	stdout, stderr, code := runCddIn(t, dir, "not json", "check", "--agent", "claude")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cdd: claude hook event")
	assert.Empty(t, stdout)

	bashEvent := `{"tool_name":"Bash","tool_input":{"command":"ls"}}`
	_, stderr, code = runCddIn(t, dir, bashEvent, "check", "--agent", "claude")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "no tool_input.file_path")
}

func TestCheckAgentFormatAndExplainCompose(t *testing.T) { // TC-A9
	dir := agentFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	event := claudeEvent(t, dir, "src/order-service.ts")

	_, stderr, code := runCddIn(t, dir, event, "check", "--agent", "claude", "--format", "json")
	assert.Equal(t, 2, code)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stderr), &doc), "stderr must be the json document")

	_, stderr, code = runCddIn(t, dir, event, "check", "--agent", "claude", "--explain")
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr, "icp: ")
}

func TestCheckAgentNeverWritesTheOutputFile(t *testing.T) { // TC-A10
	dir := agentFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	editConfig(t, dir, "  outputFile: null", `  outputFile: "report.txt"`)

	stdout, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/order-service.ts"), "check", "--agent", "claude")
	assert.Equal(t, 2, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "OrderService")
	assert.NoFileExists(t, filepath.Join(dir, "report.txt"))
}

func TestCheckAgentMissingFileIsAnError(t *testing.T) { // TC-A11
	dir := agentFixture(t)

	_, stderr, code := runCddIn(t, dir, claudeEvent(t, dir, "src/gone.ts"), "check", "--agent", "claude")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "gone.ts")
}
