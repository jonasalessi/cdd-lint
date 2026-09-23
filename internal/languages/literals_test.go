package languages

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// moduleRoot is the repository root relative to this package.
var moduleRoot = filepath.Join("..", "..")

// skippedDirs are never walked: build output, fixtures and dependencies.
// Hidden directories are skipped as well, so a nested checkout under
// .claude/ or an editor's cache does not count as part of the module.
var skippedDirs = map[string]bool{"bin": true, "testdata": true, "vendor": true}

// skipDir reports whether a directory called name is outside the module's
// own sources.
func skipDir(name string) bool {
	return skippedDirs[name] || strings.HasPrefix(name, ".")
}

// forbiddenLiterals is every id of the vocabulary: spelled out anywhere but
// vocabulary.go and the language spec files, it is a second copy of the
// vocabulary that will drift.
func forbiddenLiterals() map[string]bool {
	out := map[string]bool{}
	for _, l := range All() {
		out[string(l.Spec.ID)] = true
	}
	for _, m := range config.Metrics() {
		out[string(m)] = true
	}
	for _, s := range config.ProjectTypes() {
		out[s] = true
	}
	for _, s := range config.LegacyModes() {
		out[s] = true
	}
	for _, s := range config.ReporterFormats() {
		out[s] = true
	}
	return out
}

// literalExempt reports whether rel may spell out vocabulary ids: the
// vocabulary itself, each language's spec, and each language's standard
// library table, which is language knowledge like the spec and names
// modules such as the Node.js "console" that collide with the vocabulary.
func literalExempt(rel string) bool {
	if rel == "internal/config/vocabulary.go" {
		return true
	}
	for _, pattern := range []string{"internal/analyze/*/spec.go", "internal/analyze/*/stdlib.go"} {
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
	}
	return false
}

// languageDir reports whether rel lives where language knowledge belongs:
// the registry or a language directory.
func languageDir(rel string) bool {
	if strings.HasPrefix(rel, "internal/languages/") {
		return true
	}
	dir := path.Dir(rel)
	return strings.HasPrefix(dir, "internal/analyze/") && dir != "internal/analyze"
}

// TestLiterals is FR-9: vocabulary ids appear as string literals only where
// they are defined, and no language table regrows outside the language
// directories. make check-literals runs it.
func TestLiterals(t *testing.T) {
	forbidden := forbiddenLiterals()
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(moduleRoot, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(moduleRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		file, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		violations = append(violations, inspect(fset, file, rel, forbidden)...)
		return nil
	})
	require.NoError(t, err)
	if len(violations) > 0 {
		t.Errorf("vocabulary literals or language tables outside their home:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// inspect collects the violations of one file.
func inspect(fset *token.FileSet, file *ast.File, rel string, forbidden map[string]bool) []string {
	var out []string
	report := func(pos token.Pos, msg string) {
		out = append(out, rel+":"+strconv.Itoa(fset.Position(pos).Line)+": "+msg)
	}
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !forbiddenConcreteAnalyzerImport(rel, importPath) {
			continue
		}
		report(imp.Path.Pos(), "concrete analyzer import belongs in internal/languages or its own directory")
	}
	tags := map[*ast.BasicLit]bool{}
	inConfig := file.Name.Name == "config"
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			if n.Tag != nil {
				tags[n.Tag] = true
			}
		case *ast.BasicLit:
			if n.Kind != token.STRING || tags[n] || literalExempt(rel) {
				return true
			}
			if v, err := strconv.Unquote(n.Value); err == nil && forbidden[v] {
				report(n.Pos(), "vocabulary literal "+n.Value+" belongs in vocabulary.go or a spec.go")
			}
		case *ast.CompositeLit:
			if isLanguageTable(n, inConfig) && !languageDir(rel) {
				report(n.Pos(), "language-keyed table; put the data in each language's spec")
			}
		}
		return true
	})
	return out
}

const analyzerImportPrefix = "github.com/jonasalessi/cdd-lint/internal/analyze/"

// forbiddenConcreteAnalyzerImport reports whether rel imports a concrete
// language analyzer outside the registry or that analyzer's own directory.
func forbiddenConcreteAnalyzerImport(rel, importPath string) bool {
	analyzer, ok := strings.CutPrefix(importPath, analyzerImportPrefix)
	if !ok || analyzer == "" || strings.Contains(analyzer, "/") {
		return false
	}
	if strings.HasPrefix(rel, "internal/languages/") {
		return false
	}
	return path.Dir(rel) != "internal/analyze/"+analyzer
}

// isLanguageTable matches a non-empty map[config.Language]… literal, or
// map[Language]… inside package config.
func isLanguageTable(lit *ast.CompositeLit, inConfig bool) bool {
	m, ok := lit.Type.(*ast.MapType)
	if !ok || len(lit.Elts) == 0 {
		return false
	}
	switch key := m.Key.(type) {
	case *ast.SelectorExpr:
		pkg, ok := key.X.(*ast.Ident)
		return ok && pkg.Name == "config" && key.Sel.Name == "Language"
	case *ast.Ident:
		return inConfig && key.Name == "Language"
	}
	return false
}

// TestLiteralsHelpers pins the exemptions so a rename of the spec file or
// the registry directory is noticed.
func TestLiteralsHelpers(t *testing.T) {
	require.True(t, literalExempt("internal/config/vocabulary.go"))
	require.True(t, literalExempt("internal/analyze/kotlin/spec.go"))
	require.True(t, literalExempt("internal/analyze/typescript/stdlib.go"))
	require.False(t, literalExempt("internal/analyze/kotlin/analyzer.go"))
	require.False(t, literalExempt("internal/detect/languages.go"))

	require.True(t, languageDir("internal/languages/languages.go"))
	require.True(t, languageDir("internal/analyze/kotlin/analyzer.go"))
	require.True(t, languageDir("internal/analyze/internal/jvm/jvm.go"))
	require.False(t, languageDir("internal/analyze/analyze.go"))
	require.False(t, languageDir("internal/prompt/init_form.go"))

	require.True(t, skipDir(".git"))
	require.True(t, skipDir(".claude"))
	require.True(t, skipDir("testdata"))
	require.False(t, skipDir("internal"))

	_, err := os.Stat(filepath.Join(moduleRoot, "go.mod"))
	require.NoError(t, err, "moduleRoot must be the repository root")
}

func TestInspectRejectsConcreteAnalyzerImportsOutsideTheirBoundaries(t *testing.T) {
	tests := map[string]struct {
		rel        string
		importPath string
		violation  bool
	}{
		"command cannot import a concrete analyzer": {
			rel:        "cmd/check.go",
			importPath: "github.com/jonasalessi/cdd-lint/internal/analyze/typescript",
			violation:  true,
		},
		"registry can import a concrete analyzer": {
			rel:        "internal/languages/languages.go",
			importPath: "github.com/jonasalessi/cdd-lint/internal/analyze/typescript",
		},
		"analyzer can import its own package": {
			rel:        "internal/analyze/typescript/helper.go",
			importPath: "github.com/jonasalessi/cdd-lint/internal/analyze/typescript",
		},
		"other analyzers cannot import a concrete analyzer": {
			rel:        "internal/analyze/java/helper.go",
			importPath: "github.com/jonasalessi/cdd-lint/internal/analyze/typescript",
			violation:  true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(
				fset,
				tt.rel,
				"package test\nimport _ \""+tt.importPath+"\"\n",
				parser.SkipObjectResolution,
			)
			require.NoError(t, err)

			violations := inspect(fset, file, tt.rel, nil)
			assert.Equal(t, tt.violation, len(violations) > 0, violations)
		})
	}
}
