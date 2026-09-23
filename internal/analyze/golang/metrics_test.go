package golang

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// inMethod wraps a method body in the type Wrapper, so a rule can be pinned
// on the shortest source that shows it. The parameters cover every shape the
// tables below need; an unused one costs nothing, because an inline source
// is parsed and never compiled.
func inMethod(body string) string {
	return "package p\n\ntype Wrapper struct{}\n\n" +
		"func (Wrapper) M(a, b, c bool, x int, xs []int, m map[string]int, o any, ch chan int) {\n" +
		body + "\n}\n"
}

// occurrencesOf returns the occurrences a unit charges for one metric, in
// source order.
func occurrencesOf(u analyze.Unit, metric config.MetricID) []analyze.Occurrence {
	out := make([]analyze.Occurrence, 0, len(u.Occurrences))
	for _, o := range u.Occurrences {
		if o.Metric == metric {
			out = append(out, o)
		}
	}
	return out
}

// branchesOf returns the code_branch occurrences of a unit, in source order.
func branchesOf(u analyze.Unit) []analyze.Occurrence {
	return occurrencesOf(u, config.MetricCodeBranch)
}

// occurrenceTexts returns the source each occurrence of a metric points at,
// so a test can name what a charge landed on rather than spell coordinates.
func occurrenceTexts(t *testing.T, src []byte, u analyze.Unit, metric config.MetricID) []string {
	t.Helper()
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(u.Occurrences))
	for _, o := range occurrencesOf(u, metric) {
		require.Equal(t, o.Line, o.EndLine, "expected an occurrence within one line")
		require.LessOrEqual(t, o.EndCol-1, len(lines[o.Line-1]))
		out = append(out, lines[o.Line-1][o.Col-1:o.EndCol-1])
	}
	return out
}

// requireBranchesAndConditions asserts the two metrics this counter owns on
// the Wrapper unit of an inline source.
func requireBranchesAndConditions(t *testing.T, src string, branches, conditions int) {
	t.Helper()
	wrapper := unitNamed(t, analyzeSource(t, src), "Wrapper")
	requireCount(t, wrapper, config.MetricCodeBranch, branches)
	requireCount(t, wrapper, config.MetricCondition, conditions)
}

// TestDocExamples pins the worked rules of docs/cdd.md section 2 against
// cdd_examples.go (TC-B1, TC-B2, TC-B12).
func TestDocExamples(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.go")
	require.Empty(t, res.Warnings)

	examples := unitNamed(t, res, "Examples")
	requireCount(t, examples, config.MetricCodeBranch, 3)
	requireCount(t, examples, config.MetricCondition, 2)
	requireCount(t, examples, config.MetricExceptionHandling, 0)
}

// TestBranchesFixture pins the code_branch total of branches.go (TC-B12):
// 2 switch arms, 2 type-switch arms, 2 select arms, 4 for the if chain and
// 4 loops. The `noop` function beside it decides nothing.
func TestBranchesFixture(t *testing.T) {
	res := analyzeFixture(t, "branches.go")
	require.Empty(t, res.Warnings)

	branches := unitNamed(t, res, "Branches")
	requireCount(t, branches, config.MetricCodeBranch, 14)
	requireCount(t, branches, config.MetricCondition, 0)
	requireCount(t, branches, config.MetricLocalVariable, 3)

	noop := unitNamed(t, res, "noop")
	requireCount(t, noop, config.MetricCodeBranch, 0)
	requireCount(t, noop, config.MetricCondition, 0)
}

// TestConditionsFixture pins the condition total of conditions.go (TC-B12):
// 2 + 3 + 3 + 0 + 0, and not one branch, since no clause sits in an `if`.
func TestConditionsFixture(t *testing.T) {
	res := analyzeFixture(t, "conditions.go")
	require.Empty(t, res.Warnings)

	conditions := unitNamed(t, res, "Conditions")
	requireCount(t, conditions, config.MetricCondition, 8)
	requireCount(t, conditions, config.MetricCodeBranch, 0)
}

