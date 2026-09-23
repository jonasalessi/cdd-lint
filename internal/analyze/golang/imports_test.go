package golang

import (
	"go/ast"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// goPrefix is the module the coupling fixtures are written against, the
// `internal_coupling.packages` entry of a real project.
const goPrefix = "example.com/app"

// couplingCase is one unit's expected coupling in a fixture, stated for the
// three metrics at once so none is left implicit.
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

// isCoupling reports whether a metric is charged on an import rather than
// inside the unit it belongs to.
func isCoupling(metric config.MetricID) bool {
	switch metric {
	case config.MetricInternalCoupling, config.MetricExternalCoupling, config.MetricStdlibCoupling:
		return true
	default:
		return false
	}
}

// TestCouplingFixture pins coupling.go (TC-C1, TC-C2, TC-C3): the imports of
// a file are attributed to the units that qualify something by their name,
// the blank import charges every unit, and a parameter that shadows a
// package is not a use of it.
func TestCouplingFixture(t *testing.T) {
	res := analyzeFixture(t, "coupling.go", goPrefix)
	require.Empty(t, res.Warnings)

	requireCoupling(t, res, []couplingCase{
		{"Invoice", 2, 2, 4}, // money and shared; uuid and yaml; fmt, http, str and embed
		{"Note", 0, 0, 1},    // the blank embed alone: the fmt parameter shadows the package
		{"Plain", 0, 0, 1},   // the blank embed charges a unit with no method too
	})
	requireCount(t, unitNamed(t, res, "Invoice"), config.MetricLocalVariable, 5)
}

// TestDotImportFixture pins coupling_dot.go (TC-C4): a dot name is
// indistinguishable from a local, so the module charges every unit.
func TestDotImportFixture(t *testing.T) {
	res := analyzeFixture(t, "coupling_dot.go", goPrefix)
	require.Empty(t, res.Warnings)

	requireCoupling(t, res, []couplingCase{
		{"Invoice", 1, 0, 1}, // shared, plus math through the dot import
		{"Plain", 0, 0, 1},   // the dot import alone
	})
}

// TestCouplingOccurrencesSitOnTheImports (TC-C10): every coupling charge
// points at the ImportSpec that brings the module in, above the unit it is
// charged to, and the sort leaves them in import order.
func TestCouplingOccurrencesSitOnTheImports(t *testing.T) {
	res := analyzeFixture(t, "coupling.go", goPrefix)
	invoice := unitNamed(t, res, "Invoice")

	want := []analyze.Occurrence{
		{Metric: config.MetricStdlibCoupling, Line: 6},    // "fmt"
		{Metric: config.MetricStdlibCoupling, Line: 7},    // "net/http"
		{Metric: config.MetricStdlibCoupling, Line: 8},    // str "strings"
		{Metric: config.MetricStdlibCoupling, Line: 10},   // _ "embed"
		{Metric: config.MetricInternalCoupling, Line: 12}, // money "example.com/app/money/v2"
		{Metric: config.MetricInternalCoupling, Line: 13}, // "example.com/app/shared"
		{Metric: config.MetricExternalCoupling, Line: 15}, // "github.com/google/uuid"
		{Metric: config.MetricExternalCoupling, Line: 17}, // "gopkg.in/yaml.v3"
	}
	got := make([]analyze.Occurrence, 0, len(want))
	for _, o := range invoice.Occurrences {
		if isCoupling(o.Metric) {
			require.Less(t, o.Line, invoice.Line, "an import is written above the unit")
			require.Equal(t, 2, o.Col, "an ImportSpec starts after the block's indent")
			got = append(got, analyze.Occurrence{Metric: o.Metric, Line: o.Line})
		}
	}
	require.Equal(t, want, got)
}

// TestShadowedBindingIsNotAUse (TC-C2, TC-C13): a local named like a package
// resolves, so the selector it qualifies is a field read and not a use of
// the import — the rule Java cannot state and go/parser gives for free.
func TestShadowedBindingIsNotAUse(t *testing.T) {
	src := `package p

import "fmt"

type Shadow struct{}

func (Shadow) M() string {
	fmt := struct{ text string }{}
	return fmt.text
}

type User struct{}

func (User) M() string {
	return fmt.Sprint(1)
}
`
	res := analyzeSource(t, src)

	requireCount(t, unitNamed(t, res, "Shadow"), config.MetricStdlibCoupling, 0)
	requireCount(t, unitNamed(t, res, "User"), config.MetricStdlibCoupling, 1)
}

// TestMentionsThatAreNotUses (TC-C13): a name written in a string or in a
// comment is no node the walk sees, and a local that shadows an alias hides
// the module behind it.
func TestMentionsThatAreNotUses(t *testing.T) {
	src := `package p

import (
	str "strings"
	"time"
)

type Quiet struct{}

func (Quiet) M() string {
	// time.Now would be a use if it were code
	s := "time.Now"
	str := struct{ field string }{field: s}
	return str.field
}
`
	requireCount(t, unitNamed(t, analyzeSource(t, src), "Quiet"), config.MetricStdlibCoupling, 0)
}

// TestUsesThatCount (TC-C13): a field type, a call, a composite literal, an
// embedded type, a type argument and a qualified constant are all uses.
func TestUsesThatCount(t *testing.T) {
	src := `package p

import (
	"fmt"
	"net/http"
	"time"

	"example.com/app/shared"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

type Uses struct {
	fmt.Stringer
	Client *http.Client
	Rows   []shared.Row
	ID     uuid.UUID
}

func (Uses) M(raw []byte) {
	_ = yaml.Unmarshal(raw, nil)
	_ = errgroup.Group{}
	_ = time.RFC3339
}
`
	u := unitNamed(t, analyzeSource(t, src, goPrefix), "Uses")

	requireCount(t, u, config.MetricInternalCoupling, 1) // shared, as a type argument
	requireCount(t, u, config.MetricExternalCoupling, 3) // uuid, yaml, errgroup
	requireCount(t, u, config.MetricStdlibCoupling, 3)   // fmt embedded, http, time
}

// TestOnePointPerModule (TC-C11): however many times a unit names a package,
// the module it imports is one dependency and one occurrence.
func TestOnePointPerModule(t *testing.T) {
	src := `package p

import "fmt"

type Many struct{}

func (Many) M(a string) string {
	_ = fmt.Errorf("%s", a)
	_ = fmt.Sprint(a)
	return fmt.Sprintf("%s", a)
}
`
	u := unitNamed(t, analyzeSource(t, src), "Many")

	requireCount(t, u, config.MetricStdlibCoupling, 1)
	require.Len(t, occurrencesOf(u, config.MetricStdlibCoupling), 1)
}

// TestDuplicatePathIsOneModule (TC-C8): the same path imported twice is one
// module carrying both bindings, charged once, at the first spec.
func TestDuplicatePathIsOneModule(t *testing.T) {
	src := `package p

import (
	"fmt"
	f "fmt"
)

type Twice struct{}

func (Twice) M() string {
	return fmt.Sprint(f.Sprint(1))
}
`
	u := unitNamed(t, analyzeSource(t, src), "Twice")

	requireCount(t, u, config.MetricStdlibCoupling, 1)
	require.Equal(t, 4, occurrencesOf(u, config.MetricStdlibCoupling)[0].Line,
		"the module points at the first spec naming the path")
}

// TestAliasAloneIsTheBinding (TC-C8): an aliased import binds the alias and
// nothing else, so the assumed name no longer names the module.
func TestAliasAloneIsTheBinding(t *testing.T) {
	src := `package p

import str "strings"

type Aliased struct{}

func (Aliased) M(s string) string {
	return str.ToUpper(s)
}

type Assumed struct{}

func (Assumed) M(s string) string {
	return strings.ToUpper(s)
}
`
	res := analyzeSource(t, src)

	requireCount(t, unitNamed(t, res, "Aliased"), config.MetricStdlibCoupling, 1)
	requireCount(t, unitNamed(t, res, "Assumed"), config.MetricStdlibCoupling, 0)
}

// TestUnusedModuleIsChargedNowhere (TC-C9): an import no unit mentions costs
// nothing — not every unit, and not the nearest one — which is also what
// happens to a package used only by a top-level declaration.
func TestUnusedModuleIsChargedNowhere(t *testing.T) {
	src := `package p

import (
	"fmt"
	"sort"
	"strings"
)

var _ = sort.Ints

type A struct{}

func (A) M() string {
	return fmt.Sprint(1)
}

type B struct{}
`
	res := analyzeSource(t, src)

	requireCoupling(t, res, []couplingCase{
		{"A", 0, 0, 1}, // fmt only: sort belongs to a top-level var, strings to nobody
		{"B", 0, 0, 0},
	})
}

// TestBlankAndDotImportsChargeEveryUnit (TC-C3, TC-C4): neither binds a name
// the walk can match, so both are side effects of the file and charge every
// unit, one with no member included.
func TestBlankAndDotImportsChargeEveryUnit(t *testing.T) {
	src := `package p

import (
	_ "embed"
	. "math"

	_ "example.com/app/driver"
)

type Bare struct{}

type Rounder struct{}

func (Rounder) M(v float64) float64 {
	return Floor(v)
}
`
	res := analyzeSource(t, src, goPrefix)

	requireCoupling(t, res, []couplingCase{
		{"Bare", 1, 0, 2},
		{"Rounder", 1, 0, 2},
	})
}

// TestPrefixesClassifyThroughTheAnalyzer (TC-C5, TC-C12) runs the configured
// prefixes end to end through analyze.Options: a prefix matches its own path
// and its slash-subpaths and nothing else, no prefix leaves everything
// external or standard library, and a prefix wins over the standard library.
func TestPrefixesClassifyThroughTheAnalyzer(t *testing.T) {
	src := `package p

import (
	"fmt"

	"example.com/app/shared"
)

type U struct{}

func (U) M() string {
	return fmt.Sprint(shared.Tax())
}
`
	cases := []struct {
		name                       string
		prefixes                   []string
		internal, external, stdlib int
	}{
		{"the path itself", []string{"example.com/app/shared"}, 1, 0, 1},
		{"the module above it", []string{goPrefix}, 1, 0, 1},
		{"a longer first element", []string{"example.com/apples"}, 0, 1, 1},
		{"a prefix the slash does not follow", []string{"example.com/appshared"}, 0, 1, 1},
		{"an empty prefix", []string{""}, 0, 1, 1},
		{"no prefixes", nil, 0, 1, 1},
		{"the standard library as a project package", []string{"fmt"}, 1, 1, 0},
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

// TestClassify (TC-C5) pins the classification order: a configured prefix
// wins, then the standard library, and everything else is external.
func TestClassify(t *testing.T) {
	cases := []struct {
		path string
		want config.MetricID
	}{
		{"fmt", config.MetricStdlibCoupling},
		{"net/http", config.MetricStdlibCoupling},
		{"go/ast", config.MetricStdlibCoupling},
		{"embed", config.MetricStdlibCoupling},
		{"unsafe", config.MetricStdlibCoupling},
		{"C", config.MetricStdlibCoupling},
		{"github.com/google/uuid", config.MetricExternalCoupling},
		{"gopkg.in/yaml.v3", config.MetricExternalCoupling},
		{"golang.org/x/sync/errgroup", config.MetricExternalCoupling},
		{goPrefix, config.MetricInternalCoupling},
		{"example.com/app/shared", config.MetricInternalCoupling},
		{"example.com/apples", config.MetricExternalCoupling},
		{"example.com/appshared", config.MetricExternalCoupling},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			require.Equal(t, c.want, classify(c.path, []string{goPrefix}))
		})
	}
	require.Equal(t, config.MetricInternalCoupling, classify("fmt", []string{"fmt"}),
		"a configured prefix wins over the standard library")
	require.Equal(t, config.MetricExternalCoupling, classify("example.com/app", nil))
}

// TestAssumedName (TC-C6) pins goimports' rule for the name an import binds
// when it carries no alias.
func TestAssumedName(t *testing.T) {
	cases := map[string]string{
		"fmt":                          "fmt",
		"net/http":                     "http",
		"github.com/google/uuid":       "uuid",
		"gopkg.in/yaml.v3":             "yaml",
		"github.com/mattn/go-isatty":   "isatty",
		"github.com/nats-io/nats.go":   "nats",
		"github.com/jackc/pgx/v5":      "pgx",
		"github.com/golang-jwt/jwt/v5": "jwt",
		"example.com/app/money/v2":     "money",
		"golang.org/x/sync/errgroup":   "errgroup",
		"v2":                           "v2",
		"example.com/caf\u00e9":        "caf\u00e9",
		"example.com/caf\u00e9-go":     "caf\u00e9",
		"example.com/\u4e16\u754c.v1":  "\u4e16\u754c",
	}
	for importPath, want := range cases {
		t.Run(importPath, func(t *testing.T) {
			require.Equal(t, want, assumedName(importPath))
		})
	}
}

// TestUnquotableImportPathNamesNoModule covers the guard the parser makes
// unreachable: a spec whose path literal is not a valid Go string yields no
// module instead of a panic.
func TestUnquotableImportPathNamesNoModule(t *testing.T) {
	file := &ast.File{Imports: []*ast.ImportSpec{
		{Path: &ast.BasicLit{Kind: token.STRING, Value: "not quoted"}},
	}}
	require.Empty(t, modules(file, token.NewFileSet(), nil))
}
