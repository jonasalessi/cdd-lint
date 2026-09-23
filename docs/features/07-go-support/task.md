# Feature 07 — Go Support: CDD Analyzer on go/parser

## Goal

Give Go the analyzer its spec has been waiting for. `cdd init` already
configures a Go project — `internal/analyze/golang/spec.go` exists, detects the
module path from `go.mod` and is registered with `NewAnalyzer: nil` — but
`cdd check` on a Go file stops with `no analyzer for go yet`. Go is the last
registered language without an analyzer, and the repository itself is a Go
project that CI dogfoods with `cdd init`.

After this feature `cdd check` parses every `.go` file the configuration
matches, computes ICPs per unit, enforces `icp-limits` and reports through the
four existing reporters, exactly like TypeScript, Kotlin and Java.

The measure of success is not "counts something": it is that a Go developer
running `cdd check` on a real codebase reads the report and finds it credible.
Every rule below was chosen for cross-language comparability with the shipped
TypeScript, Kotlin and Java rules first, fidelity to `docs/cdd.md` second, and
Go-specific tuning only where the language genuinely differs.

[test-cases.md](test-cases.md) is the companion to this file: every constraint
below has a numbered test case there, and a task is done when its cases are
checked-in, passing tests.

## Current state (verified 2026-09-11)

- `internal/analyze/golang/spec.go` holds the whole language today: `Spec()`
  and `detectPackages`, which reads the `module` line of `<root>/go.mod`.
  `spec_test.go` and `testdata/module/` cover detection. There is no
  `analyzer.go`.
- `spec.go:25` declares
  `NotApplicable: []config.MetricID{config.MetricExceptionHandling, config.MetricInheritance}`,
  and `spec.go:27-30` gives `Descriptions` only two rows, `code_branch`
  ("if/else, switch/select, for") and `lambda` ("func literals"). There is no
  `stdlib_coupling` description, unlike the other three languages.
- `internal/languages/languages.go:22` registers Go as `{Spec: golang.Spec()}`,
  with no `NewAnalyzer`, while lines 23-25 give Java, Kotlin and TypeScript
  theirs. `TestEveryAnalyzerIsWired` in `internal/languages/registry_test.go:62`
  asserts a language with no `analyzer.go` in its directory is not wired, so
  the registry line and the file must land in the same commit.
- A run that meets a Go file fails in `internal/analyze/run.go:165`:
  `return fmt.Errorf("no analyzer for %s yet", lang.Spec.ID)`. `cdd init`
  warns once with the same wording from `cmd/init.go:243`.
- Four `cmd` assertions pin that warning and have to be flipped or deleted:
  `cmd/init_test.go:116-117`, `cmd/init_test.go:140`, `cmd/init_test.go:343`
  and `cmd/check_test.go:466` (the whole `TestCheckSelectedUnavailableLanguage`).
  `cmd/check_test.go:368` already asserts `NotContains` and stays as it is.
- `internal/config/templates/cdd.config.yaml.tmpl:32` reads
  `#   exception_handling 1.0  not go      try / catch / finally blocks` and
  `:36` reads `#   inheritance        1.0  not go      extends / implements, per level`.
  Line 65 says "The unit measured is a top-level type declaration for java /
  kotlin / typescript, and **the source file for go**" — which this feature
  makes wrong.
- Those three lines are copied verbatim into every generated file:
  `cmd/testdata/golden/greenfield-typescript.yaml`,
  `cmd/testdata/golden/greenfield-java-kotlin.yaml`,
  `internal/config/testdata/golden/greenfield-alpha-beta.yaml`,
  `internal/config/testdata/golden/legacy-gamma-delta.yaml`,
  `docs/features/01-init/config-template.yaml` and the repository's own
  dogfood `cdd.config.yaml` (lines 32, 36 and 60).
- `internal/config/vocabulary.go` already carries all nine metric ids,
  `stdlib_coupling` included (feature 06). Nothing in the vocabulary changes
  here; only the Go spec's `NotApplicable` list and the template prose move.
- `internal/analyze/internal/treesitter/occurrences.go:17` exports
  `SortOccurrences`, which is grammar-agnostic (it sorts `analyze.Occurrence`
  by `Line` then `Col`) and is reused unchanged by a parser that is not
  tree-sitter.

## Scope

**In:**

- `internal/analyze/golang/spec.go` — `inheritance` becomes applicable, two
  `Descriptions` rows are added and `extGo` is exported to the package (FR-1),
  with the template prose, the goldens and the dogfood config that follow from
  it.
- `internal/analyze/golang` — the analyzer: parsing, units, metric counters,
  import coupling (FR-2 … FR-8).
- Registration: `NewAnalyzer: golang.NewAnalyzer` in `languages.go` (FR-9).
- The four `cmd` assertions above, and a new `cmd/check_go_test.go`.
- Hand-maintained docs: the README language paragraph and known limitations,
  the CONTRIBUTING note that an analyzer need not use tree-sitter.

**Out:**

- Per-function units for a type's methods. A unit is a top-level type with its
  methods billed to it, or a top-level function; the limits in `icp-limits` are
  calibrated per unit across languages and per-method units would make every Go
  project pass.
- Cross-file and cross-package resolution. A same-package identifier that needs
  no import is invisible to a per-file analyzer, and so is the type of a value,
  which is why a method value is not a lambda and a field is not distinguished
  from a method reference.
- Top-level `var` and `const` declarations as units. Their initializers, even
  func literals, are invisible, exactly like Kotlin's top-level `val n = 4` and
  the statements of a Java compact source file. Documented, not worked around.
