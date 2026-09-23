package analyze

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// pool analyzes the collected files in parallel, one analyzer per worker
// and language, with the state the selected languages share.
type pool struct {
	root      string
	languages map[config.Language]languagePlan
}

// run analyzes every file in found and returns the finished reports in
// completion order.
func (w *pool) run(ctx context.Context, found []candidate) ([]FileReport, error) {
	if len(found) == 0 {
		return nil, nil
	}
	workers := min(runtime.GOMAXPROCS(0), len(found))
	g, gctx := errgroup.WithContext(ctx)
	// One slot per worker plus the producer, which would otherwise wait for
	// a slot no worker ever releases.
	g.SetLimit(workers + 1)
	jobs := make(chan candidate)
	g.Go(func() error { return produce(gctx, jobs, found) })

	var (
		mu    sync.Mutex
		files []FileReport
	)
	emit := func(f FileReport) {
		mu.Lock()
		defer mu.Unlock()
		files = append(files, f)
	}
	for range workers {
		g.Go(func() error { return w.work(gctx, jobs, emit) })
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return files, nil
}

// produce feeds the workers until the files run out or the run stops.
func produce(ctx context.Context, jobs chan<- candidate, found []candidate) error {
	defer close(jobs)
	for _, c := range found {
		select {
		case jobs <- c:
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// work analyzes files until the channel closes or the run stops. Analyzers
// are not safe for concurrent use, so each worker builds its own per
// language and releases them all on the way out.
func (w *pool) work(ctx context.Context, jobs <-chan candidate, emit func(FileReport)) (err error) {
	analyzers := make(map[config.Language]Analyzer)
	defer func() { err = errors.Join(err, closeAnalyzers(analyzers)) }()
	for c := range jobs {
		if ctx.Err() != nil {
			return nil
		}
		report, fileErr := w.analyzeFile(ctx, analyzers, c)
		if fileErr != nil {
			if stoppedEarly(fileErr) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("analyze %s: %w", c.path, fileErr)
		}
		emit(report)
	}
	return nil
}

// analyzeFile reads one file, counts it with the worker's analyzer for that
// language, and weighs the counts against the patterns the path resolves to.
func (w *pool) analyzeFile(
	ctx context.Context,
	analyzers map[config.Language]Analyzer,
	c candidate,
) (FileReport, error) {
	lang := w.languages[c.lang]
	analyzer, built := analyzers[c.lang]
	if !built {
		analyzer = lang.newAnalyzer(Options{InternalPrefixes: lang.prefixes})
		analyzers[c.lang] = analyzer
	}
	src, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(c.path)))
	if err != nil {
		return FileReport{}, err
	}
	result, err := analyzer.Analyze(ctx, c.path, src)
	if err != nil {
		return FileReport{}, err
	}
	return FileReport{
		Path:     c.path,
		Language: c.lang,
		Units:    lang.resolver.Resolve(c.path, result.Units),
		Warnings: result.Warnings,
	}, nil
}

// closeAnalyzers releases the analyzers that hold resources, which for a
// parser binding are outside the Go heap.
func closeAnalyzers(analyzers map[config.Language]Analyzer) error {
	var errs []error
	for _, a := range analyzers {
		if closer, ok := a.(io.Closer); ok {
			errs = append(errs, closer.Close())
		}
	}
	return errors.Join(errs...)
}
