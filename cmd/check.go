package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/languages"
	"github.com/jonasalessi/cdd-lint/internal/report"
)

// Exit codes of "cdd check". Anything the command cannot do at all — a
// missing configuration, an unusable flag — leaves through the root command
// as exit code 1.
const (
	// exitViolations reports that at least one unit is above its limit and
	// the configured enforcement blocks on it.
	exitViolations = 1
	// exitTimeout reports that the time budget elapsed; the report printed
	// before it covers the files that were analyzed in time.
	exitTimeout = 2
)

func newCheckCmd() *cobra.Command {
	var all, explain bool
	var format string
	c := &cobra.Command{
		Use:   "check [path...]",
		Short: "Measure the project and compare every unit with its limit",
		Long: `check runs the last two steps of CDD (docs/cdd.md, section 3):

  4. Compute the ICPs of every code unit the configured languages claim.
  5. Evaluate each unit against the ICP limit its file resolves to.

It reads the configuration named by --config (cdd.config.yaml by default) and
analyzes the tree rooted at that file's directory, so a configuration in a
subdirectory measures that subdirectory alone.

Paths narrow the run to the named files and directories, resolved from the
working directory. Several paths may share one argument separated by commas.
They must lie under the configuration's directory, since limits and internal
coupling are resolved against it. A named file must belong to a configured
language and pass the include/exclude patterns, so a plugin can re-check the
files that were just saved:

  cdd check src/order/service.ts,src/order/repository.ts --explain --format json

The report lists the units above their limit; --all lists every measured unit.
The summary counts the whole run either way.

--explain details whatever is listed, adding every counted construct with its
position and the ICPs it contributed, so an editor plugin can turn the json
report into inline hints.

--format renders the report in the given format instead of the configured
reporter.format. The destination still comes from the configuration, so a
configured outputFile keeps receiving the report.

Exit codes:

  0  no unit is above its limit, or the enforcement only reports them
  1  a unit is above its limit and the enforcement blocks on it, or the
     configuration could not be read or is invalid
  2  the timeout elapsed; the printed report covers the files analyzed in time`,
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return runCheck(c, configPath, args, format, report.Options{All: all, Explain: explain})
		},
	}
	c.Flags().BoolVar(&all, "all", false, "list every unit, not only the ones over their limit")
	c.Flags().BoolVar(&explain, "explain", false, "list every counted construct with its position and ICPs")
	c.Flags().StringVar(&format, "format", "",
		"report format, overriding reporter.format: "+strings.Join(config.ReporterFormats(), ", "))
	return c
}

// runCheck implements cdd check: load the configuration, analyze the tree it
// governs, or the paths under it, report the outcome and turn it into an
// exit code. A non-empty format replaces the configured reporter.format.
func runCheck(c *cobra.Command, path string, args []string, format string, opts report.Options) error {
	if err := validateFormat(format); err != nil {
		return err
	}
	root := filepath.Dir(path)
	paths, err := checkPaths(root, args)
	if err != nil {
		return err
	}
	cfg, err := loadCheckConfig(c, path)
	if err != nil {
		return err
	}
	if format != "" {
		cfg.Reporter.Format = format
	}
	res, runErr := analyze.Run(c.Context(), analyze.Request{
		Root:      root,
		Config:    cfg,
		Languages: languages.All(),
		Paths:     paths,
	})
	if runErr != nil && !errors.Is(runErr, analyze.ErrTimeout) {
		return runErr
	}
	if err := emitReport(c, cfg.Reporter, res, opts); err != nil {
		return err
	}
	if runErr != nil {
		fmt.Fprintf(c.ErrOrStderr(), "cdd: %v\n", runErr)
		return exitCodeError{code: exitTimeout}
	}
	return violationExit(res)
}

// validateFormat rejects a --format value the reporter cannot render. An
// empty value means the flag was not given. The check runs before the
// configuration is read, so a typo fails fast instead of after the analysis.
func validateFormat(format string) error {
	if format == "" || config.IsReporterFormat(format) {
		return nil
	}
	return fmt.Errorf("--format: %q is not one of %s", format, strings.Join(config.ReporterFormats(), ", "))
}

// checkPaths turns the paths given on the command line, relative to the
// working directory, into slash-separated paths relative to root. An
// argument may carry several paths separated by commas, so a plugin can
// hand over every changed file as one argument. A path outside root is an
// error: the run resolves limits and internal coupling against root, so a
// file elsewhere has no limit to be compared with.
func checkPaths(root string, args []string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, arg := range splitPaths(args) {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%s is outside %s, the directory of the configuration", arg, root)
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	return paths, nil
}

// splitPaths expands comma-separated arguments into one path each, dropping
// the empty entries a trailing comma or doubled separator leaves behind.
func splitPaths(args []string) []string {
	var out []string
	for _, arg := range args {
		for p := range strings.SplitSeq(arg, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// loadCheckConfig reads path and validates it against the registry. Errors
// end the command; warnings only reach stderr, since the run is still the
// one the user asked for.
func loadCheckConfig(c *cobra.Command, path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	issues := config.Validate(cfg, languages.Specs())
	if errs := issues.Errors(); len(errs) > 0 {
		return nil, fmt.Errorf("%s is invalid:\n%s", path, errs)
	}
	for _, warning := range issues.Warnings() {
		fmt.Fprintf(c.ErrOrStderr(), "warning: %s\n", warning)
	}
	return cfg, nil
}

// emitReport renders the run where the reporter points and prints the
// receipt when that was a file.
func emitReport(c *cobra.Command, r config.Reporter, res analyze.RunResult, opts report.Options) error {
	path, err := report.Emit(c.OutOrStdout(), r, res, opts)
	if err != nil {
		return err
	}
	if path != "" {
		fmt.Fprintf(c.OutOrStdout(), "Report written to %s\n", path)
	}
	return nil
}

// violationExit turns the finalized blocking outcome into the command's exit
// code. The report is already out, so a blocking run only carries the code.
func violationExit(res analyze.RunResult) error {
	if res.Blocked {
		return exitCodeError{code: exitViolations}
	}
	return nil
}