// TestIfChains pins FR-6: an `else` is a branch of its own only when it is a
// block, so an `else if` costs one and not two (TC-B2, TC-B3, TC-B4).
func TestIfChains(t *testing.T) {
	cases := map[string]int{
		"if a {}":                                  1,
		"if a {} else {}":                          2,
		"if a {} else if b {}":                     2,
		"if a {} else if b {} else {}":             3,
		"if a {} else if b {} else if c {}":        3,
		"if a {} else if b {} else if c {} else{}": 4,
		"if a { if b {} else {} } else {}":         4,
		"if x := 1; x > 0 {}":                      1,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t"+src), want, 0)
		})
	}
}

// TestSwitchArms pins that an arm is one branch however many values it
// lists, and that `default` is none (TC-B5).
func TestSwitchArms(t *testing.T) {
	cases := map[string]int{
		"switch x { case 1: }":            1,
		"switch x { case 1: case 2, 3: }": 2,
		"switch x { case 1, 2, 3: }":      1,
		"switch x { default: }":           0,
		"switch x { case 1: default: }":   1,
		"switch { case a: case b: }":      2,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t"+src), want, 0)
		})
	}
}

// TestTypeSwitchArms pins that a type switch counts exactly like a value
// switch, guard or no guard (TC-B6).
func TestTypeSwitchArms(t *testing.T) {
	cases := map[string]int{
		"switch o.(type) { case int: }":                               1,
		"switch t := o.(type) { case int: _ = t }":                    1,
		"switch o.(type) { case int: case string, []byte: default: }": 2,
		"switch o.(type) { default: }":                                0,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t"+src), want, 0)
		})
	}
}

// TestSelectArms pins that a communication clause is one branch whether it
// sends, receives or receives into a new name, and that `default` is none
// (TC-B7).
func TestSelectArms(t *testing.T) {
	cases := map[string]int{
		"select { case <-ch: }":                            1,
		"select { case y := <-ch: _ = y; case <-ch: }":     2,
		"select { case ch <- 1: }":                         1,
		"select { default: }":                              0,
		"select { case <-ch: ; case ch <- 1: ; default: }": 2,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t"+src), want, 0)
		})
	}
}

// TestLoops pins one branch per loop in every shape Go writes one: the three
// clause forms, the bare `for` and every `range` (TC-B8).
func TestLoops(t *testing.T) {
	cases := map[string]int{
		"for i := 0; i < 3; i++ {}":                  1,
		"for i := 0; i < 3; {}":                      1,
		"for a {}":                                   1,
		"for {\n\t\tbreak\n\t}":                      1,
		"for range xs {}":                            1,
		"for _, v := range xs { _ = v }":             1,
		"for k, v := range m { _, _ = k, v }":        1,
		"for i := 0; i < 3; i++ { for range xs {} }": 2,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t"+src), want, 0)
		})
	}
}

// TestConditionClauses pins FR-5: a condition is a Boolean clause, not an
// operator, and a comparison or a bitwise expression is none (TC-B9).
func TestConditionClauses(t *testing.T) {
	cases := map[string]int{
		"a && b":           2,
		"a && b || c":      3,
		"!(a || b) && x":   3,
		"a && (b || !c)":   3,
		"a > 1":            0,
		"xs[0]&1 | 3":      0,
		"!a":               0,
		"a && b && c && x": 4,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			requireBranchesAndConditions(t, inMethod("\t_ = "+src), 0, want)
		})
	}
}

// TestNestedChainsCountOnce pins that the consumed set holds: a chain folded
// into an enclosing one is never charged a second time (TC-B10).
func TestNestedChainsCountOnce(t *testing.T) {
	requireBranchesAndConditions(t, inMethod("\tf(a && b, c || a)"), 0, 4)
	requireBranchesAndConditions(t, inMethod("\tif (a && b) && (c || a) {\n\t}"), 1, 4)
}

