# Feature 09 — Agent Hook: `cdd hook claude` and `cdd check --agent`

## Goal

Let a coding agent hear about a CDD violation the moment it writes one.
`cdd hook claude` installs a `PostToolUse` hook into the Claude Code
settings of the project, and `cdd check --agent claude` is what that hook
runs: it reads the event Claude Code sends on stdin, analyzes the file the
agent just edited, and when a unit in it is over its limit, prints the
report to stderr and exits `2`, which Claude Code shows to the agent as
something to fix.

The hook is a reviewer, not a gate. It reports every unit over its limit in
the edited file whatever `block_on_ci` and `legacy_mode` say, because a
`measure_only` project still wants the agent to fix what it just wrote.
Commits and CI keep their own rules; nothing here changes
`Enforcement.Blocks()`.

Claude Code is the first agent. The maintainer plans more (issue #30,
second comment), so the agent is a strategy behind a factory: one file per
agent in `internal/agenthook`, one line to register it, and no change in
`cmd/` or anywhere else when the next one arrives. `cdd hook <agent>` and
`cdd check --agent <agent>` are both generated from the registry.

[test-cases.md](test-cases.md) is the companion to this file: every
constraint below has a numbered test case there, and a task is done when its
cases are checked-in, passing tests.

## Current state (verified 2026-09-26, `main` at 38d1b02)

- `cmd/hook.go:18-28` registers `hook` with `git` as its only subcommand;
  the help says each subcommand targets one tool.
- `cmd/hook.go:118-138` (`bakedConfig`) turns `--config` into the value the
  hook carries: empty at the default location, else the path relative to
  the tool's root. `cmd/hook.go:163-175` (`displayPath`) words the receipt.
  Both are reused here.
- `cmd/check.go:108-156` (`runCheck`) resolves the paths, loads the
  configuration, runs the analysis with `SkipUnclaimed: in.staged` and
  emits the report where `reporter` points. `analyze.RunResult.Violations()`
  (`internal/analyze/analyze.go:169`) counts the units over their limit
  regardless of enforcement.
- `cdd check <file>` on a file no language claims is an error
  (`internal/analyze/walk.go`, the editor contract); `--staged` sets
  `SkipUnclaimed` so such a file is dropped silently. A hook fed by an
  agent needs the same, since the agent edits `README.md` as readily as
  `service.ts`.
- `internal/githook/block.go:33-36` single-quotes the baked configuration
  path for a POSIX shell. Claude Code runs a hook command through a shell
  too, so the agent hook quotes the same way.
- Claude Code (docs, "hooks" reference): `PostToolUse` hooks are listed
  under `hooks.PostToolUse` in `.claude/settings.json` (shared) or
  `.claude/settings.local.json` (personal), each entry a `matcher` and a
  `hooks` list of `{"type": "command", "command": "..."}`. The command
  receives a JSON event on stdin whose `tool_input.file_path` names the
  file `Edit` or `Write` touched. Exit `2` with a message on stderr makes
  Claude Code show that message to the agent; exit `0` is silent; any other
  exit code is a non-blocking error the user sees. Hook entries merge across
  settings files, so adding one never removes another.
- `cmd/hook_e2e_test.go` builds the binary once and drives it from a
  temp repository; `buildCdd` is reusable.

## Scope

**In:**

- `internal/agenthook` — the `Agent` strategy, the registry that is the
  factory, and `claude`, the first agent: its settings file, the entry it
  installs, replaces and removes, and the field of its event that names the
  edited file (FR-1).
- `cmd/check.go` — `--agent <id>` (FR-2).
- `cmd/hook_agent.go` — `cdd hook <id> [--remove] [--local]` for every
  registered agent (FR-3).
- One end-to-end test that builds the binary, installs the hook and runs
  the installed command through `sh -c` with a synthetic event (FR-4).
- README: a `cdd hook claude` section, the `--agent` row of the `cdd check`
  table; CHANGELOG under **Added** (FR-5).

