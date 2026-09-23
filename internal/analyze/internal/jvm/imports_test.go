package jvm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// span returns a one-character range on the given line, enough to tell two
// import statements apart.
func span(line int) treesitter.Span {
	return treesitter.Span{Line: line, Col: 1, EndLine: line, EndCol: 2}
}

// TestIsInternal (TC-R1) is the classification table Kotlin used to hold, no
// parsing involved.
func TestIsInternal(t *testing.T) {
	cases := []struct {
		path     string
		prefixes []string
		want     bool
	}{
		{"com.acme.shared.Money", []string{"com.acme"}, true},
		{"com.acme.shared.Money", []string{"com.acme.shared"}, true},
		{"com.acme.shared.Money", []string{"com.acmecorp"}, false},
		{"com.acme.shared.Money", []string{"com.acme.shared.Money"}, true},
		{"com.acme.shared.Money", []string{""}, false},
		{"com.acme.shared.Money", nil, false},
		{"java.util.List", []string{"com.acme"}, false},
		{"com.acme", []string{"com.acme"}, true},
		{"com.acmecorp.X", []string{"com.acme"}, false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, IsInternal(c.path, c.prefixes), "%s with %v", c.path, c.prefixes)
	}
}

// TestBindMergesTheSamePath (TC-R2): imports naming one path are one module,
// keeping every binding they introduce and the range of the first of them.
func TestBindMergesTheSamePath(t *testing.T) {
	imports := NewImports([]string{"a"}, nil)
	imports.Bind("a.B", "B", span(1))
	imports.Bind("a.B", "B", span(2))
	imports.Bind("a.B", "C", span(3))

	mods := imports.Modules()
	require.Len(t, mods, 1)
	require.Equal(t, "a.B", mods[0].Path)
	require.Equal(t, []string{"B", "B", "C"}, mods[0].Bindings)
	require.Equal(t, span(1), mods[0].At)
	require.Equal(t, config.MetricInternalCoupling, mods[0].Metric)
	require.False(t, mods[0].Star)
}

// TestBindWithoutABinding (TC-R2): an import whose local name the grammar
// could not read still makes the module known, binding nothing.
func TestBindWithoutABinding(t *testing.T) {
	imports := NewImports(nil, nil)
	imports.Bind("a.B", "", span(1))

	mods := imports.Modules()
	require.Len(t, mods, 1)
	require.Empty(t, mods[0].Bindings)
}

// TestStarMarksTheModule (TC-R2, TC-R4): a star import binds no visible name,
// and a named import of the same path leaves the star standing.
func TestStarMarksTheModule(t *testing.T) {
	imports := NewImports(nil, nil)
	imports.Star("a.b", span(1))
	imports.Bind("a.b", "b", span(2))

	mods := imports.Modules()
	require.Len(t, mods, 1)
	require.True(t, mods[0].Star)
	require.Equal(t, span(1), mods[0].At)
}

// TestModulesAreInSourceOrder (TC-R2): the order the imports were reported
// in, whatever their paths sort as.
func TestModulesAreInSourceOrder(t *testing.T) {
	imports := NewImports([]string{"com.acme"}, nil)
	imports.Bind("zeta.Last", "Last", span(1))
	imports.Bind("com.acme.Money", "Money", span(2))
	imports.Star("alpha.util", span(3))
	imports.Bind("zeta.Last", "Alias", span(4))

	mods := imports.Modules()
	require.Equal(t, []string{"zeta.Last", "com.acme.Money", "alpha.util"}, paths(mods))
	require.Equal(t, []config.MetricID{
		config.MetricExternalCoupling,
		config.MetricInternalCoupling,
		config.MetricExternalCoupling,
	}, metrics(mods))
}

// TestEmptyImportsHaveNoModules (TC-R2).
func TestEmptyImportsHaveNoModules(t *testing.T) {
	require.Empty(t, NewImports(nil, nil).Modules())
}

// TestModuleUsedBy (TC-R4): the star is used by every unit, a named module
// only by the units mentioning one of its bindings.
func TestModuleUsedBy(t *testing.T) {
	mentioned := map[string]struct{}{"Money": {}}
	none := map[string]struct{}{}
	cases := []struct {
		name string
		mod  Module
		refs map[string]struct{}
		want bool
	}{
		{"star with no refs", Module{Star: true}, none, true},
		{"binding mentioned", Module{Bindings: []string{"Ledger", "Money"}}, mentioned, true},
		{"bindings absent", Module{Bindings: []string{"Ledger"}}, mentioned, false},
		{"no bindings at all", Module{}, mentioned, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.mod
			require.Equal(t, c.want, m.UsedBy(c.refs))
		})
	}
}

// TestClassify (TC-R5): a project prefix wins over the standard library,
// which wins over external, and a nil predicate makes nothing standard.
func TestClassify(t *testing.T) {
	isJava := func(path string) bool { return strings.HasPrefix(path, "java.") }
	cases := []struct {
		name     string
		path     string
		prefixes []string
		stdlib   func(string) bool
		want     config.MetricID
	}{
		{"project prefix", "com.acme.shared.Money", []string{"com.acme"}, isJava, config.MetricInternalCoupling},
		{"third party", "org.springframework.Bean", []string{"com.acme"}, isJava, config.MetricExternalCoupling},
		{"standard library", "java.util.List", []string{"com.acme"}, isJava, config.MetricStdlibCoupling},
		{"no predicate", "java.util.List", []string{"com.acme"}, nil, config.MetricExternalCoupling},
		{"prefix beats stdlib", "java.util.List", []string{"java"}, isJava, config.MetricInternalCoupling},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, Classify(c.path, c.prefixes, c.stdlib))
		})
	}
}

// paths returns the modules' paths, in order.
func paths(mods []Module) []string {
	out := make([]string, len(mods))
	for i := range mods {
		out[i] = mods[i].Path
	}
	return out
}

// metrics returns the modules' coupling metrics, in order.
func metrics(mods []Module) []config.MetricID {
	out := make([]config.MetricID, len(mods))
	for i := range mods {
		out[i] = mods[i].Metric
	}
	return out
}
