# Task 2 — Round 2: The `cdd init` Command

**Spec:** `features/01-init/draft.md` (§3 schema, §4 architecture, §6 Round 2)
**References:** `docs/cdd.md` §2–§4 · `features/01-init/config-template.yaml`
**Depends on:** `task1.md` complete (`internal/config` with `Render`, `Load`, `Validate`, vocabulary; `cmd/init.go` stub with flags)

## Goal

`cdd init` produces a valid, commented `cdd.config.yaml` either through an interactive session or entirely from flags, covering CDD Steps 1–2 (`docs/cdd.md` §3): choose ICP variables and weights, choose the limit calibrated by project maturity (§4A).

## Scope

**In:** language detection by file extension, internal-package detection from `go.mod` / package declarations / `tsconfig.json`, the `huh` prompt flow, the UI-agnostic `initcmd` use case, non-interactive flags, atomic write with overwrite guard, tests, dogfood regeneration, README docs.

**Out:** analyzers, `check`/`report`, `boy_scout` baseline store, per-layer override prompts, `target_limit`.

---

## Deliverables

```
internal/
├── detect/
│   ├── languages.go        # Languages(root) ([]config.Language, error)
│   ├── packages.go         # Packages(root, langs) ([]string, error)
│   ├── *_test.go
│   └── testdata/           # fixture trees (go-only, java-kotlin, ts, mixed, empty)
├── initcmd/
│   ├── answers.go          # Answers struct
│   ├── build.go            # Build(a Answers) (*config.Config, error)
│   ├── write.go            # Write(cfg, path string, force bool) error
│   └── *_test.go
└── prompt/
    ├── init_form.go        # Run(defaults Answers) (Answers, error)
    └── init_form_test.go   # validators only (forms are not driven in tests)
cmd/init.go                 # replaces the stub
cmd/init_test.go            # end-to-end via the built binary
README.md                   # `cdd init` section
cdd.config.yaml             # regenerated (T6)
```

New dependency: `github.com/charmbracelet/huh` (+ `golang.org/x/term` for TTY detection).

---

## Functional requirements (authoritative: `draft.md` §6.1)

| FR | Behaviour | Where the rule lives |
|---|---|---|
| FR-1 | Existing-file guard: interactive → `Overwrite? (y/N)`, No → print "aborted", exit 0; non-interactive without `--force` → exit 1, message says to pass `--force` | `initcmd.Write` (guard) · `prompt`/`cmd` (confirm) |
| FR-2 | Detect languages by extension (`.go` · `.java` · `.kt .kts` · `.ts .tsx .mts .cts`), skipping `.git node_modules vendor build dist target out`, under a **4 s budget** (`--scan-timeout`); on expiry use what was counted and tell the user the scan was cut short; multi-select with detected pre-checked; ≥ 1 required | `detect.Languages` · `prompt` |
| FR-3 | Project type `greenfield` / `legacy`; greenfield ⇒ `legacy_mode: strict_all`, `block_on_ci: true` | `initcmd.Build` |
| FR-4 | Legacy only: `strict_on_new_only` (default) / `boy_scout` / `measure_only`; `block_on_ci` true, forced false for `measure_only`; `boy_scout` shows "baseline not yet supported" | `initcmd.Build` · `prompt` (note text) |
| FR-5 | Limit: default `config.DefaultLimit(pt)`; integer ≥ 1; outside `LimitBand` → warning, accepted; applied to every language's `".*"` | `initcmd.Build` · `prompt` (validator) |
| FR-6 | Metrics multi-select over `config.Metrics()`, `DefaultSelection()` pre-checked, min 3; per language drop non-`Applicable`; any language < 3 → re-prompt naming it; optional weight customisation (`> 0`) | `initcmd.Build` (filtering, errors) · `prompt` (loop) |
| FR-7 | `auto_detect: true`; `packages` pre-filled from `detect.Packages`; editable | `detect.Packages` · `prompt` |
| FR-8 | `Exclude tests and generated code? (Y/n)` → `config.DefaultExcludes(lang)` per selected language; `include: []`; reporter `console`/`null`; `timeout` = `--timeout` or `config.DefaultTimeout()` (not prompted) | `initcmd.Build` |
| FR-9 | `Build → Validate (0 errors, else internal error) → Render → atomic write → summary line` | `cmd/init.go` · `initcmd.Write` |

Prompt order: `guard → languages → project type → [legacy mode] → limit → metrics → [weights] → packages → excludes → write`. Ctrl-C → exit 130, nothing written.

---

## T1 · `internal/detect`

