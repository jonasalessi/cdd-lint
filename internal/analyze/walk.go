package analyze

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/detect"
)

// candidate is one file the walk selected: its slash-separated path
// relative to the run root, and the language whose extension claims it.
type candidate struct {
	path string
	lang config.Language
}

// collect returns every file under root a configured language claims and
// the matcher includes. A non-empty paths narrows that to the named files
// and directories; a file named twice, directly or through its directory,
// is returned once.
func (p *plan) collect(ctx context.Context, root string, paths []string) ([]candidate, error) {
	if len(paths) == 0 {
		return p.walk(ctx, root, root)
	}
	var found []candidate
	seen := make(map[string]bool)
	for _, rel := range paths {
		if ctx.Err() != nil {
			break
		}
		more, err := p.collectPath(ctx, root, rel)
		if err != nil {
			return nil, err
		}
		for _, c := range more {
			if !seen[c.path] {
				seen[c.path] = true
				found = append(found, c)
			}
		}
	}
	return found, nil
}

// errIneligible marks a named file the run would pass over in a walk.
var errIneligible = errors.New("ineligible")

// ineligibleError says why one named file is not analyzed. It matches
// errIneligible through errors.Is while keeping its own message.
type ineligibleError struct{ reason string }

func (e ineligibleError) Error() string      { return e.reason }
func (ineligibleError) Is(target error) bool { return target == errIneligible }

// collectPath resolves one requested path: a directory is walked, a file
// is checked on its own. The caller asked for the file by name, so one the
// run would silently pass over in a walk is an error here, unless the plan
// skips unclaimed files.
func (p *plan) collectPath(ctx context.Context, root, rel string) ([]candidate, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	link, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if link.Mode()&os.ModeSymlink != 0 && info.IsDir() {
		return nil, fmt.Errorf("%s: symlinked directory is not supported", rel)
	}
	if info.IsDir() {
		return p.walk(ctx, root, full)
	}
	c, err := p.file(rel)
	if err != nil {
		if p.skipUnclaimed && errors.Is(err, errIneligible) {
			return nil, nil
		}
		return nil, err
	}
	return []candidate{c}, nil
}

// file claims one root-relative path for the language its extension names,
// or says why the run does not analyze it.
func (p *plan) file(rel string) (candidate, error) {
	lang, claimed := p.byExt[strings.ToLower(path.Ext(rel))]
	if !claimed {
		return candidate{}, ineligibleError{fmt.Sprintf("%s: no configured language claims this file", rel)}
	}
	if !p.matcher.Match(rel) {
		return candidate{}, ineligibleError{fmt.Sprintf("%s is excluded by the configuration", rel)}
	}
	return candidate{path: rel, lang: lang}, nil
}

// walk descends from start, which is root or a directory under it, and
// returns every file the run analyzes, with paths relative to root.
// Directories detect.SkipDir names, including version control and build
// output, are never entered unless start itself is one: the caller asked for
// it. A canceled context ends the walk with what it found so far, because Run
// turns that into a partial result rather than an error.
func (p *plan) walk(ctx context.Context, root, start string) ([]candidate, error) {
	var found []candidate
	err := filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != start && detect.SkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if c, err := p.file(filepath.ToSlash(rel)); err == nil {
			found = append(found, c)
		}
		return nil
	})
	if err != nil && !stoppedEarly(err) {
		return nil, fmt.Errorf("walk %s: %w", start, err)
	}
	return found, nil
}
