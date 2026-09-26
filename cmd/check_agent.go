package cmd

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jonasalessi/cdd-lint/internal/agenthook"
	"github.com/jonasalessi/cdd-lint/internal/analyze"
	"github.com/jonasalessi/cdd-lint/internal/config"
	"github.com/jonasalessi/cdd-lint/internal/languages"
	"github.com/jonasalessi/cdd-lint/internal/report"
)

// exitAgentFeedback is the exit code of "cdd check --agent" when a unit of
// the edited file is over its limit: the code Claude Code reads as "show
// stderr to the agent". Hook mode never reports a timeout with it.
const exitAgentFeedback = 2

// runAgentCheck implements cdd check --agent: read the agent's event from
// stdin, analyze the file it names and feed the violations back on
// stderr. A file outside the project, or one no language claims, ends
// silently.
func runAgentCheck(c *cobra.Command, path string, in checkInput) error {
	agent, err := agentInput(in)
	if err != nil {
		return err
	}
	root := filepath.Dir(path)
	edited, ok, err := editedPath(c, agent, root)
	if err != nil || !ok {
		return err
	}
	cfg, err := loadCheckConfig(c, path)
	if err != nil {
		return err
	}
	res, err := analyze.Run(c.Context(), analyze.Request{
		Root: root, Config: cfg, Languages: languages.All(),
		Paths: []string{edited}, SkipUnclaimed: true,
	})
	if err != nil {
		return err
	}
	return agentFeedback(c, res, in)
}

// agentInput resolves --agent and refuses the inputs it cannot compose
// with: the event names the file, so paths and --staged have no place.
func agentInput(in checkInput) (agenthook.Agent, error) {
	agent, ok := agenthook.Lookup(in.agent)
	if !ok {
		return nil, fmt.Errorf("--agent: %q is not one of %s", in.agent, strings.Join(agenthook.IDs(), ", "))
	}
	if len(in.args) > 0 {
		return nil, errors.New("--agent takes no paths")
	}
	if in.staged {
		return nil, errors.New("--agent and --staged are exclusive")
	}
	return agent, nil
}

// editedPath reads the event and returns the edited file as a
// slash-separated path relative to root. ok is false when the file lies
// outside root: it belongs to another project and is not this run's.
func editedPath(c *cobra.Command, agent agenthook.Agent, root string) (string, bool, error) {
	event, err := io.ReadAll(c.InOrStdin())
	if err != nil {
		return "", false, err
	}
	edited, err := agent.EditedFile(event)
	if err != nil {
		return "", false, err
	}
	absRoot, err := resolvedAbs(root)
	if err != nil {
		return "", false, err
	}
	abs, err := resolvedAbs(edited)
	if err != nil {
		return "", false, err
	}
	rel, ok := relativeTo(absRoot, abs)
	return filepath.ToSlash(rel), ok, nil
}

// agentFeedback renders the violations to stderr and exits with the
// feedback code; a file within its limits stays silent. The configured
// reporter is not consulted: the report is for the agent, not a document.
func agentFeedback(c *cobra.Command, res analyze.RunResult, in checkInput) error {
	if res.Violations() == 0 {
		return nil
	}
	format := in.format
	if format == "" {
		format = config.FormatConsole
	}
	if err := report.Write(c.ErrOrStderr(), format, res, in.opts); err != nil {
		return err
	}
	return exitCodeError{code: exitAgentFeedback}
}
