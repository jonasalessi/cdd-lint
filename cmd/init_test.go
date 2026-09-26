package cmd

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/initcmd"
	"github.com/jonasalessi/cdd-lint/internal/languages"
)

// update rewrites command-output golden files.
var update = flag.Bool("update", false, "rewrite command-output golden files")

// The e2e tests drive the real registry through --languages, so they name
// real ids.
const (
	langGo   config.Language = "go"
	langJava config.Language = "java"
)

// specs is the real registry, as cmd wires it.
func specs() []config.LanguageSpec {
	return languages.Specs()
}

// runCdd executes the command tree inside dir, keeping stdout and stderr
// apart so a test can tell a warning from the receipt. Every case passes
// --yes: the questions need a terminal, and a test binary must never depend
// on inheriting one.
func runCdd(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runCddIn(t, dir, "", args...)
}

// runCddIn is runCdd with stdin holding the given text.
func runCddIn(t *testing.T, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Chdir(dir)
	var outBuf, errBuf bytes.Buffer
	root := newRootCmd()
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	code = execute(root, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// silentCmd is a bare command whose two streams a test can read back.
func silentCmd() (c *cobra.Command, stdout, stderr *bytes.Buffer) {
	c = &cobra.Command{}
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	c.SetOut(stdout)
	c.SetErr(stderr)
	return c, stdout, stderr
}

// writeGoFixture lays out a minimal Go project.
func writeGoFixture(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/fixture\n\ngo 1.23\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644))
}

// loadConfig loads and validates the file the command wrote.
func loadConfig(t *testing.T, dir string) *config.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join(dir, "cdd.config.yaml"))
	require.NoError(t, err)
	issues := config.Validate(cfg, specs())
	require.False(t, issues.HasErrors(), "issues: %v", issues)
	return cfg
}

// TestDogfoodConfigValidates mirrors the CI gate: the file this repository
// uses on itself is hand-tuned for a legacy code base, so it is not what init
// writes, but it must load and validate without a single warning.
func TestDogfoodConfigValidates(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "cdd.config.yaml"))
	require.NoError(t, err)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Empty(t, config.Validate(cfg, specs()), "the dogfood file must validate without warnings")
}

func TestInitYesDetectsGoProject(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)

	stdout, stderr, code := runCdd(t, dir, "init", "--yes")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Created cdd.config.yaml")
	assert.NotContains(t, stderr, "no analyzer")

	cfg := loadConfig(t, dir)
	require.Len(t, cfg.Metrics, 1)
	assert.Contains(t, cfg.Metrics, langGo)
	assert.Equal(t, []string{"example.com/fixture"}, cfg.InternalCoupling.Packages)
	assert.Equal(t, config.ProjectGreenfield, cfg.ProjectType)
}

func TestInitNeverWarnsAboutTheGoAnalyzer(t *testing.T) {
	tests := map[string][]string{
		"automatically detected": nil,
		"explicitly selected":    {"--languages", "go"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoFixture(t, dir)
			args = append([]string{"init", "--yes"}, args...)

			_, stderr, code := runCdd(t, dir, args...)

			require.Equal(t, 0, code, "stderr: %s", stderr)
			assert.NotContains(t, stderr, "no analyzer")
			assert.Contains(t, loadConfig(t, dir).Metrics, langGo)
		})
	}
}

