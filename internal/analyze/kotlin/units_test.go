package kotlin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// unitHead is the identity of a unit, without its counts.
type unitHead struct {
	Name string
	Kind string
	Line int
	Col  int
}

// heads projects a result onto the unit identities, in source order.
func heads(res analyze.FileResult) []unitHead {
	out := make([]unitHead, 0, len(res.Units))
	for _, u := range res.Units {
		out = append(out, unitHead{Name: u.Name, Kind: u.Kind, Line: u.Line, Col: u.Col})
	}
	return out
}

// TestUnits pins FR-4 against units.kt: names, kinds and positions in
// source order (TC-U1, TC-U2, TC-U3, TC-U4, TC-U5, TC-U7, TC-U9).
func TestUnits(t *testing.T) {
	res := analyzeFixture(t, "units.kt")
	require.Empty(t, res.Warnings)
	require.Equal(t, []unitHead{
		{"A", unitClass, 3, 1},
		{"B", unitInterface, 4, 1},
		{"C", unitEnum, 5, 6},   // TC-U4: `class`, not `enum`
		{"D", unitClass, 6, 8},  // `class`, not `sealed`
		{"E", unitClass, 7, 6},  // TC-U4: `class`, not `data`
		{"F", unitClass, 8, 12}, // `class`, not `annotation`
		{"G", unitObject, 9, 1},
		{"h", unitFunction, 10, 1},
		{"shout", unitFunction, 11, 1}, // TC-U5: the receiver is not part of the name
		{"I", unitTypeAlias, 12, 1},
		{"j", unitProperty, 13, 1}, // TC-U4: at `val`
		{"k", unitProperty, 14, 1},
		{"l", unitProperty, 15, 1},
		{"m", unitFunction, 16, 9}, // TC-U4: `fun`, not `private`; TC-U9: no visibility filter
		{"O", unitClass, 18, 1},
		{"S", unitInterface, 26, 8},  // TC-U3: sealed interface
		{"Fn", unitInterface, 27, 1}, // TC-U3: fun interface; `fun` is a keyword here, not a modifier
		{"flat", unitFunction, 30, 1},
		{"anon", unitProperty, 36, 1},
		{"w", unitProperty, 37, 1},
		{"Q", unitClass, 39, 10},
		{"T", unitClass, 40, 11},
	}, heads(res))
}

// TestUnitsExcluded spells out, one by one, the declarations units.kt
// contains that are not units, so a regression names the case it broke
// (TC-U1, TC-U6).
func TestUnitsExcluded(t *testing.T) {
	res := analyzeFixture(t, "units.kt")
	for _, name := range []string{
		"n",     // a top-level property holding a plain value
		"cfg",   // a call initializer
		"s",     // a string template
		"o",     // an object expression
		"P",     // an inner class, billed to O
		"q",     // a companion member
		"R",     // a nested object
		"local", // a local function
	} {
		require.NotContains(t, unitNames(res), name)
	}
}

// TestUnitsAreInSourceOrder (TC-U10): Line is strictly increasing across
// the units of every fixture.
func TestUnitsAreInSourceOrder(t *testing.T) {
	for _, name := range fixtureNames(t) {
		res := analyzeFixture(t, name)
		for i := 1; i < len(res.Units); i++ {
			require.Greater(t, res.Units[i].Line, res.Units[i-1].Line, "%s: %v", name, unitNames(res))
		}
	}
}

// TestPropertyUnitForms (TC-U6, TC-U7) covers each initializer that makes
// a top-level property a unit and each that does not, inline.
func TestPropertyUnitForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []unitHead
	}{
		{"lambda", "val f = { 1 }\n", []unitHead{{"f", unitProperty, 1, 1}}},
		{"anonymous function", "val f = fun(a: Int) = a\n", []unitHead{{"f", unitProperty, 1, 1}}},
		{"getter body", "val f: Int\n    get() = 1\n", []unitHead{{"f", unitProperty, 1, 1}}},
		{"setter body", "var f: Int = 0\n    set(v) { field = v }\n", []unitHead{{"f", unitProperty, 1, 1}}},
		{"delegate", "val f by lazy { 1 }\n", []unitHead{{"f", unitProperty, 1, 1}}},
		{"private lambda", "private val f = { 1 }\n", []unitHead{{"f", unitProperty, 1, 9}}},
		{"plain value", "val n = 4\n", []unitHead{}},
		{"call", "val cfg = load()\n", []unitHead{}},
		{"string template", "val s = \"$n\"\n", []unitHead{}},
		{"object expression", "val o = object : Runnable {\n    override fun run() {}\n}\n", []unitHead{}},
		{"visibility-only accessor", "var f: Int = 0\n    private set\n", []unitHead{}},
		{"typed without value", "lateinit var f: String\n", []unitHead{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeSource(t, c.src)
			got := heads(res)
			if got == nil {
				got = []unitHead{}
			}
			require.Equal(t, c.want, got)
		})
	}
}

// TestNestedDeclarationsBillToTheEnclosingUnit (TC-U8): class O is one
// unit and the nesting below it produces none.
func TestNestedDeclarationsBillToTheEnclosingUnit(t *testing.T) {
	res := analyzeSource(
		t,
		"class O {\n    inner class P\n    companion object {\n        fun q() {}\n    }\n    object R\n    fun s() { fun local() {} }\n}\n",
	)
	require.Equal(t, []unitHead{{"O", unitClass, 1, 1}}, heads(res))
	for _, m := range config.Metrics() {
		requireCount(t, unitNamed(t, res, "O"), m, 0)
	}
}

// TestVisibilityNeverFilters (TC-U9): every top-level visibility yields a
// unit, and a protected member bills to its class.
func TestVisibilityNeverFilters(t *testing.T) {
	res := analyzeSource(
		t,
		"private class A\ninternal class B\npublic class C\nclass D {\n    protected fun f() {\n        if (x) {}\n    }\n}\n",
	)
	require.Equal(t, []unitHead{
		{"A", unitClass, 1, 9},
		{"B", unitClass, 2, 10},
		{"C", unitClass, 3, 8},
		{"D", unitClass, 4, 1},
	}, heads(res))
	requireCount(t, unitNamed(t, res, "D"), config.MetricCodeBranch, 1)
}

// TestTopLevelCompanionObject (TC-U11): `companion object` outside a class
// is not Kotlin. The grammar does not produce a companion_object node at
// the top level; it reads the three words as an infix_expression and
// parses on without error, so the file yields neither a unit nor a
// warning. A real top-level object is a unit.
func TestTopLevelCompanionObject(t *testing.T) {
	res := analyzeSource(t, "object Real {\n    val x = 1\n}\n")
	require.Equal(t, []unitHead{{"Real", unitObject, 1, 1}}, heads(res))

	loose := analyzeSource(t, "companion object Loose\n")
	require.Empty(t, loose.Units, "an infix expression is not a declaration")
}
