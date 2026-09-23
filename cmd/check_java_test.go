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

// javaAppDir is where the Java fixtures put their sources, laid out the way
// Maven and Gradle expect them.
const javaAppDir = "src/main/java/com/acme/app/"

// cleanJavaSource is one class of 1 ICP: well inside the greenfield limit.
const cleanJavaSource = `package com.acme.app;

class Greeter {
    String greet(String name) {
        if (name != null) {
            return "hi " + name;
        }
        return "hi";
    }
}
`

// overLimitJavaSource is one class of 18 ICPs, six branches and twelve
// conditions, against the greenfield limit of 10.
const overLimitJavaSource = `package com.acme.app;

class OrderService {
    int price(int a, int b, int c) {
        if (a > 0 && b > 0) { return a + b; }
        if (a > 1 && b > 1) { return a - b; }
        if (a > 2 && b > 2) { return a * b; }
        if (a > 3 && b > 3) { return a / b; }
        if (a > 4 && b > 4) { return a + c; }
        if (a > 5 && b > 5) { return b + c; }
        return 0;
    }
}
`

// writeJavaFixture lays out a Java project in dir with a configuration
// written by cdd init, so the file always matches the schema the command
// reads today. Sources are added by the caller under javaAppDir.
func writeJavaFixture(t *testing.T, dir string, extraLanguages ...string) {
	t.Helper()
	langs := append([]string{"java"}, extraLanguages...)
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", strings.Join(langs, ","),
		"--metrics", checkMetrics,
		"--packages", "com.acme",
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
}

// javaID is the language id the registry reports for Java, obtained through
// the registry rather than spelled out.
func javaID(t *testing.T) string {
	t.Helper()
	for _, l := range languages.All() {
		if l.Spec.DisplayName == "Java" {
			return string(l.Spec.ID)
		}
	}
	t.Fatal("no Java language registered")
	return ""
}

// TestCheckJavaCleanProject (TC-I2): exit 0 and the unit listed with --all.
func TestCheckJavaCleanProject(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"Greeter.java", cleanJavaSource)

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "cdd check: PASS violations=0 units=1")
	assert.Contains(t, stdout, unitLabel+" "+javaAppDir+"Greeter.java:3:1 class Greeter icp=1 limit=10\n")
	assert.Empty(t, stderr, "the analyzer exists; no warning about it")
}

// TestCheckJavaOverLimitUnitBlocks (TC-I3): exit 1 naming the unit.
func TestCheckJavaOverLimitUnitBlocks(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"OrderService.java", overLimitJavaSource)

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 1, code)
	assert.Contains(t, stdout, "cdd check: FAIL violations=1 units=1")
	assert.Contains(t, stdout,
		violationLabel+" "+javaAppDir+"OrderService.java:3:1 class OrderService icp=18 limit=10 over=8\n")
	assert.Contains(t, stdout, "  metrics: condition=12 code_branch=6\n")
	assert.Empty(t, stderr)
}

// TestCheckJavaJSONReport (TC-I4): the JSON report names the language
// through the registry and the unit's kind, and --explain adds an occurrence
// on an import line for a coupling metric.
func TestCheckJavaJSONReport(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"OrderService.java", overLimitJavaSource)
	writeFixtureFile(t, dir, javaAppDir+"Invoice.java",
		"package com.acme.app;\n\nimport java.time.Instant;\n\nclass Invoice {\n    Instant at;\n}\n")

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
		assert.Equal(t, javaID(t), f.Language)
		require.Len(t, f.Units, 1)
		assert.Equal(t, "class", f.Units[0].Kind)
	}
	invoice := doc.Files[0].Units[0]
	require.Equal(t, "Invoice", invoice.Name)
	require.Len(t, invoice.Occurrences, 1)
	assert.Equal(t, 3, invoice.Occurrences[0].Line, "the coupling occurrence sits on the import line")
	assert.Contains(t, invoice.Occurrences[0].Metric, "coupling")
}

// TestCheckJavaOtherFormats (TC-I5): xml and markdown render and name the
// unit.
func TestCheckJavaOtherFormats(t *testing.T) {
	for _, format := range []string{"xml", "markdown"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			writeJavaFixture(t, dir)
			writeFixtureFile(t, dir, javaAppDir+"OrderService.java", overLimitJavaSource)

			stdout, stderr, code := runCdd(t, dir, "check", "--format", format)
			assert.Equal(t, 1, code, "stderr: %s", stderr)
			assert.Contains(t, stdout, "OrderService")
		})
	}
}

