// Package git is the only package that runs the git binary. It answers the
// three questions cdd has for a repository: where its top level is, where
// its hooks live, and which files are staged.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepository reports that dir is not inside a git work tree.
var ErrNotRepository = errors.New("not a git repository")

// Toplevel returns the absolute root of the work tree that contains dir.
func Toplevel(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Clean(out), nil
}

// HooksDir returns the absolute directory git reads hooks from for the
// repository that contains dir. It honors core.hooksPath, linked worktrees
// and submodules, none of which a literal .git/hooks does.
func HooksDir(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	return filepath.Abs(filepath.Join(dir, out))
}

// Staged lists the files staged for commit, slash-separated and relative
// to the top level, in git's order. Deleted files are left out and a
// rename appears under its new name. Outside a repository "git diff"
// would compare two paths instead of failing, so the repository is checked
// first.
func Staged(ctx context.Context, dir string) ([]string, error) {
	if _, err := Toplevel(ctx, dir); err != nil {
		return nil, err
	}
	out, err := run(ctx, dir, "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z")
	if err != nil {
		return nil, err
	}
	names := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	return dropEmpty(names), nil
}

// dropEmpty removes the empty entry an empty listing splits into.
func dropEmpty(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// run executes one git command in dir and returns its stdout with the
// trailing newline removed.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", classify(err, stderr.String())
	}
	return strings.TrimSuffix(stdout.String(), "\n"), nil
}

// classify turns a failed git invocation into an error a command can
// print: ErrNotRepository when git says so, otherwise git's own first
// stderr line, or the exec error when git itself could not start.
func classify(err error, stderr string) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("git: %w", err)
	}
	if strings.Contains(stderr, "not a git repository") {
		return fmt.Errorf("%w", ErrNotRepository)
	}
	return fmt.Errorf("git: %s", firstLine(stderr))
}

// firstLine trims stderr to its first non-empty line, without git's
// "fatal: " prefix.
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return strings.TrimPrefix(line, "fatal: ")
		}
	}
	return "unknown error"
}
