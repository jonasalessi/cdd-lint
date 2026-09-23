# Task 1 — Round 1: Application Structure, Entrypoints & Configuration Package

**Spec:** `features/01-init/draft.md` (§2 stack, §3 schema, §4 architecture, §5 Round 1)
**References:** `docs/cdd.md` · `features/01-init/config-template.yaml`
**Depends on:** nothing · **Unblocks:** `task2.md`

## Goal

A repository that builds, lints and tests cleanly, with every CLI entrypoint wired and the configuration contract (schema v1) fully implemented and covered. **No interactive behaviour, no language analysis.** When this task is done, Round 2 only has to collect answers and call `config`.

## Scope

**In:** Go module, Makefile, CI, lint · `cdd` root command, `cdd version`, `cdd init` stub with final flag set · `internal/config`: types, vocabulary, embedded template + `Render`, order-preserving `Load`, `Validate` (V1–V11) · golden and round-trip tests · hand-written dogfood `cdd.config.yaml`.

**Out:** prompts (`huh`), directory scanning, package detection, writing files from `init`, any analyzer.

---

## Deliverables (file by file)

```
cdd-lint/
├── go.mod / go.sum                  # module github.com/your-org/cdd-lint  (rename before commit)
├── main.go
├── Makefile                         # build · test · lint · fmt · cover
├── .golangci.yml
├── .gitignore
├── .github/workflows/ci.yml
├── README.md
├── cdd.config.yaml                  # dogfood (T7)
├── cmd/
│   ├── root.go
│   ├── version.go
│   └── init.go                      # stub
├── internal/config/
│   ├── config.go
│   ├── vocabulary.go
│   ├── render.go
│   ├── load.go
│   ├── validate.go
│   ├── templates/cdd.config.yaml.tmpl
│   ├── config_test.go / vocabulary_test.go / render_test.go / load_test.go / validate_test.go
│   └── testdata/golden/
│       ├── greenfield-java-kotlin.yaml
│       └── legacy-go-typescript.yaml
```

Dependencies added in this round: `github.com/spf13/cobra`, `gopkg.in/yaml.v3`, `github.com/stretchr/testify`. **Do not add `huh` yet.**

---

## T1 · Repository & toolchain

1. `go mod init github.com/your-org/cdd-lint` (Go ≥ 1.23).
2. `Makefile` targets: `build` (→ `bin/cdd`, `-ldflags` injecting `version`, `commit`, `date`), `test` (`go test ./... -race`), `cover` (`-coverprofile` + `go tool cover -func`), `lint` (`golangci-lint run`), `fmt` (`gofmt -l -w .`).
3. `.golangci.yml`: `gofmt`, `govet`, `errcheck`, `staticcheck`, `unused`, `revive`.
4. `.github/workflows/ci.yml`: on push/PR → setup-go, `make lint`, `make test`.
5. `README.md`: three lines on CDD (link `docs/cdd.md`), install (`go install`), `cdd --help` output, "status: init in progress".

**Accept:** `make build test lint` passes on a clean clone; CI is green on the first push.

## T2 · Root command & `version`

`cmd/root.go`
- `Use: "cdd"`, short: "Cognitive-Driven Development toolkit", long help pointing to `docs/cdd.md`.
- Persistent flag `--config` (string, default `cdd.config.yaml`), stored on a package-level var read by subcommands.
- `--version` flag wired to cobra's `Version` field.
- `Execute()` prints errors to stderr and returns exit code 1 on failure.

`cmd/version.go`
- Subcommand `version` printing `cdd <version> (<commit>, <date>)`; the three vars live in `cmd` (`var version = "dev"` …) and are set by `-ldflags -X`.

**Accept:** `cdd --help` lists `init` and `version`; `cdd version` prints injected values (`dev`/`none`/`unknown` when built with plain `go build`); `cdd nope` exits 1.

## T3 · `init` stub