`languages.go`
- `func Languages(ctx context.Context, root string) (Detected, error)` where `Detected struct { Counts map[config.Language]int; Truncated bool; Elapsed time.Duration }` — counts let the prompt show "(12 files)", `Truncated` drives the "scan stopped after 4s — tick anything missing" notice.
- `filepath.WalkDir`, skip-dir list as a set, extension → language map built from `config.Languages()` + an extension table kept in `detect` (the only language-specific data outside `config`).
- The caller passes `context.WithTimeout(ctx, scanTimeout)` (default `config.DefaultScanTimeout()` = 4 s; `0` = no deadline). `WalkDir` checks `ctx.Err()` per entry; on `context.DeadlineExceeded` it stops, sets `Truncated = true` and returns what was counted with a **nil** error (a cut-short scan is not a failure).

`packages.go`
- `func Packages(root string, langs []config.Language) ([]string, error)`:
  - `go` → `module` line of `<root>/go.mod` (if present).
  - `java`/`kotlin` → scan up to 200 source files for `package x.y.z;`/`package x.y.z`, return the **shortest common prefixes** (e.g. `com.acme.billing`, `com.acme.shared`), max 5.
  - `typescript` → `tsconfig.json` `compilerOptions.paths` keys with trailing `/*` stripped (e.g. `@app/`); tolerate JSON with comments/trailing commas (strip before parsing).
- Never fails on a missing file — returns what it found.

**Accept:** fixture-tree tests for each language, a mixed tree, an empty tree, the skip-dir rule (a `.go` under `node_modules` is not counted), the budget (an already-cancelled context yields `Truncated = true`, nil error, partial counts), and each package strategy (including a `tsconfig.json` with comments).

## T2 · `internal/initcmd`

`answers.go`
```go
type Answers struct {
    Languages   []config.Language
    ProjectType string
    LegacyMode  string            // "" ⇒ default for ProjectType
    Limit       int               // 0  ⇒ DefaultLimit(ProjectType)
    Metrics     []config.MetricID // nil ⇒ DefaultSelection()
    Weights     map[config.MetricID]float64 // overrides; missing ⇒ DefaultWeight
    Packages    []string
    DefaultExcludes bool
    Timeout     time.Duration     // 0 ⇒ config.DefaultTimeout()
}
```

`build.go` — `func Build(a Answers) (*config.Config, error)` implementing FR-3…FR-8:
- validates inputs against vocabulary (unknown language/metric/mode → error naming it);
- greenfield forces `strict_all` + `block_on_ci: true`; `measure_only` forces `block_on_ci: false`;
- limit ≥ 1 (error) and band check (returned as `Warnings []string` on a second return value or via `BuildResult{Config, Warnings}` — pick one, keep it simple);
- per-language metric filtering with `config.Applicable`; language with < 3 → `ErrTooFewMetrics{Language, Have, Need}` so the prompt can re-ask;
- weights: `> 0` else error;
- `Metrics[lang] = PatternWeights{{".*", weights}}`, `ICPLimits[lang] = PatternLimits{{".*", limit}}`, reporter `console`/nil, `Timeout` from answers or `config.DefaultTimeout()` (negative → error), `InternalCoupling{true, a.Packages}`, `Include: []`, `Exclude` from `config.DefaultExcludes` when `DefaultExcludes`, deduplicated, stable order.
- Final step: `config.Validate` must return zero **errors**; if not, return `fmt.Errorf("internal: built config is invalid: %v", issues)` (this is a bug guard).

`write.go` — `func Write(cfg *config.Config, path string, force bool) error`:
- `ErrExists` (typed) when the file exists and `!force` — the caller decides whether to ask;
- `config.Render` → write to `path + ".tmp"` in the same dir → `os.Rename`; permissions `0644`.

**Accept:** table tests for every FR rule (greenfield coupling, measure_only coupling, default mode for legacy, default limit per type, band warning, applicability filtering for `go`, too-few-metrics error naming the language, weight validation, excludes per language and dedup, packages passthrough); `Write` tests for `ErrExists`, `force`, temp-file cleanup on render error, resulting file loads and validates.

## T3 · `internal/prompt`

