package java

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// inMethod wraps a method body in the class Wrapper, so a rule can be pinned
// on the shortest source that shows it.
func inMethod(body string) string {
	return "class Wrapper {\n    void f(boolean a, boolean b, boolean c, int x, Object o) {\n" +
		body + "\n    }\n}\n"
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

// TestDocExamples pins the worked rules of docs/cdd.md section 2 against
// cdd_examples.java (TC-B1, TC-B2, TC-E1).
func TestDocExamples(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.java")
	require.Empty(t, res.Warnings)

	examples := unitNamed(t, res, "Examples")
	requireCount(t, examples, config.MetricCodeBranch, 3)
	requireCount(t, examples, config.MetricCondition, 2)
	requireCount(t, examples, config.MetricExceptionHandling, 3)
	requireCount(t, examples, config.MetricLocalVariable, 0)
}

// TestBranchesFixture pins the code_branch total of branches.java (TC-B12).
// Its two loops declare a name each: the `int i` of the plain `for` and the
// binding of the enhanced one.
func TestBranchesFixture(t *testing.T) {
	res := analyzeFixture(t, "branches.java")
	require.Empty(t, res.Warnings)

	branches := unitNamed(t, res, "Branches")
	requireCount(t, branches, config.MetricCodeBranch, 13)
	requireCount(t, branches, config.MetricCondition, 0)
	requireCount(t, branches, config.MetricLocalVariable, 2)
}

// TestConditionsFixture pins the condition total of conditions.java
// (TC-B13): 2 + 3 + 3 + 0 + 0.
func TestConditionsFixture(t *testing.T) {
	res := analyzeFixture(t, "conditions.java")
	require.Empty(t, res.Warnings)

	conditions := unitNamed(t, res, "Conditions")
	requireCount(t, conditions, config.MetricCondition, 8)
	requireCount(t, conditions, config.MetricCodeBranch, 0)
}

// TestIfChains pins FR-9: an `else` is a branch of its own unless it opens
// another `if` (TC-B2, TC-B3, TC-B4).
func TestIfChains(t *testing.T) {
	cases := map[string]int{
		"if (a) { }":                                          1,
		"if (a) { } else { }":                                 2,
		"if (a) { } else if (b) { }":                          2,
		"if (a) { } else if (b) { } else { }":                 3,
		"if (a) { } else if (b) { } else if (c) { }":          3,
		"if (a) { if (b) { } else { } } else { }":             4,
		"if (a) { } else if (b) { } else if (c) { } else { }": 4,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+src))
			requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricCodeBranch, want)
		})
	}
}

// TestElseOccurrenceStartsAtTheKeyword pins where an `else` charge points
// (TC-B3): at the keyword, not back at the `if` the statement starts with.
func TestElseOccurrenceStartsAtTheKeyword(t *testing.T) {
	res := analyzeSource(t, inMethod("        if (a) {\n        } else {\n        }"))
	got := branchesOf(unitNamed(t, res, "Wrapper"))
	require.Len(t, got, 2)
	require.Equal(t, 3, got[0].Line, "the if starts the statement")
	require.Equal(t, 9, got[0].Col)
	require.Equal(t, 4, got[1].Line, "the else charge starts at the else keyword")
	require.Equal(t, 11, got[1].Col)
	require.Equal(t, 5, got[1].EndLine, "and ends with the branch")
}

// TestSwitchArms pins FR-10 over every shape the grammar produces. An
// old-style arm is spread over one group per label, so a fallthrough is a
// run of label-only groups ending in the group that carries the code; the
// arm is one branch when any label of that run tests a value (TC-B5, TC-B6,
// TC-B7, TC-B8, TC-B9).
func TestSwitchArms(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		branches int
		locals   int
	}{
		{"one label", "switch (x) { case 1: g(); }", 1, 0},
		{"fallthrough", "switch (x) { case 1: case 2: g(); }", 1, 0},
		{"fallthrough with a comment", "switch (x) { case 1: /* falls */ case 2: g(); }", 1, 0},
		{"two arms", "switch (x) { case 1: g(); case 2: g(); }", 2, 0},
		{"default only", "switch (x) { default: g(); }", 0, 0},
		{"case then default", "switch (x) { case 1: default: g(); }", 1, 0},
		{"default then case", "switch (x) { default: case 1: g(); }", 1, 0},
		{"arm then default", "switch (x) { case 1: g(); default: g(); }", 1, 0},
		{"arrow", "switch (x) { case 1 -> g(); case 2, 3 -> g(); default -> g(); }", 2, 0},
		{"arrow as expression", "int y = switch (x) { case 1 -> 1; case 2, 3 -> 2; default -> 3; };", 2, 1},
		{"pattern arms", "switch (o) { case String s -> g(); case Integer i -> g(); default -> g(); }", 2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+c.body))
			u := unitNamed(t, res, "Wrapper")
			requireCount(t, u, config.MetricCodeBranch, c.branches)
			requireCount(t, u, config.MetricLocalVariable, c.locals)
		})
	}
}

