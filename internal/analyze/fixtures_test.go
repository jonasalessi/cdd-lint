package analyze

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// The pipeline is language-agnostic, so its tests run against synthetic
// languages: a real id here would tie the package to the registry.
const (
	langAlpha config.Language = "alpha"
	langBeta  config.Language = "beta"
)

// Unit kinds the fixtures report.
const (
	kindClass = "class"
	kindFile  = "file"
)

// fakeAnalyzer returns the same result for every file and records what it
// saw, so a test can prove which files reached which analyzer.
type fakeAnalyzer struct {
	result       FileResult
	err          error
	blockPath    string
	blockStarted chan<- struct{}
	// block makes Analyze wait for the context instead of returning, which
	// is how a test drives the timeout.
	block  bool
	closes *atomic.Int64
	opts   Options

	mu    sync.Mutex
	paths []string
}

// Analyze records the path and returns the canned result.
func (a *fakeAnalyzer) Analyze(ctx context.Context, path string, _ []byte) (FileResult, error) {
	a.mu.Lock()
	a.paths = append(a.paths, path)
	a.mu.Unlock()
	if a.block && (a.blockPath == "" || a.blockPath == path) {
		if a.blockStarted != nil {
			a.blockStarted <- struct{}{}
		}
		<-ctx.Done()
		return FileResult{}, ctx.Err()
	}
	if a.err != nil {
		return FileResult{}, a.err
	}
	return a.result, nil
}

// Close counts the release, so a leaked analyzer shows up as a missing one.
func (a *fakeAnalyzer) Close() error {
	a.closes.Add(1)
	return nil
}

// analyzed returns the paths this analyzer saw.
func (a *fakeAnalyzer) analyzed() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.paths...)
}

// fakeLanguage is one synthetic language: the spec the walk needs and the
// factory that hands every worker its own analyzer.
type fakeLanguage struct {
	id     config.Language
	ext    string
	result FileResult
	err    error
	block  bool
	// blockPath limits blocking to one path when set; otherwise every
	// analyzed path blocks.
	blockPath    string
	blockStarted chan<- struct{}

	closes atomic.Int64
	mu     sync.Mutex
	built  []*fakeAnalyzer
}

// language is the registry entry the request carries.
func (f *fakeLanguage) language() Language {
	return Language{
		Spec:        config.LanguageSpec{ID: f.id, Extensions: []string{f.ext}},
		NewAnalyzer: f.newAnalyzer,
	}
}

// newAnalyzer builds one analyzer and keeps it, together with the Options
// the pipeline passed in.
func (f *fakeLanguage) newAnalyzer(opts Options) Analyzer {
	a := &fakeAnalyzer{
		result:       f.result,
		err:          f.err,
		block:        f.block,
		blockPath:    f.blockPath,
		blockStarted: f.blockStarted,
		closes:       &f.closes,
		opts:         opts,
	}
	f.mu.Lock()
	f.built = append(f.built, a)
	f.mu.Unlock()
	return a
}

// instances returns every analyzer built so far.
func (f *fakeLanguage) instances() []*fakeAnalyzer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeAnalyzer(nil), f.built...)
}

// paths returns every path this language analyzed, across workers.
func (f *fakeLanguage) paths() []string {
	var out []string
	for _, a := range f.instances() {
		out = append(out, a.analyzed()...)
	}
	return out
}

// oneUnit is the result of an analyzer that reports a single unit with the
// given counts.
func oneUnit(name string, counts map[config.MetricID]int) FileResult {
	return FileResult{Units: []Unit{{Name: name, Kind: kindClass, Line: 1, Col: 1, Counts: counts}}}
}

// writeTree lays out files under a fresh directory and returns its path.
// The keys are slash-separated paths, the values the file contents.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

// testConfig is a minimal valid configuration for the synthetic languages:
// one weight and one limit per language, both under config.PatternAll.
func testConfig(langs ...config.Language) *config.Config {
	cfg := &config.Config{
		Version:     config.SchemaVersion,
		ProjectType: config.ProjectGreenfield,
		Metrics:     make(map[config.Language]config.PatternWeights, len(langs)),
		ICPLimits:   make(map[config.Language]config.PatternLimits, len(langs)),
		Enforcement: config.Enforcement{BlockOnCI: true, LegacyMode: config.ModeStrictAll},
		Timeout:     time.Minute,
		Reporter:    config.Reporter{Format: config.FormatConsole},
	}
	for _, lang := range langs {
		cfg.Metrics[lang] = config.PatternWeights{{
			Pattern: config.PatternAll,
			Weights: map[config.MetricID]float64{
				config.MetricCodeBranch:       1,
				config.MetricCondition:        1,
				config.MetricExternalCoupling: 0.5,
			},
		}}
		cfg.ICPLimits[lang] = config.PatternLimits{{Pattern: config.PatternAll, Limit: 10}}
	}
	return cfg
}

// paths returns the path of every report, in order.
func paths(files []FileReport) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}
