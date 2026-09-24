---
name: dependency-bump
description: Land Dependabot pull requests for cdd-lint as one consolidated update. Verifies each bump comes from the public module proxy, updates the modules together with go get and go mod tidy, runs make check once, and opens one PR that closes every bot PR. Use only for bot-authored PRs that touch go.mod, go.sum or workflow action pins; anything else goes to pr-audit.
---

# Dependency bump

Several bot PRs are one unit of work. Consolidate them, verify the whole set
with one gate, and land one change.

## Step 1: inventory

```sh
gh pr list --state open --author 'app/dependabot' --json number,title,headRefName,files,url
```

A PR qualifies when its files are only `go.mod`, `go.sum` or
`.github/workflows/*.yml`, and the title is a plain bump. Anything else is
handed to `pr-audit`. Two bumps need a closer look before they qualify:

- `github.com/tree-sitter/go-tree-sitter` and the grammar modules under
  `internal/analyze/*`: AGENTS.md pins the binding, so the bump only lands
  when every analyzer's tests pass on it. `make test` covers that; say so in
  the report.
- A major version, or a module whose path changed: hand it to `pr-audit`.

## Step 2: supply-chain floor

For each bumped module confirm the version exists on the public proxy with
the checksum Go will verify:

```sh
GOPROXY=https://proxy.golang.org go list -m -json "<module>@<version>" | head -20
```

A module that resolves only from a git source, a renamed path, a name one
character away from a known one, or a version the proxy does not know stops
the batch and goes to `pr-audit`.

## Step 3: consolidate

```sh
git switch -c build/bump-deps main
go get <module>@latest ...   # every qualifying module, together
go mod tidy
git diff -- go.mod go.sum
```

The target is the newest version the existing constraints allow, which may
be newer than the bot proposed; note that in the report. Do not change the
`go` directive or add a `replace`.

For an actions bump, edit the pin in the workflow file to the version the
bot proposed and nothing else.

## Step 4: gate once

```sh
make check
```

If it fails, find whether the cause is the bump, the environment or a
pre-existing fragility, make the smallest fix as its own commit on the same
branch, and rerun. Never widen the batch back into per-PR merges to bisect;
bisect in a scratch worktree and express the result as a pin.

## Step 5: land

```sh
git add go.mod go.sum   # or the workflow file
git commit -m "build: bump <module> to <version> and <module> to <version>"
git push -u origin build/bump-deps
gh pr create --base main --title "build: bump dependencies" --body "Closes #A
Closes #B"
gh pr checks --watch
gh pr merge --rebase --delete-branch
gh pr list --state open --author 'app/dependabot'
```

Use `ci:` as the type when only workflow pins changed. Every bot PR the
merge did not close gets a comment with the reason and is closed by hand.

## Output

```markdown
## Dependency bump

- Closed: #A, #B
- Versions: <module> a -> b (newer than the bot's c), ...
- make check: <result>; ci: <conclusion> on <SHA>
- Handed to pr-audit: #C (<reason>)
```