// TestFallthroughArmStartsAtItsFirstLabel pins the range of a fallthrough
// arm (TC-B5): it covers the labels a reader has to read together, from the
// first `case` to the end of the statements they share.
func TestFallthroughArmStartsAtItsFirstLabel(t *testing.T) {
	res := analyzeFixture(t, "branches.java")
	got := branchesOf(unitNamed(t, res, "Branches"))
	require.Equal(t, 8, got[0].Line, "the arm of `case 1:`")
	require.Equal(t, 10, got[1].Line, "the fallthrough arm starts at `case 2:`")
	require.Equal(t, 12, got[1].EndLine, "and ends with the statements of `case 3:`")
}

// TestNonBranchingStatements pins what is not a branch (TC-B16, TC-B17). The
// `try` is exception_handling, which lands with T6.
func TestNonBranchingStatements(t *testing.T) {
	cases := map[string]int{
		"return;":                                       0,
		"if (o instanceof String s) { }":                1,
		"boolean t = o instanceof String;":              0,
		"assert a;":                                     0,
		"throw new IllegalStateException();":            0,
		"try { g(); } catch (Exception e) { }":          0,
		"try { while (a) { } } catch (Exception e) { }": 1,
		"outer: while (a) { break outer; }":             1,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+src))
			requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricCodeBranch, want)
		})
	}
}

// TestTernaries pins FR-10's ternary rule (TC-B10): each `? :` is one
// decision, so a nested one is two.
func TestTernaries(t *testing.T) {
	cases := map[string]int{
		"int y = a ? 1 : 2;":         1,
		"int y = a ? 1 : b ? 2 : 3;": 2,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+src))
			requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricCodeBranch, want)
		})
	}
}

// TestLoops pins one branch per loop statement (TC-B11).
func TestLoops(t *testing.T) {
	body := "        for (int i = 0; i < 3; i++) { }\n" +
		"        for (Object e : java.util.List.of()) { }\n" +
		"        while (a) { }\n" +
		"        do { } while (a);"
	res := analyzeSource(t, inMethod(body))
	requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricCodeBranch, 4)
}

// TestConditionClauses pins FR-8 shape by shape: a clause is a leaf operand
// of a chain, parentheses and `!` are transparent, and a comparison or a
// bitwise operator is no clause at all (TC-B13, TC-B14).
func TestConditionClauses(t *testing.T) {
	cases := map[string]int{
		"boolean r = a && b;":           2,
		"boolean r = a && b || c;":      3,
		"boolean r = !(a || b) && c;":   3, // De Morgan: three clauses, not four
		"boolean r = a && (b || c);":    3,
		"boolean r = !a && !b;":         2,
		"boolean r = x > 1;":            0,
		"boolean r = a;":                0,
		"int r = x & 1 | 2 ^ 3;":        0,
		"g(a && b, c || !a);":           4, // TC-B14: nothing counted twice
		"boolean r = a && b && c && a;": 4,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+src))
			requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricCondition, want)
		})
	}
}

// TestGuardedIfCountsBranchAndClauses pins that an `if` and its clauses are
// charged together, and that the `if` occurrence sorts before them (TC-B15).
func TestGuardedIfCountsBranchAndClauses(t *testing.T) {
	res := analyzeSource(t, inMethod("        if (a && b) { g(); } else { g(); }"))
	u := unitNamed(t, res, "Wrapper")
	requireCount(t, u, config.MetricCodeBranch, 2)
	requireCount(t, u, config.MetricCondition, 2)
	require.Equal(t, config.MetricCodeBranch, u.Occurrences[0].Metric, "the if comes first")
	require.Equal(t, config.MetricCondition, u.Occurrences[1].Metric)
}

