package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
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

// declsOf parses a source and returns its extracted units, so a test can
// read back the declarations each unit was billed.
func declsOf(t *testing.T, src string) []unitDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "inline"+extGo, src, parser.ParseComments)
	require.NoError(t, err)
	return units(fset, file)
}

// declNamed returns the extracted unit with the given name.
func declNamed(t *testing.T, decls []unitDecl, name string) unitDecl {
	t.Helper()
	for _, d := range decls {
		if d.name == name {
			return d
		}
	}
	t.Fatalf("no unit named %q", name)
	return unitDecl{}
}

// billedNames names the declarations a unit counts over, in the order they
// were billed to it.
func billedNames(d unitDecl) []string {
	out := make([]string, 0, len(d.nodes))
	for _, n := range d.nodes {
		switch node := n.(type) {
		case *ast.TypeSpec:
			out = append(out, node.Name.Name)
		case *ast.FuncDecl:
			out = append(out, node.Name.Name)
		}
	}
	return out
}

// TestUnits pins FR-3 against units.go: names, kinds and positions in
// declaration order (TC-U1, TC-U2, TC-U3).
func TestUnits(t *testing.T) {
	res := analyzeFixture(t, "units"+extGo)
	require.Empty(t, res.Warnings)
	require.Equal(t, []unitHead{
		{"A", unitStruct, 5, 6},    // TC-U3: at the name, not at `type`
		{"B", unitInterface, 7, 6}, // TC-U2: an interface is told from a struct
		{"C", unitType, 9, 6},      // TC-U2: a named basic type
		{"D", unitType, 11, 6},     // TC-U2: an alias
		{"E", unitType, 13, 6},     // TC-U2: a func type
		{"F", unitStruct, 18, 2},   // TC-U3: its own name inside the group
		{"G", unitType, 19, 2},     // TC-U3: a different line from F
		{"H", unitMethods, 26, 1},  // TC-U5: at the `func` of its first method
		{"Free", unitFunc, 30, 1},  // TC-U3: at the `func` keyword
		{"init", unitFunc, 32, 1},  // TC-U2: init is an ordinary func unit
	}, heads(res))
}

// TestUnitsBillEveryMethodToItsType (TC-U4, TC-U5): a method bills to the
// unit of its receiver wherever it sits — `Early` is written above the
// `type ( F; G )` group and still belongs to G, whose position stays the
// group's name.
func TestUnitsBillEveryMethodToItsType(t *testing.T) {
	decls := declsOf(t, string(readFixture(t, "units"+extGo)))
	for name, billed := range map[string][]string{
		"A": {"A", "M"},
		"B": {"B"},
		"C": {"C", "M"},
		"G": {"G", "Early"},
		"H": {"M", "N"},
	} {
		require.Equal(t, billed, billedNames(declNamed(t, decls, name)), "unit %q", name)
	}
}

// TestMethodsUnitOnlyWithoutATypeSpec (TC-U5): methods of a type this file
// does not declare form one `methods` unit at the first of them; the same
// methods next to their own `type H struct{}` produce a single struct unit
// instead, never both.
func TestMethodsUnitOnlyWithoutATypeSpec(t *testing.T) {
	const methodsOnly = "package p\n\nfunc (H) M() {}\n\nfunc (h H) N() {}\n"
	require.Equal(t, []unitHead{{"H", unitMethods, 3, 1}}, heads(analyzeSource(t, methodsOnly)))

	const withType = "package p\n\nfunc (H) M() {}\n\ntype H struct{}\n\nfunc (h H) N() {}\n"
	require.Equal(t, []unitHead{{"H", unitStruct, 5, 6}}, heads(analyzeSource(t, withType)))
	require.Equal(t, []string{"H", "M", "N"}, billedNames(declNamed(t, declsOf(t, withType), "H")))
}

// TestNestedDeclarationsAreNotUnits (TC-U6): a type declared in a function
// body, a struct nested in a struct and a func literal bound to a local are
// billed to the unit that holds them and are never units themselves.
func TestNestedDeclarationsAreNotUnits(t *testing.T) {
	const src = "package p\n\ntype Outer struct {\n\tInner struct {\n\t\tX int\n\t}\n}\n\n" +
		"func Body() {\n\ttype Local struct{}\n\tf := func() {}\n\t_, _ = f, Local{}\n}\n"
	res := analyzeSource(t, src)
	require.Equal(t, []string{"Outer", "Body"}, unitNames(res))
}

