# Feature 06 — Standard-library coupling as its own metric

## Goal

Split the standard library out of `external_coupling` and give it its own
metric, `stdlib_coupling`, weighted per language and per file pattern like
every other metric. After this feature a Java team can read a report where
`import java.util.List` and `import org.springframework.stereotype.Service`
are two different numbers, and can turn the first off without turning the
second off too.

Today every import that is not internal charges `external_coupling`. The JVM
classifier says so in its own doc comment — "everything else, `java.*` and
`kotlinx.*` included, is external"
(`internal/analyze/internal/jvm/imports.go:59-66`) — and TypeScript says the
same of `node:fs` (`internal/analyze/typescript/imports.go:168-172`). That
follows `docs/cdd.md:32`, whose External Coupling row folds "standard platform
libraries (e.g. Spring framework components, JDK infrastructure)" into one
line. The methodology text stays as it is; this feature is about what the tool
can measure separately, not about rewriting the method.

The measure of success is that the split is credible per language: a Java
reader must agree that `javax.crypto` is the JDK and `javax.inject` is not, a
Kotlin reader that `kotlinx.coroutines` is a library and `kotlin.collections`
is not, a TypeScript reader that `node:fs/promises` is the platform and
`lodash/fp` is not. Every rule below was chosen for that agreement first and
for cross-language symmetry second.

[test-cases.md](test-cases.md) is the companion to this file: every constraint
below has a numbered test case there, and a task is done when its cases are
checked-in, passing tests.

## Current state (verified 2026-09-11)

Line numbers are those of `ef0dc4c`, the commit this feature starts from.

- `internal/config/vocabulary.go` knows eight metric ids (`:19-28`), ordered in
  `metrics` (`:59-68`), weighted in `defaultWeights` (`:70-73`, only
  `external_coupling` and `local_variable` at 0.5), selected in
  `defaultSelection` (`:75-82`, the first six) and described in `descriptions`
  (`:99-108`). The generic external wording is "framework / stdlib types"
  (`:104`) — the word this feature takes away from it.
- `DefaultWeight`'s doc comment names the two 0.5 metrics (`:120-121`) and
  `DefaultSelection`'s names the two opt-in ones (`:129-130`); both become
  wrong the moment a third id lands.
- `internal/analyze/internal/jvm/imports.go` carries the JVM rule: `Module`
  holds `Internal bool` (`:12-29`, the bool at `:19`), `Module.Metric()` maps
  that bool to one of two ids (`:52-58`), `IsInternal` is the pure classifier
  (`:59-74`) and `NewImports(prefixes)` builds the collection (`:87-90`). Two
  states in one bool; there is no room for a third.
- `internal/analyze/typescript/imports.go` carries the same shape privately:
  `module.internal bool` (`:14-29`, the bool at `:20`), `isInternal`
  (`:168-185`), `isRelative` (`:186-193`) and `countCoupling`, whose body is an
  `if m.internal` over two `chargeSpan` calls (`:209-221`).
- `internal/analyze/java/spec.go:31` describes `external_coupling` as
  "framework / JDK types" — the JDK half of which moves.
  `internal/analyze/kotlin/spec.go:31-36` and
  `internal/analyze/typescript/spec.go:33-39` have no stdlib wording to fix;
  Kotlin overrides no coupling row at all and TypeScript says
  "framework / node_modules types", which stays true.
- `internal/config/templates/cdd.config.yaml.tmpl:29-37` prints the metric
  vocabulary as a comment block; the external row reads
  "framework / platform / third-party types" (`:34`).
- `internal/prompt/init_form.go:192` tells the user "Defaults: 1.0, except
  external_coupling and local_variable at 0.5".
- `internal/languages/literals_test.go` forbids vocabulary ids spelled as
  literals outside the vocabulary and the language specs (`literalExempt`,
  `:58-66`), and `TestLiteralsHelpers` (`:187-193`) pins that exemption list.
- `internal/report` orders metrics by `config.Metrics()` and skips ids absent
  from the configuration, so a new id needs no reporter change and moves no
  golden.

## Scope

**In:**

- `internal/config` — the new metric id, its order, default weight,
  description and the wording the split makes wrong (FR-1).
- `internal/analyze/internal/jvm` — `Module.Internal bool` becomes
  `Module.Metric config.MetricID`, set once by a pure `Classify` (FR-2).
- `internal/analyze/internal/jvm/stdlib.go` — `IsJDK`, shared by Java and
  Kotlin (FR-3).
