package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/languages"
)

// goAppDir is where the Go fixtures put their sources, one package below the
// module root the way a Go project lays itself out.
const goAppDir = "internal/app/"

// goModule is the module path writeGoFixture writes into go.mod, and the
// prefix the fixtures import their own packages under.
const goModule = "example.com/fixture"

// cleanGoSource is one struct of 1 ICP: well inside the greenfield limit.
const cleanGoSource = `package app

type Greeter struct{}

func (Greeter) Greet(name string) string {
	if name != "" {
		return "hi " + name
	}
	return "hi"
}
`

// overLimitGoSource is one struct of 18 ICPs, six branches and twelve
// conditions, against the greenfield limit of 10.
const overLimitGoSource = `package app

type OrderService struct{}

func (OrderService) Price(a, b, c int) int {
	if a > 0 && b > 0 {
		return a + b
	}
	if a > 1 && b > 1 {
		return a - b
	}
	if a > 2 && b > 2 {
		return a * b
	}
	if a > 3 && b > 3 {
		return a / b
	}
	if a > 4 && b > 4 {
		return a + c
	}
	if a > 5 && b > 5 {
		return b + c
	}
	return 0
}
`

// couplingGoSource imports one package of the module and one of the standard
// library, so the unit carries one charge of each and both sit on an import.
const couplingGoSource = `package app

import (
	"time"

	"` + goModule + `/shared"
)

type Invoice struct {
	Amount shared.Money
	At     time.Time
}
`

// writeGoProject lays out a Go project in dir with a configuration written
// by cdd init, so the file always matches the schema the command reads
// today. writeGoFixture's main.go goes once the module is written: its
// `func main` would be a second unit in every report, and each case writes
// the sources it wants under goAppDir.
func writeGoProject(t *testing.T, dir string, extraLanguages ...string) {
	t.Helper()
	writeGoFixture(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "main.go")))
	langs := append([]string{string(langGo)}, extraLanguages...)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", strings.Join(langs, ","),
		"--metrics", checkMetrics,
		"--packages", goModule,
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
}

// languageID is the id the registry reports for a display name, so a test
// reads the vocabulary instead of spelling it out.
func languageID(t *testing.T, displayName string) string {
	t.Helper()
	for _, l := range languages.All() {
		if l.Spec.DisplayName == displayName {
			return string(l.Spec.ID)
		}
	}
	t.Fatalf("no %s language registered", displayName)
	return ""
}

// goID is the language id the registry reports for Go.
func goID(t *testing.T) string {
	t.Helper()
	return languageID(t, "Go")
}

// TestCheckGoCleanProject (TC-I1): exit 0 and the unit listed with --all.
func TestCheckGoCleanProject(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"greeter.go", cleanGoSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "cdd check: PASS violations=0 units=1")
	assert.Contains(t, stdout, unitLabel+" "+goAppDir+"greeter.go:3:6 struct Greeter icp=1 limit=10\n")
	assert.Empty(t, stderr, "the analyzer exists; no warning about it")
}

// TestCheckGoOverLimitUnitBlocks (TC-I2): exit 1 naming the unit and its
// breakdown.
func TestCheckGoOverLimitUnitBlocks(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"order_service.go", overLimitGoSource)

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stdout,
		violationLabel+" "+goAppDir+"order_service.go:3:6 struct OrderService icp=18 limit=10 over=8\n")
	assert.Contains(t, stdout, "  metrics: condition=12 code_branch=6\n")
	assert.Empty(t, stderr)
}

