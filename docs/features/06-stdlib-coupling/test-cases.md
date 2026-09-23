# Feature 06 — Standard-library coupling: Test Cases

Companion to [task.md](task.md). Every case here is a constraint the
implementation must satisfy; a task is not done until the cases listed under it
are checked-in tests that pass. Case ids are stable — reference them in test
names or comments (`// TC-J4`) so a reviewer can map the suite back to this
file.

Levels follow CLAUDE.md: **unit** tests exercise one package through its
exported or package-level contract, with fixtures on disk where the analyzer
needs source; **integration** tests exercise the CLI boundary
(`cmd/check_java_test.go`, `cmd/check_kotlin_test.go`, `cmd/init_test.go`:
arguments, exit codes, stdout, generated files). No mocks: the classifiers are
pure functions and the parsers are cheap.

## Conventions

- Metric ids in tests come from `config.Metric…` constants, never string
  literals (`make check-literals`). The one place a literal is allowed is the
  vocabulary itself, a language `spec.go`, and — new in this feature — a
  language's `stdlib.go`.
- Coupling expectations are written as **internal / external / stdlib**, in
  that order, matching the `couplingCase` struct each language's
  `imports_test.go` uses.
- The fixtures are the ones already checked in:
  `internal/analyze/java/testdata/coupling.java` and `coupling_no_star.java`,
  `internal/analyze/kotlin/testdata/coupling.kt`, `coupling_no_star.kt` and
  `coupling_uses.kt`, `internal/analyze/typescript/testdata/coupling.ts`. This
  feature reclassifies their imports; it adds no fixture except where a case
  below says so.
- A fixture comment that states an expected value is the source of truth for
  the assertion next to it, so every comment a reclassification makes wrong is
  reworded in the same commit.

---

## T1 — The metric vocabulary (FR-1)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-V1 | unit | `config.Metrics()` order. | Nine ids, `stdlib_coupling` immediately after `external_coupling` and before `inheritance`. `internal/config/vocabulary_test.go`'s ordered slice is edited by hand, not regenerated. |
| TC-V2 | unit | `config.IsMetric("stdlib_coupling")`. | `true`. |
| TC-V3 | unit | `config.DefaultWeight` for every id. | `external_coupling`, `stdlib_coupling` and `local_variable` are 0.5; the other six are 1.0. The weight map in `vocabulary_test.go` is edited by hand. |
| TC-V4 | unit | `config.DefaultSelection()`. | Unchanged: the same six ids, `stdlib_coupling` **not** among them. This is the opt-in decision, pinned. |
| TC-V5 | unit | `config.MetricDescription`. | `stdlib_coupling` → "standard library types"; `external_coupling` → "framework / third-party types", with no occurrence of the word "stdlib" left in the generic map. |
| TC-V6 | unit | The doc comments on `DefaultWeight` and `DefaultSelection` name the right ids. | Reviewed, not asserted: `DefaultWeight` names the three 0.5 metrics, `DefaultSelection` names the three opt-in ones. Listed here so the reviewer has a line to tick. |
| TC-V7 | unit | `internal/config/spec_test.go`'s description expectations. | Updated by hand for the new generic wording; `TestSpecCompleteness` stays green. |
| TC-V8 | integration | `cdd init` output for a fresh project. | The generated `cdd.config.yaml` has no `stdlib_coupling` key under any language, and its vocabulary comment block lists `stdlib_coupling 0.5 all standard library types (optional, off by default)` between the external and inheritance rows. Column alignment is unchanged — `exception_handling: 1.0` is still the widest entry. |
| TC-V9 | integration | Golden regeneration. | `go test ./internal/config -update` and `go test ./cmd -run 'TestInitTypeScriptMatchesGolden\|TestInitDogfoodConfigReproducible' -update` leave a clean tree on a second run. `cmd/testdata/golden/greenfield-java-kotlin.yaml` and `docs/features/01-init/config-template.yaml` have no `-update` branch and are hand-edited to match. |
| TC-V10 | integration | `cdd init --metrics …,stdlib_coupling` and `--weight stdlib_coupling=0.75`. | Both accepted with no change to `internal/initcmd`: the flags already validate through `config.IsMetric`. The written file carries the metric at the given weight. |
| TC-V11 | integration | The repository's own `cdd.config.yaml` after `cdd init --yes --force --languages go --packages github.com/jonasalessi/cdd-lint`. | `git diff --exit-code` clean — the CI dogfood gate. |

