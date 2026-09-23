package golang

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// typeCase is one top-level declaration of a type named T and the two counts
// a declaration carries.
type typeCase struct {
	decl        string
	inheritance int
	locals      int
}

// requireTypeCounts analyzes a top-level declaration of a type named T and
// asserts what it embeds and what it declares.
func requireTypeCounts(t *testing.T, tc typeCase) {
	t.Helper()
	unit := unitNamed(t, analyzeSource(t, "package p\n\nimport \"fmt\"\n\n"+tc.decl+"\n"), "T")
	requireCount(t, unit, config.MetricInheritance, tc.inheritance)
	requireCount(t, unit, config.MetricLocalVariable, tc.locals)
}

// requireLocals asserts the local_variable count of a method body, and the
// branches it opens along the way, since a local often rides on one.
func requireLocals(t *testing.T, body string, locals, branches int) {
	t.Helper()
	wrapper := unitNamed(t, analyzeSource(t, inMethod("\t"+body)), "Wrapper")
	requireCount(t, wrapper, config.MetricLocalVariable, locals)
	requireCount(t, wrapper, config.MetricCodeBranch, branches)
}

// TestStructEmbedding pins that an unnamed field is one point of inheritance
// however its type is written, and that a named field is a local instead
// (TC-E2, TC-E3, TC-E4, TC-E8).
func TestStructEmbedding(t *testing.T) {
	cases := []typeCase{
		{decl: "type T struct{ Base }", inheritance: 1},
		{decl: "type T struct{ *Printer }", inheritance: 1},
		{decl: "type T struct{ fmt.Stringer }", inheritance: 1},
		{decl: "type T struct{ Base[int] }", inheritance: 1},
		{decl: "type T struct{ *List[K, V] }", inheritance: 1},
		{decl: "type T struct{ Base; name string }", inheritance: 1, locals: 1},
		{decl: "type T struct{ a, b int; c string }", locals: 3},
		{decl: "type T struct{ inner struct{ Name string } }", locals: 2},
		{decl: "type T struct{ _ [0]byte }"},
		{decl: "type T struct{}"},
	}
	for _, tc := range cases {
		t.Run(tc.decl, func(t *testing.T) {
			requireTypeCounts(t, tc)
		})
	}
}

// TestInterfaceEmbedding pins that a named embedded interface is one point,
// while a method signature and the terms of a type set are none (TC-E5,
// TC-E6, TC-E7, TC-E14).
func TestInterfaceEmbedding(t *testing.T) {
	cases := []typeCase{
		{decl: "type T interface{ Reader }", inheritance: 1},
		{decl: "type T interface{ Reader; Writer; Close() error }", inheritance: 2},
		{decl: "type T interface{ fmt.Stringer; ~string }", inheritance: 1},
		{decl: "type T interface{ ~int | ~float64 }"},
		{decl: "type T interface{ int | string }"},
		{decl: "type T interface{ ~[]byte }"},
		{decl: "type T interface{ Read(p []byte) (n int, err error) }"},
		{decl: "type T interface{}"},
		{decl: "type T func(a, b int) error"},
	}
	for _, tc := range cases {
		t.Run(tc.decl, func(t *testing.T) {
			requireTypeCounts(t, tc)
		})
	}
}

// TestEmbeddingOccurrencesSitOnTheType pins where an embedding charge points:
// at the type a reader has to go and read, pointer and qualifier included
// (TC-E1, TC-E2, TC-E3).
func TestEmbeddingOccurrencesSitOnTheType(t *testing.T) {
	src := readFixture(t, "inheritance.go")
	ledger := unitNamed(t, analyzeFixture(t, "inheritance.go"), "Ledger")
	require.Equal(t,
		[]string{"Base", "*Printer", "fmt.Stringer"},
		occurrenceTexts(t, src, ledger, config.MetricInheritance))
}

// TestValueSpecLocals pins one local per declared name of a `var` or a
// `const`, grouped or not, and none for the blank one (TC-E10).
func TestValueSpecLocals(t *testing.T) {
	cases := map[string]int{
		"var p, q int":                           2,
		"var p = 1":                              1,
		"const limit = 10":                       1,
		"const one, two = 1, 2":                  2,
		"var (\n\t\tp int\n\t\tq, r string\n\t)": 3,
		"var _ = x":                              0,
		"var _, p = 1, 2":                        1,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireLocals(t, src, want, 0)
		})
	}
}

// TestShortDeclarationLocals pins that `:=` charges the names it introduces
// and not the ones it assigns to: the second `err` of a pair of calls is a
// redeclaration, and the blank identifier declares nothing (TC-E11, TC-E13).
func TestShortDeclarationLocals(t *testing.T) {
	cases := map[string]int{
		"y := 1":                        1,
		"y, z := 1, 2":                  2,
		"_, err := split(x)\n\t_ = err": 1,
		"v1, err := split(x)\n\tv2, err := split(x)\n\t_, _, _ = v1, v2, err": 3,
		"y := 1\n\ty, z := 2, 3\n\t_, _ = y, z":                               2,
		"_ = x":                                                               0,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireLocals(t, src, want, 0)
		})
	}
}

