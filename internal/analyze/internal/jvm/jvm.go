// Package jvm holds what the Java and Kotlin analyzers share because it is
// about the JVM rather than about one language: the package-prefix detection
// in this file, since both declare packages the same way, and the coupling
// classification and per-unit attribution in imports.go, since both import
// qualified paths. Which paths are standard library differs per language, so
// each analyzer passes its own predicate. Grammar walking stays with each
// analyzer.
package jvm

import (
	"bufio"
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/detect"
)

// maxFiles caps how many sources Prefixes reads before it settles on the
// prefixes found so far.
const maxFiles = 200

// maxPrefixes caps how many package prefixes Prefixes returns.
const maxPrefixes = 5

// packageRe matches a java or kotlin package declaration; the semicolon is
// optional so one expression covers both.
var packageRe = regexp.MustCompile(`^\s*package\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)

// Prefixes scans up to maxFiles sources under root whose extension is one of
// exts for package declarations and reduces them with commonPrefixes. A
// missing root or an unreadable file yields no prefixes, not an error. If ctx
// ends during the walk, Prefixes returns the prefixes collected so far with
// the context error.
func Prefixes(ctx context.Context, root string, exts []string) ([]string, error) {
	wanted := map[string]bool{}
	for _, ext := range exts {
		wanted[strings.ToLower(ext)] = true
	}
	pkgs := map[string]bool{}
	read := 0
	walk := func(path string, entry fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if entry.IsDir() {
			if detect.SkipDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if read >= maxFiles {
			return filepath.SkipAll
		}
		if !wanted[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		read++
		if pkg := declaredPackage(path); pkg != "" {
			pkgs[pkg] = true
		}
		return nil
	}
	err := filepath.WalkDir(root, walk)
	prefixes := commonPrefixes(slices.Sorted(maps.Keys(pkgs)))
	if len(prefixes) > maxPrefixes {
		prefixes = prefixes[:maxPrefixes]
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return prefixes, err
	}
	if err != nil {
		return nil, nil
	}
	return prefixes, nil
}

// declaredPackage returns the package declared in the first 100 lines of the
// file at path, or "".
func declaredPackage(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for lines := 0; sc.Scan() && lines < 100; lines++ {
		if m := packageRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return ""
}

// commonPrefixes reduces package names to the shortest prefixes that still
// tell project areas apart: the longest shared segment prefix plus one more
// segment per branch. {com.acme.billing.api, com.acme.billing.db,
// com.acme.shared} becomes {com.acme.billing, com.acme.shared}.
func commonPrefixes(names []string) []string {
	if len(names) <= 1 {
		return names
	}
	segs := make([][]string, len(names))
	for i, name := range names {
		segs[i] = strings.Split(name, ".")
	}
	shared := segs[0]
	for _, s := range segs[1:] {
		shared = sharedSegments(shared, s)
	}
	if len(shared) == 0 {
		return groupByFirstSegment(names)
	}
	prefix := strings.Join(shared, ".")
	var out []string
	seen := map[string]bool{}
	for _, s := range segs {
		branch := prefix
		if len(s) > len(shared) {
			branch = prefix + "." + s[len(shared)]
		}
		if !seen[branch] {
			seen[branch] = true
			out = append(out, branch)
		}
	}
	slices.Sort(out)
	return out
}

// groupByFirstSegment splits names that share nothing into groups by their
// first segment and reduces each group on its own.
func groupByFirstSegment(names []string) []string {
	groups := map[string][]string{}
	for _, name := range names {
		first, _, _ := strings.Cut(name, ".")
		groups[first] = append(groups[first], name)
	}
	var out []string
	for _, first := range slices.Sorted(maps.Keys(groups)) {
		out = append(out, commonPrefixes(groups[first])...)
	}
	return out
}

// sharedSegments returns the longest common prefix of two segment lists.
func sharedSegments(a, b []string) []string {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}
