# Feature 08 — Git Hook: Test Cases

Companion to [task.md](task.md). Every case here is a constraint the
implementation must satisfy; a task is not done until the cases listed under
it are checked-in tests that pass. Case ids are stable — reference them in
test names or comments (`// TC-H3`) so a reviewer can map the suite back to
this file.

**Unit** tests exercise `internal/git`, `internal/analyze` and
`internal/githook` through their exported contract, with real files and real
`git init` repositories in `t.TempDir()`. **Integration** tests exercise the
CLI boundary in-process through `runCdd` (`cmd/hook_test.go`,
`cmd/check_test.go`). The one **end-to-end** test (`cmd/hook_e2e_test.go`)
builds and executes the binary. Nothing is mocked; `git` is on every
developer's and CI machine, and a test that needs it skips when it is not.

## Conventions

- `initRepo(t, dir)` runs `git init -q` in `dir` and sets `user.name` and
  `user.email` in that repository so commits work without a global config.
- `stage(t, dir, names...)` writes nothing; it runs `git add -- names...`.
- `commit(t, dir, msg)` runs `git commit -q -m msg` and returns its exit
  code and combined output.
- `hookPath(t, dir)` is `filepath.Join(dir, ".git", "hooks", "pre-commit")`
  unless the case sets `core.hooksPath`.
- Paths compared in assertions are made absolute and symlink-evaluated on
  both sides, because `t.TempDir()` on macOS lives under `/var`, a symlink
  to `/private/var`, and `git rev-parse` returns the resolved one.

## FR-1 — `internal/git` (unit, `internal/git/git_test.go`)

- **TC-G1** `Toplevel` from the repository root returns the root, absolute
  and symlink-resolved.
- **TC-G2** `Toplevel` from a subdirectory returns the root, not the
  subdirectory.
- **TC-G3** `Toplevel`, `HooksDir` and `Staged` in a directory that is not a
  repository return an error wrapping `ErrNotRepository`.
- **TC-G4** `HooksDir` without `core.hooksPath` is `<root>/.git/hooks`,
  absolute, whether or not the directory exists yet.
- **TC-G5** `HooksDir` with `core.hooksPath = .githooks` is
  `<root>/.githooks`, absolute, when asked from a subdirectory too.
- **TC-G6** `Staged` with a clean index returns an empty slice and no error.
- **TC-G7** `Staged` returns added and modified files as slash-separated
  paths relative to the root, including a file in a subdirectory when asked
  from that subdirectory.
- **TC-G8** `Staged` leaves a staged deletion out and reports a staged
  rename under its new name only.
- **TC-G9** `Staged` returns a name with a space and a non-ASCII name
  verbatim, with `core.quotePath` at its default.
- **TC-G10** With `PATH` pointing at an empty directory every function
  returns an error that names `git`.

## FR-2 — `analyze.Request.SkipUnclaimed` (unit, `internal/analyze/walk_test.go` or `run_test.go`)

- **TC-A1** Without `SkipUnclaimed`, `Paths` naming a `.md` file still fails
  with `no configured language claims this file` (existing behaviour,
  pinned).
- **TC-A2** With `SkipUnclaimed`, the same request analyzes the claimed
  files and drops the `.md` file: no error, `Files` lists only the claimed
  ones.
- **TC-A3** With `SkipUnclaimed`, a file that `exclude` matches is dropped
  silently.
- **TC-A4** With `SkipUnclaimed`, a path that does not exist is still an
  error.
- **TC-A5** With `SkipUnclaimed`, a directory in `Paths` is still walked
  and yields the same files as without the flag.
- **TC-A6** With `SkipUnclaimed` and every named file ineligible, `Run`
  returns an empty `Files` and no error.

## FR-3 — `cdd check --staged` (integration, `cmd/check_staged_test.go`)

Each case initializes a repository in the temp dir, writes a configuration
with `writeTSFixture`-style helpers, and stages what it needs.

- **TC-S1** `--staged` with a positional path exits 1 with
  `--staged takes no paths` on stderr; nothing is analyzed.
- **TC-S2** `--staged` outside a repository exits 1 with
  `cdd: not a git repository`.
- **TC-S3** Clean index: exit 0, empty stdout, empty stderr.
- **TC-S4** Only `README.md` staged: exit 0, empty stdout (no `PASS` line).
- **TC-S5** A file within its limit staged: exit 0, the console report
  names it in the summary (`units=1`), and an unstaged over-limit file in
  the tree is not reported.
- **TC-S6** An over-limit file staged under `strict_all` with
  `block_on_ci: true`: exit 1, the report lists the violation.
- **TC-S7** The same file under `measure_only`: exit 0, the report lists the
  violation as a warning.
- **TC-S8** Configuration in `sub/cdd.config.yaml`, run with
  `--config sub/cdd.config.yaml` from the root: a staged file in `sub/` is
  reported with a path relative to `sub/`; a staged file outside `sub/` is
  dropped without error.