// TestNestedDeclarationsBillToTheUnit (TC-U5): a branch written inside a
// nested type is the top-level unit's, like everything else nested in it.
func TestNestedDeclarationsBillToTheUnit(t *testing.T) {
	src := "class Outer {\n    static class Inner {\n" +
		"        void m(int v) {\n            if (v > 0) { }\n        }\n    }\n}\n"
	res := analyzeSource(t, src)
	require.Equal(t, []string{"Outer"}, unitNames(res))
	requireCount(t, unitNamed(t, res, "Outer"), config.MetricCodeBranch, 1)
}

// TestExceptionsFixture pins exceptions.java (TC-E4, TC-E5): a
// try-with-resources is one point like a plain `try`, a multi-catch is one
// clause, and only the resource that declares a name is a variable.
func TestExceptionsFixture(t *testing.T) {
	res := analyzeFixture(t, "exceptions.java")
	require.Empty(t, res.Warnings)

	exceptions := unitNamed(t, res, "Exceptions")
	requireCount(t, exceptions, config.MetricExceptionHandling, 2)
	requireCount(t, exceptions, config.MetricLocalVariable, 1)
	requireCount(t, exceptions, config.MetricCodeBranch, 0)
}

// TestTryShapes pins one point per guarded block, per catch and per finally
// (TC-E1, TC-E2, TC-E3, TC-E4, TC-E6). A multi-catch is one recovery path
// however many types lead into it, and a `throw` hands the problem on
// instead of handling it.
func TestTryShapes(t *testing.T) {
	cases := map[string]int{
		"try { g(); } catch (Exception e) { } finally { }":  3,
		"try { g(); } catch (Exception e) { }":              2,
		"try { g(); } finally { }":                          2,
		"try { g(); } catch (A e) { } catch (B e) { }":      3,
		"try { g(); } catch (A | B e) { }":                  2,
		"try (var in = open()) { } catch (Exception e) { }": 2,
		"throw new IllegalStateException();":                0,
		"try { try { g(); } finally { } } finally { }":      4,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, inMethod("        "+src))
			requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricExceptionHandling, want)
		})
	}
}

// TestThrowsClauseIsNotHandling pins TC-E6: declaring that a method throws
// says who handles the failure, it does not handle it.
func TestThrowsClauseIsNotHandling(t *testing.T) {
	src := "class Wrapper {\n    void f() throws java.io.IOException {\n" +
		"        throw new java.io.IOException();\n    }\n}\n"
	res := analyzeSource(t, src)
	requireCount(t, unitNamed(t, res, "Wrapper"), config.MetricExceptionHandling, 0)
}

// TestTryOccurrenceSitsOnTheGuardedBlock pins TC-E1: the try charge points
// at the block it guards, so its range stops before the catch charged next
// to it instead of swallowing the whole statement.
func TestTryOccurrenceSitsOnTheGuardedBlock(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.java")
	got := occurrencesOf(unitNamed(t, res, "Examples"), config.MetricExceptionHandling)
	require.Len(t, got, 3)
	require.Equal(t, 12, got[0].Line, "the guarded block opens on the try line")
	require.Equal(t, 13, got[0].Col, "at the brace, not at the `try` keyword")
	require.Equal(t, 14, got[0].EndLine, "and ends where the catch begins")
	require.Equal(t, 14, got[1].Line, "the catch clause")
	require.Equal(t, 16, got[2].Line, "the finally clause")
}

// TestInheritanceFixture pins inheritance.java unit by unit (TC-E7 … TC-E12):
// one point per supertype, wherever the heritage is written and whatever
// declares it.
func TestInheritanceFixture(t *testing.T) {
	res := analyzeFixture(t, "inheritance.java")
	require.Empty(t, res.Warnings)

	ledger := unitNamed(t, res, "Ledger")
	requireCount(t, ledger, config.MetricInheritance, 3)
	require.Equal(t, []string{"Base", "Auditable", "Printer"},
		occurrenceTexts(t, readFixture(t, "inheritance.java"), ledger, config.MetricInheritance))

	requireCount(t, unitNamed(t, res, "Auditable"), config.MetricInheritance, 2)

	level := unitNamed(t, res, "Level")
	requireCount(t, level, config.MetricInheritance, 1)
	requireCount(t, level, config.MetricLocalVariable, 0)

	money := unitNamed(t, res, "Money")
	requireCount(t, money, config.MetricInheritance, 1)
	requireCount(t, money, config.MetricLocalVariable, 2)

	requireCount(t, unitNamed(t, res, "Shape"), config.MetricInheritance, 0)
}