- `internal/analyze/java`, `internal/analyze/kotlin`,
  `internal/analyze/typescript` — each language's standard-library predicate
  and the descriptions that name it (FR-3 … FR-5).
- Hand-maintained docs: README metric table and language paragraphs,
  CONTRIBUTING's "Writing an analyzer" note (FR-6).

**Out:**

- `analyze.Options`. Which imports are standard library is knowledge of the
  language, not of the configuration; no option is added and none changes.
- `docs/cdd.md`. Section 2 is the methodology text and stays as written: the
  tool measures the standard library separately, the method still counts it as
  coupling.
- Browser globals for TypeScript. `document`, `window` and `fetch` are not
  imported, so a per-file import analyzer cannot see them. Documented, not
  approximated.
- Deno (`https://`, `jsr:`, `node:` under compatibility) and Bun (`bun:`)
  built-ins. Documented gap, out of scope for this feature.
- `internal/report`, `internal/analyze/weights.go`, `run.go`,
  `internal/initcmd` and the Go spec. Go's spec carries generic wording and has
  no analyzer; `cmd/init.go --metrics` and `--weight` already accept any
  `config.IsMetric` id (`internal/initcmd/build.go:194-207, 244-266`).
- Any change to what `internal_coupling` means. The internal prefix wins over
  everything, so `internal_coupling.packages: ["java"]` keeps behaving exactly
  as it does today.

## Upgrade consequence (state it in the release notes)

`stdlib_coupling` is **opt-in**. It is not in `defaultSelection`, so `cdd init`
does not write it, and an existing `cdd.config.yaml` that does not list it
**stops counting standard-library imports** the moment the binary is upgraded:
`java.*`, `kotlin.*` and `node:*` imports that charged `external_coupling`
before charge nothing after. ICPs go down, units move under limits, and no
configuration edit is needed to get there. That is accepted and deliberate —
the alternative, selecting the metric by default, changes numbers for everyone
in the other direction. A team that wants the old totals adds
`stdlib_coupling: 0.5` next to `external_coupling` in each language's `.*`
block.

## Design decisions (settled)

| Decision | Choice | Rejected |
|---|---|---|
| Config shape | A new metric id, weighted per language and per file pattern like every other metric. Absent from the config = not counted. | A boolean toggle on `external_coupling`: no weight of its own, no per-layer variation, and a third state smuggled into a two-state shape. |
| Id spelling | `stdlib_coupling` — it names what is coupled to, the way `internal_coupling` and `external_coupling` do. | `standard_coupling`. Only the constant's value differs; nothing else in this spec depends on the spelling. |
| Default | Not selected by `cdd init`; default weight 0.5, the low end of the 0.5–1.0 band `docs/cdd.md:32` gives coupling. | Selected by default — it would raise every existing project's ICPs on upgrade. |
| Precedence | internal prefix > stdlib > external. | stdlib first — it would silently override `internal_coupling.packages: ["java"]`, which is a legitimate thing to configure. |
| Where the stdlib list lives | The language packages: `internal/analyze/internal/jvm/stdlib.go` shared by Java and Kotlin, `internal/analyze/typescript/stdlib.go`. | Configuration or `analyze.Options`: it would ask every user to maintain a list of the JDK. |
| Java stdlib | `java.*`, `jdk.*`, and only the `javax.*` packages shipped in the JDK (list below). | All of `javax.*`. `javax.servlet`, `javax.persistence`, `javax.inject` and `javax.annotation.Nullable` are third-party artifacts with a JDK-looking name; calling them stdlib is the first thing a Java reader would catch. |
| Kotlin stdlib | `kotlin.*` plus the whole Java rule. | `kotlinx.*` as stdlib — it ships separately, versions separately and is a library. `strings.HasPrefix(path, "kotlin.")` already excludes it. |
| TypeScript stdlib | Node.js built-ins: any `node:` specifier, or a bare specifier whose first `/` segment is in the list below (so `fs`, `fs/promises`, `path/posix`, `stream/web`, `timers/promises`). | Browser globals — they are not imports and are invisible to the analyzer. Deno and Bun specifiers — out of scope, documented. |
| `jvm.Module` shape | `Internal bool` → `Metric config.MetricID`, set once in `module()` by a pure `Classify`. Go forbids a field and a method of the same name, so the `Metric()` method goes away. | A second `Standard bool`: three states in two bools with an unstated invariant, and a fourth state that must never occur. |
| Literal check | Exempt `internal/analyze/*/stdlib.go` in `literalExempt` the way `spec.go` is: the Node built-in `"console"` is also a reporter format id, so the list cannot be written without tripping `make check-literals`. | Hiding the list in one `strings.Fields` string to dodge the check — it makes the list unreadable to keep a lint rule quiet. |
| Wording | The generic `external_coupling` description drops "stdlib" and becomes "framework / third-party types"; Java's "framework / JDK types" likewise. | Leaving "stdlib" in the external wording while a `stdlib_coupling` metric exists — two names for one thing in the same file. |
| Commit trailers | None, on every commit of this feature. `.githooks/commit-msg` rejects them. | A session attribution trailer. |

