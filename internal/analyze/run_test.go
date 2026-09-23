package analyze

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// run is the common call: analyze root with cfg and the given languages.
func run(t *testing.T, root string, cfg *config.Config, langs ...*fakeLanguage) (RunResult, error) {
	t.Helper()
	registry := make([]Language, len(langs))
	for i, l := range langs {
		registry[i] = l.language()
	}
	return Run(t.Context(), Request{Root: root, Config: cfg, Languages: registry})
}

func TestRunAnalyzesOnlyTheConfiguredLanguages(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha":   "a",
		"src/b.beta":    "b",
		"src/c.unknown": "c",
		"README.md":     "docs",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	beta := &fakeLanguage{id: langBeta, ext: ".beta", result: oneUnit("B", nil)}

	got, err := run(t, root, testConfig(langAlpha), alpha, beta)

	require.NoError(t, err)
	assert.Equal(t, root, got.Root)
	assert.Equal(t, []string{"src/a.alpha"}, paths(got.Files))
	assert.Equal(t, langAlpha, got.Files[0].Language)
	assert.Empty(t, beta.paths(), "beta is registered but not configured")
	assert.False(t, got.Partial)
	assert.Positive(t, got.Elapsed)
}

func TestRunMatchesExtensionsCaseInsensitively(t *testing.T) {
	root := writeTree(t, map[string]string{"src/A.ALPHA": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/A.ALPHA"}, paths(got.Files))
}

func TestRunSkipsDependencyAndBuildDirectories(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha":                     "a",
		"node_modules/dep/b.alpha":        "b",
		"vendor/c.alpha":                  "c",
		"build/d.alpha":                   "d",
		"dist/e.alpha":                    "e",
		"target/f.alpha":                  "f",
		"out/g.alpha":                     "g",
		".git/objects/h.alpha":            "h",
		"src/nested/node_modules/i.alpha": "i",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a.alpha"}, paths(got.Files))
}

func TestRunExcludeWinsOverInclude(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/order.alpha":      "a",
		"src/order.test.alpha": "b",
		"lib/order.alpha":      "c",
	})
	cfg := testConfig(langAlpha)
	cfg.Include = []string{"src/**"}
	cfg.Exclude = []string{"**/*.test.alpha"}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, cfg, alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/order.alpha"}, paths(got.Files))
}

// runPaths is run narrowed to the given root-relative paths.
func runPaths(
	t *testing.T,
	root string,
	cfg *config.Config,
	paths []string,
	langs ...*fakeLanguage,
) (RunResult, error) {
	t.Helper()
	registry := make([]Language, len(langs))
	for i, l := range langs {
		registry[i] = l.language()
	}
	return Run(t.Context(), Request{Root: root, Config: cfg, Languages: registry, Paths: paths})
}

func TestRunPathsNarrowsTheRunToTheNamedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha": "a",
		"src/b.alpha": "b",
		"lib/c.alpha": "c",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := runPaths(t, root, testConfig(langAlpha), []string{"src/b.alpha"}, alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/b.alpha"}, paths(got.Files))
}

