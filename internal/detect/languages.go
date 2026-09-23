// Package detect discovers, before any configuration exists, which languages
// a project contains. cdd init uses the result to pre-fill its prompts. The
// walk is generic: which extensions belong to which language comes from the
// specs the caller passes in.
package detect

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// skipDirs are directory names never worth scanning: VCS metadata,
// dependencies and build output.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"build":        true,
	"dist":         true,
	"target":       true,
	"out":          true,
}

// SkipDir reports whether a directory called name is never worth scanning:
// VCS metadata, dependencies and build output. Package detection shares it
// so every walk of a project tree skips the same directories.
func SkipDir(name string) bool {
	return skipDirs[name]
}

// Detected is the result of Languages: how many source files were counted
// per language, whether the scan hit its deadline before finishing, and how
// long it ran.
type Detected struct {
	Counts    map[config.Language]int
	Truncated bool
	Elapsed   time.Duration
}

// Languages returns the detected languages in specs order.
func (d Detected) Languages(specs []config.LanguageSpec) []config.Language {
	var out []config.Language
	for _, spec := range specs {
		if d.Counts[spec.ID] > 0 {
			out = append(out, spec.ID)
		}
	}
	return out
}

// Languages counts source files per language under root, skipping SkipDir
// directories; a file belongs to the spec that lists its extension. The
// caller bounds the walk through ctx; when the deadline expires the partial
// counts come back with Truncated set and a nil error, because a cut-short
// scan is still usable.
func Languages(ctx context.Context, root string, specs []config.LanguageSpec) (Detected, error) {
	start := time.Now()
	d := Detected{Counts: map[config.Language]int{}}
	byExt := extensionIndex(specs)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && SkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if lang, ok := byExt[strings.ToLower(filepath.Ext(entry.Name()))]; ok {
			d.Counts[lang]++
		}
		return nil
	})
	d.Elapsed = time.Since(start)
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		d.Truncated = true
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("detect: %w", err)
	}
	return d, nil
}

// extensionIndex inverts the specs into extension -> language.
func extensionIndex(specs []config.LanguageSpec) map[string]config.Language {
	idx := make(map[string]config.Language)
	for _, spec := range specs {
		for _, ext := range spec.Extensions {
			idx[strings.ToLower(ext)] = spec.ID
		}
	}
	return idx
}