**Out:**

- Other agents (Cursor, Codex, Copilot). The registry is where they go.
- Other Claude Code events (`PreToolUse`, `Stop`), edits made through
  `Bash`, and the user-level `~/.claude/settings.json`.
- Preserving the key order of a settings file. `encoding/json` writes map
  keys sorted; the values, the other hooks and their order are preserved.
- Any change to what a metric counts or what blocks a commit or a CI run.

## Functional requirements

### FR-1 — `internal/agenthook`: the strategy, the factory and Claude Code

`Agent` is the strategy: everything that differs from one agent to the
next, and nothing else.

```go
type Agent interface {
	ID() string                       // subcommand name and --agent value
	Summary() string                  // one line for the help
	Settings(local bool) string       // settings file, relative to the project
	Install(path, command string) (updated bool, err error)
	Remove(path string) (removed bool, err error)
	EditedFile(event []byte) (string, error)
}
```

The registry is the factory: `All() []Agent` in registration order, and
`Lookup(id string) (Agent, bool)`. A new agent is one file plus its line in
the registry; `cmd/` iterates `All()` and never names an agent.

`Command(id, configPath string) string` is the command every agent
installs: `cdd check --agent <id>`, followed by `--config '<path>'` when
`configPath` is not empty, single-quoted for a POSIX shell like the git
block. It is also the prefix by which an agent recognises its own entry
when replacing or removing it.

`claude` implements the strategy for Claude Code:

- `ID()` is `claude`; `Settings(false)` is `.claude/settings.json`,
  `Settings(true)` is `.claude/settings.local.json`.
- `Install(path, command)` reads the file as a JSON object (a missing file
  is an empty object; numbers are kept verbatim) and puts one entry into
  `hooks.PostToolUse`: `{"matcher": "Edit|Write", "hooks": [{"type":
  "command", "command": <command>}]}`. An entry whose `hooks` hold a
  command starting with `Command("claude", "")` is the cdd entry; when one
  exists it is replaced in place and `updated` is true, otherwise the entry
  is appended. Every other key of the document, every other event and
  every other entry is preserved. The file is written atomically through a
  sibling `.tmp` and a rename, mode `0644`, indented with two spaces,
  without HTML escaping, ending in a newline. The directory is created
  when missing.
- `Remove(path)` drops every cdd entry. An empty `PostToolUse` list is
  dropped, then an empty `hooks` object; a document with nothing left is
  deleted rather than written as `{}`. A missing file or one without a cdd
  entry returns `false, nil` and writes nothing.
- A file that is not a JSON object, or whose `hooks` or
  `hooks.PostToolUse` has the wrong type, is an error naming the path;
  nothing is written. A symlink is `ErrSymlink`, wrapped with the path:
  a dotfiles manager owns it.
- `EditedFile(event)` returns `tool_input.file_path`. An event that is not
  JSON, or has no such field, is an error that says so.

### FR-2 — `cdd check --agent <id>`

`--agent` names a registered agent and switches `check` into hook mode:

1. An unknown id fails with `--agent: "<id>" is not one of <ids>`, exit
   1. Given with a positional path or `--staged` the command fails with
   `--agent takes no paths` or `--agent and --staged are exclusive`, exit 1.
2. Stdin is read to the end and handed to the agent's `EditedFile`. An
   unreadable event is an error, exit 1, so a misconfigured hook shows up
   as a hook error in the agent rather than passing silently.
3. The edited path is resolved like a staged one, symlinks followed, and
   made relative to the configuration's directory. A file outside it
   belongs to another project: the command prints nothing and exits 0.
4. The configuration is loaded and the file analyzed with
   `SkipUnclaimed: true`. A file no configured language claims or the
   patterns exclude ends silently with exit 0.
