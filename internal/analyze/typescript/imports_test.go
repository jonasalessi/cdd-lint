package typescript

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// appPrefix is the configured internal prefix of the coupling fixture.
const appPrefix = "@app/"

// TestCoupling pins FR-6: imports are file-level, coupling is per unit, and
// a module is charged to a unit only when the unit mentions one of its
// bindings. The expected numbers are written next to each unit in the
// fixture.
func TestCoupling(t *testing.T) {
	res := analyzeFixture(t, "coupling.ts", appPrefix)
	require.Empty(t, res.Warnings)
	cases := []struct {
		unit                       string
		internal, external, stdlib int
	}{
		{"UsesInternal", 2, 1, 0},
		{"usesExternal", 1, 2, 1},
		{"usesBoth", 2, 1, 0},
		{"Wrapper", 3, 1, 0},
		{"renderer", 1, 3, 0},
		{"Untouched", 1, 1, 0},
		{"usesRequire", 2, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.unit, func(t *testing.T) {
			u := unitNamed(t, res, c.unit)
			requireCount(t, u, config.MetricInternalCoupling, c.internal)
			requireCount(t, u, config.MetricExternalCoupling, c.external)
			requireCount(t, u, config.MetricStdlibCoupling, c.stdlib)
		})
	}
}

// TestCouplingInJSX checks that a component referenced only from JSX still
// counts as a use of the module it comes from.
func TestCouplingInJSX(t *testing.T) {
	res := analyzeFixture(t, "component.tsx")
	requireCount(t, unitNamed(t, res, "Panel"), config.MetricInternalCoupling, 1)
}

// TestClassify pins the precedence between the three coupling metrics: a
// configured internal prefix wins over a Node.js built-in name.
func TestClassify(t *testing.T) {
	cases := []struct {
		spec string
		want config.MetricID
	}{
		{"./repo", config.MetricInternalCoupling},
		{"@app/users", config.MetricInternalCoupling},
		{"path", config.MetricInternalCoupling},
		{"path/posix", config.MetricInternalCoupling},
		{"node:fs/promises", config.MetricStdlibCoupling},
		{"fs", config.MetricStdlibCoupling},
		{"lodash/fp", config.MetricExternalCoupling},
		{"node-fetch", config.MetricExternalCoupling},
	}
	prefixes := []string{appPrefix, "path"}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			require.Equal(t, c.want, classify(c.spec, prefixes))
		})
	}
}

// TestIsInternal covers the specifier classification on its own.
func TestIsInternal(t *testing.T) {
	prefixes := []string{appPrefix, "~/", "api"}
	cases := []struct {
		spec string
		want bool
	}{
		{"./repo", true},
		{"../repo", true},
		{"/abs/repo", true},
		{".", true},
		{"..", true},
		{"@app/users", true},
		{"~/lib", true},
		{"api", true},
		{"api/orders", true},
		{"api-client", false},
		{"apify", false},
		{"@apple/pie", false},
		{"node:fs", false},
		{"react", false},
		{"lodash/fp", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.spec, func(t *testing.T) {
			require.Equal(t, c.want, isInternal(c.spec, prefixes))
		})
	}
}

// TestIsInternalIgnoresEmptyPrefix guards against an empty configured
// prefix turning every module internal.
func TestIsInternalIgnoresEmptyPrefix(t *testing.T) {
	require.False(t, isInternal("react", []string{""}))
}

func TestEmptyTypeOnlyImportDoesNotContributeCoupling(t *testing.T) {
	a := newTestAnalyzer(t)
	res, err := a.Analyze(t.Context(), "empty-type-import.ts", []byte(`
import type {} from "./types";

export class EmptyTypeImport {}
`))
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
	requireCount(t, unitNamed(t, res, "EmptyTypeImport"), config.MetricInternalCoupling, 0)
}