- `exception_handling` for Go. It stays in `NotApplicable`: `if err != nil` is
  a branch and nothing else, and `defer`, `panic` and `recover` are 0.
- Any change to the metric vocabulary, the default selection or the default
  weights. `stdlib_coupling`, `local_variable` and `lambda` stay opt-in for
  every language.
- Changes to `internal/analyze` pipeline types, reporters or `cmd/check.go`.
  If one turns out to be necessary it is a separate, justified commit.

## Dependencies

**None.** The analyzer is written against `go/parser`, `go/ast` and
`go/token` from the Go standard library:

| Package | Source | Notes |
|---|---|---|
| `go/parser` | standard library | `parser.ParseFile` with `parser.ParseComments`, and **without** `SkipObjectResolution`, so `Ident.Obj` tells a shadowed local from a package qualifier. |
| `go/ast` | standard library | `ast.Inspect`, `ast.IsGenerated`, the node types the mapping below names. |
| `go/token` | standard library | A fresh `token.FileSet` per `Analyze` call, and the operator tokens `LAND`, `LOR`, `NOT`, `TILDE`, `DEFINE`. |

Consequences, all of them absent: **no `go.mod` change**, no `go.sum` entry, no
grammar pin, no CGO, no C compiler, **no growth of the binary** — the standard
library packages are already linked into any Go program that parses nothing,
and the parse tables are Go code rather than an embedded C blob. This is the
one analyzer in the project that costs nothing to ship, and the README
binary-size note should say so (T9).

The flip side, to state in CONTRIBUTING (T9): an analyzer is *not* required to
use tree-sitter. Go keeps the same file layout as the other three
(`spec.go`, `analyzer.go`, `units.go`, `metrics.go`, `imports.go`), implements
the same `analyze.Analyzer` contract and produces the same `Occurrence` spans,
but has no `parser.go`, no grammar pin, no `TestGrammarResolvesEveryKind` and
no `io.Closer`.

## Design decisions (settled)

| Decision | Choice | Rejected |
|---|---|---|
| Parser | `go/parser` + `go/ast` + `go/token` from the standard library. Pure Go, no grammar pin, no binary growth, exact positions, and the syntactic resolver tells a shadowed local from a package qualifier. | A tree-sitter Go grammar, for consistency with the other three: it would add a parse table to the binary and a fork to audit, to get less information. |
| Unit | Each top-level type, with its methods billed to it; each top-level function without a receiver. Methods of a type declared in another file form a per-file `methods` unit (`docs/cdd.md` §5, "partial classes"). | One unit per file (what the config template promises today): a 2000-line file with forty small types would read as one unmeasurable unit. |
| Unit kinds | `struct` (`*ast.StructType`), `interface` (`*ast.InterfaceType`), `type` (any other named type or alias), `func` (top-level function), `methods` (receiver type declared in another file). | One `type` kind for every `TypeSpec`: a Go reader wants to see struct vs interface. |
| Unit position | `node.Pos()`: the name for a `TypeSpec` (grouped or not), the `func` keyword for a `FuncDecl`; a `methods` unit sits at its first method's `func`. | The `type` keyword: shared by every spec of a `type (...)` group. |
| Unit order | Declaration position. A method that precedes its type still bills to the type's unit. | Position of the first member. |
| Receiver resolution | `Recv.List[0].Type` unwrapped through `StarExpr`, `ParenExpr`, `IndexExpr`, `IndexListExpr` to an `Ident`. | String matching on the receiver text. |
| Top-level `var` / `const` | Not units; their initializers (even func literals) are invisible, like Kotlin's `val n = 4` and Java's compact-file statements. Documented. | A synthetic file unit for them. |
| `exception_handling` | Stays not applicable. `if err != nil` is a branch; `defer`, `recover` and `panic` are 0. | Charging `defer`/`recover` as handling: Go has no handler construct, and the number would not compare with a Java `catch`. |
| `inheritance` | Applicable: +1 per embedded struct field and per embedded interface. This changes the Go spec's `NotApplicable`, the config template rows and the goldens. | Keeping it not applicable: embedding is exactly the "a reader must follow the supertype to know what this is" cost the metric measures. |
| `switch` / type switch / `select` arms | +1 per `CaseClause` with `List != nil` and per `CommClause` with `Comm != nil`; `default` 0; `case 1, 2:` is one arm and one point. Same as Java's `case 2, 3 ->`. | +1 per listed value. |
| `else` occurrence | The else `BlockStmt` span (`go/ast` keeps no `else` keyword position). | The whole `if`, which would swallow the consequent. |
| Bitwise `&`, `\|`, `^`, `&^` | 0 for `condition`; only `&&` / `\|\|` chains are clauses. | Counting them — they are arithmetic on bits, as in Java. |
| Type-switch guard `switch v := x.(type)` | `local_variable` 0, like Java pattern variables. | +1. |
| Named results, params, receivers, type params | 0, like every other language. | Counting named results, which are a signature detail, not a temporary. |
| Struct fields | +1 per named field, in every `StructType` inside the unit (anonymous structs in a body included). Embedded fields are `inheritance`, not `local_variable`. | Only top-level struct fields. |
| Interface type sets | `~T` terms and `A \| B` unions are 0; a named embedded interface (`Reader`, `fmt.Stringer`) is +1. | Counting terms: a constraint's type set is not a supertype to follow. |
| Method values / expressions | `lambda` 0 (`s.M` as a value is indistinguishable from a field without types). Documented. | Heuristics on capitalisation or call position. |
| Import binding without alias | goimports' assumed-name rule: `path.Base`, step over a `vN` major-version segment, strip a leading `go-`, cut at the first non-identifier character (`gopkg.in/yaml.v3` → `yaml`, `go-isatty` → `isatty`, `nats.go` → `nats`, `pgx/v5` → `pgx`). | Requiring a type checker to read the real package clause. |
| Import use | A unit uses a module when its subtree holds a `SelectorExpr` whose `X` is an `Ident` named like a binding **and unresolved** (`Obj == nil`): a parameter or local named `fmt` is not the package. One point per module per unit. | Name-only matching as Java does — Go's resolver makes the precise rule free. |
| `.` and `_` imports | Charged to every unit of the file (dot names are indistinguishable from locals; a blank import is a side effect), like Java's star import and TypeScript's side-effect import. | Charging none. |
| Import used by no unit | Charged nowhere, like Java's unused import. Covers the rare wrong assumed name and packages used only by top-level `var`s. Documented. | Charging every unit, which would turn one mistake into N. |
| Standard library | An import whose first path segment has no dot: `fmt`, `net/http`, `go/ast`, `embed`, `unsafe`, `C` (cgo). `golang.org/x/...` is external. A configured internal prefix always wins. | A hard-coded package list, which goes stale every release. |
| Internal | `path == prefix` or `path` starts with `prefix + "/"`, prefixes from `internal_coupling.packages` plus the `go.mod` module line. | The `.`-separated prefix rule of `jvm.IsInternal`: Go import paths are slash-separated and `example.com/app` must not match `example.com/apples`. |
| Generated files | `ast.IsGenerated(file)` (`// Code generated ... DO NOT EDIT.`) yields no units and no warning. Documented. | Analyzing `.pb.go`, which would drown every real unit. |
| Empty / whitespace-only file | No units, no warning — `go/parser` would say "expected 'package', found 'EOF'", and every other analyzer treats an empty file as a no-op. | A spurious syntax warning. |
| Resources | None: no `io.Closer`, a fresh `token.FileSet` per `Analyze` call. | A per-analyzer `FileSet`, which grows without bound over a long run. |
| `ast.Object` deprecation | Use `Ident.Obj` with `//nolint:staticcheck // syntactic resolution is exactly what a per-file analyzer needs`. | Re-implementing scope tracking; running `go/types` without an importer. |
| Registration timing | `NewAnalyzer: golang.NewAnalyzer` lands with the parse commit: `TestEveryAnalyzerIsWired` fails the moment `analyzer.go` exists unwired. | A red registry test between two commits. |

