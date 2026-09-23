package kotlin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// couplingCase is one unit's expected coupling in a fixture, stated for the
// three coupling metrics at once so none is left implicit (TC-K3).
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

// isCoupling reports whether a metric is charged on an import statement,
// which is the one occurrence that sits outside the unit it belongs to.
func isCoupling(m config.MetricID) bool {
	return m == config.MetricInternalCoupling ||
		m == config.MetricExternalCoupling ||
		m == config.MetricStdlibCoupling
}

// TestIsStdlib (TC-K1, TC-K2) is the Kotlin platform table: the language's
// own library and the JDK are standard library, the separately shipped
// kotlinx tree and the javax libraries outside the JDK are not.
func TestIsStdlib(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"kotlin.collections.List", true},
		{"kotlin.io.path.Path", true},
		{"kotlin.text.Regex", true},
		{"java.time.Instant", true},
		{"javax.crypto.Cipher", true},
		{"kotlinx.coroutines.flow.Flow", false},
		{"kotlinx.serialization.Serializable", false},
		{"javax.inject.Inject", false},
		{"com.acme.X", false},
		{"", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, isStdlib(c.path), c.path)
	}
}

// TestCoupling pins the worked coupling fixture: per-unit attribution by
// binding, alias and star (TC-C1, TC-C2, TC-C3, TC-K3, TC-K4).
func TestCoupling(t *testing.T) {
	res := analyzeFixture(t, "coupling.kt", acmePrefix)
	require.Empty(t, res.Warnings)
	requireCoupling(t, res, []couplingCase{
		{"Invoice", 1, 1, 1}, // Money; the kotlinx star; Instant is the JDK
		{"Note", 1, 1, 0},    // Ledger through its alias L; the star
		{"Plain", 0, 1, 0},   // the star charges every unit
	})
	requireCount(t, unitNamed(t, res, "Invoice"), config.MetricLocalVariable, 1)
	requireCount(t, unitNamed(t, res, "Note"), config.MetricLocalVariable, 1)
}

// TestCouplingWithoutTheStar (TC-C4, TC-K5): the same file minus the star
// import.
func TestCouplingWithoutTheStar(t *testing.T) {
	res := analyzeFixture(t, "coupling_no_star.kt", acmePrefix)
	require.Empty(t, res.Warnings)
	requireCoupling(t, res, []couplingCase{
		{"Invoice", 1, 0, 1},
		{"Note", 1, 0, 0},
		{"Plain", 0, 0, 0},
	})
}

// TestCouplingOccurrencesPointAtTheImport (TC-C5, TC-K6): every coupling
// occurrence sits on an import line, above the unit it is charged to, and
// the JDK import is the standard-library one.
func TestCouplingOccurrencesPointAtTheImport(t *testing.T) {
	res := analyzeFixture(t, "coupling.kt", acmePrefix)
	imports := map[int]bool{3: true, 4: true, 5: true, 6: true}
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
	require.Equal(t, occurrences([]occurrenceAt{
		{config.MetricInternalCoupling, 3, 1, 3, 29, 1},
		{config.MetricStdlibCoupling, 5, 1, 5, 25, 1},
		{config.MetricExternalCoupling, 6, 1, 6, 28, 1},
	}), invoice.Occurrences[:3])
}

// The classification table that used to sit here (TC-C6) moved to
// internal/analyze/internal/jvm as TestIsInternal (TC-R1): it is a JVM rule,
// not a Kotlin one, and no parsing is involved.

// TestCouplingUses pins what counts as a use of an import (TC-C7 … TC-C13,
// TC-K7).
func TestCouplingUses(t *testing.T) {
	res := analyzeFixture(t, "coupling_uses.kt", acmePrefix)
	require.Empty(t, res.Warnings)
	requireCoupling(t, res, []couplingCase{
		{"TypePosition", 1, 0, 0}, // TC-C7, TC-C8: Money and its alias M are one module, used as a type
		{"CallPosition", 1, 0, 0}, // TC-C8: used in a call through the alias
		{"Annotated", 0, 1, 0},    // TC-C8, TC-K7: javax.inject never shipped with the JDK
		{"Untouched", 0, 0, 0},    // TC-C9
		{"Shadowing", 1, 0, 0},    // TC-C10: by name, the shadowing local counts as a use
		{"SamePackage", 0, 0, 0},  // TC-C12: no import, nothing to attribute
		{"InString", 0, 0, 0},     // TC-C13: strings and comments are not identifiers
	})
}

// TestDuplicateImportsAreOneModule (TC-C7): `import a.B` twice is one
// module, charged once.
func TestDuplicateImportsAreOneModule(t *testing.T) {
	res := analyzeSource(
		t,
		"import a.B\nimport a.B\nimport a.B as C\n\nclass U {\n    val b = B()\n    val c = C()\n}\n",
		"a",
	)
	u := unitNamed(t, res, "U")
	requireCount(t, u, config.MetricInternalCoupling, 1)
	require.Len(t, u.Occurrences, 3, "one coupling and two locals")
	require.Equal(t, 1, u.Occurrences[0].Line, "the module points at the first import naming it")
}

// TestNoPrefixesMeansEverythingIsExternal (TC-C11): the analyzer never
// guesses from the file's own package.
func TestNoPrefixesMeansEverythingIsExternal(t *testing.T) {
	res := analyzeSource(t, "package com.acme.app\n\nimport com.acme.shared.Money\n\nclass U(val m: Money)\n")
	u := unitNamed(t, res, "U")
	requireCount(t, u, config.MetricInternalCoupling, 0)
	requireCount(t, u, config.MetricExternalCoupling, 1)
}
