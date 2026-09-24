package analyze

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/jonasalessi/cdd-lint/internal/detect"
)

// walker descends from start, which is root or a directory under it, and
// keeps every file the plan analyzes, with paths relative to root.
// Directories detect.SkipDir names, including version control and build
// output, are never entered unless start itself is one: the caller asked for
// it. A canceled context ends the walk with what it found so far, because Run
// turns that into a partial result rather than an error.
type walker struct {
	p     *plan
	root  string
	start string
	found []candidate
}

// walk descends from start and returns every file the run analyzes.
func (p *plan) walk(ctx context.Context, root, start string) ([]candidate, error) {
	return (&walker{p: p, root: root, start: start}).run(ctx)
}

// run performs the walk and returns what it kept.
func (w *walker) run(ctx context.Context) ([]candidate, error) {
	err := filepath.WalkDir(w.start, func(path string, entry fs.DirEntry, err error) error {
		return w.visit(ctx, path, entry, err)
	})
	if err != nil && !stoppedEarly(err) {
		return nil, fmt.Errorf("walk %s: %w", w.start, err)
	}
	return w.found, nil
}

// visit is the WalkDir callback: it stops on a canceled context, passes a
// walk error up, decides whether to enter a directory and keeps a file.
func (w *walker) visit(ctx context.Context, path string, entry fs.DirEntry, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	if entry.IsDir() {
		return w.enter(path, entry)
	}
	return w.keep(path)
}

// enter skips a directory the run never descends into, unless it is the
// start itself.
func (w *walker) enter(path string, entry fs.DirEntry) error {
	if path != w.start && detect.SkipDir(entry.Name()) {
		return filepath.SkipDir
	}
	return nil
}

// keep records path when the plan claims it and the configuration keeps it.
func (w *walker) keep(path string) error {
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return err
	}
	if c, err := w.p.file(filepath.ToSlash(rel)); err == nil {
		w.found = append(w.found, c)
	}
	return nil
}
