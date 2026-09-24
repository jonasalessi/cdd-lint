---
name: post-audit
description: Audit the combined state of main after a batch of merges and before a release of cdd-lint. Checks provenance of every commit in the range, cross-change interactions, the docs and CHANGELOG ledger, dependency advisories, and that make check and hosted CI are green on the exact SHA. Use after resolving more than three tickets, or as the gate before release.
---

# Post-audit

Audit the final tree, not the sum of optimistic PR summaries. This is the
last gate before a release. It does not release.

## Phase 0: freeze the range

```sh
git switch main && git pull --ff-only
git status --short --branch
BASE_SHA=$(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)
HEAD_SHA=$(git rev-parse HEAD)
RANGE="$BASE_SHA..$HEAD_SHA"
```

For an incremental audit after a batch, `BASE_SHA` is the last SHA a clean
post-audit recorded. If `HEAD` moves during the audit, inspect the new
commits and rerun the affected phases.

## Phase 1: provenance

```sh
git log --format='%h %an %s' "$RANGE"
git diff --stat "$RANGE"
git diff --name-status "$RANGE"
git diff --check "$RANGE"
```

Merges are rebases, so every commit in the range is one change. Map each
commit to its PR and issue (`gh pr list --state merged --search "<sha>"`),
and check that no commit escaped an audit, every referenced issue is closed,
and every commit message reads `<type>: <description>`.

## Phase 2: composition sweep

Read the merged code around the hunks, not the hunks alone:

1. Two changes that each keep the editor contract may break it together:
   run `bin/cdd check --all --explain --format json` on `testdata/` and
   compare the fields with `docs/editor-integration.md`.
2. A rule that lives in two analyzers now disagrees: compare the same
   construct across `internal/analyze/*` when the range touched more than
   one language.
3. Config composition: a new field plus a changed default may change what
   an existing `cdd.config.yaml` means; `cdd init --yes --force` on this
   repo must leave `cdd.config.yaml` unchanged (CI checks that).
4. Helper drift: duplicated normalisation, path handling or error wrapping
   in `cmd/` and `internal/`.
5. Test masking: a helper or fixture from one change lets another change's
   test pass without exercising production code.

## Phase 3: security sweep

Inspect the range for executable bits, symlinks, binaries, encoded blobs,
new dependencies, `replace` directives, workflow permission changes and
process execution outside `internal/git`. Then:

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Run `security-audit` over the range when it touched `internal/git`,
`internal/githook`, config loading, path resolution or a workflow. Any
credible malicious behaviour blocks the release.

## Phase 4: docs and release ledger

Build the list of user-visible surfaces from the diff, not from PR prose:
commands, flags, config fields, formats, languages, exit codes, the hook.
For each one confirm the README, the config template comments,
`docs/languages.md` and `docs/editor-integration.md` still describe the
current behaviour, and that `CHANGELOG.md` has its line under
`## [Unreleased]` in the right category. An entry that landed under an
already released heading is a finding.

Recommend the version from the ledger: Fixed only means patch; Added or
Changed means minor; **Breaking** means major (minor while the major is 0).
Name the single entry that forces the recommendation.

## Phase 5: gates on the exact SHA

```sh
make check
gh run list --branch main --commit "$HEAD_SHA" --json name,conclusion,url
```

Both must be green on `HEAD_SHA` itself. A green run on a PR head does not
count for the merged tree. Never call a pending, skipped or cancelled job
green.

## Phase 6: fix loop

Order findings by security and data loss, then correctness, then docs and
tests. Fix each through `resolve` as its own ticket and PR, then rerun this
audit over the extended range. Nothing is tagged while a blocking finding
is open.

## Output

```markdown
## Post-audit: <BASE_SHA>..<HEAD_SHA>

Commits audited: <n> (<#PR -> #issue> ...)
Gates: make check <result>; ci <conclusion> <url>

### Findings
- [SEVERITY] path:line - impact and required fix

### Security
- <sweep result, govulncheck result>

### Docs and changelog ledger
- <complete | stale: file and surface>
- Version recommendation: patch | minor | major, forced by <entry>

Release readiness: ready | blocked by <findings>
```