## T2 — Classifying JVM imports into a metric (FR-2)

No behaviour changes in this task. Java and Kotlin pass a `nil` predicate, so
every existing assertion must hold untouched.

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-M1 | unit | `TestClassify` table on `jvm.Classify`. | `Classify("com.acme.shared.Money", ["com.acme"], nil)` → internal; `Classify("java.util.List", nil, nil)` → external; `Classify("java.util.List", nil, IsJDK)` → stdlib; `Classify("org.springframework.X", nil, IsJDK)` → external. |
| TC-M2 | unit | Precedence: `Classify("java.util.List", []string{"java"}, IsJDK)`. | **internal**. A project that declares `java` as its own prefix keeps owning it; the stdlib predicate never overrides an internal match. |
| TC-M3 | unit | Nil predicate. | `Classify(p, nil, nil)` is never `stdlib_coupling`, for any `p`. This is what makes T2 a no-op for Java and Kotlin. |
| TC-M4 | unit | `jvm.IsInternal` keeps its exported table test. | `TestIsInternal` passes unchanged: exact path, dot-subpath, empty prefix matching nothing, `java.util.List` with no prefixes. |
| TC-M5 | unit | `Module.Metric` is a field, not a method. | The `Metric()` method is gone — Go forbids both — and `jvm/imports_test.go`'s call sites read `m.Metric`. The former `internals()` helper becomes `metrics()` and asserts ids, not bools. |
| TC-M6 | unit | `NewImports(prefixes, stdlib)` signature. | Both JVM analyzers compile against it; `module()` calls `Classify` once per path, so a module's metric is decided when it is first seen and not recomputed per unit. |
| TC-M7 | unit | Every Java and Kotlin fixture assertion. | Byte-identical to its pre-T2 value: `coupling.java` 2/2, 1/1, 0/1; `coupling.kt` 1/2, 1/1, 0/1; `coupling_uses.kt` unchanged; all occurrence positions unchanged. |
| TC-M8 | integration | A Kotlin and a Java fixture project, `cdd check --format json` before and after T2. | Identical JSON (ignoring `elapsed`). A script step in the PR description, not a permanent test. |

