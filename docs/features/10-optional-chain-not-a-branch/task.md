# Feature 10 — `?.` is not a branch

## Goal

Stop charging the optional chain of TypeScript and the safe call of Kotlin
(`?.`) to `code_branch`. `docs/cdd.md` lists the branches CDD counts:
`if`/`else`, `switch`, ternaries and loops. A `?.` is none of them. It
guards one access against `null` and does not add a path the reader has to
follow, so counting it inflates the score of idiomatic null-safe code.

The maintainer decided this on issue #32. There, the function `x` scores 4,
and one of those points is `errorData?.message`. After this change it
scores 3: 1 `internal_coupling` and 2 `condition` for the two clauses of
`||`.

[test-cases.md](test-cases.md) is the companion to this file: every
constraint below has a numbered test case there.

## Current state (verified 2026-10-26, `main` at 1b59eb0)

- `internal/analyze/typescript/branches.go:33-41` charges `optional_chain`
  (member and subscript access) to `code_branch`, and `countOptionalCall`
  (`branches.go:66-79`) charges the anonymous `?.` token of an optional
  call. `parser.go` resolves `kindOptionalChain` and `optionalCallToken`
  only for these two rules.
- `internal/analyze/kotlin/branches.go:44-45` sends every
  `navigation_expression` to `countSafeCall` (`branches.go:117-126`), which
  charges the anonymous `?.` token. `parser.go` resolves
  `kindNavigationExpression` and `tokenSafeCall` only for this rule.
- Feature 03 FR-10 ("`?.` remains a `code_branch`") and feature 04 TC-B7 to
  TC-B9 pin the old rule. This feature replaces them.
- The `code_branch` descriptions say `?.`: the TypeScript spec ("if/else,
  switch, ternary, loops and ?."), the Kotlin spec ("if/when, loops, safe
  calls (?.)"), the config template vocabulary ("(?. kotlin/ts)"), and the
  README metric table.
- The `condition` descriptions in the README and the config template say
  the clauses count "inside a branch" / "in branch expressions". All four
  analyzers count them in any expression, so the wording is corrected here
  too.

## Scope

In: the two analyzers, their spec descriptions, the config template, the
README metric table, `docs/languages.md`, and the regenerated golden files.

Out: the `?:` elvis of Kotlin and the `??` of TypeScript, which stay
`condition` clauses. The non-null assertion `!!` already counts nothing.

## Requirements

| Id | Requirement | Where |
| --- | --- | --- |
| FR-1 | TypeScript charges nothing for `?.`, whether it reads a member (`a?.b`), indexes (`a?.[0]`) or calls (`a?.()`). The rules and the grammar lookups that only served them are removed. The `code_branch` description becomes `if/else, switch, ternary, loops`. | `internal/analyze/typescript` |
| FR-2 | Kotlin charges nothing for `?.`. The rule and the grammar lookups that only served it are removed. The `code_branch` description becomes `if/when, loops`. | `internal/analyze/kotlin` |
| FR-3 | The README metric table, the config template vocabulary and `docs/languages.md` no longer list `?.` under `code_branch`, and say that `condition` counts clauses in any expression. The golden files that render the descriptions are regenerated. `CHANGELOG.md` gets one `Changed` line. | `README.md`, `internal/config/templates/cdd.config.yaml.tmpl`, `docs/languages.md`, `cmd/testdata/golden`, `CHANGELOG.md` |
