# Language support

`cdd` ships an analyzer for every language `init` can configure: TypeScript,
Kotlin, Java and Go. All four count the same constructs against the same
[metric vocabulary](../README.md#the-metric-vocabulary), so a mixed project
reads as one report. This page records exactly what each analyzer counts and
what it cannot see.

Every analyzer works one file at a time. That is why same-package references,
which need no import, never add coupling in any language: listing the package
in `internal_coupling.packages` does not change that.

## TypeScript

Only `.ts` and `.tsx` files are read.

| Metric | What counts |
| --- | --- |
| `code_branch` | `if` and a plain `else`, an `else if` charging itself rather than its parent; each `case` of a `switch`, while `default` is free; each ternary; each loop; every `?.`, whether it reads a member, indexes or calls |
| `condition` | `&&`, `\|\|` and `??`, one per clause, flattened through parentheses and `!`, in any expression and not only inside a branch, so `a \|\| b` as a fallback value is 2. `&&=`, `\|\|=` and `??=` count their left-hand side as one more clause |
| `exception_handling` | a `try` block, each `catch` and a `finally`, one each |
| `inheritance` | every parent an `extends` or `implements` clause lists, one each |
| `lambda` | every arrow function and function expression that is not the unit's own body |

An import is charged to `internal_coupling` when it is relative or sits under
one of the configured module prefixes, to `stdlib_coupling` when it names a
Node.js built-in, and to `external_coupling` otherwise. A built-in is any
`node:` specifier and any bare specifier whose first path segment is a
built-in name, so `node:fs`, `fs` and `fs/promises` are the standard library
while `node-fetch` and `lodash/fp` are not.

Known limitations:

- The Deno and Bun standard libraries are not recognised and stay external.
- Browser globals such as `document` and `fetch` are never imported, so they
  are invisible.

## Kotlin

Only `.kt` files are read; `.kts` scripts are not.

A unit is a top-level declaration: a class, interface, enum, object, function
or type alias, or a property whose value is code (a lambda, an anonymous
function, an accessor with a body, or a delegate such as `by lazy`).
Everything nested inside a declaration, companion objects and inner classes
included, bills to it.

| Metric | What counts |
| --- | --- |
| `code_branch` | `if`, each `when` arm with a condition, every `?.`, every loop |
| `condition` | `&&`, `\|\|` and `?:`, one per clause |
| `exception_handling` | a `try` block, each `catch` and a `finally`, one each |
| `inheritance` | every `: Base()` or `: Iface`, one level each |
| `lambda` | every lambda and every bare `::name` reference |

An import is charged to `internal_coupling` when it sits under one of the
configured package prefixes, to `stdlib_coupling` when it is under `kotlin.`
or in a JDK package, and to `external_coupling` otherwise, `kotlinx.*` and
the `javax.*` packages that never shipped with the JDK, such as
`javax.inject`, included. It counts once per unit that mentions the name it
binds, and a star import counts once per unit.

Known limitations:

- `Type::method` references (`String::trim`) parse as member accesses in the
  grammar and are not counted as lambdas; a bare `::name` is.
- Scope functions (`let`, `apply`, `run`, `also`, `with`) are lambdas like any
  other. `lambda` is off by default; a team that turns it on can weigh it.
- The grammar rejects a few layouts that the Kotlin compiler accepts: a class
  member on the same line as the opening brace (`class A { val x = 1 }`), a
  statement starting with an identifier such as `in1`, and a call to a
  function named after a soft keyword (`open(x)`). Such a file gets a
  `syntax error` warning and no units; on a large ktlint-formatted codebase
  that is about one file in a hundred.

## Java

Only `.java` files are read. The grammar is current with the language: all
264 `.java` files of Apache Commons Lang parse without a syntax warning.

A unit is a top-level type, which is a class, interface, enum, record or
annotation type, or the top-level method of a compact source file, which has
no type to bill to. Nested types, methods, constructors and initializers bill
to the type around them. There is no visibility filter, so a package-private
class is a unit like any other.

| Metric | What counts |
| --- | --- |
| `code_branch` | `if` and its `else`, an `else if` charging itself rather than its parent; each `switch` arm that tests a value; each ternary; each loop. `default` is free, and an old-style arm whose labels share one statement list is one arm |
| `condition` | `&&` and `\|\|`, one per clause, flattened through parentheses and `!` |
| `exception_handling` | a `try` block, each `catch` and a `finally`, one each; a multi-catch is one catch |
| `inheritance` | every `extends` and `implements` type, and an anonymous class such as `new Runnable() { … }`; `permits` is not |
| `local_variable` | one per declarator, so `int a, b;` is two; fields, interface constants, declared `try` resources, the binding of a `for (T x : xs)` and record components count; parameters and pattern variables do not |
| `lambda` | lambdas and method references |

An import is charged to `internal_coupling` when it sits under one of the
configured package prefixes, to `stdlib_coupling` when it names a JDK
package, which is `java.*`, `jdk.*` and the `javax.*` packages the JDK ships
such as `javax.crypto`, `javax.swing` and `javax.xml`, and to
`external_coupling` otherwise, so `javax.servlet`, `javax.persistence`,
`javax.inject` and `javafx.*` are third-party. It counts once per unit that
mentions the name it binds, which for a static import is the member, and a
star import counts once per unit.

Known limitations:

- A fully-qualified reference written inline (`java.time.Instant.now()`) has
  no import, so it adds no coupling.
- Bitwise `&` and `|` are not conditions. Their Boolean, non-short-circuit
  meaning cannot be told from the arithmetic one without resolving types, and
  counting `flags & MASK` as a clause would cost more credibility than the
  missed Boolean forms are worth.
- A compact source file's top-level statements are not a unit, so a field
  declared outside every method is invisible, the way a top-level `val` is in
  Kotlin.
- A `switch` arm made of labels alone, with no statement after them at all,
  is not counted: the arm is charged where its statements are, and such an
  arm has none.

## Go

Only `.go` files are read. The analyzer uses `go/parser` from the standard
library, so it needs no cgo. `_test.go` files and `vendor/**` are excluded by
default; drop them from `exclude` to measure them.

A unit is a top-level type with every method billed to it, wherever in the
file the method is written, or a top-level function with no receiver. The
methods of a type another file declares form one `methods` unit per file,
which applies file by file the remedy [cdd.md](cdd.md) section 5 gives for a
type split across files. Nothing nested is a unit, and the kind a unit reports
is what a reader sees at the declaration: `struct`, `interface`, `type`,
`func` or `methods`.

| Metric | What counts |
| --- | --- |
| `code_branch` | `if` and a plain `else`, an `else if` charging itself rather than its parent; each `case` of a `switch`, a type switch or a `select`, however many values it lists, while `default` is free; every `for` in all four of its shapes, and every `range` |
| `condition` | `&&` and `\|\|`, one per clause, flattened through parentheses and `!`. The bitwise `&`, `\|`, `^` and `&^` count nothing |
| `exception_handling` | never applies. `if err != nil` is a branch and nothing more, and `defer`, `recover` and `panic` are worth nothing |
| `inheritance` | an embedded struct field and an embedded interface, one level each. A type set (`~string`, or an `A \| B` union) is a constraint, not a supertype |
| `local_variable` | one per name: the fields of every struct in the unit, anonymous ones included, the names of a `var` or a `const`, the new names of a `:=`, so `z, err := split(y)` charges `err` alone when `z` already exists, and the non-blank bindings of a `range` |
| `lambda` | every func literal, the one behind a `go` or a `defer` included |

An import is charged to each unit that qualifies something by the name it
binds: its alias, or the name goimports would assume for the path, which
reads `gopkg.in/yaml.v3` as `yaml` and `github.com/jackc/pgx/v5` as `pgx`. A
parameter named after a package shadows it and costs nothing, while a `.` or
`_` import binds no name a reference can carry and is charged to every unit
of the file. It counts as `internal_coupling` when it sits under one of the
configured prefixes or under the module path in `go.mod`, as
`stdlib_coupling` when the first element of the path holds no dot, which is
the rule the go command itself resolves against GOROOT, and as
`external_coupling` otherwise, so `golang.org/x/...` is a third-party
dependency like any other.

Known limitations:

- Top-level `var` and `const` declarations are not units, so everything they
  hold is invisible: a func literal assigned to a package-level variable is
  no lambda, and an import only such a declaration uses is charged nowhere.
  It is the hole a top-level `val` leaves in Kotlin and a compact source file
  leaves in Java.
- A method value (`l.Wire`) and a method expression (`Lambdas.Wire`) are not
  lambdas. Without types, a selector that yields a function cannot be told
  from a field access, and guessing from capitalisation would be a heuristic
  rather than a rule.
- An import whose package clause disagrees with the name assumed for its
  path, which is rare and usually written with an alias anyway, matches no
  qualifier and is charged to no unit at all, the same outcome as an unused
  import.
- `import "C"` counts as standard library. The cgo pseudo-package holds no
  dot, so the rule above resolves it like `fmt`.
- Parameters, receivers, named results and the `v` of
  `switch v := x.(type)` are not local variables. A signature is a contract
  rather than a temporary to hold in mind, and the type-switch guard is Go's
  pattern variable, which Java does not count either.
- A generated file, one carrying the `// Code generated … DO NOT EDIT.` line
  the toolchain defines, yields no units and no warning. Counting a bundled
  `.pb.go` would drown every hand-written unit in the report.
- With `--explain`, the occurrence of an `else` spans its block rather than
  the `else` keyword, because `go/ast` keeps no position for the keyword.
