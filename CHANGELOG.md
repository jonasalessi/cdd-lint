# Changelog

Every user-visible change to `cdd` is recorded here, in the format of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), under versions that
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The `Unreleased` section is the ledger a release reads: entries under Fixed
alone call for a patch, an entry under Added, Changed or Removed calls for a
minor, and an entry marked **Breaking** calls for a major (a minor while the
major is still 0). Each entry ends with the issue or PR it came from.

## [Unreleased]

### Added

- `cdd hook claude` installs a Claude Code `PostToolUse` hook into the
  project's `.claude/settings.json` (`--local` for the personal file,
  `--remove` to take it out), and `cdd check --agent claude` is what it
  runs: it reads the event on stdin, analyzes the edited file and, when a
  unit is over its limit, reports it on stderr with exit `2` so the agent
  fixes it. Agents are strategies behind a registry in
  `internal/agenthook`, one file each. (#30)

### Fixed

- `cdd check --staged` no longer fails with `symlinked directory is not
  supported` when a symlink to a directory is staged; the link is skipped
  like any other file no language claims. (#29)

## [0.1.0] - 2026-09-24

### Added

- `cdd init` writes `cdd.config.yaml` for a project, interactively or with
  `--yes`, `--languages` and `--packages`, and detects the languages present.
- `cdd check` scores every code unit in Intrinsic Complexity Points and lists
  the units above their limit, with `--all`, `--explain`, comma-separated
  paths, and the `console`, `json`, `xml` and `markdown` formats.
- `cdd check --staged` analyzes the files staged in git, and `cdd hook git`
  installs or removes the pre-commit hook that runs it.
- Analyzers for Go, Java, Kotlin and TypeScript, with per-language metric
  weights, limits per path pattern, and standard-library coupling rules.
- Enforcement modes for greenfield and legacy projects, `block_on_ci`, and a
  run timeout that reports a partial result instead of hanging.
- `cdd version` prints the version, commit and build date.

### Fixed

- `reporter.outputFile` is written only inside the project: an absolute path,
  a path that leaves the configuration's directory through `..`, or a symlink
  on the way are refused, so a repository under analysis cannot make `cdd
  check` write outside itself.
- `cdd check` names a path that does not exist the way its other path errors
  do, instead of printing the raw `lstat` error. (#12)

[Unreleased]: https://github.com/jonasalessi/cdd-lint/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/jonasalessi/cdd-lint/releases/tag/v0.1.0