- **TC-S9** Run from a subdirectory of the repository with the default
  configuration path resolving there: staged files under it are found,
  because staged names are resolved against the top level, not the working
  directory.
- **TC-S10** `--staged --format json` produces the same document shape as an
  explicit-path run over the same files (compared field by field, ignoring
  `elapsed`).
- **TC-S11** A staged file whose working-tree copy was deleted after
  staging: exit 1 and an error naming the file.

## FR-4 — `internal/githook` (unit, `internal/githook/githook_test.go`)

- **TC-H1** `Install` on a missing path creates the file with `#!/bin/sh`,
  a blank line and the block, and mode `0755`.
- **TC-H2** `Install` on a file that is only `#!/bin/bash` plus a body
  inserts the block after line one with a blank line either side, and the
  body follows unchanged.
- **TC-H3** `Install` on `#!/usr/bin/env zsh` is accepted; `#!/bin/sh -e`
  is accepted.
- **TC-H4** `Install` on a file with no shebang puts the block first.
- **TC-H5** `Install` twice with the same block leaves the file byte-for-byte
  unchanged after the second call.
- **TC-H6** `Install` with a different block (a `--config` variant) over an
  existing one replaces the block only; the bytes before and after the old
  block are unchanged.
- **TC-H7** `Install` on a `0644` file leaves it `0755`; on a `0700` file
  leaves it `0700`.
- **TC-H8** `Install` on `#!/usr/bin/env python3` returns an error wrapping
  `ErrForeignHook` whose message names the path and the first line; the
  file is unchanged.
- **TC-H9** `Install` and `Remove` on a symlink return an error wrapping
  `ErrSymlink` and the target is unchanged.
- **TC-H10** `Remove` on a missing file returns `false, nil`.
- **TC-H11** `Remove` on a file without a block returns `false, nil` and the
  file is unchanged.
- **TC-H12** `Remove` on a file the command created deletes it.
- **TC-H13** `Remove` on a file with a foreign body restores the original
  bytes exactly (TC-H2's input round-trips) and keeps the mode.
- **TC-H14** `Installed` is false for a missing file and a foreign file,
  true after `Install`.
- **TC-H15** `Block("")` has no `--config`; `Block("sub/cdd.config.yaml")`
  ends its check line with `--config 'sub/cdd.config.yaml'`.
- **TC-H16** A failed write leaves no `.tmp` file behind (write into a
  read-only directory, skipped when running as root).

## FR-5 — `cdd hook git` (integration, `cmd/hook_test.go`)

- **TC-C1** `cdd hook` prints help and exits 0.
- **TC-C2** `cdd hook git` outside a repository: exit 1, stderr
  `cdd: not a git repository`.
- **TC-C3** `cdd hook git` without a configuration file: exit 1, stderr
  `cdd: cdd.config.yaml not found; run cdd init first`, no hook written.
- **TC-C4** `cdd hook git` in a fresh repository with a configuration: exit
  0, stdout `installed pre-commit hook at .git/hooks/pre-commit`, file
  exists, executable, holds the block without `--config`.
- **TC-C5** `cdd hook git` a second time: exit 0, stdout says `updated`,
  file unchanged.
- **TC-C6** `cdd hook git` with `core.hooksPath = .githooks`: the file lands
  in `.githooks/pre-commit`, the receipt names that path, and the directory
  is created when missing.
- **TC-C7** `cdd hook git` over an existing `#!/bin/sh` hook keeps its body
  and reports `installed`.
- **TC-C8** `cdd hook git` over `#!/usr/bin/env node`: exit 1, stderr names
  the file and says it was left untouched.
- **TC-C9** `cdd --config sub/cdd.config.yaml hook git` bakes
  `--config 'sub/cdd.config.yaml'`; run from `sub/` with the default path,
  the baked value is still `sub/cdd.config.yaml`, relative to the top level.
- **TC-C10** A configuration outside the repository: exit 1 with a message
  naming it.
- **TC-C11** `cdd hook git --remove` after install: exit 0, stdout
  `removed cdd hook from .git/hooks/pre-commit`, file gone.
- **TC-C12** `cdd hook git --remove` with no hook: exit 0, stdout
  `no cdd hook found at .git/hooks/pre-commit`.
- **TC-C13** `cdd hook git --remove` on a hook with a foreign body: the
  body survives, exit 0.
- **TC-C14** `cdd hook git extra` (a positional argument): exit 1.

## FR-6 — End to end (`cmd/hook_e2e_test.go`)

- **TC-E1** The scenario of task.md FR-6, steps 1–5, as one test with
  subtests in order. It skips under `-short` and when `git` is absent.
- **TC-E2** With `cdd` removed from `PATH` the commit fails and the output
  contains `cdd: not found in PATH`.

## FR-7 — Documentation

- **TC-D1** `README.md` contains `### cdd hook git`, a `--staged` row in the
  `cdd check` table and the hook line in the exit-code table. Verified by
  review, not by a test.