func TestInitFullFlagsMatchesGolden(t *testing.T) {
	// Read the golden file before runCdd, which leaves the package directory.
	want, err := os.ReadFile(filepath.Join("testdata", "golden", "greenfield-java-kotlin.yaml"))
	require.NoError(t, err)

	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes",
		"--languages", "java,kotlin",
		"--project-type", "greenfield",
		"--limit", "10",
		"--metrics", "code_branch,condition,exception_handling,internal_coupling,external_coupling,inheritance",
		"--packages", "com.acme",
		"--force",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	got, err := os.ReadFile(filepath.Join(dir, "cdd.config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestInitTypeScriptMatchesGolden(t *testing.T) {
	// The real registry must drive this test. Synthetic language fixtures do
	// not cover TypeScript's descriptions, package example, or file defaults.
	path, err := filepath.Abs(filepath.Join("testdata", "golden", "greenfield-typescript.yaml"))
	require.NoError(t, err)
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--languages", "typescript")
	require.Equal(t, 0, code, "stderr: %s", stderr)

	got, err := os.ReadFile(filepath.Join(dir, "cdd.config.yaml"))
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got),
		"real TypeScript defaults drifted; run go test ./cmd -run TestInitTypeScriptMatchesGolden -update")
}

func TestInitMetricsFlagFilteredPerLanguage(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes",
		"--languages", "go,java",
		"--metrics", "code_branch,condition,exception_handling,internal_coupling",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)

	cfg := loadConfig(t, dir)
	assert.Len(t, cfg.Metrics[langGo][0].Weights, 3, "exception_handling does not apply to go")
	assert.Len(t, cfg.Metrics[langJava][0].Weights, 4)
	assert.Contains(t, cfg.Metrics[langJava][0].Weights, config.MetricExceptionHandling)
}

func TestInitMeasureOnlyDisablesCI(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--project-type", "legacy", "--legacy-mode", "measure_only")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	cfg := loadConfig(t, dir)
	assert.False(t, cfg.Enforcement.BlockOnCI)
	assert.Equal(t, "measure_only", cfg.Enforcement.LegacyMode)
}

func TestInitNoLanguagesFails(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "--languages")
	assert.NoFileExists(t, filepath.Join(dir, "cdd.config.yaml"))
}

func TestInitExistingFileGuard(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	path := filepath.Join(dir, "cdd.config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

	_, stderr, code := runCdd(t, dir, "init", "--yes")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "--force")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))

	_, stderr, code = runCdd(t, dir, "init", "--yes", "--force")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "version: 1")
}

func TestInitZeroWeightFails(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--weight", "code_branch=0")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "code_branch")
}

func TestInitWeightFlagErrors(t *testing.T) {
	tests := map[string]struct{ flag, wantErr string }{
		"missing separator": {"code_branch", "expected id=value"},
		"not a number":      {"code_branch=heavy", "is not a number"},
		"unknown metric":    {"karma=2", "unknown metric"},
		"go cannot count it": {
			"exception_handling=2", "none of the selected languages can count",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeGoFixture(t, dir)
			_, stderr, code := runCdd(t, dir, "init", "--yes", "--weight", tt.flag)
			assert.Equal(t, 1, code)
			assert.Contains(t, stderr, tt.wantErr)
			assert.NoFileExists(t, filepath.Join(dir, "cdd.config.yaml"))
		})
	}
}

func TestInitUnknownLegacyModeOnGreenfield(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--project-type", "greenfield", "--legacy-mode", "measur_only")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "is not one of")
	assert.NoFileExists(t, filepath.Join(dir, "cdd.config.yaml"))
}

func TestInitOutputFlag(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	stdout, stderr, code := runCdd(t, dir, "init", "--yes", "--output", "other.yaml")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Created other.yaml")
	assert.FileExists(t, filepath.Join(dir, "other.yaml"))
	assert.NoFileExists(t, filepath.Join(dir, "cdd.config.yaml"))
}

func TestInitNoDefaultExcludes(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--no-default-excludes")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Empty(t, loadConfig(t, dir).Exclude)
}

func TestInitLimitOutsideBandWarns(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	stdout, stderr, code := runCdd(t, dir, "init", "--yes", "--limit", "50")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stderr, "warning: limit 50 is outside")
	assert.Contains(t, stdout, "limit: 50")
}

func TestInitTimeoutFlag(t *testing.T) {
	dir := t.TempDir()
	writeGoFixture(t, dir)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--timeout", "30s")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	data, err := os.ReadFile(filepath.Join(dir, "cdd.config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "\ntimeout: 30s\n")

	_, stderr, code = runCdd(t, dir, "init", "--yes", "--force", "--timeout", "-1s")
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "timeout")
}

