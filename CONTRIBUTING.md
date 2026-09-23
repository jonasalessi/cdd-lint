# Contributing to cdd-lint

## Run the setup first

Clone the repo, then run this once:

```sh
make setup
```

It sets `core.hooksPath` to `.githooks/`, so git runs the two hooks checked
into this repo: `pre-commit` formats and lints your Go files, and
`commit-msg` rejects any commit whose message does not match the format
below. Skip the setup and git will happily accept unformatted code and a bad
message, and the reviewer will ask you to redo both.

## What the hooks do

`pre-commit` runs `gofmt` and `golangci-lint run --fix` on the staged Go
files, re-stages whatever they fixed, and blocks the commit only when an
issue cannot be fixed automatically. It refuses partially staged Go files,
since re-staging those would commit hunks you left out on purpose.

`commit-msg` checks the message against the format in the next section. A
rejected message creates no commit, so fix the message and commit again.

## Commit message format

```
<type>: <description>
```

Allowed types: `feat`, `fix`, `refactor`, `perf`, `docs`, `test`, `build`, `ci`.

```
feat: add init command
fix: handle missing config file
docs: describe ICP calculation
```

The type is lowercase, followed by a colon and one space. Keep each commit
to one change you can describe in a sentence. Commit messages carry no
trailers; in particular, do not add `Co-Authored-By` when a coding
assistant helped with the change.

## Code style and checks

Follow [Effective Go](https://go.dev/doc/effective_go). The `pre-commit`
hook enforces the mechanical part with `gofmt` and `golangci-lint`.

Before opening a pull request, run the definition of done:

```sh
make check
```

It builds, runs the tests with the race detector, lints, and fails if any Go
file is not gofmt-clean. The four targets it chains (`build`, `test`, `lint`,
`fmt`) also run on their own.

`make lint` also runs `make check-literals`, which fails if a language id,
metric id or mode is spelled out as a string outside the places listed in
the next section, or if a language-keyed table appears outside
`internal/analyze/` and `internal/languages/`.

## Adding a language

Every language lives in one directory, `internal/analyze/<id>/`, and is
registered in one file, `internal/languages/languages.go`. No other Go code
knows the language exists: `config`, `detect`, `prompt` and `cmd` all work
from the registered specs they are handed. Two documentation files are
kept by hand; step 4 lists them.

1. Create `internal/analyze/<id>/` with a `spec.go`. The directory name is
   the language id, except that `go` lives in `golang/` because `go` is a
   keyword. The file exports one function returning the language's data:

   ```go
   package rust

   import "github.com/jonasalessi/cdd-lint/internal/config"

   func Spec() config.LanguageSpec {
       return config.LanguageSpec{
           ID:              "rust",
           DisplayName:     "Rust",
           Extensions:      []string{".rs"},
           NotApplicable:   []config.MetricID{config.MetricInheritance},
           DefaultExcludes: []string{"target/**"},
           Descriptions:    map[config.MetricID]string{config.MetricLambda: "closures"},
           PackageExample:  "acme_billing",
           LimitExamples:   []string{`# ".*/adapters/.*": 8`},
           DetectPackages:  detectPackages, // guesses internal prefixes from Cargo.toml
       }
   }
   ```

   `NotApplicable` hides the metrics the analyzer cannot count, and at least
   three must remain. `Descriptions` only lists the metrics whose constructs
   have a language-specific name; the rest use the generic wording. Ids may
   be spelled out as string literals in this file and nowhere else.

2. Add one line to `All()` in `internal/languages/languages.go`:

   ```go
   {Spec: rust.Spec()},
   ```

   A language may ship with a spec and no analyzer: `cdd init` still
   configures it but warns that no analyzer exists yet, and `cdd check`
   reports it as an error rather than counting zero ICPs. Set
   `NewAnalyzer` on the same line once the analyzer exists; the next
   section describes how to write one.

3. Run `make check`. The registry tests fail naming the
   directory if the line is missing, naming the id if the directory is
   missing, and naming the field if the spec is incomplete. The literal
   check fails if the id leaked outside `spec.go`.

4. Update the two hand-maintained lists: the `--languages` row in the
   README's flag table, and the language comments in
   `internal/config/templates/cdd.config.yaml.tmpl`.

### Writing an analyzer

Read the page for the decision in front of you and skip the rest:

- [docs/contributing/analyzer-layout.md](docs/contributing/analyzer-layout.md):
  the `analyze.Analyzer` contract, the file layout every analyzer shares,
  what lives in the shared internals and what stays in the language package,
  and how to parse without tree-sitter.
- [docs/contributing/tree-sitter-grammars.md](docs/contributing/tree-sitter-grammars.md):
  building the grammar once, the resolve-by-name test, the version pins and
  where each grammar comes from. Tree-sitter analyzers only.
- [docs/contributing/stdlib-classification.md](docs/contributing/stdlib-classification.md):
  which imports count as the standard library and where that knowledge lives.

Start from the closest existing analyzer and copy its shape, not its values:
Kotlin or Java for a tree-sitter grammar, Go for a parser from the standard
library.
