package kotlin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// TestDocExamples pins the worked rules of docs/cdd.md section 2 against
// cdd_examples.kt (TC-B1, TC-B2).
func TestDocExamples(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.kt")
	require.Empty(t, res.Warnings)

	check := unitNamed(t, res, "check")
	requireCount(t, check, config.MetricCodeBranch, 1)
	requireCount(t, check, config.MetricCondition, 2)

	ifElse := unitNamed(t, res, "ifElse")
	requireCount(t, ifElse, config.MetricCodeBranch, 2)
	requireCount(t, ifElse, config.MetricCondition, 0)
}

// TestBranches pins the code_branch and condition totals of branches.kt,
// one function per rule (TC-B3 … TC-B11, TC-B14 … TC-B16).
func TestBranches(t *testing.T) {
	res := analyzeFixture(t, "branches.kt")
	require.Empty(t, res.Warnings)
	cases := []struct {
		unit                string
		branches, condition int
	}{
		{"describe", 2, 0},    // TC-B4: `2, 3 ->` is one entry; `else` is none
		{"chain", 4, 0},       // TC-B3: three ifs and the final else, not six
		{"safe", 2, 2},        // TC-B7: two `?.`, two clauses of `?:`
		{"loops", 4, 0},       // TC-B11: for, for, while, do…while; `!!` is none
		{"subjectless", 2, 0}, // TC-B5: `a > 1` is a comparison, not a clause
		{"Holder", 2, 0},      // TC-B6: `when` as a body and inside a lambda
		{"deep", 3, 0},        // TC-B8: `a?.b?.c?.d`
		{"plain", 0, 0},       // TC-B8: `.` never counts
		{"let", 1, 0},         // TC-B9
		{"bang", 0, 0},        // TC-B10
		{"guardedIf", 2, 2},   // TC-B14
		{"elvisReturn", 0, 2}, // TC-B15: `return` is not a branch
		{"elvisThrow", 0, 2},  // TC-B15: `throw` is not a branch
		{"tryValue", 0, 0},    // TC-B16: `try` is not a branch
	}
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			u := unitNamed(t, res, c.unit)
			requireCount(t, u, config.MetricCodeBranch, c.branches)
			requireCount(t, u, config.MetricCondition, c.condition)
		})
	}
}

// TestConditionClauses pins the clause counting of conditions.kt, one
// function per shape (TC-B12, TC-B13).
func TestConditionClauses(t *testing.T) {
	res := analyzeFixture(t, "conditions.kt")
	require.Empty(t, res.Warnings)
	cases := map[string]int{
		"and2":    2,
		"andOr3":  3,
		"parens4": 4, // `(a && b) || !(c || d)`: parentheses and `!` are transparent
		"not0":    0,
		"eq0":     0,
		"elvis2":  2,
		"elvis3":  3,
		"args4":   4, // TC-B13: two chains in one argument list, nothing counted twice
	}
	for unit, want := range cases {
		t.Run(unit, func(t *testing.T) {
			u := unitNamed(t, res, unit)
			requireCount(t, u, config.MetricCondition, want)
			requireCount(t, u, config.MetricCodeBranch, 0)
		})
	}
}

// TestElseAfterAComment keeps `else // why\n {` on the branch rather than
// on the comment, which the grammar may place between the keyword and the
// block.
func TestElseAfterAComment(t *testing.T) {
	res := analyzeSource(t, "fun f(a: Boolean) {\n    if (a) {\n    } else // why\n    {\n    }\n}\n")
	requireCount(t, unitNamed(t, res, "f"), config.MetricCodeBranch, 2)
}

// TestExceptions pins exception_handling: one per guarded block, catch and
// finally; a library call is not a block (TC-E1 … TC-E4, TC-B16).
func TestExceptions(t *testing.T) {
	examples := analyzeFixture(t, "cdd_examples.kt")
	requireCount(t, unitNamed(t, examples, "guarded"), config.MetricExceptionHandling, 3)

	res := analyzeFixture(t, "exceptions.kt")
	require.Empty(t, res.Warnings)
	requireCount(t, unitNamed(t, res, "twoCatches"), config.MetricExceptionHandling, 3)
	requireCount(t, unitNamed(t, res, "finallyOnly"), config.MetricExceptionHandling, 2)
	caught := unitNamed(t, res, "caught")
	requireCount(t, caught, config.MetricExceptionHandling, 0)

	branches := analyzeFixture(t, "branches.kt")
	tryValue := unitNamed(t, branches, "tryValue")
	requireCount(t, tryValue, config.MetricExceptionHandling, 2)
	requireCount(t, tryValue, config.MetricLocalVariable, 1)
}

// TestInheritance pins inheritance: one per delegation specifier, whatever
// its shape and wherever it nests (TC-I1 … TC-I7).
func TestInheritance(t *testing.T) {
	res := analyzeFixture(t, "inheritance.kt")
	require.Empty(t, res.Warnings)
	cases := []struct {
		unit               string
		inheritance, local int
	}{
		{
			"Ledger",
			3,
			1,
		}, // TC-I1: `: Base(), Auditable, Printer by ConsolePrinter()`; repo is a property, clock a parameter
		{"Auditable", 2, 0}, // TC-I2
		{"Registry", 1, 0},  // TC-I3
		{"Bare", 0, 0},      // TC-I4
		{"Outer", 1, 0},     // TC-I5: the inner class's heritage bills to Outer
		{"Color", 1, 0},     // TC-I6: enum entries are not variables
		{"X", 1, 0},         // TC-I6: type arguments do not add
		// TC-I7: an anonymous object implements the interface as much as a
		// named one does, so its specifier is charged to the enclosing unit.
		{"anonymous", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			u := unitNamed(t, res, c.unit)
			requireCount(t, u, config.MetricInheritance, c.inheritance)
			requireCount(t, u, config.MetricLocalVariable, c.local)
		})
	}
}

