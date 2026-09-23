# Feature 02 — Language Registry: One Directory, One Line per Language

## Goal

Make adding a language to `cdd` a local change. After this feature, supporting
language X means:

1. create `internal/analyze/X/` with a `spec.go` (and, later, an analyzer);
2. add **one line** to `internal/languages/languages.go`.

Nothing else in the repository changes — not `config`, not `detect`, not
`prompt`, not the Makefile — and a test fails naming the directory if the line
is forgotten.

Today language knowledge is scattered across seven tables in four packages
(`vocabulary.go`: `notApplicable`, `defaultExcludes`, `descriptions`;
`config/render.go`: `limitExamples`; `prompt/init_form.go`: `displayNames`,
`packageExamples`; `detect/languages.go`: `extensions`) plus the language
`switch` in `detect/packages.go`. This feature collapses all of it into
per-language directories and removes every package-level language table.

It is a **pure refactor**: `cdd init` output is byte-identical before and
after. The existing `cmd/` golden tests and the CI dogfood gate are the
regression net, which is why this lands *before* the first analyzer
(feature 03) rather than with it.

## Scope

**In:**
- `config.LanguageSpec` — the per-language data contract (FR-2).
- `analyze.Analyzer`, `analyze.Unit`, `analyze.FileResult` — the analyzer
  contract types only (FR-3). No pipeline, no parsing.
- `internal/languages` — the hand-written registry (FR-4).
- `internal/analyze/{golang,java,kotlin,typescript}/spec.go` — four spec
  directories, no analyzers (`NewAnalyzer: nil`).
- Dependency injection of `[]config.LanguageSpec` into `config`, `detect`,
  `prompt`, `initcmd`, `cmd/init.go`; removal of every global language
  accessor (FR-5).
- Lint replacement: `check-literals` becomes a registry-fed Go test; a
  scatter guard prevents language tables from regrowing (FR-8, FR-9).
- Golden isolation for `internal/config` (FR-10).
- A short "Adding a language" section in the README.

**Out:**
- Any analyzer, tree-sitter, CGO — feature 03.
- The analysis pipeline (`matcher`, `weights`, `run`), reporters, `cdd check`
  — feature 03.
- Opening the metric set. The eight ICP metrics stay a closed, central
  vocabulary (FR-6).
- Runtime plugins, `go generate`d registries, build-tag slimming (the layout
  leaves room for per-file `//go:build` tags later; not done now).

## Design decisions (settled)

| Decision | Choice | Rejected |
|---|---|---|
| Extension model | Compile-time, in-repo interface; explicit registry | `init()` self-registration (loses `gochecknoinits`, determinism); Go `plugin`/WASM/subprocess (CGO ABI, no third parties exist) |
| Home of a language | Everything in `internal/analyze/<id>/` | Separate `internal/lang/<id>` data dir + `internal/analyze/<id>` analyzer dir (two touches) |
| Import cycle (`config` needs specs, specs need `config.Language`) | Two structs: `config.LanguageSpec` (data, stdlib-only) + `analyze.Analyzer` (behaviour, may be CGO); both exported from the language dir, paired in the registry | Moving `Language`/`MetricID` to a new leaf package (repo-wide rename for no gain); `config` importing language packages (CGO in `config` tests forever) |
| How consumers see languages | Injected `[]config.LanguageSpec`; no globals | Package-level default registry set at startup (a mutable global — how the seven tables happened) |
| Registry file | Hand-written, one line per language | `go generate` from directory listing (saves one line, adds a generator and a drift gate) |
| Metric set | Closed (8 CDD metrics), read through specs | Per-language metric contribution (makes `cdd.config.yaml` unportable, validation stateful) |
| Literal lint | Go test fed by the registry | Keeping the Makefile grep (hardcodes language ids — a hidden second file) |
| Directory names | `dirname == string(spec.ID)`; `golang/` for `"go"` (`go` is a keyword) | Abbreviations (`ts/`, `kt/`) |
| README language list | Exempt from the one-file rule | Generated from `cdd languages` |
| Coverage gate | None (removed from CLAUDE.md) | — |

## Functional requirements