func TestInitScanTimeoutTruncation(t *testing.T) {
	t.Run("with --languages the result is unaffected", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFixture(t, dir)
		_, stderr, code := runCdd(t, dir, "init", "--yes", "--scan-timeout", "1ns", "--languages", "go")
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "scan stopped")
		cfg := loadConfig(t, dir)
		assert.Contains(t, cfg.Metrics, langGo)
	})
	t.Run("without --languages nothing is detected in time", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFixture(t, dir)
		_, stderr, code := runCdd(t, dir, "init", "--yes", "--scan-timeout", "1ns")
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr, "scan stopped")
		assert.Contains(t, stderr, "--languages")
	})
	t.Run("a zero budget leaves the scan unbounded", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFixture(t, dir)
		_, stderr, code := runCdd(t, dir, "init", "--yes", "--scan-timeout", "0")
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.NotContains(t, stderr, "scan stopped")
		assert.NotContains(t, stderr, "no analyzer")
		assert.Contains(t, loadConfig(t, dir).Metrics, langGo)
	})
}

func TestGuardExisting(t *testing.T) {
	t.Run("force short-circuits the stat", func(t *testing.T) {
		c, _, _ := silentCmd()
		force, done, err := guardExisting(c, filepath.Join(t.TempDir(), "nope.yaml"), true, false)
		require.NoError(t, err)
		assert.True(t, force)
		assert.False(t, done)
	})
	t.Run("a missing file needs no force", func(t *testing.T) {
		c, _, _ := silentCmd()
		force, done, err := guardExisting(c, filepath.Join(t.TempDir(), "nope.yaml"), false, false)
		require.NoError(t, err)
		assert.False(t, force)
		assert.False(t, done)
	})
	t.Run("a stat failure other than not-exist is reported", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "regular")
		require.NoError(t, os.WriteFile(file, nil, 0o644))
		c, _, _ := silentCmd()
		_, _, err := guardExisting(c, filepath.Join(file, "cdd.config.yaml"), false, false)
		assert.Error(t, err, "a path below a regular file is not ErrNotExist")
	})
	t.Run("an existing file without a terminal asks for --force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cdd.config.yaml")
		require.NoError(t, os.WriteFile(path, nil, 0o644))
		c, _, _ := silentCmd()
		_, _, err := guardExisting(c, path, false, false)
		assert.ErrorContains(t, err, "--force")
	})
}

func TestGatherDefaults(t *testing.T) {
	t.Run("detection fills the languages and packages", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFixture(t, dir)
		t.Chdir(dir)
		a, det, err := gatherDefaults(t.Context(), &initOptions{scanTimeout: 4 * time.Second}, specs())
		require.NoError(t, err)
		assert.False(t, det.Truncated)
		assert.Equal(t, []config.Language{langGo}, a.Languages)
		assert.Equal(t, []string{"example.com/fixture"},
			a.PackagesByLanguage[langGo])
	})
	t.Run("flags win over detection", func(t *testing.T) {
		dir := t.TempDir()
		writeGoFixture(t, dir)
		t.Chdir(dir)
		a, _, err := gatherDefaults(t.Context(), &initOptions{
			languages: []string{" java ", ""},
			metrics:   []string{"code_branch", " condition "},
			packages:  []string{"com.acme"},
			weights:   []string{"code_branch = 2"},
		}, specs())
		require.NoError(t, err)
		assert.Equal(t, []config.Language{langJava}, a.Languages)
		assert.Equal(t, []config.MetricID{config.MetricCodeBranch, config.MetricCondition}, a.Metrics)
		assert.Equal(t, []string{"com.acme"}, a.Packages)
		assert.Nil(t, a.PackagesByLanguage, "an explicit --packages skips package detection")
		assert.Equal(t, map[config.MetricID]float64{config.MetricCodeBranch: 2}, a.Weights)
	})
	t.Run("a bad weight flag stops before detection", func(t *testing.T) {
		t.Chdir(t.TempDir())
		_, _, err := gatherDefaults(t.Context(), &initOptions{weights: []string{"oops"}}, specs())
		assert.ErrorContains(t, err, "expected id=value")
	})
}

