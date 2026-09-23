package cmd

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// Build information, injected by the Makefile through -ldflags -X. A build
// without them, such as "go install github.com/jonasalessi/cdd-lint@latest",
// keeps these defaults and falls back to what the Go tool stamps into the
// binary.
var (
	version = defaultVersion
	commit  = defaultCommit
	date    = defaultDate
)

const (
	defaultVersion = "dev"
	defaultCommit  = "none"
	defaultDate    = "unknown"

	// develVersion is the main module version the Go tool stamps when the
	// binary was not installed from a published module version.
	develVersion = "(devel)"

	// commitWidth is how much of a revision hash the version line shows, the
	// width "git rev-parse --short" prints.
	commitWidth = 7
)

// readBuildInfo reads the build information embedded in this binary. Tests
// replace it to keep the version line independent of how they were compiled.
var readBuildInfo = debug.ReadBuildInfo

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the cdd version",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			fmt.Fprintln(c.OutOrStdout(), versionLine())
		},
	}
}

// build is what a binary knows about how it was built. An empty field is one
// this build cannot tell.
type build struct {
	version string
	commit  string
	date    string
}

// versionLine formats the build as "cdd <version> (<commit>, <date>)", leaving
// out the parts this binary does not know.
func versionLine() string {
	b := resolveBuild(readBuildInfo)
	if detail := b.detail(); detail != "" {
		return fmt.Sprintf("cdd %s (%s)", b.version, detail)
	}
	return "cdd " + b.version
}

// resolveBuild takes the values injected through -ldflags and fills whatever
// is missing from the build information the Go tool embeds: the module version
// for "go install <module>@<version>", and the VCS stamps for a build made
// inside a repository.
func resolveBuild(read func() (*debug.BuildInfo, bool)) build {
	b := build{
		version: known(version, defaultVersion),
		commit:  known(commit, defaultCommit),
		date:    known(date, defaultDate),
	}
	info, ok := read()
	if ok {
		b.fillFrom(info)
	}
	if b.version == "" {
		b.version = defaultVersion
	}
	return b
}

// fillFrom copies the module version and the VCS stamps into the fields that
// are still empty. Values injected through -ldflags always win.
func (b *build) fillFrom(info *debug.BuildInfo) {
	if b.version == "" && info.Main.Version != "" && info.Main.Version != develVersion {
		b.version = info.Main.Version
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if b.commit == "" {
				b.commit = shortCommit(setting.Value)
			}
		case "vcs.time":
			if b.date == "" {
				b.date = setting.Value
			}
		}
	}
}

// detail joins the commit and the date the way the version line shows them.
func (b build) detail() string {
	parts := make([]string, 0, 2)
	for _, part := range []string{b.commit, b.date} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

// known reports value, or the empty string when it is still the placeholder a
// build without that information carries.
func known(value, placeholder string) string {
	if value == placeholder {
		return ""
	}
	return value
}

// shortCommit trims a revision hash to the width git prints.
func shortCommit(revision string) string {
	if len(revision) <= commitWidth {
		return revision
	}
	return revision[:commitWidth]
}