func TestRunPathsWalksANamedDirectoryAndDeduplicates(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha":              "a",
		"src/nested/b.alpha":       "b",
		"src/node_modules/c.alpha": "c",
		"lib/d.alpha":              "d",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := runPaths(t, root, testConfig(langAlpha), []string{"src", "src/a.alpha", "src/a.alpha"}, alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a.alpha", "src/nested/b.alpha"}, paths(got.Files))
}

func TestRunPathsEntersANamedSkippedDirectory(t *testing.T) {
	root := writeTree(t, map[string]string{"node_modules/dep/a.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := runPaths(t, root, testConfig(langAlpha), []string{"node_modules/dep"}, alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"node_modules/dep/a.alpha"}, paths(got.Files), "the caller asked for it")
}

func TestRunPathsRejectsASymlinkedDirectory(t *testing.T) {
	root := writeTree(t, map[string]string{"target/a.alpha": "a"})
	link := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Join(root, "target"), link); err != nil {
		t.Skipf("cannot create directory symlink: %v", err)
	}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	_, err := runPaths(t, root, testConfig(langAlpha), []string{"linked"}, alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "linked")
	assert.Contains(t, err.Error(), "symlinked directory")
}

func TestRunPathsRejectsAFileNoLanguageClaims(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "docs"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := runPaths(t, root, testConfig(langAlpha), []string{"README.md"}, alpha)

	require.EqualError(t, err, "README.md: no configured language claims this file")
}

func TestRunPathsRejectsAnExcludedFile(t *testing.T) {
	root := writeTree(t, map[string]string{"src/a.test.alpha": "a"})
	cfg := testConfig(langAlpha)
	cfg.Exclude = []string{"**/*.test.alpha"}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := runPaths(t, root, cfg, []string{"src/a.test.alpha"}, alpha)

	require.EqualError(t, err, "src/a.test.alpha is excluded by the configuration")
}

func TestRunPathsReportsAMissingPath(t *testing.T) {
	root := writeTree(t, map[string]string{"src/a.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := runPaths(t, root, testConfig(langAlpha), []string{"src/missing.alpha"}, alpha)

	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRunPathsCanceledContextStopsCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	got, err := Run(ctx, Request{
		Root:      t.TempDir(),
		Config:    testConfig(langAlpha),
		Languages: []Language{alpha.language()},
		Paths:     []string{"missing.alpha"},
	})

	require.ErrorIs(t, err, ErrTimeout)
	assert.True(t, got.Partial)
}

func TestRunResolvesWeightsAndLimitsPerFile(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/app/order.alpha": "a",
		"src/dto/order.alpha": "b",
	})
	cfg := testConfig(langAlpha)
	cfg.Metrics[langAlpha] = append(cfg.Metrics[langAlpha], config.PatternWeight{
		Pattern: ".*/dto/.*",
		Weights: map[config.MetricID]float64{config.MetricCodeBranch: 0.5},
	})
	cfg.ICPLimits[langAlpha] = append(cfg.ICPLimits[langAlpha], config.PatternLimit{
		Pattern: ".*/dto/.*",
		Limit:   20,
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("Order", map[config.MetricID]int{
		config.MetricCodeBranch:       12,
		config.MetricExternalCoupling: 4,
	})}

	got, err := run(t, root, cfg, alpha)

	require.NoError(t, err)
	require.Len(t, got.Files, 2)

	app := got.Files[0].Units[0]
	assert.Equal(t, "src/app/order.alpha", got.Files[0].Path)
	assert.InDelta(t, 14.0, app.Total, 0, "12 branches plus 4 external couplings at 0.5")
	assert.Equal(t, 10, app.Limit)
	assert.True(t, app.Exceeds)

	dto := got.Files[1].Units[0]
	assert.Equal(t, "src/dto/order.alpha", got.Files[1].Path)
	assert.InDelta(t, 8.0, dto.Total, 0, "the dto pattern halves the branch weight")
	assert.Equal(t, 20, dto.Limit)
	assert.False(t, dto.Exceeds)

	assert.Equal(t, 1, got.Violations())
	assert.Equal(t, 2, got.UnitCount())
}

func TestRunEvaluatesTheBlockingOutcome(t *testing.T) {
	tests := map[string]struct {
		counts      map[config.MetricID]int
		enforcement config.Enforcement
		blocked     bool
	}{
		"no violations never block": {
			enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictAll},
		},
		"non-blocking enforcement warns": {
			counts:      map[config.MetricID]int{config.MetricCodeBranch: 11},
			enforcement: config.Enforcement{LegacyMode: config.ModeMeasureOnly},
		},
		"blocking enforcement fails": {
			counts:      map[config.MetricID]int{config.MetricCodeBranch: 11},
			enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictAll},
			blocked:     true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"a.alpha": "a"})
			cfg := testConfig(langAlpha)
			cfg.Enforcement = tt.enforcement
			alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", tt.counts)}

			got, err := run(t, root, cfg, alpha)

			require.NoError(t, err)
			assert.Equal(t, tt.blocked, got.Blocked)
		})
	}
}

func TestRunDropsDisabledMetrics(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", map[config.MetricID]int{
		config.MetricCodeBranch: 2,
		config.MetricLambda:     9,
	})}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	unit := got.Files[0].Units[0]
	assert.NotContains(t, unit.Counts, config.MetricLambda, "lambda has no weight, so it is disabled")
	assert.NotContains(t, unit.Scores, config.MetricLambda)
	assert.InDelta(t, 2.0, unit.Total, 0)
}

func TestRunCarriesAnalyzerWarnings(t *testing.T) {
	root := writeTree(t, map[string]string{"broken.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: FileResult{
		Warnings: []string{"broken.alpha:1:1: syntax error"},
	}}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	require.Len(t, got.Files, 1)
	assert.Equal(t, []string{"broken.alpha:1:1: syntax error"}, got.Files[0].Warnings)
	assert.Empty(t, got.Files[0].Units, "a skipped file contributes no units")
	assert.Empty(t, got.Warnings, "a file warning is not a run warning")
}

