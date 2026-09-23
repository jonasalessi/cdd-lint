package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/languages"
)

// kotlinAppDir is where the Kotlin fixtures put their sources, laid out the
// way Gradle expects them.
const kotlinAppDir = "src/main/kotlin/com/acme/app/"

// cleanKotlinSource is one class of 1 ICP: well inside the greenfield limit.
const cleanKotlinSource = `package com.acme.app

class Greeter {
    fun greet(name: String?): String {
        if (name != null) {
            return "hi $name"
        }
        return "hi"
    }
}
`

// overLimitKotlinSource is one class of 18 ICPs, six branches and twelve
// conditions, against the greenfield limit of 10.
const overLimitKotlinSource = `package com.acme.app

class OrderService {
    fun price(a: Int, b: Int, c: Int): Int {
        if (a > 0 && b > 0) { return a + b }
        if (a > 1 && b > 1) { return a - b }
        if (a > 2 && b > 2) { return a * b }
        if (a > 3 && b > 3) { return a / b }
        if (a > 4 && b > 4) { return a + c }
        if (a > 5 && b > 5) { return b + c }
        return 0
    }
}
`

// writeKotlinFixture lays out a Kotlin project in dir with a configuration
// written by cdd init, so the file always matches the schema the command
// reads today. Sources are added by the caller under kotlinAppDir.
func writeKotlinFixture(t *testing.T, dir string, extraLanguages ...string) {
	t.Helper()
	langs := append([]string{"kotlin"}, extraLanguages...)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", strings.Join(langs, ","),
		"--metrics", checkMetrics,
		"--packages", "com.acme",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
}

// kotlinID is the language id the registry reports for Kotlin, obtained
// through the registry rather than spelled out.
func kotlinID(t *testing.T) string {
	t.Helper()
	for _, l := range languages.All() {
		if l.Spec.DisplayName == "Kotlin" {
			return string(l.Spec.ID)
		}
	}
	t.Fatal("no Kotlin language registered")
	return ""
}

// TestCheckKotlinCleanProject (TC-Z1): exit 0 and the unit listed with
// --all.
func TestCheckKotlinCleanProject(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"Greeter.kt", cleanKotlinSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "cdd check: PASS violations=0 units=1")
	assert.Contains(t, stdout, unitLabel+" "+kotlinAppDir+"Greeter.kt:3:1 class Greeter icp=1 limit=10\n")
	assert.Empty(t, stderr, "the analyzer exists; no warning about it")
}

// TestCheckKotlinOverLimitUnitBlocks (TC-Z2): exit 1 naming the unit.
func TestCheckKotlinOverLimitUnitBlocks(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"OrderService.kt", overLimitKotlinSource)

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stdout,
		violationLabel+" "+kotlinAppDir+"OrderService.kt:3:1 class OrderService icp=18 limit=10 over=8\n")
	assert.Contains(t, stdout, "  metrics: condition=12 code_branch=6\n")
	assert.Empty(t, stderr)
}

// TestCheckKotlinJSONReport (TC-Z3, TC-Z9): the JSON report names the
// language through the registry and the unit's kind, and --explain adds an
// occurrence on an import line for a coupling metric.
func TestCheckKotlinJSONReport(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"OrderService.kt", overLimitKotlinSource)
	writeFixtureFile(t, dir, kotlinAppDir+"Invoice.kt",
		"package com.acme.app\n\nimport java.time.Instant\n\nclass Invoice(val at: Instant)\n")

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
	require.Len(t, doc.Files, 2)
	for _, f := range doc.Files {
		assert.Equal(t, kotlinID(t), f.Language)
		require.Len(t, f.Units, 1)
		assert.Equal(t, "class", f.Units[0].Kind)
	}
	invoice := doc.Files[0].Units[0]
	require.Equal(t, "Invoice", invoice.Name)
	require.Len(t, invoice.Occurrences, 1)
	assert.Equal(t, 3, invoice.Occurrences[0].Line, "the coupling occurrence sits on the import line")
	assert.Contains(t, invoice.Occurrences[0].Metric, "coupling")
}

// TestCheckKotlinOtherFormats (TC-Z4): xml and markdown render and name the
// unit.
func TestCheckKotlinOtherFormats(t *testing.T) {
	for _, format := range []string{"xml", "markdown"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			writeKotlinFixture(t, dir)
			writeFixtureFile(t, dir, kotlinAppDir+"OrderService.kt", overLimitKotlinSource)

			stdout, stderr, code := runCdd(t, dir, "check", "--format", format)
			assert.Equal(t, 1, code, "stderr: %s", stderr)
			assert.Contains(t, stdout, "OrderService")
		})
	}
}

