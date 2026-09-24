package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/report"
)

// checkMetrics is the metric set every fixture enables, so a fixture's ICPs
// do not move when the default selection does.
const checkMetrics = "code_branch,condition,exception_handling," +
	"internal_coupling,external_coupling,stdlib_coupling,inheritance"

// violationLabel opens the console record of a unit above its limit, and
// unitLabel the record of one within it.
const (
	violationLabel = "violation:"
	unitLabel      = "unit:"
)

// cleanSource is one class of 1 ICP: well inside the greenfield limit.
const cleanSource = `export class Greeter {
  greet(name: string): string {
    if (name) {
      return ` + "`hi ${name}`" + `;
    }
    return "hi";
  }
}
`

// overLimitSource is one class of 18 ICPs — six branches and twelve
// conditions — against the greenfield limit of 10.
const overLimitSource = `export class OrderService {
  price(a: number, b: number, c: number): number {
    if (a > 0 && b > 0) { return a + b; }
    if (a > 1 && b > 1) { return a - b; }
    if (a > 2 && b > 2) { return a * b; }
    if (a > 3 && b > 3) { return a / b; }
    if (a > 4 && b > 4) { return a + c; }
    if (a > 5 && b > 5) { return b + c; }
    return 0;
  }
}
`

// writeTSFixture lays out a TypeScript project in dir: a tsconfig.json with
// one path alias and a configuration written by cdd init, so the file always
// matches the schema the command reads today.
func writeTSFixture(t *testing.T, dir string) {
	t.Helper()
	writeFixtureFile(t, dir, "tsconfig.json", `{"compilerOptions":{"paths":{"@app/*":["src/*"]}}}`+"\n")
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", "typescript",
		"--metrics", checkMetrics,
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
}

// writeFixtureFile writes one source file, creating its directories.
func writeFixtureFile(t *testing.T, dir, rel, src string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
}

// editConfig rewrites the fixture configuration, replacing each old/new pair
// once. A pair that does not match is a broken test, not a silent no-op.
func editConfig(t *testing.T, dir string, pairs ...string) {
	t.Helper()
	path := filepath.Join(dir, "cdd.config.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(data)
	for i := 0; i+1 < len(pairs); i += 2 {
		require.Contains(t, out, pairs[i])
		out = strings.Replace(out, pairs[i], pairs[i+1], 1)
	}
	require.NoError(t, os.WriteFile(path, []byte(out), 0o644))
}

// legacyProject moves a fixture off greenfield, which is the only project
// type that accepts a mode other than strict_all.
func legacyProject(t *testing.T, dir, blockOnCI, mode string) {
	t.Helper()
	editConfig(t, dir,
		"project_type: greenfield", "project_type: legacy",
		"  block_on_ci: true\n  legacy_mode: strict_all\n",
		"  block_on_ci: "+blockOnCI+"\n  legacy_mode: "+mode+"\n",
	)
}

func TestCheckCleanProject(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "cdd check: PASS violations=0 units=1")
	assert.NotContains(t, stdout, unitLabel, "a unit within its limit is not listed")
	assert.NotContains(t, stdout, violationLabel)
	assert.Empty(t, stderr)
}

func TestCheckAllListsTheUnitsWithinTheirLimit(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, unitLabel+" src/greeter.ts:1:8 class Greeter icp=1 limit=10\n")
	assert.Contains(t, stdout, "  metrics: ")
	assert.NotContains(t, stdout, violationLabel)
}

// TestCheckExplainIsAccepted pins the flag and the console shape, not the
// positions: locating a construct is the analyzer's job, and a unit with no
// located construct simply gains no line.
func TestCheckExplainIsAccepted(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all", "--explain")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, unitLabel+" src/greeter.ts:1:8 class Greeter icp=1 limit=10\n")
	for line := range strings.SplitSeq(stdout, "\n") {
		if after, ok := strings.CutPrefix(line, "  icp: "); ok {
			assert.Regexp(t, `^\d+:\d+-\d+:\d+ \w+ \+[0-9.]+$`, after)
		}
	}
}