## T3 — JDK imports in Java (FR-3)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-J1 | unit | `jvm.IsJDK` table, in: `java.util.List`, `java.time.Instant`, `jdk.incubator.vector.X`, `javax.crypto.Cipher`, `javax.xml.parsers.DocumentBuilder`, `javax.swing.JFrame`, `javax.annotation.processing.Processor`. | `true` for each. |
| TC-J2 | unit | `jvm.IsJDK` table, out: `javax.inject.Inject`, `javax.servlet.http.HttpServlet`, `javax.persistence.Entity`, `javax.annotation.Nullable`, `javafx.scene.Node`, `org.springframework.stereotype.Service`, `kotlin.collections.List`. | `false` for each. `javax.annotation.Nullable` and `javax.annotation.processing.Processor` together are the case that proves the list is by prefix segment, not by `javax.annotation`. |
| TC-J3 | unit | `jvm.IsJDK("javax")`, `IsJDK("")`, `IsJDK("javaxfoo.Bar")`, `IsJDK("javax.crypto")`. | `false`, `false`, `false`, **`true`**. The prefixes end in a dot, so an unrelated package that merely starts with the same letters does not match, and `javax` names no listed prefix. `javax.crypto` does: a path that equals a listed prefix without its trailing dot counts, because that is the path a star import is recorded under (`import javax.crypto.*;` has path `javax.crypto`) and it must be the JDK. |
| TC-J4 | unit | `Invoice` in `coupling.java`. | **2 / 0 / 2** — `Money` and `rate` internal; `java.time.Instant` and the `java.util.*` star both stdlib; nothing external. |
| TC-J5 | unit | `Note` and `Plain` in `coupling.java`. | **1 / 0 / 1** and **0 / 0 / 1** — the star still charges every unit, now to stdlib. |
| TC-J6 | unit | `coupling_no_star.java`. | `Invoice` **2 / 0 / 1**, `Note` **1 / 0 / 0**, `Plain` **0 / 0 / 0**. |
| TC-J7 | unit | `TestCouplingOccurrencesPointAtTheImport`, extended to all three coupling ids. | `Invoice`'s stdlib occurrence texts are exactly `["import java.time.Instant;", "import java.util.*;"]`, in source order; its internal texts are unchanged; its external list is empty. Every coupling occurrence is still at `Col` 1 on an import line above the unit. |
| TC-J8 | unit | `TestPrefixesClassifyThroughTheAnalyzer`, with `java.util.List` in the source. | `java.util.List` moves from the external column to the stdlib column in every row: `{"com.acme.shared"} → 1/0/1`, `{"com.acme"} → 1/0/1`, `{"com.acme.shared.Money"} → 1/0/1`, `{"com.acmecorp"} → 0/1/1`, `{""} → 0/1/1`, `nil → 0/1/1`. The `Money` import is external only when no prefix matches it. |
| TC-J9 | unit | `TestReferencesWithoutAnImportAreInvisible`. | Gains a third assertion, `stdlib_coupling` 0 — a fully qualified `java.time.Instant.now()` with no import charges nothing, exactly as before. |
| TC-J10 | unit | `couplingCase` / `requireCoupling` in `java/imports_test.go`. | Gain a `stdlib` field and a third `requireCount`, so every case in the file states all three numbers and none can be left implicit. |
| TC-J11 | unit | The Java spec. | `Descriptions[stdlib_coupling]` = "JDK types (java.*, javax.*, jdk.*)" and `Descriptions[external_coupling]` = "framework / third-party types"; `java/spec_test.go`'s whole-struct `TestSpec` pins both. |
| TC-J12 | unit | `isCoupling` in `java/occurrences_test.go`. | Includes `stdlib_coupling`. Without it `TestNonCouplingOccurrencesAreInsideTheUnit` fails on `coupling.java`, and the import-line invariant goes blind on `branches.java`, `exceptions.java` and `lambdas.java`, which all import `java.*`. |
| TC-J13 | unit | Cross-cutting: `TestEveryUnitCarriesEveryMetric` over every `testdata/*.java`. | Each unit's `Counts` has exactly `len(config.Metrics())` keys — nine after T1. |
| TC-J14 | integration | `cmd/check_java_test.go`'s coupling case, with `stdlib_coupling` appended to the shared `checkMetrics` selection. | `class Invoice icp=1.5 limit=10`, `internal_coupling=1` and `stdlib_coupling=1x0.5` on the metric line; no `external_coupling`. The ICP is unchanged because the weight is the same 0.5. |
| TC-J15 | integration | `cmd/testdata/golden/greenfield-java-kotlin.yaml`. | Hand-edited to the new Java external wording; `cmd/init_test.go`'s comparison is green. |

## T4 — kotlin.* and JDK imports in Kotlin (FR-4)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-K1 | unit | The Kotlin predicate, in: `kotlin.collections.List`, `kotlin.text.Regex`, `java.time.Instant`, `javax.crypto.Cipher`. | `true` — `kotlin.` plus the whole `IsJDK` rule. |
| TC-K2 | unit | The Kotlin predicate, out: `kotlinx.coroutines.flow.Flow`, `kotlinx.serialization.Serializable`, `kotlin` (bare), `javax.inject.Inject`, `com.acme.shared.Money`. | `false`. The `kotlinx` pair is the case the decision table rejects: it ships and versions separately from the language. |
| TC-K3 | unit | `Invoice` in `coupling.kt`. | **1 / 1 / 1** — `Money` internal, the `kotlinx.coroutines.*` star external, `java.time.Instant` stdlib. The only unit in the suite that carries all three. |
| TC-K4 | unit | `Note` and `Plain` in `coupling.kt`. | **1 / 1 / 0** and **0 / 1 / 0** — the alias `L` is internal, the star stays external. |
| TC-K5 | unit | `coupling_no_star.kt`. | `Invoice` **1 / 0 / 1**, `Note` **1 / 0 / 0**, `Plain` **0 / 0 / 0**. |
| TC-K6 | unit | `TestCouplingOccurrencesPointAtTheImport`. | `Invoice`'s first three occurrences are internal on line 3, **stdlib** on line 5 and external on line 6, in that order, each at `Col` 1 with its existing end position. |
| TC-K7 | unit | `TestCouplingUses` over `coupling_uses.kt`. | `Annotated` stays **0 / 1 / 0**: `javax.inject.Inject` is not the JDK. This is the javax allow-list proved end to end through a parser rather than against the table alone. Every other unit in the file gains a stdlib 0. |
| TC-K8 | unit | The Kotlin spec. | `Descriptions[stdlib_coupling]` = "kotlin.* and JDK types"; `kotlin/spec_test.go`'s whole-struct `TestSpec` pins the map, which until now carried no coupling row at all. |
| TC-K9 | unit | Fixture comments. | `coupling.kt`'s per-unit comments and `coupling_no_star.kt`'s are reworded to the new triples in the same commit, since a stale comment next to a changed number is what a reader trusts first. |
| TC-K10 | integration | `cmd/check_kotlin_test.go`'s coupling case. | `internal_coupling=1` and `stdlib_coupling=1x0.5`, `icp=1.5` unchanged, no `external_coupling`. |