### The JDK `javax` prefixes

These and only these count as standard library under `javax.`:

`javax.accessibility`, `javax.annotation.processing`, `javax.crypto`,
`javax.imageio`, `javax.lang.model`, `javax.management`, `javax.naming`,
`javax.net`, `javax.print`, `javax.rmi.ssl`, `javax.script`,
`javax.security.auth`, `javax.security.cert`, `javax.security.sasl`,
`javax.smartcardio`, `javax.sound`, `javax.sql`, `javax.swing`, `javax.tools`,
`javax.transaction.xa`, `javax.xml`.

Everything else under `javax.` is external, `javax.servlet`,
`javax.persistence`, `javax.inject`, `javax.ws.rs`, `javax.validation` and
`javax.annotation.Nullable` included. `javafx.*` is external: JavaFX has
shipped separately since JDK 11.

### The Node.js built-in modules

`assert`, `async_hooks`, `buffer`, `child_process`, `cluster`, `console`,
`constants`, `crypto`, `dgram`, `diagnostics_channel`, `dns`, `domain`,
`events`, `fs`, `http`, `http2`, `https`, `inspector`, `module`, `net`, `os`,
`path`, `perf_hooks`, `process`, `punycode`, `querystring`, `readline`, `repl`,
`stream`, `string_decoder`, `sys`, `timers`, `tls`, `trace_events`, `tty`,
`url`, `util`, `v8`, `vm`, `wasi`, `worker_threads`, `zlib`.

A submodule is matched by its first `/` segment, so `fs/promises`,
`path/posix`, `stream/web`, `timers/promises` and `dns/promises` are covered
without listing them. `node:test`, `node:sea` and `node:sqlite` have no bare
form and are covered by the `node:` prefix rule alone. `node-fetch`, `fsx`,
`@types/node` and `lodash/fp` are not built-ins.

## Functional requirements

