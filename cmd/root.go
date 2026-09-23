// Package cmd wires the cdd command tree.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// defaultConfigPath is where every subcommand looks for the project
// configuration unless --config says otherwise.
const defaultConfigPath = "cdd.config.yaml"

// configPath holds the value of the persistent --config flag. Subcommands
// read it instead of asking cobra for the flag again.
var configPath = defaultConfigPath

// newRootCmd builds the command tree. A fresh tree per call keeps flag state
// out of tests and out of any future in-process reuse.
func newRootCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cdd",
		Short: "Cognitive-Driven Development toolkit",
		Long: `cdd measures code with Cognitive-Driven Development (CDD): every branch,
condition, coupling or exception block adds Intrinsic Complexity Points (ICPs)
to a code unit, and a unit above the agreed limit must be refactored before it
is merged.

Run "cdd init" to write a cdd.config.yaml for your project, then "cdd check"
to measure it.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	c.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "path to the cdd configuration file")
	c.Version = versionLine()
	c.SetVersionTemplate("{{.Version}}\n")
	c.AddCommand(newVersionCmd(), newInitCmd(), newCheckCmd(), newHookCmd())
	return c
}

// Execute runs the command tree with os.Args. Errors go to stderr and turn
// into exit code 1; success returns 0.
func Execute() int {
	return execute(newRootCmd(), os.Stderr)
}

func execute(c *cobra.Command, stderr io.Writer) int {
	if err := c.Execute(); err != nil {
		var ec exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		fmt.Fprintf(stderr, "cdd: %v\n", err)
		return 1
	}
	return 0
}

// exitCodeError carries a specific exit code out of a RunE without printing
// anything: init uses it for the ctrl-c convention, code 130, and check for
// its violation and timeout codes, both of which report themselves first.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit code %d", e.code)
}
