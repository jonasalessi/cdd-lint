package analyze

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// ErrTimeout ends a run that hit its time budget, or whose caller canceled
// it, before every file was analyzed. Run returns it wrapped, together with
// the partial RunResult, so a command can print what was measured and still
// exit non-zero.
var ErrTimeout = errors.New("analysis stopped before every file was analyzed")

// Run analyzes every file under req.Root, or under req.Paths when given,
// that a configured language claims and the include/exclude patterns keep,
// and returns the weighed report.
//
// A configured language the registry does not know is rejected up front. An
// unavailable analyzer is rejected only when the requested files select its
// language, and is still rejected before any candidate is analyzed.
//
// Files are analyzed in parallel, one analyzer per worker and language, and
// the reports come back in path order. When req.Config.Timeout elapses the
// remaining files are dropped, RunResult.Partial is set and the error wraps
// ErrTimeout; any other failure aborts the run naming the file.
func Run(ctx context.Context, req Request) (RunResult, error) {
	start := time.Now()
	if req.Config == nil {
		return RunResult{}, errors.New("analyze: no configuration")
	}
	if req.Config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Config.Timeout)
		defer cancel()
	}
	p, err := newPlan(req)
	if err != nil {
		return RunResult{}, err
	}
	found, err := p.collect(ctx, req.Root, req.Paths)
	if err != nil {
		return RunResult{}, err
	}
	if err := p.initializeSelected(ctx, req.Config, req.Root, found); err != nil {
		if stoppedEarly(err) {
			return p.stoppedResult(start, req.Root, nil, len(found), false)
		}
		return RunResult{}, err
	}
	files, err := p.analyze(ctx, req.Root, found)
	if err != nil {
		return RunResult{}, err
	}
	slices.SortFunc(files, func(a, b FileReport) int { return strings.Compare(a.Path, b.Path) })
	result := RunResult{Root: req.Root, Files: files, Warnings: p.warnings, Elapsed: time.Since(start)}
	result.Blocked = result.Violations() > 0 && req.Config.Enforcement.Blocks()
	if ctx.Err() == nil {
		return result, nil
	}
	return p.stoppedResult(start, req.Root, files, len(found)-len(files), result.Blocked)
}

// languagePlan is the analyzer-specific state a selected language shares
// with every worker.
type languagePlan struct {
	newAnalyzer func(Options) Analyzer
	resolver    *resolver
	prefixes    []string
}

// plan is the read-only knowledge a run shares with every worker: which
// configured language claims each extension, the state of the languages
// selected by candidates, and which files the configuration keeps.
type plan struct {
	configured []Language
	languages  map[config.Language]languagePlan
	byExt      map[string]config.Language
	matcher    *matcher
	// skipUnclaimed is Request.SkipUnclaimed.
	skipUnclaimed bool
	timeout       time.Duration
	warnings      []string
}

// newPlan indexes the configured registry metadata needed to collect
// candidates. Analyzer-specific state is initialized after collection.
func newPlan(req Request) (*plan, error) {
	p := &plan{
		languages: make(map[config.Language]languagePlan),
		byExt:     make(map[string]config.Language),
		timeout:   req.Config.Timeout,
		warnings:  enforcementWarnings(req.Config.Enforcement),

		skipUnclaimed: req.SkipUnclaimed,
	}
	if err := p.indexConfigured(req); err != nil {
		return nil, err
	}
	matcher, err := newMatcher(req.Config.Include, req.Config.Exclude)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}
	p.matcher = matcher
	return p, nil
}

// indexConfigured records every configured language known to the registry,
// regardless of whether its analyzer is available.
func (p *plan) indexConfigured(req Request) error {
	known := make(map[config.Language]bool, len(req.Languages))
	for _, lang := range req.Languages {
		id := lang.Spec.ID
		known[id] = true
		if _, configured := req.Config.Metrics[id]; !configured {
			continue
		}
		p.configured = append(p.configured, lang)
		for _, ext := range lang.Spec.Extensions {
			p.byExt[strings.ToLower(ext)] = id
		}
	}
	return checkUnknown(req.Config, known)
}