// TestStatementsThatAreNotBranches pins what decides nothing (TC-B11). Go
// has no handler construct either, so exception_handling stays 0 while the
// key is still carried.
func TestStatementsThatAreNotBranches(t *testing.T) {
	body := "\tf()\nL:\n\tf()\n\tgoto L\n\tbreak\n\tcontinue\n\tfallthrough\n" +
		"\tgo f()\n\tdefer f()\n\tv, ok := o.(int)\n\t_, _ = v, ok\n" +
		"\t_ = recover()\n\tpanic(\"x\")\n\treturn"
	wrapper := unitNamed(t, analyzeSource(t, inMethod(body)), "Wrapper")
	requireCount(t, wrapper, config.MetricCodeBranch, 0)
	requireCount(t, wrapper, config.MetricCondition, 0)
	requireCount(t, wrapper, config.MetricExceptionHandling, 0)
}

// TestBranchesAndConditionsCombine pins that the two rules add up where a
// reader meets them together: a switch arm that tests a chain, and a chain
// inside a loop.
func TestBranchesAndConditionsCombine(t *testing.T) {
	requireBranchesAndConditions(t, inMethod("\tswitch { case a && b: }"), 1, 2)
	requireBranchesAndConditions(t, inMethod("\tfor a && b {\n\t}"), 1, 2)
	requireBranchesAndConditions(t, inMethod("\tfor range xs {\n\t\tif a && b {\n\t\t}\n\t}"), 2, 2)
}

// TestElseOccurrenceSpansTheBlock pins where an `else` charge points
// (TC-B3): at the block it opens, since go/ast keeps no position for the
// keyword, while the `if` charge starts the statement.
func TestElseOccurrenceSpansTheBlock(t *testing.T) {
	res := analyzeSource(t, inMethod("\tif a {\n\t} else {\n\t}"))
	got := branchesOf(unitNamed(t, res, "Wrapper"))
	require.Len(t, got, 2)
	require.Equal(t, 6, got[0].Line, "the if starts the statement")
	require.Equal(t, 2, got[0].Col)
	require.Equal(t, 7, got[1].Line, "the else charge sits on its block")
	require.Equal(t, 9, got[1].Col, "at the opening brace")
	require.Equal(t, 8, got[1].EndLine, "and ends with the block")
}

// TestClauseOccurrencesSitOnTheLeaves pins that a condition charge points at
// the operand a reader evaluates, `a` rather than `!a` or the whole chain
// (TC-B9).
func TestClauseOccurrencesSitOnTheLeaves(t *testing.T) {
	src := inMethod("\t_ = !(a || b) && c")
	wrapper := unitNamed(t, analyzeSource(t, src), "Wrapper")
	require.Equal(t,
		[]string{"a", "b", "c"},
		occurrenceTexts(t, []byte(src), wrapper, config.MetricCondition))
}

// TestCountsSumTheirOccurrences pins the invariant every unit carries: a raw
// count is the number of places it was charged from.
func TestCountsSumTheirOccurrences(t *testing.T) {
	metrics := []config.MetricID{
		config.MetricCodeBranch,
		config.MetricCondition,
		config.MetricInheritance,
		config.MetricLocalVariable,
		config.MetricLambda,
		config.MetricInternalCoupling,
		config.MetricExternalCoupling,
		config.MetricStdlibCoupling,
	}
	fixtures := []string{
		"cdd_examples.go", "branches.go", "conditions.go",
		"inheritance.go", "locals.go", "lambdas.go",
		"coupling.go", "coupling_dot.go",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			for _, u := range analyzeFixture(t, name, goPrefix).Units {
				for _, metric := range metrics {
					requireCount(t, u, metric, len(occurrencesOf(u, metric)))
				}
			}
		})
	}
}