func TestRunFeedsInternalPrefixesToEveryAnalyzer(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	cfg := testConfig(langAlpha)
	cfg.InternalCoupling = config.InternalCoupling{AutoDetect: true, Packages: []string{"@app/", "com.acme"}}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	entry := alpha.language()
	entry.Spec.DetectPackages = func(ctx context.Context, dir string) ([]string, error) {
		assert.NoError(t, ctx.Err())
		assert.Equal(t, root, dir, "detection runs against the run root")
		return []string{"@lib/", "@app/"}, nil
	}

	_, err := Run(t.Context(), Request{Root: root, Config: cfg, Languages: []Language{entry}})

	require.NoError(t, err)
	built := alpha.instances()
	require.Len(t, built, 1)
	assert.Equal(t, []string{"@app/", "@lib/", "com.acme"}, built[0].opts.InternalPrefixes,
		"configured and detected prefixes, sorted and deduplicated")
}

func TestRunIgnoresDetectionWhenAutoDetectIsOff(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	cfg := testConfig(langAlpha)
	cfg.InternalCoupling = config.InternalCoupling{Packages: []string{"com.acme"}}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	entry := alpha.language()
	entry.Spec.DetectPackages = func(context.Context, string) ([]string, error) {
		t.Error("detection must not run when auto_detect is off")
		return nil, nil
	}

	_, err := Run(t.Context(), Request{Root: root, Config: cfg, Languages: []Language{entry}})

	require.NoError(t, err)
	assert.Equal(t, []string{"com.acme"}, alpha.instances()[0].opts.InternalPrefixes)
}

func TestRunReportsAFailingPackageDetection(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	cfg := testConfig(langAlpha)
	cfg.InternalCoupling = config.InternalCoupling{AutoDetect: true}
	entry := (&fakeLanguage{id: langAlpha, ext: ".alpha"}).language()
	entry.Spec.DetectPackages = func(context.Context, string) ([]string, error) {
		return nil, errors.New("no manifest")
	}

	_, err := Run(t.Context(), Request{Root: root, Config: cfg, Languages: []Language{entry}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "detect alpha packages")
}

func TestRunClosesOneAnalyzerPerWorkerAndLanguage(t *testing.T) {
	files := map[string]string{}
	for i := range 40 {
		files[filepath.Join("src", string(rune('a'+i%20))+string(rune('a'+i/20))+".alpha")] = "x"
	}
	root := writeTree(t, files)
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	assert.Len(t, got.Files, len(files))
	built := alpha.instances()
	assert.NotEmpty(t, built)
	assert.Equal(t, int64(len(built)), alpha.closes.Load(), "every analyzer is closed exactly once")
}

func TestRunReportsFilesInPathOrder(t *testing.T) {
	root := writeTree(t, map[string]string{
		"z.alpha":       "z",
		"a/b.alpha":     "b",
		"a.alpha":       "a",
		"a/a/c.alpha":   "c",
		"m/n/o/d.alpha": "d",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, testConfig(langAlpha), alpha)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.alpha", "a/a/c.alpha", "a/b.alpha", "m/n/o/d.alpha", "z.alpha"}, paths(got.Files))
}

func TestRunRejectsSelectedUnavailableLanguage(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	entry := Language{Spec: config.LanguageSpec{ID: langAlpha, Extensions: []string{".alpha"}}}

	_, err := Run(t.Context(), Request{Root: root, Config: testConfig(langAlpha), Languages: []Language{entry}})

	require.Error(t, err)
	assert.Equal(t, "no analyzer for alpha yet", err.Error())
}

func TestRunAllowsUnselectedUnavailableLanguage(t *testing.T) {
	entry := Language{Spec: config.LanguageSpec{ID: langAlpha, Extensions: []string{".alpha"}}}

	got, err := Run(t.Context(), Request{
		Root:      t.TempDir(),
		Config:    testConfig(langAlpha),
		Languages: []Language{entry},
	})

	require.NoError(t, err)
	assert.Empty(t, got.Files)
}

func TestRunMixedConfigurationOnlyInitializesSelectedLanguage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha": "a",
		"src/b.beta":  "b",
	})
	cfg := testConfig(langAlpha, langBeta)
	cfg.InternalCoupling.AutoDetect = true
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	beta := Language{Spec: config.LanguageSpec{
		ID:         langBeta,
		Extensions: []string{".beta"},
		DetectPackages: func(context.Context, string) ([]string, error) {
			t.Error("an unselected language must not detect packages")
			return nil, nil
		},
	}}

	got, err := Run(t.Context(), Request{
		Root:      root,
		Config:    cfg,
		Languages: []Language{alpha.language(), beta},
		Paths:     []string{"src/a.alpha"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a.alpha"}, paths(got.Files))
}

func TestRunSelectedUnavailableLanguageStopsBeforeAnalysis(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/a.alpha": "a",
		"src/b.beta":  "b",
	})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	beta := Language{Spec: config.LanguageSpec{ID: langBeta, Extensions: []string{".beta"}}}

	_, err := Run(t.Context(), Request{
		Root:      root,
		Config:    testConfig(langAlpha, langBeta),
		Languages: []Language{alpha.language(), beta},
	})

	require.EqualError(t, err, "no analyzer for beta yet")
	assert.Empty(t, alpha.instances(), "no candidate is analyzed before every selected language is available")
}