| # | Rule | Where the rule lives |
|---|---|---|
| FR-1 | The vocabulary gains `MetricStdlibCoupling MetricID = "stdlib_coupling"`, inserted in `metrics` directly after `MetricExternalCoupling`, with `defaultWeights` 0.5 and the description "standard library types". It is **not** in `defaultSelection`. The generic `external_coupling` description becomes "framework / third-party types", and the doc comments on `DefaultWeight` and `DefaultSelection` name the third id. The config template's vocabulary block gains a `stdlib_coupling 0.5 all standard library types (optional, off by default)` row and drops "platform" from the external row; column width is set by `exception_handling: 1.0`, so no alignment moves. The init form's weights hint names the three 0.5 metrics. | `internal/config/vocabulary.go`, `internal/config/templates/cdd.config.yaml.tmpl`, `internal/prompt/init_form.go` |
| FR-2 | `jvm.Module.Internal bool` becomes `jvm.Module.Metric config.MetricID`, and the `Metric()` method goes away. A pure `Classify(path string, prefixes []string, stdlib func(string) bool) config.MetricID` decides it: an internal prefix wins, then the `stdlib` predicate, else external; a nil predicate means nothing is standard library. `NewImports(prefixes []string, stdlib func(string) bool)` takes the predicate and `module()` calls `Classify` once per path. `IsInternal` stays exported with its table test. Java and Kotlin pass `nil` in this commit and charge `m.Metric`, so **no fixture assertion changes and no report moves**. | `internal/analyze/internal/jvm/imports.go`, `internal/analyze/java/imports.go`, `internal/analyze/kotlin/imports.go` |
| FR-3 | Java charges the JDK to `stdlib_coupling`: `jvm.IsJDK(path) bool` is true for `java.`, `jdk.` and the `javax.` prefixes listed above, false for everything else; `java/imports.go` passes it to `NewImports`; the Java spec describes `stdlib_coupling` as "JDK types (java.*, javax.*, jdk.*)" and `external_coupling` as "framework / third-party types". Attribution is unchanged — per unit, by binding, star to every unit, occurrence on the `import_declaration`. | `internal/analyze/internal/jvm/stdlib.go`, `internal/analyze/java/imports.go`, `internal/analyze/java/spec.go` |
| FR-4 | Kotlin charges `kotlin.*` **and** the JDK to `stdlib_coupling`: the predicate is `strings.HasPrefix(path, "kotlin.") || jvm.IsJDK(path)`. `kotlinx.*` stays external — the `kotlin.` prefix ends in a dot and does not match it. The Kotlin spec gains a `stdlib_coupling` description, "kotlin.* and JDK types". Aliases, stars and per-unit attribution are unchanged. | `internal/analyze/kotlin/imports.go`, `internal/analyze/kotlin/spec.go` |
| FR-5 | TypeScript charges Node.js built-ins to `stdlib_coupling`: `isBuiltin(spec string) bool` is true for any `node:` specifier and for a bare specifier whose first `/` segment is in the built-in list. `module.internal bool` becomes `metric config.MetricID`, set by `classify(spec, prefixes)` — relative or internal prefix, then `isBuiltin`, else external — and `countCoupling` collapses to one `chargeSpan(m.metric, m.at, 1)`. `isInternal` and `isRelative` keep their tests. The TypeScript spec describes `stdlib_coupling` as "Node.js built-in modules (node:fs, path)". `internal/analyze/*/stdlib.go` joins `literalExempt`, because `"console"` is both a Node built-in and a reporter format id. | `internal/analyze/typescript/stdlib.go`, `internal/analyze/typescript/imports.go`, `internal/analyze/typescript/spec.go`, `internal/languages/literals_test.go` |
| FR-6 | The hand-maintained docs describe the metric: README gains a `stdlib_coupling` row after `external_coupling`, says which six are ticked by default and which three are off, restates external coupling without "platform", says "internal, standard-library or external" where it explains classification, and each of the Kotlin, Java and TypeScript paragraphs says what that language treats as standard library and what deliberately does not qualify (`kotlinx.*`, non-JDK `javax.*`, Deno and Bun specifiers, browser globals). CONTRIBUTING's "Writing an analyzer" gains a paragraph: standard-library knowledge lives in the language package's `stdlib.go`, JVM platform knowledge in `internal/analyze/internal/jvm`, and `stdlib.go` is exempt from the literal check. | `README.md`, `CONTRIBUTING.md` |

## Worked per-unit expectations

Every triple below is **internal / external / stdlib**, with all three coupling
metrics enabled. The internal and external numbers of every unit not listed are
unchanged by this feature, and its stdlib number is 0.

### Java — `internal/analyze/java/testdata/coupling.java`

`InternalPrefixes = ["com.acme"]`. Imports: `com.acme.shared.Money`,
`com.acme.shared.Ledger`, `static com.acme.shared.Rates.rate`,
`java.time.Instant`, `java.util.*`.

| Unit | Before (internal / external) | After (internal / external / stdlib) | Why |
|---|---|---|---|
| `Invoice` | 2 / 2 | **2 / 0 / 2** | `Money` and `rate` internal; `Instant` and the `java.util.*` star are both JDK. |
| `Note` | 1 / 1 | **1 / 0 / 1** | `Ledger` internal; the star charges every unit and is now stdlib. |
| `Plain` | 0 / 1 | **0 / 0 / 1** | The star only. |

`coupling_no_star.java`, the same file without `java.util.*`: `Invoice`
**2 / 0 / 1**, `Note` **1 / 0 / 0**, `Plain` **0 / 0 / 0**.

The two occurrences charged to `Invoice` under `stdlib_coupling` are
`import java.time.Instant;` (line 6) and `import java.util.*;` (line 7), in
that order — occurrences still point at the import, never at the use.

### Kotlin — `internal/analyze/kotlin/testdata/coupling.kt`

`InternalPrefixes = ["com.acme"]`. Imports: `com.acme.shared.Money`,
`com.acme.shared.Ledger as L`, `java.time.Instant`, `kotlinx.coroutines.*`.

| Unit | Before (internal / external) | After (internal / external / stdlib) | Why |
|---|---|---|---|
| `Invoice` | 1 / 2 | **1 / 1 / 1** | `Money` internal; `Instant` is the JDK; the `kotlinx.coroutines.*` star stays external. |
| `Note` | 1 / 1 | **1 / 1 / 0** | The alias `L` internal; the star external. |
| `Plain` | 0 / 1 | **0 / 1 / 0** | The star only, still external. |