// TestAnonymousClassIsInheritance pins TC-E11: an anonymous class implements
// its interface as much as a named class does, so it is one supertype and no
// lambda, and the charge names the type a reader has to look up.
func TestAnonymousClassIsInheritance(t *testing.T) {
	res := analyzeFixture(t, "inheritance.java")

	factory := unitNamed(t, res, "Factory")
	requireCount(t, factory, config.MetricInheritance, 1)
	requireCount(t, factory, config.MetricLocalVariable, 1)
	requireCount(t, factory, config.MetricLambda, 0)
	require.Equal(t, []string{"Runnable"},
		occurrenceTexts(t, readFixture(t, "inheritance.java"), factory, config.MetricInheritance))
}

// TestInheritanceShapes pins what does and does not make an edge (TC-E13,
// TC-E14, TC-E15).
func TestInheritanceShapes(t *testing.T) {
	cases := map[string]int{
		"class Outer { }": 0,
		"class Outer { class In extends Base { } }":                     1,
		"class Outer { void f() { new Runnable() { }; } }":              1,
		"class Outer { void f() { new Runnable() { }; new A() { }; } }": 2,
		"class Outer { void f() { Object o = new Object(); } }":         0,
		"class Outer implements A, B { }":                               2,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, src)
			requireCount(t, unitNamed(t, res, "Outer"), config.MetricInheritance, want)
		})
	}
}

// TestLocalsFixture pins locals.java (TC-E16 … TC-E20): a declarator is one
// variable wherever it is written, and an enum constant is not one.
func TestLocalsFixture(t *testing.T) {
	res := analyzeFixture(t, "locals.java")
	require.Empty(t, res.Warnings)

	locals := unitNamed(t, res, "Locals")
	requireCount(t, locals, config.MetricLocalVariable, 6)
	requireCount(t, locals, config.MetricCodeBranch, 1)
	requireCount(t, locals, config.MetricExceptionHandling, 2)

	requireCount(t, unitNamed(t, res, "Consts"), config.MetricLocalVariable, 1)
	requireCount(t, unitNamed(t, res, "Color"), config.MetricLocalVariable, 0)
}

// TestLocalShapes pins what declares a variable and what only names one
// (TC-E17, TC-E18, TC-E20, TC-E21, TC-E23). A pattern variable and a catch
// parameter are the reading of a value the branch already charged for.
func TestLocalShapes(t *testing.T) {
	cases := map[string]int{
		"class W { void f() { int x = 1, y = 2; } }":                      2,
		"class W { void f() { var z = 1; } }":                             1,
		"class W { void f(Object o) { if (o instanceof String s) { } } }": 0,
		"class W { void f() { try { } catch (Exception e) { } } }":        0,
		"class W { void f(int a, String... xs) { } }":                     0,
		"class W { W(int a) { } }":                                        0,
		"class W { Runnable r = () -> { }; void f() { g(x -> x); } }":     1,
		"class W { class In { int f; } }":                                 1,
		"class W { { int local = 1; } }":                                  1,
		"enum W { A { void m() { int q = 1; } } }":                        1,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, src)
			requireCount(t, unitNamed(t, res, "W"), config.MetricLocalVariable, want)
		})
	}
}

// TestRecordComponentsAreVariables pins TC-E10: a component is a field with
// a shorter spelling, and a method's parameters next to it still are not.
func TestRecordComponentsAreVariables(t *testing.T) {
	src := "record R(int amount, String currency) {\n    int scaled(int factor) {\n" +
		"        return amount * factor;\n    }\n}\n"
	res := analyzeSource(t, src)
	requireCount(t, unitNamed(t, res, "R"), config.MetricLocalVariable, 2)
}

