---
name: add-language
description: Use when adding a language spec or analyzer to cdd, or when the registry test names a missing directory, id or spec field.
---

Follow the "Adding a language" checklist in `CONTRIBUTING.md`. It is four
steps: a `spec.go` under `internal/analyze/<id>/`, one line in
`internal/languages/languages.go`, `make check`, and the two hand-kept lists.

Read the topic page the step points to and nothing else:

- `docs/contributing/analyzer-layout.md` for the file layout and the
  `analyze.Analyzer` contract every analyzer shares.
- `docs/contributing/tree-sitter-grammars.md` only if the analyzer parses
  with tree-sitter.
- `docs/contributing/stdlib-classification.md` when deciding which imports
  count as the standard library.

Copy the shape of the closest existing analyzer, not its values: Kotlin or
Java for a tree-sitter grammar, Go for a parser from the standard library.
