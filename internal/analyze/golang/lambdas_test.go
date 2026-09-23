package golang

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// requireLambdas asserts the func literals a method body holds and the locals
// it declares beside them, since a literal is often bound to a name.
func requireLambdas(t *testing.T, body string, lambdas, locals int) {
	t.Helper()
	wrapper := unitNamed(t, analyzeSource(t, inMethod("\t"+body)), "Wrapper")
	requireCount(t, wrapper, config.MetricLambda, lambdas)
	requireCount(t, wrapper, config.MetricLocalVariable, locals)
}

// TestFuncLiterals pins one lambda per literal wherever it is written — bound
// to a name, passed as an argument, started with `go`, deferred or nested
// inside another literal (TC-L2, TC-L4).
func TestFuncLiterals(t *testing.T) {
	cases := map[string]struct{ lambdas, locals int }{
		"double := func() {}\n\t_ = double":       {lambdas: 1, locals: 1},
		"f(func() {})":                            {lambdas: 1},
		"go func() {}()":                          {lambdas: 1},
		"defer func() {}()":                       {lambdas: 1},
		"f(func() { g(func() {}) })":              {lambdas: 2},
		"_ = func(i, j int) int { return i + j }": {lambdas: 1},
		"f()": {},
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireLambdas(t, src, want.lambdas, want.locals)
		})
	}
}

// TestGoAndDeferChargeTheLiteral pins where those two charges point (TC-L2):
// at the func literal a reader has to read, not at the statement that
// schedules it.
func TestGoAndDeferChargeTheLiteral(t *testing.T) {
	src := inMethod("\tgo func() {}()\n\tdefer func() {}()")
	wrapper := unitNamed(t, analyzeSource(t, src), "Wrapper")
	require.Equal(t,
		[]string{"func() {}", "func() {}"},
		occurrenceTexts(t, []byte(src), wrapper, config.MetricLambda))
}

// TestLiteralBodyBillsToTheUnit pins that a literal hides nothing: its own
// parameters are no locals, while the branches of its body are the unit's
// like any other statement (TC-L1).
func TestLiteralBodyBillsToTheUnit(t *testing.T) {
	body := "\t_ = func(i, j int) bool {\n\t\tif a {\n\t\t\treturn i < j\n\t\t}\n\t\treturn b\n\t}"
	wrapper := unitNamed(t, analyzeSource(t, inMethod(body)), "Wrapper")
	requireCount(t, wrapper, config.MetricLambda, 1)
	requireCount(t, wrapper, config.MetricCodeBranch, 1)
	requireCount(t, wrapper, config.MetricLocalVariable, 0)
}

// TestMethodValuesAreNotLambdas pins the documented limitation (TC-L3):
// without types a selector that yields a function reads exactly like a field,
// so a method value and a method expression are 0 rather than a guess.
func TestMethodValuesAreNotLambdas(t *testing.T) {
	cases := map[string]string{
		"a method value":      "_ = o.Wire",
		"a method expression": "_ = Wrapper.M",
		"a field read":        "_ = o.field",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			requireLambdas(t, body, 0, 0)
		})
	}
}

// TestTopLevelFuncLiteralsAreInvisible pins TC-L5: a top-level `var` or
// `const` is not a unit, so the literals it holds are charged nowhere.
func TestTopLevelFuncLiteralsAreInvisible(t *testing.T) {
	src := "package p\n\nvar handler = func() {}\n\n" +
		"var (\n\tfirst  = func() {}\n\tsecond = func() int { return 1 }\n)\n\n" +
		"const limit = 10\n\ntype T struct{}\n"
	res := analyzeSource(t, src)

	require.Equal(t, []string{"T"}, unitNames(res))
	requireCount(t, unitNamed(t, res, "T"), config.MetricLambda, 0)
}

// TestLambdasFixture pins lambdas.go (TC-L1, TC-L3): four literals in Wire,
// one local for the name that holds the first, and nothing for the method
// value Value returns.
func TestLambdasFixture(t *testing.T) {
	res := analyzeFixture(t, "lambdas.go")
	require.Empty(t, res.Warnings)

	lambdas := unitNamed(t, res, "Lambdas")
	requireCount(t, lambdas, config.MetricLambda, 4)
	requireCount(t, lambdas, config.MetricLocalVariable, 1)
	requireCount(t, lambdas, config.MetricStdlibCoupling, 1)

	value := unitNamed(t, res, "Value")
	requireCount(t, value, config.MetricLambda, 0)
	requireCount(t, value, config.MetricLocalVariable, 0)
}