// TestTopLevelVarAndConstAreNotUnits (TC-U7): a top-level `var`, `const` or
// func literal is invisible — it is no unit, and it charges nothing to the
// units the file does hold.
func TestTopLevelVarAndConstAreNotUnits(t *testing.T) {
	const src = "package p\n\nimport \"sync\"\n\nvar v sync.Mutex\n\nconst k = 1\n\n" +
		"var f = func() {\n\tif k == 1 {\n\t}\n}\n\ntype Only struct{}\n"
	res := analyzeSource(t, src)
	require.Equal(t, []string{"Only"}, unitNames(res))
	requireCount(t, unitNamed(t, res, "Only"), config.MetricLambda, 0)
	for _, m := range config.Metrics() {
		require.Equal(t, 0, totalCount(res, m), "metric %q", m)
	}
}

// TestInitFunctionsAreOneUnitEach (TC-U2): Go allows several `init`
// functions in a file and each one is measured on its own.
func TestInitFunctionsAreOneUnitEach(t *testing.T) {
	res := analyzeSource(t, "package p\n\nfunc init() {}\n\nfunc init() {}\n")
	require.Equal(t, []unitHead{
		{"init", unitFunc, 3, 1},
		{"init", unitFunc, 5, 1},
	}, heads(res))
}

// TestReceiverUnwrapping (TC-U8): a pointer, a parenthesized type and the
// type arguments of a generic receiver all unwrap to the base identifier; a
// receiver that names no type bills nowhere and warns nothing.
func TestReceiverUnwrapping(t *testing.T) {
	const src = "package p\n\ntype Stack[T any] struct{}\n\nfunc (s Stack[T]) Push() {}\n\n" +
		"func (s *Stack[T]) Pop() {}\n\nfunc (s (*Stack)) Peek() {}\n\n" +
		"func (p *Pair[K, V]) Key() {}\n\nfunc (*[]int) Orphan() {}\n"
	res := analyzeSource(t, src)
	require.Equal(t, []unitHead{
		{"Stack", unitStruct, 3, 6},
		{"Pair", unitMethods, 11, 1},
	}, heads(res))

	decls := declsOf(t, src)
	require.Equal(t, []string{"Stack", "Push", "Pop", "Peek"}, billedNames(declNamed(t, decls, "Stack")))
	require.Equal(t, []string{"Key"}, billedNames(declNamed(t, decls, "Pair")))
}

// TestUnitsAreInSourceOrder (TC-U9): units are reported in declaration
// order, so (Line, Col) strictly increases even in units.go, where `Early`
// precedes the declaration of the type it bills to.
func TestUnitsAreInSourceOrder(t *testing.T) {
	res := analyzeFixture(t, "units"+extGo)
	require.NotEmpty(t, res.Units)
	for i := 1; i < len(res.Units); i++ {
		previous, current := res.Units[i-1], res.Units[i]
		require.True(t,
			current.Line > previous.Line || (current.Line == previous.Line && current.Col > previous.Col),
			"unit %q at %d:%d follows %q at %d:%d",
			current.Name, current.Line, current.Col, previous.Name, previous.Line, previous.Col)
	}
}

// TestUnitsCarryEveryMetric (FR-4): every unit is reported with a key for
// every metric, enabled or not, and units.go declares nothing to charge —
// the counters land with the tasks that follow.
func TestUnitsCarryEveryMetric(t *testing.T) {
	res := analyzeFixture(t, "units"+extGo)
	require.Len(t, res.Units, 10)
	for _, u := range res.Units {
		require.Empty(t, u.Occurrences, "unit %q", u.Name)
		for _, m := range config.Metrics() {
			requireCount(t, u, m, 0)
		}
	}
}

// totalCount sums one raw count over every unit of a result.
func totalCount(res analyze.FileResult, metric config.MetricID) int {
	total := 0
	for _, u := range res.Units {
		total += u.Counts[metric]
	}
	return total
}

// TestReceiverWithoutAnEntryNamesNoType covers the guard the parser makes
// unreachable: a hand-built method whose receiver list is empty bills to no
// type instead of indexing past the list.
func TestReceiverWithoutAnEntryNamesNoType(t *testing.T) {
	fn := &ast.FuncDecl{Recv: &ast.FieldList{}, Name: ast.NewIdent("M")}
	require.Empty(t, receiverName(fn))
}
