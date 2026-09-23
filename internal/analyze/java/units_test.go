package java

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

// TestUnits pins FR-3 against units.java: names, kinds and positions in
// source order (TC-U1, TC-U2, TC-U3).
func TestUnits(t *testing.T) {
	res := analyzeFixture(t, "units.java")
	require.Empty(t, res.Warnings)
	require.Equal(t, []unitHead{
		{"A", unitClass, 3, 8}, // TC-U3: at `class`, not `public`
		{"B", unitInterface, 4, 1},
		{"C", unitEnum, 5, 1},
		{"D", unitRecord, 6, 1},     // TC-U3: at `record`
		{"E", unitAnnotation, 7, 1}, // TC-U3: at `@interface`
		{"F", unitClass, 8, 10},     // TC-U3: at `class`, not `abstract`
		{"G", unitClass, 10, 7},     // TC-U3: at `class`, not `final`
	}, heads(res))
}

// TestUnitsExcluded spells out, one by one, the declarations units.java
// contains that are not units, so a regression names the case it broke
// (TC-U1).
func TestUnitsExcluded(t *testing.T) {
	res := analyzeFixture(t, "units.java")
	for _, name := range []string{
		"Inner",  // a nested static class, billed to G
		"Nested", // a nested inner class
		"Shape",  // a nested interface
		"Kind",   // a nested enum
		"R",      // a nested record
		"m",      // a member method: only a compact file's method is a unit
		"Local",  // a class declared inside a method body
	} {
		require.NotContains(t, unitNames(res), name)
	}
}

// TestCompactSourceFile (TC-U4): the top-level method of a Java 25 compact
// source file is the file's only unit; the top-level statement above it is
// not one and its complexity stays invisible.
func TestCompactSourceFile(t *testing.T) {
	res := analyzeFixture(t, "compact.java")
	require.Empty(t, res.Warnings)
	require.Equal(t, []unitHead{{"main", unitMethod, 3, 1}}, heads(res))
	main := unitNamed(t, res, "main")
	requireCount(t, main, config.MetricLocalVariable, 0)
	requireCount(t, main, config.MetricCodeBranch, 1)
}

// TestVisibilityNeverFilters (TC-U6): every top-level visibility yields a
// unit, and a private nested class is still not one.
func TestVisibilityNeverFilters(t *testing.T) {
	res := analyzeSource(t, "public class A {}\nfinal class B {}\nclass C {\n    private class Hidden {}\n}\n")
	require.Equal(t, []unitHead{
		{"A", unitClass, 1, 8},
		{"B", unitClass, 2, 7},
		{"C", unitClass, 3, 1},
	}, heads(res))
	require.NotContains(t, unitNames(res), "Hidden")
}

// TestDeclarationsThatAreNotUnits (TC-U8): a file holding only a package
// declaration and its imports has nothing to measure.
func TestDeclarationsThatAreNotUnits(t *testing.T) {
	res := analyzeSource(t, "package com.acme.app;\n\nimport java.util.List;\n")
	require.Empty(t, res.Units)
}

// TestNameIgnoresTypeParametersAndAnnotations (TC-U9, TC-U10): the name is
// the `name` field alone, and an annotation never moves the position.
func TestNameIgnoresTypeParametersAndAnnotations(t *testing.T) {
	res := analyzeSource(t, "class Box<T extends Number> {}\n@Deprecated @SuppressWarnings(\"x\") class H {}\n")
	require.Equal(t, []unitHead{
		{"Box", unitClass, 1, 1},
		{"H", unitClass, 2, 36},
	}, heads(res))
}

// TestUnitsCarryEveryMetric: every unit is reported with a key for every
// metric, enabled or not. units.java declares nothing but shapes, so its
// only charges are the record components — the one of `D`, and the one of
// the record nested in `G`, which bills to the unit holding it (TC-U5).
func TestUnitsCarryEveryMetric(t *testing.T) {
	res := analyzeFixture(t, "units.java")
	require.Len(t, res.Units, 7)

	components := map[string]int{"D": 1, "G": 1}
	for _, u := range res.Units {
		for _, m := range config.Metrics() {
			want := 0
			if m == config.MetricLocalVariable {
				want = components[u.Name]
			}
			requireCount(t, u, m, want)
		}
	}
}