## Functional requirements

| # | Rule | Where the rule lives |
|---|---|---|
| FR-1 | The Go spec drops `MetricInheritance` from `NotApplicable`, gains `Descriptions` for `inheritance` ("embedded structs and interfaces") and `stdlib_coupling` ("standard library packages (fmt, net/http)"), and exports `const extGo = ".go"` for the analyzer to use. The template's vocabulary row for `inheritance` reads `all`, and its "unit measured" sentence describes Go's unit instead of "the source file for go". The four goldens, `docs/features/01-init/config-template.yaml` and the dogfood `cdd.config.yaml` regenerate. `exception_handling` stays `not go`. | `internal/analyze/golang/spec.go`, `internal/config/templates/cdd.config.yaml.tmpl`, goldens |
| FR-2 | `Analyze` accepts only `.go` (case-insensitive; any other extension returns an error naming the path and the extension, and no units), returns `ctx.Err()` when the context is canceled, yields no units and no warning for an empty or whitespace-only file and for one `ast.IsGenerated` reports, and yields no units plus exactly one warning `syntax error at L:C` when `parser.ParseFile(fset, path, src, parser.ParseComments)` fails. `L:C` is the position of the first `scanner.Error` in the returned `scanner.ErrorList`, or `1:1` when the error carries none. `err` is nil on a syntax error — a file that does not parse is a warning, not a failed run. | `internal/analyze/golang/analyzer.go` |
| FR-3 | A Go **unit** is each `TypeSpec` of a top-level `GenDecl` with `Tok == token.TYPE` (kind from the type expression: `struct`, `interface`, otherwise `type`), each top-level `FuncDecl` without a receiver (kind `func`), and one `methods` unit per receiver type that has no `TypeSpec` in this file. `Name` is the identifier; `Line`/`Col` come from `fset.Position(node.Pos())`. A method bills to its receiver's unit wherever it sits in the file, including before the type's own declaration. Top-level `var`, `const` and `import` declarations are not units and their contents are invisible. | `internal/analyze/golang/units.go` |
| FR-4 | ICPs are counted over a unit's subtrees — the `TypeSpec` plus every method `FuncDecl` billed to it — with the mapping below. Every unit carries a key for every `config.Metrics()` entry; `Counts[m]` equals the sum of the `Count` of its occurrences for `m`; occurrences are sorted by position with `treesitter.SortOccurrences`, which is grammar-agnostic and reused as is. Spans are 1-based with byte columns and an exclusive end, the contract `treesitter.SpanOf` produces. | `internal/analyze/golang/metrics.go` |
| FR-5 | `condition` counts Boolean **clauses**, not operators: the leaf operands of a chain of `BinaryExpr` nodes whose `Op` is `token.LAND` or `token.LOR`, flattened through `ParenExpr` and `UnaryExpr{Op: token.NOT}` exactly as Kotlin and Java do. `a && b` = 2, `a && b \|\| c` = 3, `!(a \|\| b) && x` = 3, `x > 1` = 0, `a&b \| 3` = 0. A nested chain is marked consumed so an inner `&&` is not counted twice. | `internal/analyze/golang/metrics.go` |
| FR-6 | `if / else if / else` is 3, not 4: an `IfStmt` is +1, and its `Else` is +1 only when it is a `*ast.BlockStmt`. The else occurrence spans the block, since `go/ast` keeps no position for the `else` keyword. | `internal/analyze/golang/metrics.go` |
| FR-7 | `local_variable` is +1 per name of a `ValueSpec` inside the unit (`var`, `const`, grouped or not); +1 per **new** LHS `Ident` of an `AssignStmt` with `token.DEFINE` (`Name != "_"`, `Obj != nil`, `Obj.Decl == stmt`, so a redeclaration in `z, err := f()` charges only `err`), excluding the `AssignStmt` that is a `TypeSwitchStmt.Assign`; +1 per non-blank `Key`/`Value` of a `RangeStmt` whose `Tok` is `token.DEFINE`; +1 per named field of every `StructType` in the unit, anonymous struct types included. 0 for parameters, receivers, named results, interface method signatures, `FuncLit` parameters, labels, type parameters, the type-switch guard, `_`, and anything at top level. | `internal/analyze/golang/metrics.go` |
| FR-8 | Imports: one module per path (a path imported twice under two names is one module with two bindings and the `At` of the first `ImportSpec`); classification internal > stdlib > external; the binding is `ImportSpec.Name` when present, otherwise the assumed name; `.` and `_` bind nothing and charge every unit of the file; every other module is charged once to each unit whose subtree holds a `SelectorExpr` with an unresolved `Ident` named like one of its bindings; the occurrence sits on the `ImportSpec`. A module no unit uses is charged nowhere. | `internal/analyze/golang/imports.go`, `internal/analyze/golang/stdlib.go` |
| FR-9 | `internal/languages/languages.go:22` gains `NewAnalyzer: golang.NewAnalyzer`. The literals `"go"` and `".go"` appear only in `spec.go`, and metric ids only through the `config.Metric…` constants, so `make check-literals` stays green. No file outside `internal/analyze/golang`, `languages.go`, the four `cmd` assertions, `cmd/check_go_test.go`, the template, the goldens of FR-1 and the docs listed under Deliverables changes. | `internal/languages/languages.go` |