| # | Rule | Where |
|---|---|---|
| FR-1 | Adding a language touches exactly one existing file: `internal/languages/languages.go`. Everything else the language needs lives under `internal/analyze/<id>/`. Enforced by FR-7/FR-8 tests, not by review. | repo-wide |
| FR-2 | `config.LanguageSpec` carries all per-language data: `ID Language`, `DisplayName string`, `Extensions []string`, `NotApplicable []MetricID`, `DefaultExcludes []string`, `Descriptions map[MetricID]string` (overrides of the generic metric wording), `PackageExample string`, `LimitExamples []string`, `DetectPackages func(root string) ([]string, error)`. Stdlib-only; no CGO reaches `config`. Methods: `Applicable() []MetricID`, `IsApplicable(MetricID) bool`, `Description(MetricID) string` (override, else the generic `config.MetricDescription(m)`). | `internal/config/spec.go` |
| FR-3 | `analyze.Analyzer` is `Analyze(ctx context.Context, path string, src []byte) (FileResult, error)`. `FileResult{Units []Unit, Warnings []string}`; `Unit{Name string, Kind string, Line, Col int, Counts map[config.MetricID]int}`. Counts are **raw occurrences**; weights, disabled metrics, limits and enforcement are the pipeline's job (feature 03). An analyzer never reads config. | `internal/analyze/analyze.go` |
| FR-4 | `languages.Language{Spec config.LanguageSpec; NewAnalyzer func() analyze.Analyzer}`; `languages.All() []Language` returns a fresh slice in registration order. Hand-written composite literal; no `init()`. `NewAnalyzer == nil` means "no analyzer yet" and must surface as an error in `cdd check` (feature 03), never as zero ICPs. `languages.Specs() []config.LanguageSpec` is the convenience projection every consumer takes. | `internal/languages/languages.go` |
| FR-5 | No package-level language tables or accessors anywhere. Removed from `config`: `Languages()`, `IsLanguage()`, `Applicable()`, `IsApplicable()`, `DefaultExcludes()`, `MetricDescription(m, lang)` (the two-arg form; the generic one-arg form stays). Signatures gain a specs parameter: `config.Render(cfg, specs)`, `config.Validate(cfg, specs)`, `detect.Languages(ctx, root, specs)`, `prompt.Run(defaults, det, specs)`, `initcmd.Build(a, specs)`. `detect.Packages` is deleted; callers use `spec.DetectPackages(root)`. `cmd/init.go` obtains specs from `languages.Specs()` — the only place outside tests that imports `internal/languages`. | `config`, `detect`, `prompt`, `initcmd`, `cmd` |
| FR-6 | The metric vocabulary stays closed and central: `Metrics()`, `IsMetric`, `DefaultWeight`, `DefaultSelection`, `MetricDescription(m)` (generic wording), project types, legacy modes, reporter formats, limit bands all remain in `vocabulary.go`. Only the *per-language* dimension moves out. | `internal/config/vocabulary.go` |
| FR-7 | Directory ↔ id: for every `Language` in `All()`, a directory `internal/analyze/<dir>/` exists where `dir == string(Spec.ID)`, except `"go"` → `golang`. Conversely every subdirectory of `internal/analyze/` other than `internal/` and `testdata/` is registered. The failure message names the offending directory or id. | `internal/languages/registry_test.go` |
| FR-8 | Spec completeness: each registered spec has non-empty `ID`, `DisplayName`, `Extensions`, `PackageExample`; `len(Applicable()) >= config.MinMetrics`; `Description(m) != ""` for every applicable metric; `DetectPackages != nil`; no two specs share an id or an extension. | `internal/languages/registry_test.go` |
| FR-9 | Literal lint, replacing the Makefile grep. A Go test builds the forbidden set from `languages.All()` ids ∪ `config.Metrics()` ∪ project types ∪ legacy modes ∪ **reporter formats** (new), walks every non-test `.go` file under the module with `go/ast`, and fails on any string `BasicLit` equal to a forbidden value. Exempt: `internal/config/vocabulary.go`, `internal/analyze/*/spec.go`, struct tags. Scatter guard in the same test: outside `internal/languages/` and `internal/analyze/*/`, no `case config.Lang…` and no language-keyed composite-literal table (`map[config.Language]…{`). `make check-literals` keeps its name and delegates to this test so `.githooks/pre-commit` keeps working. | `internal/languages/literals_test.go`, `Makefile` |
| FR-10 | Golden isolation. `internal/config` tests construct synthetic specs (e.g. ids `alpha`, `beta`) and never import `internal/languages`; adding a real language changes no `internal/config` golden. Real languages appear only in `cmd/` e2e tests with explicit `--languages`. The dogfood gate (`--languages go`) is unchanged. | `internal/config/*_test.go`, `cmd/*_test.go` |

## Package graph after this feature

```
internal/config          LanguageSpec, vocabulary (closed metric set), Render/Validate(cfg, specs)
       ▲       ▲
       │       └────────────── internal/analyze          Analyzer, Unit, FileResult (contract only)
       │                              ▲
internal/analyze/golang ──────────────┤   spec.go (+ analyzer.go in later features)
internal/analyze/java   ──────────────┤
internal/analyze/kotlin ──────────────┤
internal/analyze/typescript ──────────┘
       ▲
internal/languages       All(), Specs()  — the one file; imports every language dir
       ▲
cmd/                     init.go passes languages.Specs() down; never a language package directly

internal/detect          generic extension-counting walk, skipDirs, scan timeout; takes specs
internal/prompt, internal/initcmd   take specs; no language tables
internal/analyze/internal/jvm       shared JVM package-prefix walk used by java/ and kotlin/
```

`config` imports nothing language-specific. `internal/analyze/<id>` may later
import tree-sitter; that CGO dependency is reachable only through
`internal/languages` and therefore only from `cmd/`.

