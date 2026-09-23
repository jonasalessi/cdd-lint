package java

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// acmePrefix is the internal package the coupling fixtures are written
// against, the `internal_coupling.packages` entry of a real project.
const acmePrefix = "com.acme"

// couplingCase is one unit's expected coupling in a fixture, stated for the
// three coupling metrics at once so none is left implicit (TC-J10).
type couplingCase struct {
	unit                       string
	internal, external, stdlib int
}

// requireCoupling asserts the coupling counts of every listed unit.
func requireCoupling(t *testing.T, res analyze.FileResult, cases []couplingCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			u := unitNamed(t, res, c.unit)
			requireCount(t, u, config.MetricInternalCoupling, c.internal)
			requireCount(t, u, config.MetricExternalCoupling, c.external)
			requireCount(t, u, config.MetricStdlibCoupling, c.stdlib)
		})
	}
}

// TestCoupling pins the worked coupling fixture: per-unit attribution by
// binding, by the member a static import binds, and by star (TC-C1, TC-C2,
// TC-C3, TC-C5).
func TestCoupling(t *testing.T) {
	res := analyzeFixture(t, "coupling.java", acmePrefix)
	require.Empty(t, res.Warnings)

	requireCoupling(t, res, []couplingCase{
		{"Invoice", 2, 0, 2}, // Money and rate; Instant and the star are the JDK
		{"Note", 1, 0, 1},    // Ledger; the star only
		{"Plain", 0, 0, 1},   // the star charges every unit
	})
	requireCount(t, unitNamed(t, res, "Invoice"), config.MetricLocalVariable, 1)
	requireCount(t, unitNamed(t, res, "Note"), config.MetricLocalVariable, 1)
}

// TestCouplingWithoutTheStar (TC-C4): the same file minus the star import.
func TestCouplingWithoutTheStar(t *testing.T) {
	res := analyzeFixture(t, "coupling_no_star.java", acmePrefix)
	require.Empty(t, res.Warnings)

	requireCoupling(t, res, []couplingCase{
		{"Invoice", 2, 0, 1},
		{"Note", 1, 0, 0},
		{"Plain", 0, 0, 0},
	})
}

// TestCouplingOccurrencesPointAtTheImport (TC-C7, TC-J7): every coupling
// occurrence sits on an import statement, above the unit it is charged to,
// and the JDK imports are the stdlib ones.
func TestCouplingOccurrencesPointAtTheImport(t *testing.T) {
	src := readFixture(t, "coupling.java")
	res := analyzeFixture(t, "coupling.java", acmePrefix)

	imports := map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true}
	for _, u := range res.Units {
		for _, o := range u.Occurrences {
			if !isCoupling(o.Metric) {
				continue
			}
			require.True(t, imports[o.Line], "unit %q: %+v", u.Name, o)
			require.Less(t, o.Line, u.Line)
			require.Equal(t, 1, o.Col)
		}
	}
	invoice := unitNamed(t, res, "Invoice")
	require.Equal(t,
		[]string{"import com.acme.shared.Money;", "import static com.acme.shared.Rates.rate;"},
		occurrenceTexts(t, src, invoice, config.MetricInternalCoupling))
	require.Equal(t,
		[]string{"import java.time.Instant;", "import java.util.*;"},
		occurrenceTexts(t, src, invoice, config.MetricStdlibCoupling))
	require.Empty(t, occurrenceTexts(t, src, invoice, config.MetricExternalCoupling))
}

// TestStaticImportBindsTheMember (TC-C5): `import static a.b.Rates.rate`
// binds `rate`, so the unit that calls it is charged and the one that names
// only the class holding it is not.
func TestStaticImportBindsTheMember(t *testing.T) {
	src := "import static a.b.Rates.rate;\n\n" +
		"class Caller {\n    int f() {\n        return rate();\n    }\n}\n\n" +
		"class Holder {\n    void g() {\n        Rates.other();\n    }\n}\n"
	res := analyzeSource(t, src, "a")

	requireCount(t, unitNamed(t, res, "Caller"), config.MetricInternalCoupling, 1)
	requireCount(t, unitNamed(t, res, "Holder"), config.MetricInternalCoupling, 0)
}

// TestStaticStarImportChargesEveryUnit (TC-C6): `import static a.b.C.*` is a
// star like any other -- it binds no name the analyzer can see.
func TestStaticStarImportChargesEveryUnit(t *testing.T) {
	res := analyzeSource(t, "import static a.b.C.*;\n\nclass U {\n}\n", "a")

	requireCount(t, unitNamed(t, res, "U"), config.MetricInternalCoupling, 1)
}

