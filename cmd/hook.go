package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/jonasalessi/cdd-lint/internal/agenthook"
	"github.com/jonasalessi/cdd-lint/internal/git"
	"github.com/jonasalessi/cdd-lint/internal/githook"
)

// preCommitHook is the git hook the block is written into.
const preCommitHook = "pre-commit"

func newHookCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "Install hooks that run cdd where code changes",
		Long: `hook installs cdd into the tools that see code change. Each subcommand
targets one tool: "cdd hook git" is the git pre-commit hook, and "cdd hook
claude" the Claude Code hook that checks every file the agent edits.`,
	}
	c.AddCommand(newHookGitCmd())
	for _, agent := range agenthook.All() {
		c.AddCommand(newHookAgentCmd(agent))
	}
	return c
}

func newHookGitCmd() *cobra.Command {
	var remove bool
	c := &cobra.Command{
		Use:   "git",
		Short: "Install a pre-commit hook that runs cdd check --staged",
		Long: `hook git writes a block into the repository's pre-commit hook that runs
"cdd check --staged" on every commit, so a commit is blocked exactly when a
CI run would be: block_on_ci true with legacy_mode strict_all. Any other
enforcement reports and lets the commit through.

The hook file is the one git reads, honoring core.hooksPath and worktrees.
A missing file is created. An existing shell script keeps its content and
gets the block after its shebang; running the command again replaces the
block, so an upgrade of cdd refreshes it. A hook of another interpreter or
a symlink is left untouched and reported.

The block finds cdd on PATH at commit time and blocks the commit when it is
missing. Skip it once with "git commit --no-verify".

--remove takes the block out again and deletes the file when nothing else
is in it.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if remove {
				return runHookGitRemove(c)
			}
			return runHookGit(c, configPath)
		},
	}
	c.Flags().BoolVar(&remove, "remove", false, "take the cdd block out of the pre-commit hook")
	return c
}

// runHookGit implements cdd hook git: locate the repository and its hook,
// check the configuration exists, then write the block.
func runHookGit(c *cobra.Command, path string) error {
	top, err := git.Toplevel(c.Context(), ".")
	if err != nil {
		return err
	}
	hook, err := hookPath(c)
	if err != nil {
		return err
	}
	baked, err := bakedConfig(top, path)
	if err != nil {
		return err
	}
	return installBlock(c, hook, githook.Block(baked))
}

// installBlock writes block into hook and prints the receipt.
func installBlock(c *cobra.Command, hook, block string) error {
	updating, err := githook.Installed(hook)
	if err != nil {
		return untouched(err)
	}
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		return err
	}
	if err := githook.Install(hook, block); err != nil {
		return untouched(err)
	}
	verb := "installed"
	if updating {
		verb = "updated"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s %s hook at %s\n", verb, preCommitHook, displayPath(hook))
	return nil
}

// runHookGitRemove implements cdd hook git --remove.
func runHookGitRemove(c *cobra.Command) error {
	hook, err := hookPath(c)
	if err != nil {
		return err
	}
	removed, err := githook.Remove(hook)
	if err != nil {
		return untouched(err)
	}
	if removed {
		fmt.Fprintf(c.OutOrStdout(), "removed cdd hook from %s\n", displayPath(hook))
	} else {
		fmt.Fprintf(c.OutOrStdout(), "no cdd hook found at %s\n", displayPath(hook))
	}
	return nil
}

// hookPath is the pre-commit hook of the repository around the working
// directory.
func hookPath(c *cobra.Command) (string, error) {
	dir, err := git.HooksDir(c.Context(), ".")
	if err != nil {
		return "", err
	}
	return resolvedAbs(filepath.Join(dir, preCommitHook))
}

// bakedConfig is the --config value the block carries: the configuration's
// path relative to the top level, or empty at the default location. The
// file must exist, since a hook that fails on every commit is worse than
// none.
func bakedConfig(top, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s not found; run cdd init first", path)
		}
		return "", err
	}
	abs, err := resolvedAbs(path)
	if err != nil {
		return "", err
	}
	rel, ok := relativeTo(top, abs)
	if !ok {
		return "", fmt.Errorf("%s is outside the repository %s", path, top)
	}
	if rel == defaultConfigPath {
		return "", nil
	}
	return filepath.ToSlash(rel), nil
}

// refusals are the errors that mean a file could not take the hook.
var refusals = []error{
	githook.ErrForeignHook, githook.ErrSymlink,
	agenthook.ErrForeignSettings, agenthook.ErrSymlink,
}

// untouched adds to a refusal that nothing was written.
func untouched(err error) error {
	if slices.ContainsFunc(refusals, func(refusal error) bool { return errors.Is(err, refusal) }) {
		return fmt.Errorf("%w; the file was left untouched", err)
	}
	return err
}

// displayPath shows hook relative to the working directory when it lies
// under it, which is the common case, and absolute otherwise.
func displayPath(hook string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return hook
	}
	if cwd, err = resolvedAbs(cwd); err != nil {
		return hook
	}
	if rel, ok := relativeTo(cwd, hook); ok {
		return rel
	}
	return hook
}