// TestLocals pins local_variable: property declarations and `val`/`var`
// constructor parameters, one per declaration (TC-L1 … TC-L11).
func TestLocals(t *testing.T) {
	res := analyzeFixture(t, "locals.kt")
	require.Empty(t, res.Warnings)
	cases := []struct {
		unit  string
		local int
	}{
		{"P", 2},         // TC-L2: `c` is a parameter
		{"body", 4},      // TC-L3: destructuring is one
		{"Members", 3},   // TC-L4: a getter and a delegate still declare
		{"loops", 2},     // TC-L5
		{"Shape", 1},     // TC-L6: `x` is a shape, `y` has an accessor body
		{"Abstract", 1},  // TC-L7: the exemption is for interfaces only
		{"Colors", 0},    // TC-L8
		{"params", 1},    // TC-L9: parameters, lambda parameters, `it`, catch bindings are none
		{"j", 0},         // TC-L10: the unit's own declaration
		{"Companion", 2}, // TC-L11: companion member and init-block local
	}
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			requireCount(t, unitNamed(t, res, c.unit), config.MetricLocalVariable, c.local)
		})
	}
	units := analyzeFixture(t, "units.kt")
	requireCount(t, unitNamed(t, units, "E"), config.MetricLocalVariable, 1) // TC-L1
	requireCount(t, unitNamed(t, units, "C"), config.MetricLocalVariable, 0)
	branches := analyzeFixture(t, "branches.kt")
	requireCount(t, unitNamed(t, branches, "loops"), config.MetricLocalVariable, 2)
}

// TestLambdas pins lambda: literals, anonymous functions and callable
// references, with no scope-function exemption and the property unit's own
// body excluded (TC-F1 … TC-F9, TC-F4, TC-B9, TC-E4).
func TestLambdas(t *testing.T) {
	res := analyzeFixture(t, "lambdas.kt")
	require.Empty(t, res.Warnings)
	cases := []struct {
		unit          string
		lambda, local int
	}{
		{"total", 4, 3},      // TC-F1
		{"onEvent", 0, 0},    // TC-F3: the unit's own body
		{"scopes", 6, 0},     // TC-F2: let, apply, also, run, with, takeIf
		{"lazyValue", 1, 0},  // TC-F4: the lambda is lazy's argument, not the unit's body
		{"nested", 2, 0},     // TC-F5
		{"references", 1, 4}, // TC-F6, TC-F7: see TestCallableReferenceForms
		{"WithLambda", 1, 1}, // TC-F8: only top-level property units skip their body
		{"coroutines", 3, 0}, // TC-F9, TC-E4
	}
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			u := unitNamed(t, res, c.unit)
			requireCount(t, u, config.MetricLambda, c.lambda)
			requireCount(t, u, config.MetricLocalVariable, c.local)
		})
	}
	require.NotContains(t, unitNames(res), "plain")

	branches := analyzeFixture(t, "branches.kt")
	requireCount(t, unitNamed(t, branches, "let"), config.MetricLambda, 1) // TC-B9
	requireCount(t, unitNamed(t, branches, "Holder"), config.MetricLambda, 1)
	units := analyzeFixture(t, "units.kt")
	requireCount(t, unitNamed(t, units, "l"), config.MetricLambda, 1)
	requireCount(t, unitNamed(t, units, "j"), config.MetricLambda, 0)
	requireCount(t, unitNamed(t, units, "anon"), config.MetricLambda, 0)
	exceptions := analyzeFixture(t, "exceptions.kt")
	requireCount(t, unitNamed(t, exceptions, "caught"), config.MetricLambda, 1) // TC-E4
}

// TestCallableReferenceForms (TC-F6, TC-F7) pins what the grammar makes of
// each `::` form. In tree-sitter-kotlin v1.1.0 only the bare `::name`
// parses as a callable_reference; `String::trim`, `this::render` and
// `Foo::class` parse as a navigation_expression and count 0. The grammar
// is ambiguous on the point: the same `String::trim` becomes a
// callable_reference when it is the last thing in the file, so every case
// here is followed by another declaration, the way real code is. A grammar
// bump that settles the first two as references will move their counts to
// 1 and land here, where the change can be accepted deliberately;
// `Foo::class` is a class literal and must stay 0.
func TestCallableReferenceForms(t *testing.T) {
	cases := map[string]int{
		"::render":         1,
		"xs.map(::render)": 1,
		"String::trim":     0,
		"this::render":     0,
		"Foo::class":       0,
	}
	for form, want := range cases {
		t.Run(form, func(t *testing.T) {
			res := analyzeSource(t, "fun f() = "+form+"\nfun g() = 1\n")
			requireCount(t, unitNamed(t, res, "f"), config.MetricLambda, want)
		})
	}
}
