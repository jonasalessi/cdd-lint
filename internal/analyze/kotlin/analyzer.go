package kotlin

import (
	"context"
	"fmt"
	"path"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/jvm"
	"github.com/jonasalessi/cdd-lint/internal/analyze/internal/treesitter"
)

// analyzer counts the Kotlin ICP constructs of one file at a time. It owns
// one tree-sitter parser and one reusable cursor, neither of which is safe
// for concurrent use, so the pipeline builds one analyzer per worker and
// closes it when the worker exits.
type analyzer struct {
	prefixes []string
	grammar  *grammar
	parser   *ts.Parser
	cursor   *ts.TreeCursor
}

// NewAnalyzer returns a Kotlin analyzer. The returned value holds native
// resources and implements io.Closer; the pipeline must close it.
func NewAnalyzer(opts analyze.Options) analyze.Analyzer {
	return &analyzer{
		prefixes: opts.InternalPrefixes,
		grammar:  sharedGrammar(),
		parser:   ts.NewParser(),
	}
}

// Close releases the parser and cursor. The tree-sitter binding installs no
// finalizers, so anything not closed leaks on the C heap. Close is
// idempotent.
func (a *analyzer) Close() error {
	if a.cursor != nil {
		a.cursor.Close()
		a.cursor = nil
	}
	if a.parser != nil {
		a.parser.Close()
		a.parser = nil
	}
	return nil
}

// Analyze parses src and returns the raw counts of every unit it contains.
// A file that does not parse yields no units and one warning naming the
// position of the first syntax error (FR-6). Only `.kt` is accepted: any
// other extension is an error, never a silent guess.
func (a *analyzer) Analyze(ctx context.Context, p string, src []byte) (analyze.FileResult, error) {
	if ext := path.Ext(p); !strings.EqualFold(ext, extKotlin) {
		return analyze.FileResult{}, fmt.Errorf("%s: unsupported Kotlin extension %q", p, ext)
	}
	tree, err := a.parse(ctx, src)
	if err != nil {
		return analyze.FileResult{}, fmt.Errorf("%s: %w", p, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		return analyze.FileResult{Warnings: []string{treesitter.SyntaxWarning(root)}}, nil
	}
	mods := modules(a.grammar, root, src, a.prefixes)
	decls := units(a.grammar, root, src)
	out := make([]analyze.Unit, 0, len(decls))
	for i := range decls {
		out = append(out, a.measure(&decls[i], mods, src))
	}
	return analyze.FileResult{Units: out}, nil
}

// parse binds the grammar on first use and runs the parser over src within
// the shared parse budget.
func (a *analyzer) parse(ctx context.Context, src []byte) (*ts.Tree, error) {
	if a.parser == nil {
		return nil, fmt.Errorf("analyzer is closed")
	}
	if a.parser.Language() == nil {
		if err := a.parser.SetLanguage(a.grammar.lang); err != nil {
			return nil, fmt.Errorf("set grammar: %w", err)
		}
	}
	return treesitter.Parse(ctx, a.parser, src)
}

// measure counts one unit, attributes the file's imports to it, and
// locates every construct it charged.
func (a *analyzer) measure(d *unitDecl, mods []jvm.Module, src []byte) analyze.Unit {
	c := newCounter(a.grammar, src, d)
	treesitter.Walk(a.treeCursor(&d.node), &d.node, c.visit)
	c.countCoupling(mods)
	return analyze.Unit{
		Name:        d.name,
		Kind:        d.kind,
		Line:        d.line,
		Col:         d.col,
		Counts:      c.counts,
		Occurrences: c.sortedOccurrences(),
	}
}

// treeCursor returns the analyzer's cursor, creating it on first use. A
// cursor is not bound to the tree it was created from: the walk resets it
// onto whichever node it is given.
func (a *analyzer) treeCursor(n *ts.Node) *ts.TreeCursor {
	if a.cursor == nil {
		a.cursor = n.Walk()
	}
	return a.cursor
}
