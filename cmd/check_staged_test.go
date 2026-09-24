package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stagedFixture is a repository with a TypeScript configuration and one
// committed clean file, ready for a test to stage more.
func stagedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initRepo(t, dir)
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	stage(t, dir, ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func TestCheckStagedRefusesPaths(t *testing.T) { // TC-S1
	dir := stagedFixture(t)

	stdout, stderr, code := runCdd(t, dir, "check", "--staged", "src/greeter.ts")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "--staged takes no paths")
	assert.Empty(t, stdout)
}

func TestCheckStagedOutsideARepository(t *testing.T) { // TC-S2
	dir := t.TempDir()
	writeTSFixture(t, dir)

	stdout, stderr, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "cdd: not a git repository")
	assert.Empty(t, stdout)
}

func TestCheckStagedCleanIndexIsSilent(t *testing.T) { // TC-S3
	dir := stagedFixture(t)

	stdout, stderr, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestCheckStagedOnlyUnclaimedFilesIsSilent(t *testing.T) { // TC-S4
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "README.md", "# docs\n")
	stage(t, dir, "README.md")

	stdout, stderr, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestCheckStagedAnalyzesOnlyTheStagedFiles(t *testing.T) { // TC-S5
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "src/greeter.ts", strings.ReplaceAll(cleanSource, "Greeter", "Staged"))
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	stage(t, dir, "src/greeter.ts")

	stdout, stderr, code := runCdd(t, dir, "check", "--staged", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "cdd check: PASS violations=0 units=1")
	assert.Contains(t, stdout, "class Staged")
	assert.NotContains(t, stdout, "OrderService", "an unstaged file is not part of the commit")
}

func TestCheckStagedOverLimitBlocks(t *testing.T) { // TC-S6
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	stage(t, dir, "src/order-service.ts")

	stdout, _, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stdout, violationLabel+" src/order-service.ts:1:8 class OrderService")
}

func TestCheckStagedMeasureOnlyReports(t *testing.T) { // TC-S7
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	legacyProject(t, dir, "false", "measure_only")
	stage(t, dir, "src/order-service.ts")

	stdout, _, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 0, code)
	assert.Contains(t, stdout, "violations=1")
	assert.Contains(t, stdout, "OrderService")
}

func TestCheckStagedConfigInASubdirectory(t *testing.T) { // TC-S8
	dir := t.TempDir()
	initRepo(t, dir)
	writeTSFixture(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.Rename(filepath.Join(dir, "cdd.config.yaml"), filepath.Join(dir, "sub", "cdd.config.yaml")))
	writeFixtureFile(t, dir, "sub/src/inside.ts", strings.ReplaceAll(cleanSource, "Greeter", "Inside"))
	writeFixtureFile(t, dir, "outside.ts", strings.ReplaceAll(cleanSource, "Greeter", "Outside"))
	stage(t, dir, "sub/src/inside.ts", "outside.ts")

	stdout, stderr, code := runCdd(t, dir, "check", "--staged", "--all", "--config", "sub/cdd.config.yaml")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "unit: src/inside.ts:1:8 class Inside")
	assert.NotContains(t, stdout, "Outside", "a staged file outside the configuration's directory is dropped")
}

func TestCheckStagedFromASubdirectory(t *testing.T) { // TC-S9
	dir := t.TempDir()
	initRepo(t, dir)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	writeTSFixture(t, sub)
	writeFixtureFile(t, sub, "src/inside.ts", strings.ReplaceAll(cleanSource, "Greeter", "Inside"))
	stage(t, dir, "sub/src/inside.ts")

	stdout, stderr, code := runCdd(t, sub, "check", "--staged", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "unit: src/inside.ts:1:8 class Inside")
}

func TestCheckStagedJSONMatchesAnExplicitRun(t *testing.T) { // TC-S10
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
	stage(t, dir, "src/order-service.ts")

	staged, _, _ := runCdd(t, dir, "check", "--staged", "--format", "json")
	explicit, _, _ := runCdd(t, dir, "check", "src/order-service.ts", "--format", "json")
	assert.Equal(t, withoutElapsed(t, explicit), withoutElapsed(t, staged))
}

// withoutElapsed decodes a json report and drops its timing.
func withoutElapsed(t *testing.T, doc string) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(doc), &m), doc)
	delete(m, "elapsed_ms")
	return m
}

func TestCheckStagedDeletedWorkingCopyIsAnError(t *testing.T) { // TC-S11
	dir := stagedFixture(t)
	writeFixtureFile(t, dir, "src/gone.ts", cleanSource)
	stage(t, dir, "src/gone.ts")
	require.NoError(t, os.Remove(filepath.Join(dir, "src", "gone.ts")))

	_, stderr, code := runCdd(t, dir, "check", "--staged")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "gone.ts")
}