## Metric → go/ast mapping

| Metric | Counted |
|---|---|
| `code_branch` | `IfStmt` +1, occurrence on the whole statement; an `Else` that is a `BlockStmt` +1, occurrence on the block (FR-6). `CaseClause` with `List != nil` +1, in a `SwitchStmt` and in a `TypeSwitchStmt` alike; `CommClause` with `Comm != nil` +1; `default` (`List == nil`, `Comm == nil`) 0. `ForStmt` +1 in all four forms (`for i := …`, `for cond`, `for`, with or without post), `RangeStmt` +1. Nothing for `return`, `break`, `continue`, `goto`, `fallthrough`, `LabeledStmt`, `GoStmt`, `DeferStmt`, `TypeAssertExpr`, `panic` or `recover`. |
| `condition` | Leaf operands of `&&` / `\|\|` chains, one occurrence per leaf (FR-5). `x > 1` = 0; `a&b \| 3` = 0. |
| `exception_handling` | Never counted; the key is present at 0 and the spec hides it. |
| `inheritance` | A `Field` with `Names == nil` in a `StructType.Fields` +1, occurrence on the field's type (`Base`, `*Base`, `pkg.Base` and `Base[T]` included). A `Field` with `Names == nil` in an `InterfaceType.Methods` +1, unless its type is a `BinaryExpr` (a `\|` union) or a `UnaryExpr{Op: token.TILDE}` (an approximation term). |
| `local_variable` | Per FR-7. |
| `lambda` | `FuncLit` +1, occurrence on the literal, including the literal of `go func(){}()` and `defer func(){}()`. A method value (`l.Wire`) or method expression (`Lambdas.Wire`) is 0. |
| `internal_coupling` / `stdlib_coupling` / `external_coupling` | Per FR-8, occurrence on the `ImportSpec`. |

## Worked fixtures (acceptance values)

Each block below is a checked-in file under `internal/analyze/golang/testdata/`
with a test asserting the exact per-unit counts. Totals assume every metric
enabled at weight 1.0, and the coupling fixtures assume
`InternalPrefixes = ["example.com/app"]`. A trailing `// comment` on a line is
the source of truth for the assertion next to it, and the `// unit X: …` line
after a block is the per-unit total.

Two properties of these files matter:

- **They are gofmt-clean and they do not compile as a package.** `make fmt`
  runs `gofmt -l -w .` over the whole repository, `testdata/` included, so a
  misaligned struct comment fails the Definition of Done. Nothing else applies:
  `testdata` is outside `go build`, `go vet` and the linters, which is why
  `units.go` may reference an undeclared `H`, `coupling.go` may import modules
  that do not exist and both coupling fixtures may declare `Invoice` in the
  same package.
- **A fixture that must not parse carries a `.txt` suffix** — `broken.go.txt`
  and `empty.go.txt` — for the same reason: `gofmt -l -w .` would fail on a
  `.go` file it cannot parse. The test reads the file and analyzes it under the
  corresponding `.go` name.

```go
// cdd_examples.go — docs/cdd.md section 2, in Go

package app

type Examples struct{}

func (Examples) Check(a, b, c, d int) bool {
	if a > b && c < d { // code_branch 1, condition 2
		return true
	}
	return false
}

func (Examples) IfElse(x int) int {
	if x > 0 { // code_branch 1
		return 1
	} else { // code_branch 1 — the alternative is a block
		return 2
	}
}

// unit Examples: struct, code_branch 3, condition 2, local_variable 0
```