func TestGatherDefaultsScanTimeoutCancelsPackageDetection(t *testing.T) {
	t.Chdir(t.TempDir())
	spec := config.LanguageSpec{
		ID:         "alpha",
		Extensions: []string{".alpha"},
		DetectPackages: func(ctx context.Context, _ string) ([]string, error) {
			<-ctx.Done()
			return []string{"partial.prefix"}, ctx.Err()
		},
	}

	a, det, err := gatherDefaults(t.Context(), &initOptions{
		languages:   []string{"alpha"},
		scanTimeout: 20 * time.Millisecond,
	}, []config.LanguageSpec{spec})

	require.NoError(t, err)
	assert.True(t, det.Truncated)
	assert.Equal(t, []string{"partial.prefix"}, a.PackagesByLanguage["alpha"])
}

func TestWriteConfig(t *testing.T) {
	answers := func() initcmd.Answers {
		return initcmd.Answers{Languages: []config.Language{langGo}, DefaultExcludes: true}
	}
	t.Run("a build error never touches the file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cdd.config.yaml")
		a := answers()
		a.Limit = -1
		c, _, _ := silentCmd()
		assert.ErrorContains(t, writeConfig(c, a, specs(), path, false), "at least 1")
		assert.NoFileExists(t, path)
	})
	t.Run("warnings go to stderr and the receipt to stdout", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cdd.config.yaml")
		a := answers()
		a.Limit = 50
		c, stdout, stderr := silentCmd()
		require.NoError(t, writeConfig(c, a, specs(), path, false))
		assert.Contains(t, stderr.String(), "warning: limit 50 is outside")
		assert.Contains(t, stdout.String(), "Created "+path)
	})
	t.Run("an existing file asks for --force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cdd.config.yaml")
		require.NoError(t, os.WriteFile(path, nil, 0o644))
		c, _, _ := silentCmd()
		assert.ErrorContains(t, writeConfig(c, answers(), specs(), path, false), "--force")
	})
	t.Run("a write failure is passed through", func(t *testing.T) {
		c, _, _ := silentCmd()
		path := filepath.Join(t.TempDir(), "missing", "cdd.config.yaml")
		assert.Error(t, writeConfig(c, answers(), specs(), path, false))
	})
}

func TestSummary(t *testing.T) {
	cfg, _, err := initcmd.Build(initcmd.Answers{
		Languages: []config.Language{langGo, langJava},
	}, specs())
	require.NoError(t, err)
	assert.Equal(t,
		"Created cdd.config.yaml — languages: go, java · project: greenfield · limit: 10 · metrics: 6",
		summary("cdd.config.yaml", cfg, specs()),
		"the metric count is the union across languages")
}

func TestParseWeightFlags(t *testing.T) {
	t.Run("no pairs means no overrides", func(t *testing.T) {
		got, err := parseWeightFlags(nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
	t.Run("spaces around both halves are trimmed", func(t *testing.T) {
		got, err := parseWeightFlags([]string{" code_branch = 1.5 "})
		require.NoError(t, err)
		assert.Equal(t, map[config.MetricID]float64{config.MetricCodeBranch: 1.5}, got)
	})
}

func TestToLanguagesAndMetrics(t *testing.T) {
	assert.Nil(t, toLanguages(nil))
	assert.Nil(t, toMetrics([]string{"", "  "}))
	assert.Equal(t, []config.Language{langGo}, toLanguages([]string{" go ", " "}))
	assert.Equal(t, []config.MetricID{config.MetricLambda}, toMetrics([]string{"", " lambda "}))
}