## T5 — Node.js built-ins in TypeScript (FR-5)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-T1 | unit | `isBuiltin`, in: `fs`, `node:fs`, `fs/promises`, `node:fs/promises`, `path`, `path/posix`, `stream/web`, `timers/promises`, `node:test`, `node:sqlite`, `console`. | `true`. `node:test` and `node:sqlite` have no bare form and pass on the `node:` prefix alone. |
| TC-T2 | unit | `isBuiltin`, out: `node-fetch`, `fsx`, `@types/node`, `lodash/fp`, `react`, `reflect-metadata`, `./repo`, `` (empty). | `false`. `node-fetch` and `fsx` are the near-misses the first-segment rule must reject. |
| TC-T3 | unit | `isBuiltin("node:")` and `isBuiltin("nodefs")`. | `false` and `false` — an empty specifier after the scheme names nothing. |
| TC-T4 | unit | `classify(spec, prefixes)` precedence. | Relative or internal-prefix first, then `isBuiltin`, then external: `classify("./repo", …)` internal, `classify("@app/services", ["@app/"])` internal, `classify("node:fs", ["node:"])` **internal** (the configured prefix wins, mirroring TC-M2), `classify("node:fs", nil)` stdlib, `classify("lodash/fp", nil)` external. |
| TC-T5 | unit | `isInternal` and `isRelative` keep `TestIsInternal`. | Passes unchanged — extracting `classify` does not change what "internal" means. |
| TC-T6 | unit | `usesExternal` in `coupling.ts`. | **1 / 2 / 1** — `node:fs/promises` stdlib; `lodash/fp` and `reflect-metadata` external; `./polyfill` internal. |
| TC-T7 | unit | `usesRequire` in `coupling.ts`. | **2 / 1 / 1** — `import nodePath = require("node:path")` is a built-in through the `require` form too, not only through `import … from`. |
| TC-T8 | unit | The other five units of `coupling.ts`. | `UsesInternal` 2 / 1 / 0, `usesBoth` 2 / 1 / 0, `Wrapper` 3 / 1 / 0, `renderer` 1 / 3 / 0, `Untouched` 1 / 1 / 0 — internal and external unchanged, stdlib 0. |
| TC-T9 | unit | Occurrence pinning in `typescript/occurrences_test.go`. | The line-11 case (`import { readFile } from "node:fs/promises";`) asserts `stdlib_coupling`; a new case asserts `external_coupling` on line 12 (`import * as lodash from "lodash/fp";`), so the suite keeps one pinned occurrence per coupling metric. |
| TC-T10 | unit | `countCoupling` shape. | One `chargeSpan(m.metric, m.at, 1)`; no branch on the metric, so a fourth coupling id would need no edit here. `Counts` still equals the sum of `Occurrences` (`TestOccurrencesAccountForEveryCount`). |
| TC-T11 | unit | A side-effect import of a built-in: `import "node:crypto";`. | Charged to **every** unit of the file under `stdlib_coupling` — a built-in follows the side-effect rule like any other module. |
| TC-T12 | unit | The TypeScript spec. | `Descriptions[stdlib_coupling]` = "Node.js built-in modules (node:fs, path)"; `typescript/spec_test.go` gains a `Descriptions` assertion, which it has never had. |
| TC-T13 | unit | `coupling.ts` fixture comments. | The header list (`:1-6`) and the per-unit comments for `usesExternal` (`:31-32`) and `usesRequire` (`:63-64`) name a stdlib group and are reworded in the same commit. |
| TC-T14 | lint | `make check-literals` with the built-in list checked in. | Green. `literalExempt` matches `internal/analyze/*/stdlib.go`, which is required because `"console"` is both a Node built-in and a `config.Format…` reporter id. |
| TC-T15 | unit | `TestLiteralsHelpers`. | Gains `require.True(t, literalExempt("internal/analyze/typescript/stdlib.go"))` and keeps `require.False` for a non-exempt file in the same directory, so the new pattern is pinned as narrowly as the `spec.go` one. |
| TC-T16 | unit | `jvm/stdlib.go` and the literal check. | The JVM list lives at `internal/analyze/internal/jvm/stdlib.go`, which the `internal/analyze/*/stdlib.go` pattern does **not** match; assert either that it needs no exemption (it spells no metric id) or that the pattern covers it, whichever the implementation chooses — and pin the choice. |