// TestCheckJavaAndKotlinTogether (TC-I6): each file is reported under its own
// language and the exit code reflects the worst unit.
func TestCheckJavaAndKotlinTogether(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir, "kotlin")
	writeFixtureFile(t, dir, javaAppDir+"Greeter.java", cleanJavaSource)
	writeFixtureFile(t, dir, kotlinAppDir+"OrderService.kt", overLimitKotlinSource)

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
	assert.Equal(t, javaID(t), byPath[javaAppDir+"Greeter.java"])
	assert.Equal(t, kotlinID(t), byPath[kotlinAppDir+"OrderService.kt"])
}

// TestCheckJavaIgnoresTests (TC-I7): a file under src/test is not in the
// report, because the excludes cdd init writes still apply.
func TestCheckJavaIgnoresTests(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"Greeter.java", cleanJavaSource)
	writeFixtureFile(t, dir, "src/test/java/com/acme/app/OrderServiceTest.java",
		strings.ReplaceAll(overLimitJavaSource, "OrderService", "OrderServiceTest"))

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "units=1")
	assert.Contains(t, stdout, "Greeter")
	assert.NotContains(t, stdout, "OrderServiceTest")
}

// TestCheckJavaPathNarrowsTheRun (TC-I8): only the named file is reported.
func TestCheckJavaPathNarrowsTheRun(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"Ledger.java", strings.ReplaceAll(cleanJavaSource, "Greeter", "Ledger"))
	writeFixtureFile(t, dir, javaAppDir+"OrderService.java", overLimitJavaSource)

	stdout, stderr, code := runCdd(t, dir, "check", filepath.FromSlash(javaAppDir+"Ledger.java"), "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Ledger")
	assert.NotContains(t, stdout, "OrderService")
}

// TestCheckJavaSyntaxErrorIsAWarning (TC-I9): the broken file is reported as
// a warning, the others are analyzed, and the exit code does not change.
func TestCheckJavaSyntaxErrorIsAWarning(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	writeFixtureFile(t, dir, javaAppDir+"Greeter.java", cleanJavaSource)
	writeFixtureFile(t, dir, javaAppDir+"Broken.java", "class Oops { void f( { }\n")

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "warning: "+javaAppDir+"Broken.java: syntax error at ")
	assert.Contains(t, stdout, "Greeter")
	assert.Contains(t, stdout, "violations=0")
}

// TestCheckJavaAutoDetectsPackages (TC-I10): with auto_detect on and no
// packages listed, the com.acme prefixes come from the package declarations
// and the import is classified internal.
func TestCheckJavaAutoDetectsPackages(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runCdd(t, dir, "init", "--yes", "--force",
		"--languages", "java",
		"--metrics", checkMetrics,
	)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	writeFixtureFile(t, dir, "src/main/java/com/acme/shared/Money.java",
		"package com.acme.shared;\n\nclass Money {\n}\n")
	writeFixtureFile(t, dir, javaAppDir+"Invoice.java",
		"package com.acme.app;\n\nimport com.acme.shared.Money;\nimport java.time.Instant;\n\n"+
			"class Invoice {\n    Money amount;\n    Instant at;\n}\n")

	stdout, stderr, code := runCdd(t, dir, "check", "--all")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	assert.Contains(t, stdout, "class Invoice icp=1.5 limit=10")
	assert.Contains(t, stdout, "internal_coupling=1")
	assert.Contains(t, stdout, "stdlib_coupling=1x0.5")
}

// TestCheckJavaTimeoutReportsPartially (TC-I11): the Java analyzer honors the
// deadline through parse.
func TestCheckJavaTimeoutReportsPartially(t *testing.T) {
	dir := t.TempDir()
	writeJavaFixture(t, dir)
	for _, name := range []string{"A", "B", "C", "D"} {
		writeFixtureFile(t, dir, javaAppDir+name+".java", strings.ReplaceAll(cleanJavaSource, "Greeter", name))
	}
	editConfig(t, dir, "\ntimeout: 5m\n", "\ntimeout: 1ns\n")

	stdout, stderr, code := runCdd(t, dir, "check")
	assert.Equal(t, 2, code)
	assert.Contains(t, stdout, "partial=true")
	assert.Contains(t, stderr, "timeout")
}