5. When no unit is over its limit: nothing printed, exit 0. Otherwise the
   report is rendered to **stderr** and the command exits `2`. The format
   is `--format` when given, else `console`; `--all` and `--explain`
   compose as usual. The configured `reporter` (format and `outputFile`)
   is not consulted: the report is feedback for the agent, not a document.
6. Any analysis error, the timeout included, is an error with exit 1.

The exit code `2` is what Claude Code reads as "show stderr to the agent";
in hook mode it never means a timeout.

### FR-3 — `cdd hook <id> [--remove] [--local]`

`cdd hook` gains one subcommand per registered agent, `Use` being the id
and `Short` its summary. It takes no positional argument and two flags.

`cdd hook claude`:

1. The project is the working directory: that is where Claude Code reads
   `.claude/settings.json` and where it runs the hook command. No git
   repository is needed.
2. Resolves `--config` from the working directory and requires the file
   to exist: `cdd: <path> not found; run cdd init first`, exit 1. Makes it
   relative to the working directory through `bakedConfig`; a
   configuration outside it is an error, exit 1.
3. Calls `Install` on `<cwd>/<Settings(local)>` with
   `Command("claude", baked)`.
4. Prints one line: `installed claude hook at .claude/settings.json` on a
   new entry, `updated claude hook at .claude/settings.json` when one was
   replaced. `--local` names `.claude/settings.local.json` instead.
5. A refused file comes out as `cdd: <message>` with exit 1 and says it
   was left untouched.

`cdd hook claude --remove`:

1. No configuration check.
2. Calls `Remove`. Prints `removed cdd hook from .claude/settings.json` or
   `no cdd hook found at .claude/settings.json`. Exit 0 in both cases.

### FR-4 — End to end through the installed command

`cmd/hook_agent_e2e_test.go` builds the binary with `buildCdd`, lays out a
TypeScript project in a temp directory, runs `cdd hook claude`, reads the
installed command back out of `.claude/settings.json` and runs it the way
Claude Code does: `sh -c <command>` from the project directory with the
binary on `PATH` and a synthetic `PostToolUse` event on stdin.

1. An event naming a file within its limit: exit 0, nothing printed.
2. An event naming a file over its limit: exit 2, stderr names the unit.
3. An event naming `README.md`: exit 0, nothing printed.
4. After `cdd hook claude --remove`, `.claude/settings.json` is gone.

Skipped under `-short` and when `sh` is not on `PATH`.

### FR-5 — Documentation

- README `## Usage` command list gains `cdd hook claude`.
- A new `### cdd hook claude` section after `### cdd hook git`: what it
  installs and where, `--local`, replace and remove, what the agent sees,
  and that enforcement does not apply.
- The `cdd check` table gains an `--agent` row; the exit-code table gets
  the hook-mode line.
- `CHANGELOG.md` gets one line under **Added**, `(#30)`.

## Decisions and their reasons

- **A string `--agent <id>`, not a `--claude-hook` boolean.** The audit
  proposed the boolean; the maintainer's reply asked for a design that
  takes the next agent without touching what exists. A flag per agent is
  a change in `cmd/check.go` per agent; one flag whose value the registry
  validates is not.
- **An interface with one implementation.** The repository avoids that
  unless there is an architectural reason. The reason is on the issue:
  the next agents are planned, and the point of the pattern is that they
  cost one file each.
- **Every violation, whatever the enforcement.** Chosen in the audit and
  not amended: the hook asks the agent to fix what it just wrote, it does
  not gate anything, so `measure_only` still gets feedback.
- **Silent outside the project and on unclaimed files.** The agent edits
  whatever the task needs; a hook that errors on `README.md` trains the
  user to remove it.
- **Report on stderr, exit 2.** That is the one channel Claude Code feeds
  back to the agent without a prompt from the user.
- **Working directory as the project.** Claude Code reads the project
  settings from the directory it was started in and runs hooks there; git
  is not involved, so `git rev-parse` would be the wrong root.
- **Delete an emptied settings file.** Symmetric with `hook git --remove`:
  what the command created, the command removes.
