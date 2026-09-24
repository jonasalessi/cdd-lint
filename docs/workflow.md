# From issue to release

A change to `cdd` starts as a GitHub issue and ends as a tagged release,
with an agent doing the work in between and a fixed set of gates deciding
what lands. This page is the map; each step is a skill under
`.claude/skills/` that a Claude Code session runs, in a terminal or
headless through `scripts/pipeline.sh`.

![The pipeline: audit, resolve, post-audit, release, and where the maintainer decides](workflow.svg)

## The steps

| Step | Skill | What it produces |
| --- | --- | --- |
| 1 | `pr-audit` | a verdict per open pull request, from evidence in the diff and `make check` in an isolated worktree |
| 1 | `dependency-bump` | one consolidated PR that closes every Dependabot PR |
| 2 | `issue-audit` | a verdict per issue: reproduced or not, root cause, fix plan, changelog category |
| 3 | `resolve` | one branch, one PR and one merge per approved ticket, test first |
| 4 | `post-audit` | an audit of everything on `main` since the last tag, required after more than three tickets and before every release |
| 5 | `release` | the changelog finalised, `main` tagged, the archives and the GitHub release built by `release.yml` |
| all | `pipeline` | the five steps in order, with the approval policy that stands in for a maintainer |
| on demand | `security-audit` | a threat-model review of the surfaces a code-analysis CLI has |

## Running it

Interactively, in a Claude Code session opened in the repository:

```
/pipeline                 audit and resolve every open PR and issue
/pipeline 12              only issue 12
/pipeline 12 release      issue 12, then cut a release
```

Headless, from a shell or a cron job:

```sh
scripts/pipeline.sh 12 release
```

The script runs `claude -p` with the same arguments. It needs `gh` logged
in with the `repo` and `workflow` scopes, `make setup` run once so the
commit hooks are installed, and a C compiler for the Tree-sitter analyzers.

## What the maintainer decides

The approval policy in the `pipeline` skill approves what an audit calls
`Fix now` or `Documentation only`, and merges a PR with no finding above a
nit. Bugs and docs flow without the maintainer.

New functionality does not. When the audit answers a feature request with
`Fix with spec`, the pipeline posts the plan as a comment on the issue
(the change, where it goes, tests, docs, size, what is left out) and labels
it `needs-decision`. The maintainer answers with a label: `approved` lets
the next run build the plan as written, `wontfix` ends it, and a reply
that amends the plan is followed over the comment. An issue filed with
`approved` already on it skips the wait; an issue the maintainer labels
`needs-decision` by hand is held back even when it is a plain bug.

Everything else stays open with a comment that states the evidence: a
question for the reporter, a duplicate, a decline, or a blocking finding.
The maintainer reads the run report's "left for the maintainer" list and
the comments, and closes or reopens what the agent would not.

Untrusted text never changes a verdict. Issue bodies, PR descriptions,
commit messages and code comments are evidence; an instruction found there
is quoted as a claim and ignored.

## The ledger

`CHANGELOG.md` is the single record a release reads. Every merged change
adds one line under `Unreleased`, in the category that names its release
impact:

| Category | Next version |
| --- | --- |
| Fixed only | patch |
| Added, Changed or Removed | minor |
| an entry marked **Breaking** | major, or minor while the major is 0 |

The release moves the section under the new version with the date, commits
`build: release vX.Y.Z` on `main`, and pushes an annotated tag once CI is
green on that exact commit.

## The release workflow

`.github/workflows/release.yml` runs on a pushed `v*` tag. It builds
`cdd` natively on Linux (amd64, arm64), macOS (amd64, arm64) and Windows
(amd64), because the Tree-sitter grammars are compiled through cgo, then
packages each build with the licence, README and changelog, writes a
SHA-256 checksum file, creates the GitHub release with the changelog
section as its notes, and installs the tagged module through the Go proxy
as a smoke test. `workflow_dispatch` runs the build matrix alone, to try a
change to the workflow without publishing.

## Gates that never move

- `make check` is the definition of done for every commit, and hosted CI
  must be green on the exact SHA before a merge and before a tag.
- A PR lands with a merge commit, so every commit in it reaches `main` as
  `<type>: #<issue> <description>` with no trailer; the `commit-msg` hook
  rejects the rest.
- Nothing is tagged on red, pending or skipped CI, with a blocking finding
  open, or without an explicit ask to release.
- A pushed tag is never moved or deleted; a bad release gets a patch.