// TestDuplicateImportsAreOneModule (TC-C9): the same path imported twice is
// one module, charged once.
func TestDuplicateImportsAreOneModule(t *testing.T) {
	res := analyzeSource(t, "import a.B;\nimport a.B;\n\nclass U {\n    B b;\n}\n", "a")

	u := unitNamed(t, res, "U")
	requireCount(t, u, config.MetricInternalCoupling, 1)
	require.Len(t, u.Occurrences, 2, "one coupling and one field")
	require.Equal(t, 1, u.Occurrences[0].Line, "the module points at the first import naming it")
}

// TestUnusedImportIsChargedToNoUnit (TC-C11): an import no unit mentions
// costs nothing, and its sibling is charged all the same.
func TestUnusedImportIsChargedToNoUnit(t *testing.T) {
	res := analyzeSource(t, "import a.Used;\nimport a.Unused;\n\nclass A {\n    Used u;\n}\n\nclass B {\n}\n", "a")

	requireCount(t, unitNamed(t, res, "A"), config.MetricInternalCoupling, 1)
	requireCount(t, unitNamed(t, res, "B"), config.MetricInternalCoupling, 0)
}

// TestTypeMentionIsAUse (TC-C10): a binding the unit names only as a type or
// as an annotation is a use -- refs hold `type_identifier` as well as
// `identifier`.
func TestTypeMentionIsAUse(t *testing.T) {
	src := "import a.Money;\nimport a.Tag;\n\n" +
		"class U {\n    java.util.List<Money> all;\n\n    @Tag\n    void f() {\n    }\n}\n"
	res := analyzeSource(t, src, "a")

	requireCount(t, unitNamed(t, res, "U"), config.MetricInternalCoupling, 2)
}

// TestShadowingCountsAsAUse (TC-C12, TC-C13): the reference test is by name,
// so a local that shadows an imported name looks like a use, while a name
// written in a string or a comment is no node the walk sees.
func TestShadowingCountsAsAUse(t *testing.T) {
	src := "import a.Money;\nimport a.Tag;\n\n" +
		"class Shadow {\n    int Money = 1;\n}\n\n" +
		"class InString {\n    String s = \"Tag\"; // Tag\n}\n"
	res := analyzeSource(t, src, "a")

	requireCount(t, unitNamed(t, res, "Shadow"), config.MetricInternalCoupling, 1)
	requireCount(t, unitNamed(t, res, "InString"), config.MetricInternalCoupling, 0)
}

// TestReferencesWithoutAnImportAreInvisible (TC-C14) pins the documented
// blind spot: a same-package type and a fully qualified name need no import,
// and an analyzer that reads imports cannot see either.
func TestReferencesWithoutAnImportAreInvisible(t *testing.T) {
	src := "package com.acme.billing;\n\n" +
		"class U {\n    Ledger l;\n\n    long at() {\n        return java.time.Instant.now().toEpochMilli();\n    }\n}\n"
	res := analyzeSource(t, src, acmePrefix)

	u := unitNamed(t, res, "U")
	requireCount(t, u, config.MetricInternalCoupling, 0)
	requireCount(t, u, config.MetricExternalCoupling, 0)
	requireCount(t, u, config.MetricStdlibCoupling, 0)
}

// TestPrefixesClassifyThroughTheAnalyzer (TC-C8, TC-C15, TC-J8) runs the
// internal prefixes end to end through analyze.Options: a prefix matches its
// own path and its dot-subpaths, an empty prefix matches nothing, and the JDK
// import stays standard library however the project is configured -- the
// analyzer never guesses from the file's own package.
func TestPrefixesClassifyThroughTheAnalyzer(t *testing.T) {
	src := "package com.acme.billing;\n\nimport com.acme.shared.Money;\nimport java.util.List;\n\n" +
		"class U {\n    Money amount;\n    List<String> tags;\n}\n"
	cases := []struct {
		name                       string
		prefixes                   []string
		internal, external, stdlib int
	}{
		{"exact package", []string{"com.acme.shared"}, 1, 0, 1},
		{"parent package", []string{acmePrefix}, 1, 0, 1},
		{"the path itself", []string{"com.acme.shared.Money"}, 1, 0, 1},
		{"a longer name", []string{"com.acmecorp"}, 0, 1, 1},
		{"an empty prefix", []string{""}, 0, 1, 1},
		{"no prefixes", nil, 0, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := unitNamed(t, analyzeSource(t, src, c.prefixes...), "U")
			requireCount(t, u, config.MetricInternalCoupling, c.internal)
			requireCount(t, u, config.MetricExternalCoupling, c.external)
			requireCount(t, u, config.MetricStdlibCoupling, c.stdlib)
		})
	}
}
