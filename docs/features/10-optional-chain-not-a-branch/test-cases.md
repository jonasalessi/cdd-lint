# Feature 10 — `?.` is not a branch: Test Cases

Companion to [task.md](task.md). Every case is a checked-in test that
passes. **Unit** tests run the analyzer in-process on a probe or a fixture.

## FR-1 — TypeScript (unit, `internal/analyze/typescript/*_test.go`)

| Id | Kind | Input | Expected |
| --- | --- | --- | --- |
| TC-O1 | unit | `TestOptionalChains`: `a?.b`, `a?.[0]`, `a?.()`, `a.b?.()`, `a?.b?.(1)?.[2]` | `code_branch` 0 for each. |
| TC-O2 | unit | The `branches` and `optionalCalls` units of `branches.ts` | `code_branch` 11 and 0. |
| TC-O3 | unit | The function `x` of issue #32 | `internal_coupling` 1, `condition` 2, `code_branch` 0. |
| TC-O4 | unit | `TestSpec…`: the `code_branch` description | `if/else, switch, ternary, loops`. |

## FR-2 — Kotlin (unit, `internal/analyze/kotlin/*_test.go`)

| Id | Kind | Input | Expected |
| --- | --- | --- | --- |
| TC-O5 | unit | `safe`: `s?.trim()?.length ?: 0` | `code_branch` 0, `condition` 2. Replaces feature 04 TC-B7. |
| TC-O6 | unit | `deep`: `a?.b?.c?.d`, `let`: `x?.let { }` | `code_branch` 0 for each. Replaces feature 04 TC-B8 and TC-B9. |
| TC-O7 | unit | The occurrences of `v?.trim()?.length ?: 0` | The two `condition` clauses of `?:` and nothing else. |
| TC-O8 | unit | `TestSpec`: the `code_branch` description | `if/when, loops`. |

## FR-3 — Shared docs (integration, `cmd/*_test.go`)

| Id | Kind | Input | Expected |
| --- | --- | --- | --- |
| TC-O9 | integration | The `init` golden files for TypeScript and Java + Kotlin | No `?.` under `code_branch`; the regenerated diff changes only the description lines. |
