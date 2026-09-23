package golang

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// analyzer counts the Go ICP constructs of one file at a time. It parses
// with go/parser, so it owns no native resource, needs no grammar and does
// not implement io.Closer; every call builds its own token.FileSet, which
// keeps a long run from accumulating positions.
type analyzer struct {
	prefixes []string
}

// NewAnalyzer returns a Go analyzer. The internal prefixes are the import
// paths the configuration counts as internal coupling.
func NewAnalyzer(opts analyze.Options) analyze.Analyzer {
	return &analyzer{prefixes: opts.InternalPrefixes}
}

// Analyze parses src and returns the raw counts of every unit it contains.
// Only `.go` is accepted: any other extension is an error, never a silent
// guess. A file that does not parse yields no units and one warning naming
// the position of the first syntax error; an empty file and a generated one
// yield no units and no warning (FR-2).
func (a *analyzer) Analyze(ctx context.Context, p string, src []byte) (analyze.FileResult, error) {
	if ext := path.Ext(p); !strings.EqualFold(ext, extGo) {
		return analyze.FileResult{}, fmt.Errorf("%s: unsupported Go extension %q", p, ext)
	}
	if err := ctx.Err(); err != nil {
		return analyze.FileResult{}, fmt.Errorf("%s: %w", p, err)
	}
	if len(bytes.TrimSpace(src)) == 0 {
		return analyze.FileResult{}, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, p, src, parser.ParseComments)
	if err != nil {
		return analyze.FileResult{Warnings: []string{syntaxWarning(err)}}, nil
	}
	if ast.IsGenerated(file) {
		return analyze.FileResult{}, nil
	}
	return analyze.FileResult{Units: a.measure(fset, file)}, nil
}

// measure turns the declarations of a parsed file into measured units, each
// carrying a key for every metric (FR-4). The file's imports are read once
// and offered to every unit, which is charged for the ones it uses (FR-8).
func (a *analyzer) measure(fset *token.FileSet, file *ast.File) []analyze.Unit {
	mods := modules(file, fset, a.prefixes)
	decls := units(fset, file)
	out := make([]analyze.Unit, 0, len(decls))
	for _, d := range decls {
		out = append(out, measureUnit(fset, d, mods))
	}
	return out
}

// syntaxWarning names the position of the first error go/parser reported,
// falling back to 1:1 when the error carries no position. The path stays out
// of it: the caller already attaches the warning to the file it came from.
func syntaxWarning(err error) string {
	line, col := 1, 1
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 && list[0].Pos.IsValid() {
		line, col = list[0].Pos.Line, list[0].Pos.Column
	}
	return treesitter.SyntaxError + " at " + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}