// TestCheckGoJSONReport (TC-I3): the JSON report names the language through
// the registry and the unit's kind, and --explain puts every coupling charge
// on the import that brings the dependency in.
func TestCheckGoJSONReport(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"order_service.go", overLimitGoSource)
	writeFixtureFile(t, dir, goAppDir+"invoice.go", couplingGoSource)
	writeFixtureFile(t, dir, "shared/money.go", "package shared\n\ntype Money struct{}\n")

	stdout, _, code := runCdd(t, dir, "check", "--all", "--explain", "--format", "json")
	assert.Equal(t, 1, code)

	var doc struct {
		Files []struct {
			Path     string `json:"path"`
			Language string `json:"language"`
			Units    []struct {
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Occurrences []struct {
					Metric string `json:"metric"`
					Line   int    `json:"line"`
				} `json:"occurrences"`
			} `json:"units"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "the report must be valid JSON: %s", stdout)
	require.Len(t, doc.Files, 3)
	for _, f := range doc.Files {
		assert.Equal(t, goID(t), f.Language)
		require.Len(t, f.Units, 1)
		assert.Equal(t, "struct", f.Units[0].Kind)
	}
	invoice := doc.Files[0].Units[0]
	require.Equal(t, "Invoice", invoice.Name)
	charged := map[string]int{}
	for _, o := range invoice.Occurrences {
		charged[o.Metric] = o.Line
	}
	assert.Equal(t, map[string]int{"stdlib_coupling": 4, "internal_coupling": 6}, charged,
		"each coupling occurrence sits on its import line")
}

// TestCheckGoOtherFormats (TC-I4): xml and markdown render and name the
// unit.
func TestCheckGoOtherFormats(t *testing.T) {
	for _, format := range []string{"xml", "markdown"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			writeGoProject(t, dir)
			writeFixtureFile(t, dir, goAppDir+"order_service.go", overLimitGoSource)

			stdout, stderr, code := runCdd(t, dir, "check", "--format", format)
			assert.Equal(t, 1, code, "stderr: %s", stderr)
			assert.Contains(t, stdout, "OrderService")
		})
	}
}

// TestCheckGoAndTypeScriptTogether (TC-I5): each file is reported under its
// own language and the exit code reflects the worst unit.
func TestCheckGoAndTypeScriptTogether(t *testing.T) {
	dir := t.TempDir()
	typescript := languageID(t, "TypeScript")
	writeFixtureFile(t, dir, "tsconfig.json", `{"compilerOptions":{"paths":{"@app/*":["src/*"]}}}`+"\n")
	writeGoProject(t, dir, typescript)
	writeFixtureFile(t, dir, goAppDir+"greeter.go", cleanGoSource)
	writeFixtureFile(t, dir, "src/order-service.ts", overLimitSource)

	stdout, _, code := runCdd(t, dir, "check", "--all", "--format", "json")
	assert.Equal(t, 1, code)
	var doc struct {
		Files []struct {
			Path     string `json:"path"`
			Language string `json:"language"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.Len(t, doc.Files, 2)
	byPath := map[string]string{}
	for _, f := range doc.Files {
		byPath[f.Path] = f.Language
	}
	assert.Equal(t, goID(t), byPath[goAppDir+"greeter.go"])
	assert.Equal(t, typescript, byPath["src/order-service.ts"])
}

// TestCheckGoIgnoresTestsAndVendor (TC-I6): neither a _test.go file nor a
// vendored one is in the report, because the excludes cdd init writes still
// apply.
func TestCheckGoIgnoresTestsAndVendor(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"greeter.go", cleanGoSource)
	writeFixtureFile(t, dir, goAppDir+"greeter_test.go",
		strings.ReplaceAll(overLimitGoSource, "OrderService", "GreeterTest"))
	writeFixtureFile(t, dir, "vendor/example.com/dep/dep.go",
		strings.ReplaceAll(overLimitGoSource, "OrderService", "Vendored"))

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "units=1")
	assert.Contains(t, stdout, "Greeter ")
	assert.NotContains(t, stdout, "GreeterTest")
	assert.NotContains(t, stdout, "Vendored")
}

// TestCheckGoPathNarrowsTheRun (TC-I7): only the named file is reported.
func TestCheckGoPathNarrowsTheRun(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"ledger.go", strings.ReplaceAll(cleanGoSource, "Greeter", "Ledger"))
	writeFixtureFile(t, dir, goAppDir+"order_service.go", overLimitGoSource)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.FromSlash(goAppDir+"ledger.go"), "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Ledger")
	assert.NotContains(t, stdout, "OrderService")
}

// TestCheckGoSyntaxErrorIsAWarning (TC-I8): the broken file is reported as a
// warning, the others are analyzed, and the exit code does not change.
func TestCheckGoSyntaxErrorIsAWarning(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"greeter.go", cleanGoSource)
	writeFixtureFile(t, dir, goAppDir+"broken.go", "package app\n\nfunc f( {\n")

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "warning: "+goAppDir+"broken.go: syntax error at ")
	assert.Contains(t, stdout, "Greeter")
	assert.Contains(t, stdout, "violations=0")
}

// TestCheckGoAutoDetectsPackages (TC-I9): with auto_detect on and no
// packages listed, the module prefix comes from the go.mod line and the
// import is classified internal. The module is written after init, which is
// what leaves `packages` empty.
func TestCheckGoAutoDetectsPackages(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", string(langGo),
		"--metrics", checkMetrics,
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Empty(t, loadConfig(t, dir).InternalCoupling.Packages)
	writeGoFixture(t, dir)
	require.NoError(t, os.Remove(filepath.Join(dir, "main.go")))
	writeFixtureFile(t, dir, "shared/money.go", "package shared\n\ntype Money struct{}\n")
	writeFixtureFile(t, dir, goAppDir+"invoice.go", couplingGoSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "struct Invoice icp=1.5 limit=10")
	assert.Contains(t, stdout, "internal_coupling=1")
	assert.Contains(t, stdout, "stdlib_coupling=1x0.5")
}

// TestCheckGoSkipsGeneratedFiles (TC-I10): a file carrying the Code-generated
// header is absent from the report, with no warning and no effect on the
// exit code, however far over the limit it is.
func TestCheckGoSkipsGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	writeFixtureFile(t, dir, goAppDir+"greeter.go", cleanGoSource)
	writeFixtureFile(t, dir, goAppDir+"message.pb.go",
		"// Code generated by protoc-gen-go. DO NOT EDIT.\n\n"+overLimitGoSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "units=1")
	assert.NotContains(t, stdout, "OrderService")
	assert.NotContains(t, stdout, "warning")
}

// TestCheckGoTimeoutReportsPartially (TC-I11): the Go analyzer honors the
// deadline through its ctx.Err() check.
func TestCheckGoTimeoutReportsPartially(t *testing.T) {
	dir := t.TempDir()
	writeGoProject(t, dir)
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFixtureFile(t, dir, goAppDir+name+".go",
			strings.ReplaceAll(cleanGoSource, "Greeter", strings.ToUpper(name)))
	}
	editConfig(t, dir, "\ntimeout: 5m\n", "\ntimeout: 1ns\n")

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, "partial=true")
	assert.Contains(t, stderr, "timeout")
}