`coupling_no_star.kt`: `Invoice` **1 / 0 / 1**, `Note` **1 / 0 / 0**, `Plain`
**0 / 0 / 0**.

`Invoice`'s first three occurrences become internal on line 3, **stdlib** on
line 5 and external on line 6 — the one assertion in the suite that shows all
three metrics on one unit, in source order.

`coupling_uses.kt` is the javax allow-list's proof: its `javax.inject.Inject`
annotation keeps charging `external_coupling`, so `Annotated` stays
**0 / 1 / 0**.

### TypeScript — `internal/analyze/typescript/testdata/coupling.ts`

`InternalPrefixes = ["@app/"]`. Only two units move.

| Unit | Before (internal / external) | After (internal / external / stdlib) | Why |
|---|---|---|---|
| `usesExternal` | 1 / 3 | **1 / 2 / 1** | `node:fs/promises` is a built-in; `lodash/fp` and `reflect-metadata` are not. |
| `usesRequire` | 2 / 2 | **2 / 1 / 1** | `import nodePath = require("node:path")` is a built-in; `reflect-metadata` is not. |
| `UsesInternal` | 2 / 1 | 2 / 1 / 0 | unchanged |
| `usesBoth` | 2 / 1 | 2 / 1 / 0 | unchanged |
| `Wrapper` | 3 / 1 | 3 / 1 / 0 | unchanged |
| `renderer` | 1 / 3 | 1 / 3 / 0 | `react`, `ink` and `reflect-metadata` are all packages |
| `Untouched` | 1 / 1 | 1 / 1 / 0 | unchanged |

The occurrence pinned on line 11 (`import { readFile } from "node:fs/promises";`)
moves from `external_coupling` to `stdlib_coupling`; line 12
(`import * as lodash from "lodash/fp";`) gains an `external_coupling`
assertion in its place, so the suite keeps one pinned occurrence of each.

### End to end

`cmd/check_java_test.go` and `cmd/check_kotlin_test.go` both analyze an
`Invoice` that imports one internal type and `java.time.Instant`. With
`stdlib_coupling` appended to the shared `checkMetrics` selection, both read
`internal_coupling=1 stdlib_coupling=1x0.5` and **`icp=1.5` is unchanged** —
the weight of the metric the import moved to is the same 0.5 it had before.

## Cross-cutting invariants

These hold after every commit of this feature, for every fixture of every
language, and they are the reason a third coupling id cannot be added by
touching only the vocabulary.

| Invariant | Consequence for this feature |
|---|---|
| Every unit's `Counts` has exactly `len(config.Metrics())` keys. | Adding an id makes every existing `requireCount` helper assert a wider map; a language that never charges the new id still carries the key with value 0. |
| `Counts[m]` equals the sum of `Occurrences` for `m`. | The new id charges through the same `chargeSpan`, so the invariant is free — but it fails loudly if a `countCoupling` branch charges one metric and records an occurrence for another. |
| A coupling occurrence sits on the import, above the unit's own `Line`; every other occurrence sits inside the unit's line range. | **Every `isCoupling`-style helper in the test suites must include `stdlib_coupling`.** `internal/analyze/java/occurrences_test.go:63-67` is one such filter; left alone it makes `TestNonCouplingOccurrencesAreInsideTheUnit` fail on `coupling.java` and makes the import-line check go blind on `branches.java`, `exceptions.java` and `lambdas.java`, which all import `java.*`. |
| Occurrences are non-decreasing by `(Line, Col)` within a unit. | Unchanged: the three coupling metrics are charged in one pass over the modules in source order. |
| `internal/report` orders metrics by `config.Metrics()` and skips ids absent from the configuration. | No reporter change, and none of the 16 report goldens moves: they all configure three metrics. |
| `make check-literals` forbids a vocabulary id spelled outside `vocabulary.go` and `internal/analyze/*/spec.go`. | `internal/analyze/*/stdlib.go` must join the exemption, and `TestLiteralsHelpers` must pin that it did. Tests keep using `config.Metric…` constants. |

## Tasks

Commit per task; the task title is the commit message, verbatim and alone — no
trailers of any kind. Each task's acceptance criterion is shorthand for the
cases listed under the same task heading in [test-cases.md](test-cases.md);
those cases are the contract.

- **T0 — `docs: add the stdlib coupling feature spec`**. This file and
  [test-cases.md](test-cases.md). *Accept:* the rules, expectations and case
  ids below are the ones the following six commits implement.