// TestCheckKotlinAndTypeScriptTogether (TC-Z5): each file is reported under
// its own language and the exit code reflects the worst unit.
func TestCheckKotlinAndTypeScriptTogether(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "tsconfig.json", `{"compilerOptions":{"paths":{"@app/*":["src/*"]}}}`+"\n")
	writeKotlinFixture(t, dir, "typescript")
	writeFixtureFile(t, dir, kotlinAppDir+"Greeter.kt", cleanKotlinSource)
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
	assert.Equal(t, kotlinID(t), byPath[kotlinAppDir+"Greeter.kt"])
	assert.NotEqual(t, kotlinID(t), byPath["src/order-service.ts"])
	assert.NotEmpty(t, byPath["src/order-service.ts"])
}

// TestCheckKotlinIgnoresScriptsAndTests (TC-Z6, TC-Z7): a .kts file next to
// the sources and a file under src/test are not in the report.
func TestCheckKotlinIgnoresScriptsAndTests(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"Greeter.kt", cleanKotlinSource)
	writeFixtureFile(t, dir, "build.gradle.kts", strings.ReplaceAll(overLimitKotlinSource, "OrderService", "Script"))
	writeFixtureFile(t, dir, "src/test/kotlin/com/acme/app/OrderServiceTest.kt",
		strings.ReplaceAll(overLimitKotlinSource, "OrderService", "OrderServiceTest"))

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "units=1")
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stdout, "Script")
	assert.NotContains(t, stdout, "OrderServiceTest")
}

// TestCheckKotlinPathNarrowsTheRun (TC-Z8): only the named file is
// reported.
func TestCheckKotlinPathNarrowsTheRun(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"Ledger.kt", strings.ReplaceAll(cleanKotlinSource, "Greeter", "Ledger"))
	writeFixtureFile(t, dir, kotlinAppDir+"OrderService.kt", overLimitKotlinSource)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.FromSlash(kotlinAppDir+"Ledger.kt"), "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Ledger")
	assert.NotContains(t, stdout, "OrderService")
}

// TestCheckKotlinSyntaxErrorIsAWarning (TC-Z10): the broken file is
// reported as a warning, the others are analyzed, and the exit code does
// not change.
func TestCheckKotlinSyntaxErrorIsAWarning(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	writeFixtureFile(t, dir, kotlinAppDir+"Greeter.kt", cleanKotlinSource)
	writeFixtureFile(t, dir, kotlinAppDir+"Broken.kt", "class Oops {\n    fun f( {\n}\n")

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "warning: "+kotlinAppDir+"Broken.kt: syntax error at ")
	assert.Contains(t, stdout, "Greeter")
	assert.Contains(t, stdout, "violations=0")
}

// TestCheckKotlinAutoDetectsPackages (TC-Z11): with auto_detect on and no
// packages listed, the com.acme prefixes come from the package
// declarations and the import is classified internal.
func TestCheckKotlinAutoDetectsPackages(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", "kotlin",
		"--metrics", checkMetrics,
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	writeFixtureFile(t, dir, "src/main/kotlin/com/acme/shared/Money.kt", "package com.acme.shared\n\nclass Money\n")
	writeFixtureFile(
		t,
		dir,
		kotlinAppDir+"Invoice.kt",
		"package com.acme.app\n\nimport com.acme.shared.Money\nimport java.time.Instant\n\nclass Invoice(val amount: Money, val at: Instant)\n",
	)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "class Invoice icp=1.5 limit=10")
	assert.Contains(t, stdout, "internal_coupling=1")
	assert.Contains(t, stdout, "stdlib_coupling=1x0.5")
}

// TestCheckKotlinTimeoutReportsPartially (TC-Z12): the Kotlin analyzer
// honors the deadline through parse.
func TestCheckKotlinTimeoutReportsPartially(t *testing.T) {
	dir := t.TempDir()
	writeKotlinFixture(t, dir)
	for _, name := range []string{"A", "B", "C", "D"} {
		writeFixtureFile(t, dir, kotlinAppDir+name+".kt", strings.ReplaceAll(cleanKotlinSource, "Greeter", name))
	}
	editConfig(t, dir, "\ntimeout: 5m\n", "\ntimeout: 1ns\n")

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, "partial=true")
	assert.Contains(t, stderr, "timeout")
}
