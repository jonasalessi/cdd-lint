package analyze

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jonasalessi/cdd-lint/internal/config"
)

// matcher decides which files a run analyzes. An empty include list
// includes everything, and exclude always wins over include. The defaults
// for tests and generated code are not built in: cdd init writes them into
// the configuration, so the matcher only ever reads what the file says.
type matcher struct {
	include []*regexp.Regexp
	exclude []*regexp.Regexp
}

// newMatcher compiles the include and exclude entries of a configuration.
// An entry prefixed with config.RegexPrefix is a Go RE2 regex matched
// anywhere in the path; every other entry is a glob, with the optional
// config.GlobPrefix, matched against the whole path.
func newMatcher(include, exclude []string) (*matcher, error) {
	in, err := compilePatterns(include)
	if err != nil {
		return nil, fmt.Errorf("include: %w", err)
	}
	ex, err := compilePatterns(exclude)
	if err != nil {
		return nil, fmt.Errorf("exclude: %w", err)
	}
	return &matcher{include: in, exclude: ex}, nil
}

// Match reports whether the file at path is analyzed. path is
// slash-separated and relative to the configuration file.
func (m *matcher) Match(path string) bool {
	if matchesAny(m.exclude, path) {
		return false
	}
	return len(m.include) == 0 || matchesAny(m.include, path)
}

// matchesAny reports whether at least one pattern matches path.
func matchesAny(patterns []*regexp.Regexp, path string) bool {
	for _, p := range patterns {
		if p.MatchString(path) {
			return true
		}
	}
	return false
}

// compilePatterns compiles one include or exclude list.
func compilePatterns(list []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(list))
	for _, entry := range list {
		compiled, err := compilePattern(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, compiled)
	}
	return out, nil
}

// compilePattern turns one entry into a regexp, honoring the two prefixes
// of the schema.
func compilePattern(entry string) (*regexp.Regexp, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil, errors.New("empty pattern")
	}
	if expr, ok := strings.CutPrefix(entry, config.RegexPrefix); ok {
		compiled, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", expr, err)
		}
		return compiled, nil
	}
	glob, _ := strings.CutPrefix(entry, config.GlobPrefix)
	for strings.HasPrefix(glob, "./") {
		glob = strings.TrimPrefix(glob, "./")
	}
	return compileGlob(glob)
}

// compileGlob translates a glob and compiles the result.
func compileGlob(glob string) (*regexp.Regexp, error) {
	expr, err := globExpr(glob)
	if err != nil {
		return nil, err
	}
	compiled, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("invalid glob %q: %w", glob, err)
	}
	return compiled, nil
}

// globExpr translates a glob into an RE2 expression anchored to the whole
// path. "**/" spans any number of path segments, including none, and a
// trailing "**" spans the rest of the path; "*" and "?" never cross a
// separator; "[...]" is a character class, negated with a leading "!".
func globExpr(glob string) (string, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); {
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString(`(?:[^/]+/)*`)
			i += len("**/")
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(`.*`)
			i += len("**")
		case glob[i] == '*':
			b.WriteString(`[^/]*`)
			i++
		case glob[i] == '?':
			b.WriteString(`[^/]`)
			i++
		case glob[i] == '[':
			n, err := writeClass(&b, glob[i:])
			if err != nil {
				return "", err
			}
			i += n
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
			i++
		}
	}
	b.WriteString("$")
	return b.String(), nil
}

// writeClass copies a glob character class into the expression, turning a
// leading "!" into the RE2 negation, and returns how many bytes of glob it
// consumed.
func writeClass(b *strings.Builder, glob string) (int, error) {
	end := strings.IndexByte(glob[1:], ']')
	if end < 1 {
		return 0, fmt.Errorf("invalid glob %q: unterminated character class", glob)
	}
	body := glob[1 : 1+end]
	b.WriteString("[")
	if rest, ok := strings.CutPrefix(body, "!"); ok {
		b.WriteString("^")
		body = rest
	}
	b.WriteString(body)
	b.WriteString("]")
	return end + 2, nil
}