```go
// branches.go — code_branch

package app

type Branches struct{}

func (Branches) Switch(x int) string {
	switch x {
	case 1: // code_branch 1 — an arm of one value
		return "one"
	case 2, 3: // code_branch 1 — one arm, two values
		return "few"
	default: // code_branch 0
		return "many"
	}
}

func (Branches) TypeSwitch(v any) string {
	switch t := v.(type) { // local_variable 0 — the type-switch guard
	case int: // code_branch 1
		_ = t
	case string, []byte: // code_branch 1 — one arm, two types
		_ = t
	default: // code_branch 0
	}
	return ""
}

func (Branches) Select(a, b chan int) {
	select {
	case x := <-a: // code_branch 1, local_variable 1 (x)
		_ = x
	case <-b: // code_branch 1
	default: // code_branch 0
	}
}

func (Branches) Chain(a, b, c bool) int {
	if a { // code_branch 1
		return 1
	} else if b { // code_branch 1 — the if; its else is an if, so no point
		return 2
	} else if c { // code_branch 1
		return 3
	} else { // code_branch 1 — the alternative is a block
		return 4
	}
}

func (Branches) Loops(xs []int, flag bool) {
	for i := 0; i < 3; i++ { // code_branch 1, local_variable 1 (i)
		noop(i)
	}
	for _, x := range xs { // code_branch 1, local_variable 1 (x); `_` is 0
		noop(x)
	}
	for flag { // code_branch 1
		break
	}
	for { // code_branch 1
		break
	}
}

func noop(_ ...int) {}

// unit Branches: struct, code_branch 14, condition 0, local_variable 3
// unit noop: func, every metric 0 — a variadic `_` parameter is not a variable
```

```go
// conditions.go — condition clauses

package app

type Conditions struct{}

func (Conditions) Both(a, b bool) bool {
	return a && b // condition 2
}

func (Conditions) Either(a, b, c bool) bool {
	return a && b || c // condition 3
}

func (Conditions) Negated(a, b, x bool) bool {
	return !(a || b) && x // condition 3 — flattened through ! and ( )
}

func (Conditions) Compare(x int) bool {
	return x > 1 // condition 0 — a comparison is not a clause
}

func (Conditions) Bits(a, b int) int {
	return a&b | 3 // condition 0 — bitwise operators are not clauses
}

// unit Conditions: struct, condition 8, code_branch 0
```

```go
// inheritance.go — embedding

package app

import "fmt"

type Base struct{}

type Printer struct{}

type Reader interface {
	Read() error
}

type Writer interface {
	Write() error
}

type Ledger struct {
	Base                // inheritance 1
	*Printer            // inheritance 1 — a pointer embedding still embeds
	fmt.Stringer        // inheritance 1, stdlib_coupling 1 (fmt)
	name         string // local_variable 1
}

type ReadWriter interface {
	Reader // inheritance 1
	Writer // inheritance 1
	Close() error
}

type Number interface {
	~int | ~float64 // inheritance 0 — type terms and unions are not embedding
}

type Stringish interface {
	fmt.Stringer // inheritance 1, stdlib_coupling 1 (fmt)
	~string      // inheritance 0
}

// unit Ledger: struct, inheritance 3, local_variable 1, stdlib_coupling 1
// unit ReadWriter: interface, inheritance 2
// unit Number: interface, inheritance 0
// unit Stringish: interface, inheritance 1, stdlib_coupling 1
// units Base, Printer, Reader, Writer: every metric 0
```

```go
// locals.go — local_variable

package app

type Locals struct {
	a, b int    // local_variable 2 — one per name
	c    string // local_variable 1
}

func (l Locals) Body(o any) {
	var x, y = 1, 2    // local_variable 2
	z := x             // local_variable 1
	z, err := split(y) // local_variable 1 — z redeclares, err is new
	if err != nil {    // code_branch 1
		return
	}
	const limit = 10             // local_variable 1
	if s, ok := o.(string); ok { // code_branch 1, local_variable 2 (s, ok)
		_ = s
	}
	switch v := o.(type) { // local_variable 0 — the type-switch guard
	case int: // code_branch 1
		_ = v
	}
	_, _, _ = z, limit, l
}

func split(n int) (int, error) { // local_variable 0 — parameters and results
	return n, nil
}

func named() (n int, err error) { // local_variable 0 — named results
	return 0, nil
}

// unit Locals: struct, local_variable 10, code_branch 3
// units split, named: func, local_variable 0
```

```go
// lambdas.go — func literals

package app

import "sort"

type Lambdas struct{}

func (Lambdas) Wire(xs []int) {
	double := func(x int) int { // lambda 1, local_variable 1 (double)
		return x * 2
	}
	sort.Slice(xs, func(i, j int) bool { // lambda 1, stdlib_coupling 1 (sort)
		return xs[i] < xs[j]
	})
	go func() {}()    // lambda 1
	defer func() {}() // lambda 1
	_ = double
}

func Value(l Lambdas) func([]int) {
	return l.Wire // lambda 0 — a method value is not a func literal
}

// unit Lambdas: struct, lambda 4, local_variable 1, stdlib_coupling 1
// unit Value: func, lambda 0, local_variable 0
```