// TestCheckExplainAddsOccurrencesToTheJSONReport is the contract an editor
// plugin reads: cdd check --all --explain with format json.
func TestCheckExplainAddsOccurrencesToTheJSONReport(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir, "  format: console", "  format: json")

	stdout, stderr, code := runCdd(t, dir, "check", "--all", "--explain")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	var doc struct {
		Explain bool `json:"explain"`
		Files   []struct {
			Units []struct {
				Name        string            `json:"name"`
				Occurrences *[]map[string]any `json:"occurrences"`
			} `json:"units"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "the report must be valid JSON")
	assert.True(t, doc.Explain)
	require.NotEmpty(t, doc.Files)
	require.NotEmpty(t, doc.Files[0].Units)
	unit := doc.Files[0].Units[0]
	assert.Equal(t, "Greeter", unit.Name)
	assert.NotNil(t, unit.Occurrences, "every listed unit carries the key, even when it is empty")
}

func TestCheckWithoutExplainOmitsOccurrences(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir, "  format: console", "  format: json")

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, `"explain": false`)
	assert.NotContains(t, stdout, "occurrences")
}

func TestCheckOverLimitUnitBlocks(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stdout, violationLabel+" src/order-service.ts:1:8 class OrderService icp=18 limit=10 over=8\n")
	assert.Contains(t, stdout, "  metrics: condition=12 code_branch=6\n")
	assert.Empty(t, stderr, "the report already names the violation")
}

func TestCheckEnforcementVariants(t *testing.T) {
	t.Run("measure_only reports without blocking", func(t *testing.T) {
		dir := t.TempDir()
		writeTSFixture(t, dir)
		writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
		legacyProject(t, dir, "false", "measure_only")

		stdout, stderr, code := runCdd(t, dir, "check")
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Contains(t, stdout, "OrderService")
		assert.Contains(t, stdout, "violations=1")
		assert.Contains(t, stderr, "warning:", "a limit of 10 is outside the legacy band")
	})
	t.Run("strict_on_new_only says it is not enforced", func(t *testing.T) {
		dir := t.TempDir()
		writeTSFixture(t, dir)
		writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)
		legacyProject(t, dir, "true", "strict_on_new_only")

		stdout, _, code := runCdd(t, dir, "check")
		assert.Equal(t, 0, code)
		assert.Contains(t, stdout, "warning: ")
		assert.Contains(t, stdout, "not enforced yet")
		assert.Contains(t, stdout, "violations=1")
	})
}

func TestCheckConfigFlagSetsTheRoot(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app"), 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(dir, "cdd.config.yaml"),
		filepath.Join(dir, "app", "cdd.config.yaml"),
	))
	writeFixtureFile(t, dir, "outside.ts", strings.ReplaceAll(cleanSource, "Greeter", "OutsideService"))
	writeFixtureFile(t, dir, "app/src/inside.ts", strings.ReplaceAll(cleanSource, "Greeter", "InsideService"))

	stdout, stderr, code := runCdd(t, dir, "check", "--all", "--config", filepath.Join("app", "cdd.config.yaml"))
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "InsideService")
	assert.NotContains(t, stdout, "OutsideService", "a file above the configuration is outside the run")
}

func TestCheckConfigRelativeOutputFileUsesTheConfigurationRoot(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app", "reports"), 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(dir, "cdd.config.yaml"),
		filepath.Join(dir, "app", "cdd.config.yaml"),
	))
	writeFixtureFile(t, dir, "app/src/inside.ts", cleanSource)
	configPath := filepath.Join(dir, "app", "cdd.config.yaml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	data = []byte(strings.Replace(string(data), "  outputFile: null", `  outputFile: "reports/cdd.json"`, 1))
	require.NoError(t, os.WriteFile(configPath, data, 0o644))

	stdout, stderr, code := runCdd(t, dir, "check", "--config", filepath.Join("app", "cdd.config.yaml"))

	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Report written to "+filepath.Join("app", "reports", "cdd.json"))
	assert.FileExists(t, filepath.Join(dir, "app", "reports", "cdd.json"))
	assert.NoFileExists(t, filepath.Join(dir, "reports", "cdd.json"))
}

func TestCheckRefusesAnOutputFileOutsideTheProject(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir, "  outputFile: null", `  outputFile: "../canary.json"`)

	_, stderr, code := runCdd(t, dir, "check")

	require.Equal(t, 1, code)
	assert.Contains(t, stderr, "outputFile")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(dir), "canary.json"))
}

func TestCheckJSONReportToFile(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir,
		"  format: console", "  format: json",
		"  outputFile: null", `  outputFile: "report.json"`,
	)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Report written to report.json")

	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc), "the report must be valid JSON")
	assert.Contains(t, string(data), "Greeter")
	assert.Contains(t, string(data), `"filter": "all"`)
}

// TestCheckFormatOverridesTheConfiguredFormat covers --format: the
// configuration says console, the flag says json, and json wins.
func TestCheckFormatOverridesTheConfiguredFormat(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all", "--format", "json")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "the report must be valid JSON: %s", stdout)
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stdout, unitLabel, "the console layout must not leak into the json report")
}

// TestCheckFormatKeepsTheConfiguredOutputFile: --format changes only the
// format; a configured outputFile still receives the report.
func TestCheckFormatKeepsTheConfiguredOutputFile(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir,
		"  format: console", "  format: json",
		"  outputFile: null", `  outputFile: "report.md"`,
	)

	stdout, stderr, code := runCdd(t, dir, "check", "--all", "--format", "markdown")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Report written to report.md")

	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "# cdd check\n"), "expected a markdown report, got: %s", data)
	assert.Contains(t, string(data), "Greeter")
}

func TestCheckFormatRejectsAnUnknownValue(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--format", "yaml")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout, "nothing must be analyzed when the flag is unusable")
	assert.Contains(t, stderr, `--format: "yaml" is not one of console, json, xml, markdown`)
}

// TestCheckPathNarrowsTheRunToOneFile is the editor-plugin call:
// cdd check <file> --explain --format json after a save.
func TestCheckPathNarrowsTheRunToOneFile(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	writeFixtureFile(t, dir, "src/order.ts", overLimitSource)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.Join("src", "greeter.ts"),
		"--all", "--explain", "--format", "json")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "the report must be valid JSON: %s", stdout)
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stdout, "OrderService", "the other file is not part of the run")
	assert.Contains(t, stdout, `"occurrences"`)
}

func TestCheckMixedConfiguredLanguagesAllowsSupportedPath(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "tsconfig.json", `{"compilerOptions":{"paths":{"@app/*":["src/*"]}}}`+"\n")
	_, initStderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", "go,typescript",
		"--metrics", checkMetrics,
		"--packages", "@app/",
	)
	require.Equal(t, 0, code, "stderr: %s", initStderr)
	writeFixtureFile(t, dir, "src/app.ts", cleanSource)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.Join("src", "app.ts"), "--all")

	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stderr, "no analyzer for go yet")
}

func TestCheckPathsAcceptACommaSeparatedList(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	writeFixtureFile(t, dir, "src/order.ts", overLimitSource)
	writeFixtureFile(t, dir, "src/other.ts", overLimitSource)
	legacyProject(t, dir, "false", "measure_only")

	list := filepath.Join("src", "greeter.ts") + ", " + filepath.Join("src", "order.ts") + ","
	stdout, stderr, code := runCdd(t, dir, "check", list, "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "src/greeter.ts")
	assert.Contains(t, stdout, "src/order.ts")
	assert.NotContains(t, stdout, "src/other.ts", "only the listed files are part of the run")
}

func TestCheckPathResolvesFromTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	writeFixtureFile(t, dir, "src/order.ts", overLimitSource)

	stdout, stderr, code := runCdd(t, filepath.Join(dir, "src"), "check", "greeter.ts", "--all",
		"--config", filepath.Join("..", "cdd.config.yaml"))
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "src/greeter.ts")
	assert.NotContains(t, stdout, "OrderService")
}

func TestCheckPathAcceptsADirectory(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	writeFixtureFile(t, dir, "lib/order.ts", overLimitSource)

	stdout, stderr, code := runCdd(t, dir, "check", "src", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stdout, "OrderService")
}

func TestCheckPathOutsideTheConfigurationIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "elsewhere/greeter.ts", cleanSource)
	app := filepath.Join(dir, "app")
	writeTSFixture(t, app)

	stdout, stderr, code := runCdd(t, app, "check", filepath.Join("..", "elsewhere", "greeter.ts"))
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "is outside ., the directory of the configuration")
}

func TestCheckPathThatNoLanguageClaimsIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "README.md", "docs\n")

	stdout, stderr, code := runCdd(t, dir, "check", "README.md")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "README.md: no configured language claims this file")
}

func TestCheckMissingPathIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.Join("src", "missing.ts"))
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "src/missing.ts: no such file or directory")
	assert.NotContains(t, stderr, "lstat")
}

// TestCheckMissingPathIsNamedLikeOtherPathErrors covers a missing directory
// and a missing member of a comma-separated list: the message names the
// path, not the system call that looked it up (#12).
func TestCheckMissingPathIsNamedLikeOtherPathErrors(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)

	for _, tc := range []struct{ arg, want string }{
		{"nope", "nope: no such file or directory"},
		{"src," + filepath.Join("src", "nope.ts"), "src/nope.ts: no such file or directory"},
	} {
		stdout, stderr, code := runCdd(t, dir, "check", tc.arg)
		assert.Equal(t, 1, code, tc.arg)
		assert.Empty(t, stdout, tc.arg)
		assert.Contains(t, stderr, tc.want, tc.arg)
		assert.NotContains(t, stderr, "lstat", tc.arg)
	}
}

func TestCheckTimeoutReportsPartially(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir, "\ntimeout: 5m\n", "\ntimeout: 1ns\n")

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, "partial=true")
	assert.Contains(t, stderr, "timeout")
}

func TestCheckMissingConfig(t *testing.T) {
	_, stderr, code := runCdd(t, t.TempDir(), "check", "--config", "nowhere.yaml")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "nowhere.yaml")
}

func TestCheckInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	editConfig(t, dir, "version: 1", "version: 2")

	_, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "is invalid")
	assert.Contains(t, stderr, "version")
}

func TestCheckUnwritableReport(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/greeter.ts", cleanSource)
	editConfig(t, dir, "  outputFile: null", `  outputFile: "nowhere/report.json"`)

	_, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "output directory")
}

func TestEmitReportRejectsAnUnknownFormat(t *testing.T) {
	c, _, _ := silentCmd()
	err := emitReport(c, config.Reporter{Format: "toml"}, analyze.RunResult{}, report.Options{})
	assert.ErrorContains(t, err, "toml")
}

func TestCheckSyntaxErrorIsAWarning(t *testing.T) {
	dir := t.TempDir()
	writeTSFixture(t, dir)
	writeFixtureFile(t, dir, "src/broken.ts", "export class Broken { if if if (\n")

	stdout, stderr, code := runCdd(t, dir, "check")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "warning: src/broken.ts: ")
	assert.Contains(t, stdout, "syntax error")
	assert.Contains(t, stdout, "violations=0")
}
