package typescript

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
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

// TestUnits pins FR-1 against a fixture holding every unit kind and every
// declaration that must not become a unit.
func TestUnits(t *testing.T) {
	res := analyzeFixture(t, "units.ts")
	require.Empty(t, res.Warnings)
	require.Equal(t, []unitHead{
		{"Exported", unitClass, 4, 8},
		{"Internal", unitClass, 6, 1},
		{"Abstract", unitClass, 8, 8},
		{"Shape", unitInterface, 10, 8},
		{"Color", unitEnum, 14, 8},
		{"Alias", unitType, 18, 8},
		{"exported", unitFunction, 20, 8},
		{"plain", unitFunction, 26, 1},
		{"generated", unitFunction, 28, 8},
		{"arrow", unitFunction, 32, 14},
		{"expr", unitFunction, 34, 14},
		{"first", unitFunction, 36, 14},
		{"second", unitFunction, 37, 3},
		{"overloaded", unitFunction, 49, 1},
		{defaultName, unitClass, 62, 16},
	}, heads(res))
}

// TestUnitsExcluded spells out, one by one, the declarations the fixture
// contains that are not units, so a regression names the case it broke.
func TestUnitsExcluded(t *testing.T) {
	res := analyzeFixture(t, "units.ts")
	for _, name := range []string{
		"nested",       // a function nested in another unit
		"notExported",  // a non-exported arrow constant
		"value",        // an exported constant that is not a function
		"ambient",      // declare function
		"ambientConst", // declare const
		"side",         // declare module
		"helper",       // a re-export
	} {
		require.NotContains(t, unitNames(res), name)
	}
}

// TestExportForms covers the export shapes that are easier to read inline
// than in a fixture.
func TestExportForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []unitHead
	}{
		{"re-export", "export * from \"./x\";\n", []unitHead{}},
		{"named re-export", "export { a } from \"./x\";\n", []unitHead{}},
		{"default value", "export default 42;\n", []unitHead{}},
		{"default arrow", "export default () => {};\n", []unitHead{{defaultName, unitFunction, 1, 16}}},
		{
			"default generator",
			"export default function* () {}\n",
			[]unitHead{{defaultName, unitFunction, 1, 16}},
		},
		{"exported let arrow", "export let h = () => {};\n", []unitHead{{"h", unitFunction, 1, 12}}},
		{"destructured export", "export const [a, b] = f();\n", []unitHead{}},
		{"import alias", "import x = require(\"y\");\n", []unitHead{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newTestAnalyzer(t)
			res, err := a.Analyze(context.Background(), "inline.ts", []byte(c.src))
			require.NoError(t, err)
			require.Empty(t, res.Warnings)
			require.Equal(t, c.want, heads(res))
		})
	}
}

// TestDefaultFunctionUnit covers the other anonymous default export.
func TestDefaultFunctionUnit(t *testing.T) {
	res := analyzeFixture(t, "default_function.ts")
	require.Equal(t, []unitHead{{defaultName, unitFunction, 2, 16}}, heads(res))
}