```go
// units.go — the expected units, in order, with kinds

package app

import "sync"

type A struct{}

type B interface{}

type C int

type D = A

type E func(int) error

func (g G) Early() {}

type (
	F struct{}
	G int
)

func (a A) M() {}

func (c *C) M() {}

func (H) M() {}

func (h H) N() {}

func Free() {}

func init() {}

var v sync.Mutex

const k = 1

// units in order: A struct, B interface, C type, D type, E type, F struct,
// G type, H methods, Free func, init func — ten.
// A sits at the name `A`, F and G at their names inside the group and not at
// the shared `type` keyword; `Early` precedes the group and still bills to G;
// the `methods` unit H sits at the `func` of `func (H) M()`, the first method
// of a receiver with no TypeSpec here.
// `var v sync.Mutex` and `const k = 1` are not units, so `sync` is charged to
// no unit and neither declaration adds a local_variable anywhere.
```

```go
// coupling.go — with InternalPrefixes = ["example.com/app"]

package billing

import (
	"fmt"
	"net/http"
	str "strings"

	_ "embed"

	money "example.com/app/money/v2"
	"example.com/app/shared"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

type Invoice struct {
	ID     uuid.UUID    // local_variable 1, external_coupling 1 (uuid)
	Amount money.Amount // local_variable 1, internal_coupling 1 (money)
	Client *http.Client // local_variable 1, stdlib_coupling 1 (net/http)
}

func (i Invoice) Render(raw []byte) string {
	var cfg struct { // local_variable 1 (cfg)
		Name string // local_variable 1 — a field of an anonymous struct
	}
	_ = yaml.Unmarshal(raw, &cfg) // external_coupling 1 (yaml)
	shared.Format(cfg.Name)       // internal_coupling 1 (shared)
	return fmt.Sprint(i.ID, str.ToUpper(cfg.Name))
}

type Note struct{}

func (Note) Text(fmt interface{ Sprint() string }) string {
	return fmt.Sprint() // stdlib_coupling 0 — the parameter shadows the package
}

type Plain struct{}

var _ = errgroup.Group{}

// unit Invoice: internal_coupling 2 (money, shared), external_coupling 2
//   (uuid, yaml), stdlib_coupling 4 (fmt, net/http, strings, embed),
//   local_variable 5
// unit Note: 0 / 0 / 1 — only the blank `embed` import, which charges every
//   unit; `fmt` here is a parameter, so its Ident resolves and is not the
//   package
// unit Plain: 0 / 0 / 1 — same blank import, nothing else
// `golang.org/x/sync/errgroup` is external and used only by a top-level var,
//   so it is charged to no unit at all.
// Every coupling occurrence sits on its ImportSpec line, above unit Invoice.
```

```go
// coupling_dot.go — a dot import charges every unit

package billing

import (
	. "math"

	"example.com/app/shared"
)

type Invoice struct{}

func (Invoice) Round(v float64) float64 {
	return Floor(v) * shared.Tax() // internal_coupling 1 (shared)
}

type Plain struct{}

// unit Invoice: 1 / 0 / 1 — shared, plus `math` through the dot import
// unit Plain: 0 / 0 / 1 — the dot import charges every unit, because a dot
//   name is indistinguishable from a local
```

```go
// header_only.go — a package clause, an import and a top-level var

package app

import "fmt"

var _ = fmt.Sprint()

// expected: no units, no warnings, no error; `fmt` is charged to no unit
```

```go
// generated.go — a file with a Code-generated header

// Code generated by protoc-gen-go. DO NOT EDIT.

package app

type Message struct {
	Name string
}

func (m Message) Valid() bool {
	if m.Name == "" {
		return false
	}
	return true
}

// expected: no units and no warnings — ast.IsGenerated reports true, so the
// struct field and the `if` are never counted
```

`broken.go.txt` is exactly three lines and no more, so the position the test
pins does not move:

```go
package app

func f( {
```

Expected: no units, exactly one warning `syntax error at 3:9` — the position
`go/parser` reports for `expected ')', found '{'` — and `err == nil`.

`empty.go.txt` is zero bytes. Expected: no units, no warnings, no error.
`go/parser` would report `1:1: expected 'package', found 'EOF'`, so the
analyzer short-circuits a source that holds nothing but whitespace before it
parses.

## Implementation guidance

- Parse with `parser.ParseComments` — `ast.IsGenerated` needs the comments —
  and **without** `parser.SkipObjectResolution`: `Ident.Obj` is what makes the
  import-use rule and the redeclaration rule of FR-7 precise. Silence the
  `staticcheck` deprecation once, at the two places that read `Obj`, with
  `//nolint:staticcheck // syntactic resolution is exactly what a per-file
  analyzer needs`.
- Walk with `ast.Inspect`. Split the counter's `visit` into `countControlFlow`
  and `countDeclaration` from the start, so `funlen` (80 lines / 60 statements)
  and `gocyclo` (20) never bite, with `countElse`, `countCondition`/`clauses`,
  `countEmbedding`, `countDefine`, `countRange`, `countFields` and
  `countFuncLit` as their own small functions.
- A `unitDecl` holds `[]ast.Node` — the `TypeSpec` plus every method billed to
  it — so `measure` walks each node and the cross-cutting invariant test can
  read the declaration ranges back. `units()` returns them in declaration
  order; a `methods` unit is created on first sight of a receiver with no
  `TypeSpec` in the file and ordered by that first method.
- Spans: `fset.Position(n.Pos())` and `fset.Position(n.End())` are 1-based with
  byte columns and an exclusive end — the same contract `treesitter.SpanOf`
  produces, so occurrences compare across languages.
- `TypeSwitchStmt.Assign` is an `AssignStmt` with `token.DEFINE`. Mark it
  consumed when the `TypeSwitchStmt` is visited, before the generic `:=` rule
  sees it, or the guard would charge a `local_variable`.
