# Feature 08 — Git Hook: `cdd hook git` and `cdd check --staged`

## Goal

Let a project gate its commits on CDD without writing a hook by hand.
`cdd hook git` installs a `pre-commit` hook that runs `cdd check --staged`,
and `cdd hook git --remove` takes it out again. `--staged` is a new mode of
`cdd check` that analyzes the files about to be committed and nothing else.

The hook is a thin trigger. What blocks a commit is exactly what blocks a CI
run today: `block_on_ci: true` with `legacy_mode: strict_all`. Every other
mode reports and exits 0, as it does on the command line. A future baseline
feature makes `strict_on_new_only` a real gate; nothing here anticipates it.

`hook` is a parent command. `git` is its first kind; later kinds (an agent
hook, for instance) hang off the same parent and share no code with `git`.

[test-cases.md](test-cases.md) is the companion to this file: every
constraint below has a numbered test case there, and a task is done when its
cases are checked-in, passing tests.

## Current state (verified 2026-09-23)

- `cmd/root.go:40` registers `version`, `init` and `check`. There is no
  `hook` command and nothing under `cmd/` or `internal/` runs git.
- `cdd check [path...]` (`cmd/check.go:29-80`) analyzes the tree rooted at
  the configuration's directory, narrowed by positional paths. `checkPaths`
  (`check.go:136`) rejects a path outside that directory.
- A named file that no configured language claims, or that the
  include/exclude patterns drop, is an error: `plan.file` in
  `internal/analyze/walk.go:78-88`. The editor contract in
  `docs/editor-integration.md` relies on that; it must not change for
  explicit paths.
- A deleted file named on the command line fails in `collectPath`
  (`walk.go:54-57`) with the `os.Lstat` error.
- `Enforcement.Blocks()` (`internal/config/config.go:56-63`) is the single
  place that decides whether violations block.
- `cmd/init.go:132-157` (`guardExisting`) and `internal/initcmd/write.go`
  set the house style for a command that writes a file: a sentinel error,
  `--force` on `init`, exit 1 with a message that names the fix.
- The repository's own contributor hooks live in `.githooks/` and are
  enabled with `git config core.hooksPath .githooks` (`Makefile:16-20`). So a
  hook written literally to `.git/hooks/` would never run here.
- Tests in `cmd/` run the tree in-process through `runCdd`
  (`cmd/init_test.go:40-50`); nothing builds or executes the binary yet.

## Scope

**In:**

- `internal/git` — the only package that runs the `git` binary: repository
  top level, hooks directory, staged files (FR-1).
- `internal/analyze` — `Request.SkipUnclaimed`, so a caller that names files
  it did not choose can have the ineligible ones dropped instead of erroring
  (FR-2).
- `cmd/check.go` — `--staged` (FR-3).
- `internal/githook` — the hook block, install and remove on a hook file
  (FR-4).
- `cmd/hook.go` — `cdd hook` and `cdd hook git [--remove]` (FR-5).
- One end-to-end test that builds the binary and drives a real
  `git commit` through the installed hook (FR-6).
- README: a `cdd hook git` section, the `--staged` row of the `cdd check`
  table, and the hook's exit behaviour (FR-7).

**Out:**

- Analyzing the index blobs instead of the working tree. `--staged` reads
  the working-tree content of the staged paths, so a partially staged file
  is judged on edits that are not in the commit. The README says so.
- Husky, lefthook and the `pre-commit` framework. The README shows the one
  line to add to them.
- Any hook other than `pre-commit`, and any bypass other than
  `git commit --no-verify`.
- A Windows `.cmd` hook. Git for Windows runs `#!/bin/sh` hooks through its
  bundled shell.
- Changing what blocks. `Enforcement.Blocks()` is not touched.

## Functional requirements

### FR-1 — `internal/git`: the git runner

One package owns every call to the `git` binary. It runs `git` with the
working directory it is given, under the caller's context, and never
changes the process's own.

- `Toplevel(ctx, dir string) (string, error)` — `git rev-parse --show-toplevel`,
  returned as a cleaned absolute path.
- `HooksDir(ctx, dir string) (string, error)` — `git rev-parse --git-path hooks`,
  made absolute against `dir` when git prints it relative. This honours
  `core.hooksPath`, linked worktrees and submodules, which a literal
  `.git/hooks` does not.