func TestRunScanTimeoutCancelsPackageDetection(t *testing.T) {
	root := writeTree(t, map[string]string{"src/a.alpha": "a"})
	cfg := testConfig(langAlpha)
	cfg.Timeout = 20 * time.Millisecond
	cfg.InternalCoupling.AutoDetect = true
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	entry := alpha.language()
	entry.Spec.DetectPackages = func(ctx context.Context, _ string) ([]string, error) {
		<-ctx.Done()
		return []string{"partial.prefix"}, ctx.Err()
	}

	got, err := Run(t.Context(), Request{Root: root, Config: cfg, Languages: []Language{entry}})

	require.ErrorIs(t, err, ErrTimeout)
	assert.True(t, got.Partial)
	assert.Empty(t, alpha.instances(), "the deadline expires before analyzer construction")
}

func TestRunRejectsAnUnknownLanguage(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := run(t, root, testConfig(langAlpha, langBeta), alpha)

	require.Error(t, err)
	assert.Equal(t, "unknown language beta in metrics", err.Error())
}

func TestRunRejectsAMissingConfiguration(t *testing.T) {
	_, err := Run(t.Context(), Request{Root: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configuration")
}

func TestRunRejectsAnInvalidFilterPattern(t *testing.T) {
	cfg := testConfig(langAlpha)
	cfg.Exclude = []string{"regex:("}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := run(t, t.TempDir(), cfg, alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exclude: invalid regex")
}

func TestRunRejectsAnInvalidWeightPattern(t *testing.T) {
	cfg := testConfig(langAlpha)
	cfg.Metrics[langAlpha] = config.PatternWeights{{Pattern: "("}}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}
	root := writeTree(t, map[string]string{"a.alpha": "a"})

	_, err := run(t, root, cfg, alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "metrics.alpha")
}

func TestRunReportsAMissingRoot(t *testing.T) {
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	_, err := run(t, filepath.Join(t.TempDir(), "absent"), testConfig(langAlpha), alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "walk ")
}

func TestRunReportsAnUnreadableFile(t *testing.T) {
	root := writeTree(t, map[string]string{"secret.alpha": "a"})
	require.NoError(t, os.Chmod(filepath.Join(root, "secret.alpha"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "secret.alpha"), 0o644) })
	if _, err := os.ReadFile(filepath.Join(root, "secret.alpha")); err == nil {
		t.Skip("the test user reads any file")
	}
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	_, err := run(t, root, testConfig(langAlpha), alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "analyze secret.alpha")
}

func TestRunAbortsOnAnAnalyzerError(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", err: errors.New("parser exploded")}

	_, err := run(t, root, testConfig(langAlpha), alpha)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "analyze a.alpha: parser exploded")
}

func TestRunReturnsAPartialResultWhenTheTimeoutElapses(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.alpha": "a", "b.alpha": "b", "c.alpha": "c", "d.alpha": "d",
	})
	cfg := testConfig(langAlpha)
	cfg.Timeout = 30 * time.Millisecond
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", block: true}

	got, err := run(t, root, cfg, alpha)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrTimeout)
	assert.Contains(t, err.Error(), "timeout of 30ms elapsed")
	assert.True(t, got.Partial)
	assert.False(t, got.Blocked)
	assert.Empty(t, got.Files, "every analyzer was still waiting")
	require.Len(t, got.Warnings, 1)
	assert.Equal(t, "timeout of 30ms elapsed; 0 files analyzed, 4 skipped", got.Warnings[0])
	assert.Equal(t, int64(len(alpha.instances())), alpha.closes.Load(), "the workers still release their analyzers")
}