- `RangeStmt` bindings do **not** carry `Obj.Decl == stmt` (the resolver
  synthesizes an assignment), so count them on the `RangeStmt` node directly
  rather than through the `AssignStmt` rule.
- The receiver unwrap must handle `IndexExpr` and `IndexListExpr`
  (`func (s Stack[T]) Push(…)`, `func (p Pair[K, V]) Key(…)`) as well as
  `StarExpr` and `ParenExpr`.
- Classification order is internal, then stdlib, then external, so a configured
  prefix such as `example.com/app` wins over anything else and a hypothetical
  dotless internal prefix wins over the stdlib rule.
- Inline test helper: `inMethod(src)` wraps a snippet in
  `package p; type Wrapper struct{}; func (Wrapper) M(a, b, c bool, xs []int) { … }`,
  which gives a table test three booleans and a slice without redeclaring them
  per case.
- `newTestAnalyzer` does **not** assert `io.Closer`: there is nothing to close.
  Drop the leak, close-idempotency and grammar tests the Java suite has; keep
  the cancel, determinism and concurrency ones.
- Fixtures under `testdata/` are outside `go build` and the linters but inside
  `gofmt -l -w .`. Keep them gofmt-clean and give an unparsable one the `.txt`
  suffix.

## Deliverables

```
docs/features/07-go-support/
    task.md, test-cases.md           T0 — this plan expanded: FRs, fixtures, TC ids
internal/analyze/golang/
    spec.go, spec_test.go            FR-1, extGo, whole-struct TestSpec (DetectPackages nil'd)
    analyzer.go                      NewAnalyzer, Analyze, parse, syntaxWarning, measure (FR-2, FR-4)
    units.go                         units, unitDecl{name, kind, line, col, nodes},
                                     receiverName, kindOf (FR-3)
    metrics.go                       counter, visit, countControlFlow, countDeclaration,
                                     countElse, countCondition/clauses, countEmbedding,
                                     countDefine, countRange, countFields, countFuncLit
                                     (FR-4 … FR-7)
    imports.go                       modules, assumedName, classify, module.usedBy,
                                     countCoupling (FR-8)
    stdlib.go                        isStdlib — first path segment without a dot
    helper_test.go, analyzer_test.go, units_test.go, metrics_test.go,
    imports_test.go, occurrences_test.go
    testdata/                        the fixtures above (testdata/module/ stays)
internal/languages/languages.go      FR-9, one line
internal/config/templates/cdd.config.yaml.tmpl        FR-1 ripple
internal/config/testdata/golden/greenfield-alpha-beta.yaml,
internal/config/testdata/golden/legacy-gamma-delta.yaml,
cmd/testdata/golden/greenfield-typescript.yaml,
cmd/testdata/golden/greenfield-java-kotlin.yaml,
docs/features/01-init/config-template.yaml, cdd.config.yaml
cmd/init_test.go, cmd/check_test.go  drop the "no analyzer for go yet" expectations
cmd/check_go_test.go                 e2e, mirroring cmd/check_java_test.go (reuse writeGoFixture)
README.md, CONTRIBUTING.md           docs
```

Reuse: `treesitter.SortOccurrences`
(`internal/analyze/internal/treesitter/occurrences.go`), `config.Metrics()` for
the zero counts, the shape of the Java test helpers
(`internal/analyze/java/helper_test.go`, `occurrences_test.go`), and
`writeGoFixture` and `runCdd` from `cmd/init_test.go`. Not reused:
`internal/analyze/internal/jvm` (dot-separated prefixes, a JVM stdlib rule) and
the tree-sitter walk and span helpers.

## Tasks

One commit per task. The task title **is** the commit message, verbatim and
alone: no body, no trailers of any kind — the repository's commit-msg hook
rejects a `Co-Authored-By` line, and nothing else belongs there either. Each
task's acceptance criterion is the set of cases listed under the same heading
in [test-cases.md](test-cases.md).

- **T0 — `docs: add Go support feature spec`**. This file and
  [test-cases.md](test-cases.md). *Accept:* the rules, fixtures and case ids
  here are the ones the following nine commits implement.

- **T1 — `feat: make inheritance applicable to Go`** (FR-1). The spec, the
  template's `inheritance` row and its "unit measured" sentence, then
  `go test ./internal/config -update` and
  `go test ./cmd -run 'TestInitTypeScriptMatchesGolden|TestInitDogfoodConfigReproducible' -update`;
  hand-edit `cmd/testdata/golden/greenfield-java-kotlin.yaml` and
  `docs/features/01-init/config-template.yaml`, and commit the regenerated
  `cdd.config.yaml`. *Accept:* TC-S1 … TC-S7.

- **T2 — `feat: parse Go with go/parser`** (FR-2, and the FR-9 registry line).
  `analyzer.go` returning zero units, the one-line registration, the fixtures
  `broken.go.txt`, `empty.go.txt`, `header_only.go` and `generated.go`;
  `cmd/init_test.go:116-117`, `:140` and `:343` become `NotContains(stderr,
  "no analyzer")` assertions, and `TestCheckSelectedUnavailableLanguage`
  (`cmd/check_test.go:466`) is deleted — the same behaviour is already covered
  by `internal/analyze/run_test.go` with fake languages. *Accept:* TC-P1 …
  TC-P12.

- **T3 — `feat: extract Go units`** (FR-3). `units.go`, the `units.go` fixture.
  *Accept:* TC-U1 … TC-U9.

