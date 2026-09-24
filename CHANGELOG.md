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

- `cdd check` names a path that does not exist the way its other path errors
  do, instead of printing the raw `lstat` error. (#12)

[Unreleased]: https://github.com/jonasalessi/cdd-lint/commits/main
