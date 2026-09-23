// Package languages is the registry of every language cdd supports. It is
// the one existing file a new language touches: create
// internal/analyze/<id>/ with a spec.go, then add a line to All below.
// Every consumer receives the registered specs from cmd; nothing else in the
// repository imports this package.
package languages

import (
	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/analyze/golang"
	"github.com/jonasalessi/cdd-lint/internal/analyze/java"
	"github.com/jonasalessi/cdd-lint/internal/analyze/kotlin"
	"github.com/jonasalessi/cdd-lint/internal/analyze/typescript"
	"github.com/jonasalessi/cdd-lint/internal/config"
)

// All returns every supported language in registration order, which is the
// order languages are listed and rendered everywhere. Each call returns a
// fresh slice.
func All() []analyze.Language {
	return []analyze.Language{
		{Spec: golang.Spec(), NewAnalyzer: golang.NewAnalyzer},
		{Spec: java.Spec(), NewAnalyzer: java.NewAnalyzer},
		{Spec: kotlin.Spec(), NewAnalyzer: kotlin.NewAnalyzer},
		{Spec: typescript.Spec(), NewAnalyzer: typescript.NewAnalyzer},
	}
}

// Specs projects All onto the specs, which is what config, detect, prompt
// and initcmd take.
func Specs() []config.LanguageSpec {
	all := All()
	specs := make([]config.LanguageSpec, len(all))
	for i, l := range all {
		specs[i] = l.Spec
	}
	return specs
}