func TestRunPreservesFinalizedBlockingOutcomeWhenCancellationReturnsAPartialResult(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a", "b.alpha": "b"})
	cfg := testConfig(langAlpha)
	cfg.Timeout = time.Second
	blockStarted := make(chan struct{}, 1)
	alpha := &fakeLanguage{
		id:           langAlpha,
		ext:          ".alpha",
		result:       oneUnit("A", map[config.MetricID]int{config.MetricCodeBranch: 11}),
		block:        true,
		blockPath:    "b.alpha",
		blockStarted: blockStarted,
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		select {
		case <-blockStarted:
			cancel()
		case <-ctx.Done():
		}
	}()

	got, err := Run(ctx, Request{Root: root, Config: cfg, Languages: []Language{alpha.language()}})

	require.ErrorIs(t, err, ErrTimeout)
	assert.True(t, got.Partial)
	assert.True(t, got.Blocked, "partial results must retain the finalized blocking outcome")
	assert.Equal(t, []string{"a.alpha"}, paths(got.Files))
}

func TestRunReturnsAPartialResultWhenTheCallerCancels(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a", "b.alpha": "b"})
	cfg := testConfig(langAlpha)
	cfg.Timeout = 0
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", block: true}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for len(alpha.instances()) == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	got, err := Run(ctx, Request{Root: root, Config: cfg, Languages: []Language{alpha.language()}})

	require.ErrorIs(t, err, ErrTimeout)
	assert.True(t, got.Partial)
	require.Len(t, got.Warnings, 1)
	assert.Contains(t, got.Warnings[0], "run canceled;")
}

func TestRunWithoutATimeoutAnalyzesEverything(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a", "b.alpha": "b"})
	cfg := testConfig(langAlpha)
	cfg.Timeout = 0
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

	got, err := run(t, root, cfg, alpha)

	require.NoError(t, err)
	assert.Len(t, got.Files, 2)
}

func TestRunOnAnEmptyTree(t *testing.T) {
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha"}

	got, err := run(t, t.TempDir(), testConfig(langAlpha), alpha)

	require.NoError(t, err)
	assert.Empty(t, got.Files)
	assert.Empty(t, alpha.instances(), "no file, no analyzer")
	assert.Zero(t, got.Violations())
	assert.Zero(t, got.UnitCount())
}

func TestRunAnalyzesSeveralLanguagesTogether(t *testing.T) {
	root := writeTree(t, map[string]string{"a.alpha": "a", "b.beta": "b"})
	alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}
	beta := &fakeLanguage{
		id:     langBeta,
		ext:    ".beta",
		result: FileResult{Units: []Unit{{Name: "b.beta", Kind: kindFile}}},
	}

	got, err := run(t, root, testConfig(langAlpha, langBeta), alpha, beta)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.alpha", "b.beta"}, paths(got.Files))
	assert.Equal(t, langBeta, got.Files[1].Language)
	assert.Equal(t, kindFile, got.Files[1].Units[0].Kind)
}

func TestRunWarnsAboutAnEnforcementModeItCannotHonorYet(t *testing.T) {
	tests := map[string]struct {
		enforcement config.Enforcement
		warning     string
	}{
		config.ModeStrictAll: {
			enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictAll},
		},
		config.ModeStrictOnNewOnly: {
			enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictOnNewOnly},
			warning:     "legacy_mode strict_on_new_only is not enforced yet; reporting only",
		},
		config.ModeBoyScout: {
			enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeBoyScout},
			warning:     "legacy_mode boy_scout is not enforced yet; reporting only",
		},
		config.ModeMeasureOnly: {
			enforcement: config.Enforcement{LegacyMode: config.ModeMeasureOnly},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"a.alpha": "a"})
			cfg := testConfig(langAlpha)
			cfg.Enforcement = tt.enforcement
			alpha := &fakeLanguage{id: langAlpha, ext: ".alpha", result: oneUnit("A", nil)}

			got, err := run(t, root, cfg, alpha)

			require.NoError(t, err)
			if tt.warning == "" {
				assert.Empty(t, got.Warnings)
				return
			}
			assert.Equal(t, []string{tt.warning}, got.Warnings)
		})
	}
}

func TestEnforcementBlocksOnlyUnderStrictAll(t *testing.T) {
	tests := []struct {
		enforcement config.Enforcement
		blocks      bool
	}{
		{config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictAll}, true},
		{config.Enforcement{BlockOnCI: false, LegacyMode: config.ModeStrictAll}, false},
		{config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictOnNewOnly}, false},
		{config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeBoyScout}, false},
		{config.Enforcement{BlockOnCI: false, LegacyMode: config.ModeMeasureOnly}, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.blocks, tt.enforcement.Blocks(), tt.enforcement)
	}
}
