// Package githook writes the cdd block into a git pre-commit hook and takes
// it out again. The block sits between two marker lines, so it can be
// found, replaced and removed inside a hook the project already owns.
package githook

import (
	"fmt"
	"strings"
)

// The marker lines that delimit the block in a hook file.
const (
	BeginMarker = "# >>> cdd hook git >>>"
	EndMarker   = "# <<< cdd hook git <<<"
)

// Block renders the hook block. configPath, relative to the repository's
// top level, is baked in as --config when not empty; the default location
// needs no flag. The block fails closed: a missing cdd blocks the commit.
func Block(configPath string) string {
	check := "cdd check --staged"
	if configPath != "" {
		check += " --config " + shellQuote(configPath)
	}
	return fmt.Sprintf(`%s
# Installed by "cdd hook git"; remove with "cdd hook git --remove".
if ! command -v cdd >/dev/null 2>&1; then
  echo 'cdd: not found in PATH; install cdd or run "cdd hook git --remove"' >&2
  exit 1
fi
%s || exit $?
%s`, BeginMarker, check, EndMarker)
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