// checkUnknown reports a language the configuration names and the registry
// does not know; config.Validate rejects the same file earlier.
func checkUnknown(cfg *config.Config, known map[config.Language]bool) error {
	var unknown []string
	for id := range cfg.Metrics {
		if !known[id] {
			unknown = append(unknown, string(id))
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	return fmt.Errorf("unknown language %s in metrics", strings.Join(unknown, ", "))
}

// initializeSelected validates every language candidates selected before it
// constructs any analyzer-specific state. The two passes ensure an
// unavailable language stops the run before another candidate is analyzed.
func (p *plan) initializeSelected(
	ctx context.Context,
	cfg *config.Config,
	root string,
	found []candidate,
) error {
	selected := make(map[config.Language]bool)
	for _, candidate := range found {
		selected[candidate.lang] = true
	}
	for _, lang := range p.configured {
		if selected[lang.Spec.ID] && lang.NewAnalyzer == nil {
			return fmt.Errorf("no analyzer for %s yet", lang.Spec.ID)
		}
	}
	for _, lang := range p.configured {
		id := lang.Spec.ID
		if !selected[id] {
			continue
		}
		resolver, err := newResolver(cfg, id)
		if err != nil {
			return err
		}
		prefixes, err := internalPrefixes(ctx, cfg, lang.Spec, root)
		if err != nil {
			return err
		}
		p.languages[id] = languagePlan{
			newAnalyzer: lang.NewAnalyzer,
			resolver:    resolver,
			prefixes:    prefixes,
		}
	}
	return nil
}

// internalPrefixes merges the configured internal package prefixes with the
// ones the language detects under root when auto_detect is on. The result
// is sorted and deduplicated so two runs feed the analyzer the same list.
func internalPrefixes(
	ctx context.Context,
	cfg *config.Config,
	spec config.LanguageSpec,
	root string,
) ([]string, error) {
	prefixes := slices.Clone(cfg.InternalCoupling.Packages)
	if cfg.InternalCoupling.AutoDetect && spec.DetectPackages != nil {
		detected, err := spec.DetectPackages(ctx, root)
		if err != nil {
			return nil, fmt.Errorf("detect %s packages: %w", spec.ID, err)
		}
		prefixes = append(prefixes, detected...)
	}
	slices.Sort(prefixes)
	return slices.Compact(prefixes), nil
}

// enforcementWarnings reports an enforcement setting the pipeline cannot
// honor yet, so the caller prints the report instead of blocking on it.
func enforcementWarnings(e config.Enforcement) []string {
	if !e.BlockOnCI || e.Blocks() {
		return nil
	}
	return []string{fmt.Sprintf("legacy_mode %s is not enforced yet; reporting only", e.LegacyMode)}
}

// analyze runs the worker pool over found and returns the finished reports
// in completion order.
func (p *plan) analyze(ctx context.Context, root string, found []candidate) ([]FileReport, error) {
	return (&pool{root: root, languages: p.languages}).run(ctx, found)
}

// stoppedResult builds the partial result returned when the shared run
// context ends during setup, collection, or analysis.
func (p *plan) stoppedResult(
	start time.Time,
	root string,
	files []FileReport,
	skipped int,
	blocked bool,
) (RunResult, error) {
	stopped := stoppedMessage(p.timeout, len(files), skipped)
	result := RunResult{
		Root:     root,
		Files:    files,
		Warnings: append(slices.Clone(p.warnings), stopped),
		Partial:  true,
		Elapsed:  time.Since(start),
	}
	result.Blocked = blocked
	return result, fmt.Errorf("%s: %w", stopped, ErrTimeout)
}

// stoppedEarly reports whether err is the run ending rather than failing.
func stoppedEarly(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// stoppedMessage says why a run ended early and how much of it finished.
func stoppedMessage(timeout time.Duration, analyzed, skipped int) string {
	if timeout > 0 {
		return fmt.Sprintf("timeout of %s elapsed; %d files analyzed, %d skipped", timeout, analyzed, skipped)
	}
	return fmt.Sprintf("run canceled; %d files analyzed, %d skipped", analyzed, skipped)
}