- **T1 — `feat: add stdlib_coupling to the metric vocabulary`** (FR-1).
  Vocabulary, config template, init form, and the goldens they drive:
  `go test ./internal/config -update`, then
  `go test ./cmd -run 'TestInitTypeScriptMatchesGolden|TestInitDogfoodConfigReproducible' -update`,
  which also rewrites the repository's own `cdd.config.yaml` that CI diffs.
  `cmd/testdata/golden/greenfield-java-kotlin.yaml` and
  `docs/features/01-init/config-template.yaml` have no `-update` branch and are
  edited by hand. *Accept:* TC-V1 … TC-V11.

- **T2 — `refactor: classify JVM imports into a coupling metric`** (FR-2). No
  behaviour change. *Accept:* TC-M1 … TC-M8; every Java and Kotlin fixture
  assertion is byte-identical to its pre-T2 value.

- **T3 — `feat: charge JDK imports to stdlib_coupling in Java`** (FR-3).
  *Accept:* TC-J1 … TC-J15; the `coupling.java` triples above reproduce.

- **T4 — `feat: charge kotlin and JDK imports to stdlib_coupling in Kotlin`**
  (FR-4). *Accept:* TC-K1 … TC-K10; the `coupling.kt` triples above reproduce
  and `javax.inject.Inject` is still external.

- **T5 — `feat: charge Node.js built-ins to stdlib_coupling in TypeScript`**
  (FR-5). *Accept:* TC-T1 … TC-T16; the `coupling.ts` triples above reproduce
  and `make check-literals` is green with the built-in list checked in.

- **T6 — `docs: describe stdlib coupling`** (FR-6). README, CONTRIBUTING, and
  ticking the Definition of done below. *Accept:* TC-D1 … TC-D6.

## Verification

After every task: `make build`, `make test`, `make lint`, `make fmt` with no
diff, then one commit with no trailers.

End to end, from the repository root, after T5:

```sh
go build -o /tmp/cdd .
cd internal/analyze/java/testdata
/tmp/cdd init --yes --languages java --force \
  --metrics code_branch,condition,internal_coupling,external_coupling,stdlib_coupling
/tmp/cdd check --all --explain coupling.java
```

`Invoice` shows `internal_coupling=2 stdlib_coupling=2x0.5` and no
`external_coupling`, with the two `+0.5` occurrences pointing at lines 6 and 7.
Re-run `init` with `--metrics code_branch,condition,internal_coupling,external_coupling`
and the two `java.*` imports add nothing at all — that is the upgrade
consequence, visible. Repeat on `internal/analyze/kotlin/testdata/coupling.kt`
(line 5 stdlib, line 6 `kotlinx` external) and
`internal/analyze/typescript/testdata/coupling.ts` (`node:fs/promises` stdlib,
`lodash/fp` external). Finish with `cdd init --yes --force --languages go
--packages github.com/jonasalessi/cdd-lint` at the root and
`git diff --exit-code`, which is what the CI dogfood gate runs.

## Definition of done

- [x] `make build`
- [x] `make test` (race detector on)
- [x] `make lint` (including `check-literals`)
- [x] `make fmt` leaves no diff
- [ ] Every case in [test-cases.md](test-cases.md) is a checked-in test, citing
      its id, and passes. The suite is green, but only the T3, T4 and
      cross-cutting cases cite their ids; the T1, T2 and T5 cases are tested
      without a `TC-` comment.
- [x] Coverage does not drop for `internal/config`,
      `internal/analyze/internal/jvm`, `internal/analyze/java`,
      `internal/analyze/kotlin` or `internal/analyze/typescript`; the two new
      `stdlib.go` files are covered by their own table tests
- [x] The T2 refactor produces byte-identical Java and Kotlin reports on the
      existing fixtures
- [x] Every `isCoupling`-style test helper includes `stdlib_coupling`
- [x] CI dogfood gate green: the repository's own `cdd.config.yaml` is what
      `cdd init` regenerates
- [ ] The upgrade consequence is stated in the README and in the release notes.
      The README states it; the repository keeps no release-notes file yet.
- [x] Nothing outside `internal/config`, `internal/prompt`,
      `internal/analyze/{internal/jvm,java,kotlin,typescript}`,
      `internal/languages/literals_test.go`, `cmd` test files, the goldens
      listed in T1 and the docs listed above changed. Verified with
      `git diff --stat ef0dc4c..HEAD`, the feature's own base, since `main`
      predates the Java and Kotlin work this branch also carries.