`init_form.go` — `func Run(defaults Answers) (Answers, error)` building `huh` groups in the prompt order:
1. Languages `MultiSelect` — options from `config.Languages()`, labels `Go (12 files)`, detected pre-selected, `Validate` min 1; when `Detected.Truncated`, the group description reads `scan stopped after <budget> — tick anything missing`.
2. Project type `Select` — descriptions: greenfield "strict from day one, limit 7–14 (cdd.md §4A)", legacy "measure existing, enforce new, limit 20–40".
3. Legacy mode `Select` — only when legacy; descriptions from `draft.md` §3.2, `boy_scout` suffixed with "(baseline not yet supported)".
4. Limit `Input` — default from `DefaultLimit`; validator: integer ≥ 1; out-of-band shows the band as a **warning in the description**, does not block.
5. Metrics `MultiSelect` — options `config.Metrics()` with `cdd.md` descriptions; pre-selected `DefaultSelection`; `Validate` ≥ 3. After the group, call `initcmd.Build` dry-run; on `ErrTooFewMetrics` show the message and re-run this group.
6. Weights `Confirm` ("Customise weights?") → one `Input` per selected metric, default `DefaultWeight`, validator `> 0`.
7. Packages `Input` — comma-separated, pre-filled.
8. Excludes `Confirm` — default yes.
All validators delegate to small pure functions (`validateLimit`, `validateWeight`, `parseCSV`) that are unit-tested; the form itself is not driven in tests.

Overwrite confirm (`FR-1`) is a separate `func ConfirmOverwrite(path string) (bool, error)` so `cmd` can call it before the form.

**Accept:** validator unit tests; a manual run-through checklist in the PR description (screenshots optional).

## T4 · `cmd/init.go` (replace stub)

Flow:
1. Resolve `path` (`--output` else root `--config`).
2. `interactive := !yes && term.IsTerminal(stdin)`.
3. Guard: if exists and `!force`: interactive → `prompt.ConfirmOverwrite`; No → print "aborted", return nil (exit 0); non-interactive → error `"<path> exists; pass --force to overwrite"` (exit 1).
4. Defaults: `detect.Languages(ctx)` with `context.WithTimeout(--scan-timeout)` + `detect.Packages` → `Answers` defaults (flags override each field); in non-interactive mode a truncated scan prints the notice to stderr.
5. Interactive → `prompt.Run(defaults)`; else → require `Languages` non-empty (from flags or detection) — if still empty, error listing the missing flags; `--legacy-mode` with greenfield → warning printed, ignored.
6. `initcmd.Build` → print warnings to stderr → `initcmd.Write` → print `Created cdd.config.yaml — languages: go, typescript · project: legacy · limit: 25 · metrics: 5`.
7. Ctrl-C from `huh` (`huh.ErrUserAborted`) → exit 130 silently.

Flag → field mapping exactly as `draft.md` §6.3; `--weight id=value` parsed into `Answers.Weights`.

**Accept:** see T5.

## T5 · Tests

- `initcmd` and `detect` unit tests (T1, T2).
- `cmd/init_test.go` (build binary once in `TestMain`, run in temp dirs):
  - `cdd init --yes` in a fixture with `go.mod` + `.go` files → file exists, `config.Load` + `Validate` zero errors, languages `[go]`, packages from `go.mod`;
  - full flag set (`--languages java,kotlin --project-type greenfield --limit 10 --metrics … --packages com.acme --force`) → golden comparison with `greenfield-java-kotlin.yaml` from Task 1;
  - `--project-type legacy --legacy-mode measure_only` → `block_on_ci: false`;
  - non-interactive with no languages detectable and no `--languages` → exit 1, message lists `--languages`;
  - existing file, non-interactive, no `--force` → exit 1; with `--force` → overwritten;
  - `--weight code_branch=0` → exit 1;
  - `--timeout 30s` → file contains `timeout: 30s`; `--timeout -1s` → exit 1;
  - `--scan-timeout 1ns` on a fixture with files → still succeeds (partial/empty detection) and prints the "scan stopped" notice; with `--languages go` the result is unaffected.

## T6 · Dogfood

`cdd init --yes --force --languages go --packages github.com/your-org/cdd-lint` at repo root; `git diff --exit-code cdd.config.yaml` must be empty against the Task 1 hand-written file. Add this as a CI step.

## T7 · Docs

README: "Getting started" with the interactive flow (one screenshot or text transcript), the flag table from `draft.md` §6.3, and a note that per-layer limits are edited by hand in `icp-limits` for now.

---

## Definition of done

- [ ] T1–T7 accepted; CI green including the dogfood diff step.
- [ ] `cdd init` on this repository yields zero validation errors **and** zero warnings.
- [ ] `internal/initcmd` and `internal/detect` coverage ≥ 90 %; `internal/prompt` validators covered.
- [ ] `cmd/init.go` and `internal/prompt/init_form.go` contain no business rules (all defaults/couplings/filters are in `initcmd.Build` or `config`).
- [ ] Exit codes: 0 success/aborted, 1 error, 130 Ctrl-C.

## Suggested order

T1 (detect) → T2 (initcmd, test-first) → T4 non-interactive path + T5 flag tests → T3 (prompt) → T4 interactive path → T6 → T7.