## T6 — Documentation (FR-6)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-D1 | review | README metric table. | A `stdlib_coupling` row directly after `external_coupling`, weight 0.5, languages "all", counts "Standard library types. Off by default"; the `external_coupling` row no longer says "platform". |
| TC-D2 | review | README default-selection sentence. | Says the first six are ticked by default and that `stdlib_coupling`, `local_variable` and `lambda` are off — three names where there were two. |
| TC-D3 | review | README classification sentence and language paragraphs. | "internal, standard-library or external" where the three-way split is explained; the Kotlin paragraph says `kotlin.*` and the JDK are standard library and `kotlinx.*` is not; the Java paragraph says `java.*`, `jdk.*` and the JDK `javax.*` packages are, and `javax.servlet` / `javax.inject` / `javafx` are not; a TypeScript sentence names Node built-ins and states the Deno, Bun and browser-global gaps. |
| TC-D4 | review | README upgrade note. | States the consequence in [task.md](task.md): a configuration that omits `stdlib_coupling` stops counting standard-library imports, so ICPs fall after upgrading, and how to get the old totals back. |
| TC-D5 | review | CONTRIBUTING "Writing an analyzer". | One paragraph: a language's standard-library predicate lives in its own `stdlib.go`, JVM platform knowledge in `internal/analyze/internal/jvm`, and `stdlib.go` is exempt from the literal check for the same reason `spec.go` is. |
| TC-D6 | review | `docs/cdd.md`. | Unchanged. Section 2 is the methodology; the split is a tool capability, and saying so in the README is the whole of the documentation debt. |

## Cross-cutting invariants (every language, every fixture)

| ID | Level | Case | Expectation |
|---|---|---|---|
| TC-X1 | unit | `TestEveryUnitCarriesEveryMetric` in Java, Kotlin and TypeScript. | Each unit's `Counts` has exactly `len(config.Metrics())` keys, for every fixture in each package. |
| TC-X2 | unit | `TestOccurrencesAccountForEveryCount`. | For every unit, every metric: the sum of `Occurrences[i].Count` equals `Counts[metric]`, `stdlib_coupling` included. |
| TC-X3 | unit | `TestOccurrencesAreWellFormed` and `TestOccurrencesAreSorted`. | Unchanged and still green: the three coupling metrics are charged in one pass over the modules in source order. |
| TC-X4 | unit | Every `isCoupling`-style helper in every suite. | Lists all three coupling ids. A grep for `MetricExternalCoupling` in test helpers finds no site that names two of the three. |
| TC-X5 | unit | `TestNonCouplingOccurrencesAreInsideTheUnit` and the import-line invariant. | Pass in all three languages, with the stdlib occurrences treated as coupling. |
| TC-X6 | unit | Determinism. | Analyzing each coupling fixture twice with two analyzers yields `reflect.DeepEqual` results; `-race` is on in `make test`. |
| TC-X7 | unit | `internal/report` goldens. | None moves: all 16 configure three metrics, and the reporter orders by `config.Metrics()` and skips absent ids. |
| TC-X8 | integration | A configuration that omits `stdlib_coupling` entirely. | Standard-library imports charge nothing at all — the upgrade consequence, asserted rather than only described: the same `coupling.java` that scores `stdlib_coupling=2x0.5` with the metric selected scores no coupling beyond `internal_coupling=2` without it. |

## Coverage targets

- `internal/analyze/internal/jvm/stdlib.go` and
  `internal/analyze/typescript/stdlib.go`: every branch of the predicate
  covered by TC-J1 … TC-J3 and TC-T1 … TC-T3; these are pure functions with no
  excuse for a gap.
- `internal/config`, `internal/analyze/internal/jvm`, `internal/analyze/java`,
  `internal/analyze/kotlin` and `internal/analyze/typescript`: coverage must
  not fall below its pre-T1 value. Run `make cover` and paste the per-package
  table in the PR description.