- `Staged(ctx, dir string) ([]string, error)` —
  `git diff --cached --name-only --diff-filter=ACMR -z`, split on NUL, each
  entry slash-separated and relative to the top level, in git's order.
  `--diff-filter=ACMR` leaves deleted files out; a rename yields its new
  name. `-z` keeps non-ASCII names unquoted whatever `core.quotePath` says.
- Outside a repository every function returns `ErrNotRepository`, wrapped,
  so a command can print `not a git repository` rather than git's own
  stderr. Any other git failure returns an error that carries git's stderr
  trimmed to one line.
- A missing `git` binary is an error naming it, not a panic.

### FR-2 — `analyze.Request.SkipUnclaimed`

`Request` gains `SkipUnclaimed bool`. When true, a *file* in `Paths` that
`plan.file` would reject — no configured language claims its extension, or
the include/exclude patterns drop it — is left out of the run silently.
When false, behaviour is unchanged: the error stays, and the editor
contract with it.

`SkipUnclaimed` does not swallow anything else. A path that does not exist,
a symlinked directory, and a directory in `Paths` behave exactly as before;
a directory is still walked and the walk already skips what it does not
claim.

### FR-3 — `cdd check --staged`

`--staged` replaces the positional paths with the files staged in the git
index. Given together with a positional path the command fails with
`--staged takes no paths` and exit 1. `--all`, `--explain` and `--format`
compose with it as usual.

The command:

1. Asks `git.Toplevel` and `git.Staged` from the working directory. Outside
   a repository: `cdd: not a git repository`, exit 1.
2. Resolves every staged name against the top level, keeps the ones under
   the configuration's directory, and turns them into slash-separated paths
   relative to that directory. A staged file elsewhere in the repository is
   dropped, not an error: a monorepo commit touches more than one project.
3. Runs the analysis with those paths and `SkipUnclaimed: true`.
4. When nothing is staged under the configuration's directory, or every
   staged file was skipped, prints nothing and exits 0. A commit that only
   touches documentation must be silent, not `PASS units=0`.
5. Otherwise reports and exits exactly like a run with explicit paths: the
   configured reporter, `--format`, and the `0` / `1` / `2` exit codes of
   `check`.

The configuration path is still `--config`, resolved from the working
directory. Git runs a hook from the top level, which is why FR-4 bakes the
flag into the script when the configuration is not at the default place.

### FR-4 — `internal/githook`: the hook block

The hook is a block of POSIX shell delimited by two marker lines, so it can
be found, replaced and removed inside a file the project may already own:

```sh
# >>> cdd hook git >>>
# Installed by "cdd hook git"; remove with "cdd hook git --remove".
if ! command -v cdd >/dev/null 2>&1; then
  echo 'cdd: not found in PATH; install cdd or run "cdd hook git --remove"' >&2
  exit 1
fi
cdd check --staged || exit $?
# <<< cdd hook git <<<
```

The block fails closed: a missing `cdd` blocks the commit with a message
that names both ways out. `cdd check --staged` runs from the top level,
which is where git puts a hook's working directory. When the configuration
is not `cdd.config.yaml` at the top level, the last command carries
`--config <path>` with the path relative to the top level, single-quoted.

`Install(path, block string) error` writes the block into the hook file at
`path`:

- No file: create it as `#!/bin/sh`, a blank line and the block, mode
  `0755`.
- Existing file whose first line is a shebang for `sh`, `bash` or `zsh`
  (any directory, `env` form included): insert the block after that first
  line, with one blank line on each side, and add the execute bit wherever
  the read bit is set (`0644` becomes `0755`, `0700` stays `0700`). The block goes at the top, not the end, because an
  existing script that ends in `exit 0` would never reach an appended
  block.
- Existing file without a shebang: insert the block at the top.
- Existing file already holding a block: replace it in place, so re-running
  after an upgrade refreshes the script. The rest of the file is preserved
  byte for byte.
- Existing file with any other shebang (`#!/usr/bin/env python3`,
  `#!/usr/bin/node`, …): `ErrForeignHook`, wrapped with the path and the
  first line. Nothing is written.
- Path is a symlink: `ErrSymlink`. A hook manager owns that file.
- Writes are atomic through a sibling `.tmp` and a rename, like
  `initcmd.Write`.

`Remove(path string) (removed bool, err error)`:

- No file, or a file without a block: `false, nil`.
- Block present: strip it and the blank lines that separate it from its
  neighbours. When what remains is only the shebang line and whitespace,
  delete the file. Otherwise write the remainder back with the file's mode
  preserved. Returns `true, nil`.
- Symlink: `ErrSymlink`.

`Installed(path string) (bool, error)` reports whether the file holds a
block; it is what `cdd hook git` uses to word its receipt.

### FR-5 — `cdd hook` and `cdd hook git`

`cdd hook` with no subcommand prints its help and exits 0, as cobra does
for a parent. `cdd hook git` takes no positional arguments and one flag,
`--remove`.

`cdd hook git`:

1. Locates the repository with `git.Toplevel` and the hook with
   `git.HooksDir` from the working directory. Outside a repository:
   `cdd: not a git repository`, exit 1.
2. Resolves `--config` from the working directory and requires the file to
   exist: `cdd: <path> not found; run cdd init first`, exit 1. A hook that
   blocks every commit with a missing configuration is worse than no hook.
3. Makes the configuration path relative to the top level. A configuration
   outside the repository is an error. When the relative path is exactly
   `cdd.config.yaml`, the block carries no `--config`; otherwise it carries
   `--config <relative path>`.
4. Calls `githook.Install` on `<hooks dir>/pre-commit`. Creates the hooks
   directory when it does not exist.
5. Prints one line to stdout: `installed pre-commit hook at <path>` on a new
   file or a first block, `updated pre-commit hook at <path>` when a block
   was replaced. `<path>` is the hook path relative to the working
   directory when it lies under it, absolute otherwise.
6. `ErrForeignHook` and `ErrSymlink` come out as `cdd: <message>` with exit
   1; the message says the file was left untouched.

`cdd hook git --remove`:

1. Same repository lookup, no configuration check.
2. Calls `githook.Remove`. Prints `removed cdd hook from <path>` when a
   block was removed, `no cdd hook found at <path>` otherwise. Exit 0 in
   both cases.

No `--force`. Install is idempotent and never destroys a foreign hook, so
there is nothing for `--force` to do.

### FR-6 — End-to-end through a real commit

`cmd/hook_e2e_test.go` builds the binary once per test run with `go build`
into a temp directory, prepends that directory to `PATH` for the git
commands it runs, and in a fresh repository:

1. Writes a TypeScript project with `cdd init --yes` set to `strict_all`
   and `block_on_ci: true`, commits it, runs `cdd hook git`.
2. Stages a file within its limit and a `README.md`; `git commit` succeeds.
3. Stages a file over its limit; `git commit` exits non-zero and its output
   names the violation.
4. `git commit --no-verify` with the same file succeeds.
5. `cdd hook git --remove`, then the over-limit commit succeeds.

The test is skipped under `-short` and when `git` is not on `PATH`. It is
the only test that executes the binary; everything else stays in-process.

### FR-7 — Documentation

- README `## Usage` command list gains `cdd hook git`.
- A new `### cdd hook git` section after `### cdd check`: what it installs,
  where, the append and replace rules, `--remove`, `--no-verify`, the
  working-tree caveat, and the one-liner for husky / lefthook /
  pre-commit-framework users (`cdd check --staged`).
- The `cdd check` table gains a `--staged` row, and the prose says a run
  with nothing eligible is silent.
- The `#### Exit codes` table gets a `cdd hook git` line.
- `docs/editor-integration.md` is not touched: `--staged` changes nothing
  in the json/xml output.

## Decisions and their reasons

- **Working tree, not index blobs.** Reading the index means feeding
  analyzers from memory, and the tree-sitter path reads bytes already, but
  the Go path and the resolver read paths. The caveat is documented and the
  same one this repository's own `.githooks/pre-commit` lives with.
- **Filtering in Go, not in the script.** Extension and exclude rules live
  in the configuration; a shell script cannot know them without
  duplicating the matcher. `SkipUnclaimed` is one boolean and keeps the
  editor contract intact.
- **Insert after the shebang, not append.** See FR-4. A block at the top
  also means the CDD result is the first thing a developer reads.
- **Fail closed on a missing `cdd`.** A gate that silently opens when the
  tool is absent is not a gate.
- **`git rev-parse`, not `.git/hooks`.** This repository sets
  `core.hooksPath`; a literal path would install a hook git never runs.
- **No `--force`.** Nothing is ever overwritten that the command did not
  write.
