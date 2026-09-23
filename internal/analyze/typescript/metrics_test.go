package typescript

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// probe wraps one expression statement in a unit, so a table case reads as
// the expression it measures.
func probe(expr string) []byte {
	return []byte("export function probe(a: any, b: any, c: any, d: any, x: any): void {\n  " +
		expr + ";\n}\n")
}

// TestConditionClauses pins the clause-counting rule: one ICP per Boolean
// clause, flattened through nested logical operators, parentheses and `!`.
func TestConditionClauses(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"a > 1", 0},
		{"!a", 0},
		{"a && b", 2},
		{"a || b", 2},
		{"a ?? b", 2},
		{"a && b || c", 3},
		{"(a && b) || c", 3},
		{"a && b && c && d", 4},
		{"a || (b && (c || d))", 4},
		{"!(a && b)", 2},
		{"!(a || b) && x", 3},
		{"!a && b", 2},
		{"!!a || b", 2},
		{"(a) && (b)", 2},
		{"typeof a === 'x' && b", 2},
		{"a ||= b", 2},
		{"a &&= b", 2},
		{"a ??= b", 2},
		{"a ||= b && c", 3},
		{"a += b", 0},
		{"f(a && b) || c", 4},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			an := newTestAnalyzer(t)
			res, err := an.Analyze(context.Background(), "probe.ts", probe(c.expr))
			require.NoError(t, err)
			require.Len(t, res.Units, 1)
			requireCount(t, res.Units[0], config.MetricCondition, c.want)
		})
	}
}

// TestOptionalChains pins one branch per `?.`, whichever of the two shapes
// the grammar uses: the named optional_chain node of a member or subscript
// access, or the anonymous `?.` token of an optional call.
func TestOptionalChains(t *testing.T) {
	cases := []struct {
		expr string
		want int
	}{
		{"a.b", 0},
		{"a.b()", 0},
		{"a?.b", 1},
		{"a?.[0]", 1},
		{"a?.b()", 1},
		{"a?.()", 1},
		{"a.b?.()", 1},
		{"a?.b?.()", 2},
		{"a?.[0]?.()", 2},
		{"a?.b?.(1)?.[2]", 3},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			an := newTestAnalyzer(t)
			res, err := an.Analyze(context.Background(), "probe.ts", probe(c.expr))
			require.NoError(t, err)
			require.Len(t, res.Units, 1)
			requireCount(t, res.Units[0], config.MetricCodeBranch, c.want)
		})
	}
}

// TestLoopBindings pins the `for…of` / `for…in` rule on its own: the loop
// declares one local when it carries a `kind` keyword, whatever shape the
// binding has, and none when it assigns to an existing variable.
func TestLoopBindings(t *testing.T) {
	cases := []struct {
		stmt string
		want int
	}{
		{"for (const i of [1]) {}", 1},
		{"for (let i of [1]) {}", 1},
		{"for (var i of [1]) {}", 1},
		{"for (const k in x) {}", 1},
		{"for (let k in x) {}", 1},
		{"for (const [p, q] of [[1, 2]]) {}", 1},
		{"for (const { p, q } of [x]) {}", 1},
		{"for (a of [1]) {}", 0},
		{"for (a in x) {}", 0},
		{"for (a.b of [1]) {}", 0},
	}
	for _, c := range cases {
		t.Run(c.stmt, func(t *testing.T) {
			an := newTestAnalyzer(t)
			res, err := an.Analyze(context.Background(), "probe.ts", probe(c.stmt))
			require.NoError(t, err)
			require.Len(t, res.Units, 1)
			requireCount(t, res.Units[0], config.MetricLocalVariable, c.want)
			requireCount(t, res.Units[0], config.MetricCodeBranch, 1)
		})
	}
}

// TestMetrics checks each metric against its own fixture; the fixtures
// carry the hand-computed contribution of every construct as a comment
// beside it, so the totals below can be re-added by reading the file.
func TestMetrics(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		unit    string
		metric  config.MetricID
		want    int
	}{
		{"branches", "branches.ts", "branches", config.MetricCodeBranch, 14},
		{"optional calls", "branches.ts", "optionalCalls", config.MetricCodeBranch, 9},
		{"conditions", "conditions.ts", "conditions", config.MetricCondition, 32},
		{"conditions branch only once", "conditions.ts", "conditions", config.MetricCodeBranch, 9},
		{"exceptions", "exceptions.ts", "exceptions", config.MetricExceptionHandling, 5},
		{"extends and implements", "inheritance.ts", "Both", config.MetricInheritance, 3},
		{"extends with type arguments", "inheritance.ts", "OnlyExtends", config.MetricInheritance, 1},
		{"interface extends", "inheritance.ts", "Wide", config.MetricInheritance, 3},
		{"no heritage", "inheritance.ts", "None", config.MetricInheritance, 0},
		{"declarators and loop bindings", "locals.ts", "locals", config.MetricLocalVariable, 10},
		{"class fields", "locals.ts", "Fields", config.MetricLocalVariable, 5},
		{"property signatures", "locals.ts", "Props", config.MetricLocalVariable, 0},
		{"lambdas", "lambdas.ts", "lambdas", config.MetricLambda, 3},
		{"the unit's own arrow", "lambdas.ts", "unitArrow", config.MetricLambda, 1},
		{"methods are not lambdas", "lambdas.ts", "Methods", config.MetricLambda, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeFixture(t, c.fixture)
			requireCount(t, unitNamed(t, res, c.unit), c.metric, c.want)
		})
	}
}

// TestDocExamples reproduces the worked examples of docs/cdd.md exactly.
func TestDocExamples(t *testing.T) {
	res := analyzeFixture(t, "cdd_examples.ts")
	cases := []struct {
		unit    string
		metric  config.MetricID
		want    int
		comment string
	}{
		{"docCondition", config.MetricCodeBranch, 1, "1 for the if"},
		{"docCondition", config.MetricCondition, 2, "1 for each Boolean condition"},
		{"docIf", config.MetricCodeBranch, 1, "if = 1"},
		{"docIfElse", config.MetricCodeBranch, 2, "if-else = 2"},
		{"docTryCatchFinally", config.MetricExceptionHandling, 3, "1 for each block"},
	}
	for _, c := range cases {
		t.Run(c.unit+" "+c.comment, func(t *testing.T) {
			requireCount(t, unitNamed(t, res, c.unit), c.metric, c.want)
		})
	}
}