- **T4 — `feat: count Go branches and conditions`** (FR-5, FR-6).
  *Accept:* `cdd_examples.go`, `branches.go` and `conditions.go` reproduce
  exactly; TC-B1 … TC-B12.

- **T5 — `feat: count Go embedding and locals`** (FR-7 and the `inheritance`
  half of FR-4). *Accept:* `inheritance.go` and `locals.go` reproduce;
  TC-E1 … TC-E15.

- **T6 — `feat: count Go func literals`**. *Accept:* `lambdas.go` reproduces;
  TC-L1 … TC-L5.

- **T7 — `feat: attribute Go imports to the units that use them`** (FR-8).
  `imports.go`, `stdlib.go`, `coupling.go`, `coupling_dot.go`, table tests for
  `assumedName`, `isStdlib` and `classify`. *Accept:* TC-C1 … TC-C13, and the
  end-to-end check below runs.

- **T8 — `test: cover the Go analyzer end to end`**. `occurrences_test.go`
  invariants over every fixture, and `cmd/check_go_test.go`. *Accept:*
  TC-I1 … TC-I11 and TC-X1 … TC-X8.

- **T9 — `docs: describe Go support`**. README: the installation paragraph (the
  Go analyzer is pure Go and adds nothing to the binary), the metric table
  (`inheritance` → all), and a "Language support" rewrite — all four languages
  now have analyzers — with a Go rules paragraph and the limitations: top-level
  `var`/`const` invisible, method values not lambdas, a wrong assumed name
  charged nowhere, `C` classified as stdlib, named results / parameters /
  type-switch guards not variables, generated files skipped, per-file `methods`
  units for split types, `exception_handling` never applicable. CONTRIBUTING:
  reword "as `go` does today" and add that an analyzer need not use
  tree-sitter — Go uses `go/parser`, the same file layout, no grammar pin and
  no resolve-by-name test. Run the smoke test below before this commit and
  record anything it turns up. *Accept:* README and CONTRIBUTING match the
  shipped behaviour.

## Verification

After every task: `make build`, `make test`, `make lint`, `make fmt` (no diff),
then one commit.

End to end after T7:

```sh
go build -o /tmp/cdd .
cd internal/analyze/golang/testdata
/tmp/cdd init --yes --force --languages go --packages example.com/app \
  --metrics code_branch,condition,internal_coupling,external_coupling,stdlib_coupling,inheritance,local_variable,lambda
/tmp/cdd check --all --explain coupling.go     # Invoice: internal 2, external 2, stdlib 4x0.5
/tmp/cdd check --all --explain branches.go     # Branches: code_branch 14
rm cdd.config.yaml
```

Dogfood gate, what CI runs, from the repository root:

```sh
./bin/cdd init --yes --force --languages go --packages github.com/jonasalessi/cdd-lint
git diff --exit-code
./bin/cdd check --all          # smoke: zero syntax warnings over this repository
```

Smoke before T9: `cdd check` over `$(go env GOROOT)/src/net/http` with a
throwaway config. The syntax-warning rate must be zero, and every unit kind and
count must read as credible.

Smoke result (Go 1.26.3 `net/http`, all eight metrics, limit raised out of the
way): 50 files, 499 units, **zero** syntax warnings. The kinds are 311 `func`,
144 `struct`, 22 `interface`, 18 `type` and 4 `methods`, and the heaviest units
are the ones a reader would name — `Transport` 374, `persistConn` 244,
`Request` 192, `conn` 138 — while the median unit scores 3. Five of the 55
non-test files yield no units, each for a documented reason: `h2_bundle.go`,
`socks_bundle.go` and `internal/httpcommon/httpcommon.go` carry a
`Code generated … DO NOT EDIT.` header, `doc.go` holds only a package comment,
and `method.go` holds only top-level `const` declarations. The same run over
this repository reports 410 units and 19 violations with zero warnings; the
violations are this repository's own analyzers against its greenfield limit of
10, which is information rather than a defect of the Go analyzer.

## Definition of done

- [x] `make build`
- [x] `make test` (race detector on)
- [x] `make lint` (including `check-literals`)
- [x] `make fmt` leaves no diff — `testdata/*.go` included
- [x] Coverage ≥ 90 % for `internal/analyze/golang`; no other analyzer package
      drops — measured **98.3 %** of statements
- [x] Every worked fixture above is a checked-in test with the stated totals
- [x] Every case in [test-cases.md](test-cases.md) is a checked-in test, citing
      its id, and passes; TC-X1 … TC-X8 run over every fixture under `testdata/`
- [x] `cdd check` runs clean over this repository with zero syntax warnings,
      and the dogfood `cdd init` output is byte-identical to the committed
      `cdd.config.yaml`
- [x] Nothing outside `internal/analyze/golang`, `languages.go`, the four `cmd`
      assertions, `cmd/check_go_test.go`, the template, the goldens and the
      docs listed under Deliverables changed — verified with
      `git diff --stat`, with two honest deviations:
      `internal/languages/registry_test.go` gained the Go analyzer to its wiring
      expectations, which the Deliverables list did not anticipate, and a
      follow-up commit, `test: rename the init warning test now that Go has an
      analyzer`, renamed one `cmd` test the T2 flip had left with a stale name.

## Suggested order

T0 → T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9. T1 is independent of the
analyzer and may land in either order with T2, but it must land before the
goldens would otherwise conflict. T4 … T7 each depend on T3 and all touch
`metrics.go`, so they land one at a time in the order listed. T8 depends on all
of them, and T9 depends on the smoke test T8 makes possible.