// TestLoopBindingOccurrence pins TC-E22: the enhanced `for` charge points at
// the name the loop declares, not at the whole statement.
func TestLoopBindingOccurrence(t *testing.T) {
	src := "class W { void f(java.util.List<String> xs) { for (String item : xs) { } } }\n"
	res := analyzeSource(t, src)
	w := unitNamed(t, res, "W")
	requireCount(t, w, config.MetricLocalVariable, 1)
	require.Equal(t, []string{"item"},
		occurrenceTexts(t, []byte(src), w, config.MetricLocalVariable))
}

// TestLambdasFixture pins lambdas.java (TC-L1): a lambda expression and a
// method reference are one function value each, and the name each one is
// assigned to is still a variable.
func TestLambdasFixture(t *testing.T) {
	res := analyzeFixture(t, "lambdas.java")
	require.Empty(t, res.Warnings)

	lambdas := unitNamed(t, res, "Lambdas")
	requireCount(t, lambdas, config.MetricLambda, 4)
	requireCount(t, lambdas, config.MetricLocalVariable, 4)
}

// TestLambdaShapes pins the forms that are function values and the ones that
// only look like them (TC-L2, TC-L3, TC-L4, TC-L7).
func TestLambdaShapes(t *testing.T) {
	cases := map[string]int{
		"class W { void f() { g(x -> x * 2); } }":                               1,
		"class W { void f() { g(x -> { h(x); }); } }":                           1,
		"class W { void f() { g(() -> { }); } }":                                1,
		"class W { void f() { xs.forEach(x -> ys.forEach(y -> use(x, y))); } }": 2,
		"class W { void f() { g(String::valueOf); } }":                          1,
		"class W { void f() { g(ArrayList::new); } }":                           1,
		"class W { void f() { g(this::m); } }":                                  1,
		"class W { void f() { g(super::toString); } }":                          1,
		"class W { void f() { g(Map.Entry::getKey); } }":                        1,
		"class W { void f() { g(Foo.class); h((Runnable) o); } }":               0,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			res := analyzeSource(t, src)
			requireCount(t, unitNamed(t, res, "W"), config.MetricLambda, want)
		})
	}
}

// TestLambdaBodyBillsToTheUnit pins TC-L4: a block body is one lambda, and
// the branches written inside it belong to the unit that holds the lambda,
// which is the only unit a Java file has to charge them to.
func TestLambdaBodyBillsToTheUnit(t *testing.T) {
	src := "class W { void f() { g(x -> { if (x > 0) { h(x); } }); } }\n"
	res := analyzeSource(t, src)

	w := unitNamed(t, res, "W")
	requireCount(t, w, config.MetricLambda, 1)
	requireCount(t, w, config.MetricCodeBranch, 1)
}

// TestAnonymousClassIsNoLambda pins TC-L5: an anonymous class names the type
// it implements, which a lambda never does, so it is inheritance.
func TestAnonymousClassIsNoLambda(t *testing.T) {
	src := "class W { void f() { g(new Runnable() { public void run() { } }); } }\n"
	res := analyzeSource(t, src)

	w := unitNamed(t, res, "W")
	requireCount(t, w, config.MetricLambda, 0)
	requireCount(t, w, config.MetricInheritance, 1)
}

// TestLambdaOnAFieldIsCounted pins TC-L6: Java has no property unit, so a
// lambda initializing a field is charged like any other, next to the
// variable it is assigned to.
func TestLambdaOnAFieldIsCounted(t *testing.T) {
	src := "class W { Runnable r = () -> { }; }\n"
	res := analyzeSource(t, src)

	w := unitNamed(t, res, "W")
	requireCount(t, w, config.MetricLambda, 1)
	requireCount(t, w, config.MetricLocalVariable, 1)
}

// TestLambdaParametersAreNotVariables pins TC-E21 for lambdas: a parameter
// names a value the caller already had, whether a method or a lambda
// receives it.
func TestLambdaParametersAreNotVariables(t *testing.T) {
	src := "class W { void f() { g((a, b) -> a + b); } }\n"
	res := analyzeSource(t, src)

	w := unitNamed(t, res, "W")
	requireCount(t, w, config.MetricLambda, 1)
	requireCount(t, w, config.MetricLocalVariable, 0)
}
