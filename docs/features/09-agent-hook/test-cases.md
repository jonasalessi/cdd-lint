# Feature 09 — Agent Hook: Test Cases

Companion to [task.md](task.md). Every case here is a constraint the
implementation must satisfy; a task is not done until the cases listed under
it are checked-in tests that pass. Case ids are stable — reference them in
test names or comments (`// TC-K3`) so a reviewer can map the suite back to
this file.

**Unit** tests exercise `internal/agenthook` through its exported contract
with real files in `t.TempDir()`. **Integration** tests exercise the CLI
boundary in-process through `runCdd`, feeding the event through the
command's stdin. The one **end-to-end** test builds and executes the binary
through `sh -c`, the way Claude Code runs a hook command. Nothing is mocked.

## Conventions

- `event(path)` is the smallest `PostToolUse` document Claude Code sends:
  `{"tool_name":"Edit","tool_input":{"file_path":"<path>"}}` with an
  absolute path.
- `settings(t, dir)` reads `.claude/settings.json` back as a generic JSON
  document, so a test can assert on foreign keys and on the cdd entry.
- Paths in events are made absolute and symlink-evaluated where they are
  compared, for the same `/var` and `/private/var` reason as feature 08.

## FR-1 — `internal/agenthook` (unit, `internal/agenthook/*_test.go`)

- **TC-R1** `All()` lists the Claude Code agent and `Lookup("claude")`
  returns it; `Lookup("nope")` reports false.
- **TC-R2** `Command("claude", "")` is `cdd check --agent claude`;
  `Command("claude", "sub/cdd.config.yaml")` ends with
  `--config 'sub/cdd.config.yaml'`; a path holding a single quote is
  quoted the POSIX way.
- **TC-K1** `Install` on a missing path creates the directory and the
  file, mode `0644`, holding exactly one `PostToolUse` entry with matcher
  `Edit|Write` and one command hook running the command; `updated` is
  false. The file ends with a newline and `|` is not escaped.
- **TC-K2** `Install` on a file with other keys (`permissions`, a `Stop`
  hook, another `PostToolUse` entry and a numeric value) keeps all of them,
  appends the cdd entry after the existing `PostToolUse` entry and leaves
  the number verbatim.
- **TC-K3** `Install` twice with the same command leaves the file
  byte-for-byte unchanged after the second call and reports `updated`.
- **TC-K4** `Install` with a different command (a `--config` variant) over
  an existing entry replaces it at its position, and a foreign entry after
  it stays after it.
- **TC-K5** `Install` on a file that is not a JSON object (an array, or
  `not json`) returns an error naming the path; the file is unchanged.
- **TC-K6** `Install` on a file whose `hooks.PostToolUse` is not a list
  returns an error naming the path; the file is unchanged.
- **TC-K7** `Install` and `Remove` on a symlink return an error wrapping
  `ErrSymlink`; the target is unchanged.
- **TC-K8** `Remove` on a missing file returns `false, nil`; on a file
  without a cdd entry returns `false, nil` and the file is unchanged.
- **TC-K9** `Remove` on a file the command created deletes it.
- **TC-K10** `Remove` on TC-K2's file restores the foreign content: the
  other `PostToolUse` entry, the `Stop` hook, `permissions` and the number
  are still there and the cdd entry is gone.
- **TC-K11** `Remove` when the cdd entry was the only `PostToolUse` entry
  but a `Stop` hook exists drops `PostToolUse` and keeps `hooks.Stop`.
- **TC-K12** `EditedFile` returns `tool_input.file_path` from a full
  Claude Code event; an event without the field and a non-JSON event
  return errors that say so.
- **TC-K13** `Settings(false)` is `.claude/settings.json`,
  `Settings(true)` is `.claude/settings.local.json`; `ID()` is `claude`.
- **TC-K14** A failed write leaves no `.tmp` behind (read-only directory,
  skipped as root).

## FR-2 — `cdd check --agent` (integration, `cmd/check_agent_test.go`)

- **TC-A1** `--agent nope`: exit 1, stderr `--agent: "nope" is not one of
  claude`, nothing analyzed.
- **TC-A2** `--agent claude src/x.ts`: exit 1, `--agent takes no paths`.
  `--agent claude --staged`: exit 1, `--agent and --staged are exclusive`.
- **TC-A3** An event naming a file within its limit: exit 0, empty stdout
  and stderr.
- **TC-A4** An event naming a file over its limit: exit 2, empty stdout,
  stderr holds the console report with the `violation:` line naming the
  unit and its path relative to the configuration's directory.
- **TC-A5** The same under `measure_only` with `block_on_ci: false`: still
  exit 2 and the violation on stderr.
- **TC-A6** An event naming `README.md`: exit 0, nothing printed.
- **TC-A7** An event naming a file outside the configuration's directory
  (configuration in `sub/`, edit at the root): exit 0, nothing printed.
- **TC-A8** An event that is not JSON, and one without
  `tool_input.file_path`: exit 1, stderr says so, stdout empty.
- **TC-A9** `--agent claude --format json` puts the json document on
  stderr; `--explain` adds occurrence lines to the console report.
- **TC-A10** A configured `reporter.outputFile` is not written in hook
  mode.
- **TC-A11** An event naming a file that does not exist: exit 1 with an
  error naming it.

## FR-3 — `cdd hook claude` (integration, `cmd/hook_agent_test.go`)

- **TC-C1** `cdd hook` help lists `claude` next to `git`.
- **TC-C2** `cdd hook claude` without a configuration: exit 1, stderr
  `cdd: cdd.config.yaml not found; run cdd init first`, no file written.
- **TC-C3** `cdd hook claude` with a configuration and no git repository:
  exit 0, stdout `installed claude hook at .claude/settings.json`, the
  file holds the entry with command `cdd check --agent claude`.
- **TC-C4** A second run: exit 0, stdout says `updated`, file unchanged.
- **TC-C5** `--local`: the file is `.claude/settings.local.json` and the
  receipt names it.
- **TC-C6** `cdd --config sub/cdd.config.yaml hook claude`: the command
  carries `--config 'sub/cdd.config.yaml'`.
- **TC-C7** A configuration outside the working directory: exit 1 with a
  message naming it.
- **TC-C8** Over an existing settings file with a `permissions` key: the
  key survives; over a file that is not JSON: exit 1, stderr names the
  file and says it was left untouched.
- **TC-C9** `--remove` after install: exit 0, stdout
  `removed cdd hook from .claude/settings.json`, file gone.
  `--remove` with nothing installed: exit 0, stdout
  `no cdd hook found at .claude/settings.json`.
- **TC-C10** `cdd hook claude extra`: exit 1.

## FR-4 — End to end (`cmd/hook_agent_e2e_test.go`)

- **TC-E1** The scenario of task.md FR-4, steps 1–4, as one test with
  subtests in order. It skips under `-short` and when `sh` is absent.

## FR-5 — Documentation

- **TC-D1** `README.md` contains `### cdd hook claude`, an `--agent` row
  in the `cdd check` table and the hook-mode line in the exit-code table;
  `CHANGELOG.md` has the `(#30)` line under Unreleased / Added. Verified
  by review, not by a test.