`cmd/init.go`
- `Use: "init"`, short: "Create a cdd.config.yaml for this project", long text describing the two CDD steps it covers.
- Declare **every Round 2 flag now** so `--help` is stable: `--force`, `--languages` (string slice), `--project-type`, `--legacy-mode`, `--limit` (int), `--metrics` (string slice), `--weight` (string slice, repeatable `id=value`), `--packages` (string slice), `--no-default-excludes`, `--timeout` (duration, default `5m`), `--scan-timeout` (duration, default `4s`), `--yes`, `--output`.
- `RunE` returns `errors.New("init: not implemented yet")`.

**Accept:** `cdd init --help` shows the full flag set; `cdd init` exits 1 with that message.

## T4 · `internal/config` — types & vocabulary

`config.go` — exactly the types in `draft.md` §4.2:

```go
type Language string
type MetricID string

type Config struct {
    Version          int                         `yaml:"version"`
    ProjectType      string                      `yaml:"project_type"`
    Metrics          map[Language]PatternWeights `yaml:"metrics"`
    ICPLimits        map[Language]PatternLimits  `yaml:"icp-limits"`
    Enforcement      Enforcement                 `yaml:"enforcement"`
    Timeout          time.Duration               `yaml:"timeout"`
    Reporter         Reporter                    `yaml:"reporter"`
    InternalCoupling InternalCoupling            `yaml:"internal_coupling"`
    Include          []string                    `yaml:"include"`
    Exclude          []string                    `yaml:"exclude"`
}
type PatternWeights []PatternWeight            // ordered
type PatternWeight  struct { Pattern string; Weights map[MetricID]float64 }
type PatternLimits  []PatternLimit             // ordered
type PatternLimit   struct { Pattern string; Limit int }
type Enforcement      struct { BlockOnCI bool `yaml:"block_on_ci"`; LegacyMode string `yaml:"legacy_mode"` }
type Reporter         struct { Format string `yaml:"format"`; OutputFile *string `yaml:"outputFile"` }
type InternalCoupling struct { AutoDetect bool `yaml:"auto_detect"`; Packages []string `yaml:"packages"` }
```

`vocabulary.go` — all tables from `draft.md` §3.2 as exported data + accessors:

| Function | Returns |
|---|---|
| `Languages() []Language` | `go, java, kotlin, typescript` (stable order) |
| `Metrics() []MetricID` | the 8 ids in table order |
| `Applicable(l Language) []MetricID` | per-language subset (`go` lacks `exception_handling`, `inheritance`) |
| `DefaultWeight(m MetricID) float64` | `0.5` for `external_coupling`, `local_variable`; else `1.0` |
| `DefaultSelection() []MetricID` | the 6 defaults (excludes `local_variable`, `lambda`) |
| `MetricDescription(m MetricID, l Language) string` | inline comment text; language-specific for `code_branch`/`condition` (kotlin `?.` `?:`, typescript `?.` `??`, go `switch/select`) |
| `ProjectTypes() []string` | `greenfield, legacy` |
| `DefaultLimit(pt string) int` | `10` / `25` |
| `LimitBand(pt string) (lo, hi int)` | `7,14` / `20,40` |
| `LegacyModes() []string` | `strict_all, strict_on_new_only, boy_scout, measure_only` |
| `ReporterFormats() []string` | `console, json, xml, markdown` |
| `DefaultTimeout() time.Duration` | `5 * time.Minute` (analysis budget written to the file) |
| `DefaultScanTimeout() time.Duration` | `4 * time.Second` (init detection budget; not part of the schema) |
| `DefaultExcludes(l Language) []string` | per `draft.md` FR-8 (data only; used in Round 2) |

Constants for every id/mode string (`MetricCodeBranch`, `ProjectGreenfield`, `ModeStrictAll`, …) — no raw string literals outside `vocabulary.go`.

**Accept:** `vocabulary_test.go` table-tests pin every value above to the spec (a change in either must break the test).

## T5 · Template & `Render`