// TestLocalsInStatementHeaders pins the names a statement header introduces:
// an `if` initializer is two locals and one branch, while the guard of a type
// switch is none (TC-E12, TC-E13).
func TestLocalsInStatementHeaders(t *testing.T) {
	cases := map[string]struct{ locals, branches int }{
		"if v, ok := m[\"k\"]; ok {\n\t\t_ = v\n\t}":          {locals: 2, branches: 1},
		"switch v := o.(type) {\n\tcase int:\n\t\t_ = v\n\t}": {locals: 0, branches: 1},
		"switch o.(type) {\n\tcase int:\n\t}":                 {locals: 0, branches: 1},
		"select {\ncase y := <-ch:\n\t\t_ = y\n\t}":           {locals: 1, branches: 1},
		"for i := 0; i < 3; i++ {\n\t\t_ = i\n\t}":            {locals: 1, branches: 1},
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireLocals(t, src, want.locals, want.branches)
		})
	}
}

// TestRangeLocals pins the bindings of a `range`, which the resolver leaves
// without a declaration of their own: `:=` binds them, `=` assigns to names
// that already exist and the blank one binds nothing (TC-E12).
func TestRangeLocals(t *testing.T) {
	cases := map[string]int{
		"for i := range 10 {\n\t\t_ = i\n\t}":         1,
		"for _, v := range xs {\n\t\t_ = v\n\t}":      1,
		"for k, v := range m {\n\t\t_, _ = k, v\n\t}": 2,
		"for range xs {\n\t}":                         0,
		"for x = range xs {\n\t}":                     0,
		"for i := range xs {\n\t\t_ = i\n\t}":         1,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireLocals(t, src, want, 1)
		})
	}
}

// TestSignaturesDeclareNoLocals pins the zero cases of FR-7: what a signature
// names is not a temporary a reader has to carry, and neither is a label
// (TC-E14).
func TestSignaturesDeclareNoLocals(t *testing.T) {
	cases := map[string]string{
		"parameters and results":  "func F(a, b int) (int, error) { return a, nil }",
		"named results":           "func F() (n int, err error) { return 0, nil }",
		"type parameters":         "func F[T any](v T) { _ = v }",
		"a receiver":              "type Recv struct{}\n\nfunc (r Recv) F() {}",
		"func literal parameters": "func F() {\n\t_ = func(i, j int) int { return i + j }\n}",
		"a label":                 "func F() {\nLoop:\n\tgoto Loop\n}",
		"an interface method":     "type Recv interface{ F(a, b int) error }\n\nfunc F() {}",
	}
	for name, decl := range cases {
		t.Run(name, func(t *testing.T) {
			res := analyzeSource(t, "package p\n\n"+decl+"\n")
			for _, unit := range res.Units {
				requireCount(t, unit, config.MetricLocalVariable, 0)
			}
		})
	}
}

// TestInheritanceFixture pins inheritance.go (TC-E1, TC-E5, TC-E6, TC-E7):
// three embeddings in Ledger, two in ReadWriter, one in Stringish, and none
// in the type set of Number or in the four types the others embed. An
// embedded qualified type is both an edge and a use of the package it comes
// from, so Ledger and Stringish carry a stdlib_coupling point as well.
func TestInheritanceFixture(t *testing.T) {
	res := analyzeFixture(t, "inheritance.go")
	require.Empty(t, res.Warnings)

	ledger := unitNamed(t, res, "Ledger")
	requireCount(t, ledger, config.MetricInheritance, 3)
	requireCount(t, ledger, config.MetricLocalVariable, 1)
	requireCount(t, ledger, config.MetricStdlibCoupling, 1)

	requireCount(t, unitNamed(t, res, "ReadWriter"), config.MetricInheritance, 2)
	requireCount(t, unitNamed(t, res, "Number"), config.MetricInheritance, 0)

	stringish := unitNamed(t, res, "Stringish")
	requireCount(t, stringish, config.MetricInheritance, 1)
	requireCount(t, stringish, config.MetricStdlibCoupling, 1)

	for _, name := range []string{"Base", "Printer", "Reader", "Writer"} {
		unit := unitNamed(t, res, name)
		requireCount(t, unit, config.MetricInheritance, 0)
		requireCount(t, unit, config.MetricLocalVariable, 0)
	}
}

// TestLocalsFixture pins locals.go (TC-E15): three fields, two `var` names,
// two new `:=` names, a `const`, and the two names of an `if` initializer,
// while the type-switch guard, the parameters and the named results are none.
func TestLocalsFixture(t *testing.T) {
	res := analyzeFixture(t, "locals.go")
	require.Empty(t, res.Warnings)

	locals := unitNamed(t, res, "Locals")
	requireCount(t, locals, config.MetricLocalVariable, 10)
	requireCount(t, locals, config.MetricCodeBranch, 3)

	for _, name := range []string{"split", "named"} {
		requireCount(t, unitNamed(t, res, name), config.MetricLocalVariable, 0)
	}
}
