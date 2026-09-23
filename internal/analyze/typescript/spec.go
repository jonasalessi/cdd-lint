// Package typescript analyzes TypeScript source code for Intrinsic Complexity
// Points and supplies its language configuration and package detection.
package typescript

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// Spec returns the TypeScript language spec.
func Spec() config.LanguageSpec {
	return config.LanguageSpec{
		ID:          "typescript",
		DisplayName: "TypeScript",
		Extensions:  slices.Clone(typeScriptExtensions[:]),
		DefaultExcludes: []string{
			"**/*.test.ts", "**/*.spec.ts",
			"**/*.test.tsx", "**/*.spec.tsx",
			"**/*.test.mts", "**/*.spec.mts",
			"**/*.test.cts", "**/*.spec.cts",
			"**/*.d.ts", "**/*.d.mts", "**/*.d.cts",
			"**/node_modules/**", "**/dist/**",
		},
		Descriptions: map[config.MetricID]string{
			config.MetricCodeBranch:       "if/else, switch, ternary, loops and ?.",
			config.MetricCondition:        "&&, || and ?? clauses",
			config.MetricInternalCoupling: "references to project modules",
			config.MetricExternalCoupling: "framework / node_modules types",
			config.MetricStdlibCoupling:   "Node.js built-in modules (node:fs, path)",
			config.MetricLambda:           "arrow functions and callbacks",
		},
		PackageExample: "@app/",
		LimitExamples:  []string{`# ".*/adapters/.*": 8`},
		DetectPackages: detectPackages,
	}
}

// detectPackages returns the compilerOptions.paths keys of
// <root>/tsconfig.json with the trailing "*" stripped, so "@app/*" becomes
// "@app/". tsconfig files may carry comments and trailing commas; both are
// removed before parsing, and a file that still does not parse yields no
// prefixes.
func detectPackages(ctx context.Context, root string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		CompilerOptions struct {
			Paths map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(data), &doc); err != nil {
		return nil, nil //nolint:nilerr // a tsconfig we cannot read is a guess we cannot make
	}
	aliases := make([]string, 0, len(doc.CompilerOptions.Paths))
	for key := range doc.CompilerOptions.Paths {
		if alias := strings.TrimSuffix(key, "*"); alias != "" {
			aliases = append(aliases, alias)
		}
	}
	slices.Sort(aliases)
	return slices.Compact(aliases), nil
}

// trailingComma matches a comma that directly precedes a closing brace or
// bracket, which strict JSON forbids.
var trailingComma = regexp.MustCompile(`,(\s*[}\]])`)

// stripJSONC removes // and /* */ comments outside strings, then trailing
// commas, so a JSONC document passes encoding/json.
func stripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	var inStr, inLine, inBlock bool
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				out = append(out, c)
			}
		case inBlock:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				inBlock = false
				i++
			}
		case inStr:
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			inLine = true
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			inBlock = true
			i++
		default:
			out = append(out, c)
		}
	}
	return trailingComma.ReplaceAll(out, []byte("$1"))
}