`templates/cdd.config.yaml.tmpl` (`//go:embed`)
- Reproduces `draft.md` §3.1 verbatim including section banners and comments.
- Ranges over languages in `Languages()` order (only those present in `cfg`), patterns in slice order, metrics in `Metrics()` order; each metric line gets `MetricDescription` as inline comment, aligned.
- `outputFile: null` when `Reporter.OutputFile == nil`; `packages: []` / `include: []` / `exclude: []` inline when empty, block list otherwise.
- `timeout:` rendered compactly via a `formatDuration` helper (`5m`, `30s`, `1h30m`, `0s`) — never `time.Duration.String()`'s `5m0s`; section banner and comment as in `draft.md` §3.1.
- A commented example under `icp-limits` showing a layer override (`# ".*/adapters/.*": 8`).

`render.go` — `func Render(cfg *Config) ([]byte, error)`; floats formatted with one decimal (`1.0`, `0.5`); durations via `formatDuration` (unit-tested: `0`, `30s`, `5m`, `90m`→`1h30m`).

**Accept:** golden tests for `greenfield-java-kotlin` (limit 10, `strict_all`, packages `["com.acme.billing"]`, java/kotlin excludes) and `legacy-go-typescript` (limit 25, `strict_on_new_only`, packages `["github.com/acme/billing","@app/"]`, go/ts excludes); byte-for-byte equality; `UPDATE_GOLDEN=1` regenerates.

## T6 · `Load` & `Validate`

`load.go`
- `func Load(path string) (*Config, error)` and `func Parse(r io.Reader) (*Config, error)`.
- Decode with `yaml.v3` `KnownFields(true)`; `PatternWeights` / `PatternLimits` implement `yaml.Unmarshaler` walking the `MappingNode` content pairwise to keep key order.
- Missing file → wrapped `os.ErrNotExist`; YAML error → includes line number.

`validate.go`
- `type Issue struct { Rule, Severity, Message string }` (`Severity`: `error` | `warning`), `func Validate(cfg *Config) []Issue`, helper `func (issues Issues) HasErrors() bool`.
- Rules V1–V12 from `draft.md` §3.5, aggregated (never fail-fast). Messages name the language / pattern / metric involved. (V12: `timeout ≥ 0`; a negative or unparsable value is a Load error from `yaml.v3` or a V12 error respectively.)

**Accept:**
- one positive + one negative test per rule (V1–V12), asserting `Rule` id and severity;
- V6 out-of-band limit yields **warning** not error; V9 `boy_scout` yields warning;
- **round trip:** for both golden `Config`s, `Render → Parse → Validate` has zero errors and `Parse` result deep-equals the original, including pattern order with three patterns per language;
- unknown top-level key → error; unknown metric id → V4 error; `go` with `exception_handling` → V4 error.

## T7 · Dogfood placeholder

- Commit `cdd.config.yaml` at repo root: `go`, greenfield, limit 10, metrics `code_branch, condition, internal_coupling, external_coupling` (the four applicable defaults), `timeout: 5m`, packages `["github.com/your-org/cdd-lint"]`, excludes `**/*_test.go, vendor/**`.
- Produce it with a small test helper (`go test -run TestWriteDogfood -update`) or by hand-copying golden output — it must be **byte-identical** to `Render` of the equivalent `Config`; add a test asserting that so Round 2's `cdd init --force` diff-clean check has a stable baseline.

---

## Definition of done

- [ ] T1–T7 acceptance criteria met; CI green.
- [ ] `go test ./... -race` passes; `internal/config` coverage ≥ 90 %.
- [ ] No `huh` import, no prompts, no `os.Stdin` use anywhere.
- [ ] No metric/language/mode string literals outside `internal/config/vocabulary.go` (grep check in CI is fine).
- [ ] `cdd`, `cdd version`, `cdd init --help`, `cdd init` behave as in T2/T3.

## Suggested order

T1 → T2 → T3 → T4 → T5 → T6 → T7 (T4–T6 can be developed test-first against the golden files).