## Deliverables

```
internal/config/
    spec.go                 LanguageSpec + methods (FR-2)
    vocabulary.go           per-language tables removed; metric vocabulary stays (FR-6)
    render.go validate.go   take specs (FR-5); limitExamples deleted
    *_test.go               synthetic specs (FR-10)
internal/analyze/
    analyze.go              Analyzer, Unit, FileResult (FR-3)
    golang/spec.go          data + goModule detection (from detect/packages.go)
    java/spec.go            data + JVM detection via internal/jvm
    kotlin/spec.go          data + JVM detection via internal/jvm
    typescript/spec.go      data + tsconfig paths detection + stripJSONC (from detect/packages.go)
    internal/jvm/           jvmPrefixes, declaredPackage, commonPrefixes, groupByFirstSegment, sharedSegments
    */spec_test.go          each spec's DetectPackages against its own testdata
internal/languages/
    languages.go            the registry (FR-4)
    registry_test.go        FR-7, FR-8
    literals_test.go        FR-9
internal/detect/
    languages.go            extensions table removed; Languages(ctx, root, specs)
    packages.go             deleted
internal/prompt/init_form.go     displayNames/packageExamples removed; Run(…, specs)
internal/initcmd/build.go        Build(a, specs)
cmd/init.go                      wires languages.Specs()
Makefile                         check-literals → go test ./internal/languages -run TestLiterals
.githooks/pre-commit             unchanged (still calls make check-literals)
README.md                        "Adding a language" section
```

## Tasks

- **T1 — Contracts.** `config.LanguageSpec` with methods; `analyze.Analyzer`,
  `Unit`, `FileResult`. Pure additions, nothing consumes them yet.
  *Accept:* compiles; `LanguageSpec` method tests with a synthetic spec.

- **T2 — Language directories.** Four `spec.go` files populated from the
  current tables, byte-for-byte the same strings. Move `goModule` →
  `golang/`, `tsAliases` + `stripJSONC` → `typescript/`, the JVM walk →
  `internal/analyze/internal/jvm/` used by `java/` and `kotlin/`. Move the
  corresponding tests from `detect/packages_test.go` next to the code.
  *Accept:* every moved detection test passes in its new home; `detect`
  still compiles (old code not yet deleted).

- **T3 — Registry.** `internal/languages/languages.go` with four entries,
  `All()`, `Specs()`; `registry_test.go` implementing FR-7 and FR-8.
  *Accept:* temporarily creating `internal/analyze/rust/` fails the test
  naming `rust`; temporarily removing an entry fails naming the id.

- **T4 — Injection.** Thread `[]config.LanguageSpec` through `config.Render`
  / `Validate`, `detect.Languages`, `prompt.Run`, `initcmd.Build`,
  `cmd/init.go`. Delete `detect.Packages`, the seven tables, and the global
  accessors listed in FR-5. The compiler drives this task.
  *Accept:* `make build`; `cmd/` goldens unchanged; `./bin/cdd init --yes
  --force --languages go --packages github.com/jonasalessi/cdd-lint && git
  diff --exit-code cdd.config.yaml` clean.

- **T5 — Golden isolation.** Convert `internal/config` tests to synthetic
  specs (FR-10). Where a golden encoded real-language wording, keep the
  wording in the synthetic spec so the golden's bytes are unchanged unless
  the test is genuinely about the roster.
  *Accept:* `grep -r languages internal/config` finds no import.

- **T6 — Lint.** `literals_test.go` (FR-9); `Makefile` `check-literals`
  delegates to it; `make lint` still depends on it.
  *Accept:* planting `"kotlin"` in `internal/detect/languages.go` fails the
  test with file:line; planting `"json"` (reporter format) fails; the same
  string in `internal/analyze/kotlin/spec.go` passes; a
  `map[config.Language]string{…}` in `internal/prompt` fails.

- **T7 — Docs.** README "Adding a language": create the dir, write
  `spec.go`, add the registry line, run `make test`. Note that `NewAnalyzer`
  may be nil until an analyzer exists.
  *Accept:* a reader can follow it without opening any other file.

## Definition of done

- [ ] `make build`
- [ ] `make test` (race detector on)
- [ ] `make lint` (including the new `check-literals`)
- [ ] `make fmt` leaves no diff
- [ ] CI dogfood gate green; `cmd/testdata/golden/*` unchanged by this feature
- [ ] `git grep -n 'map\[config.Language\]' -- ':!*_test.go' ':!internal/languages' ':!internal/analyze'`
      returns only the config schema types in `internal/config/config.go`
- [ ] The T3 and T6 "plant a violation" checks were actually performed

## Suggested order

T1 → T2 → T3 → T4 → T5 → T6 → T7. T2 and T3 can be developed together; T4
is the big-bang compile-driven step and should be one commit; T6 last so it
validates the final layout rather than an intermediate one.
