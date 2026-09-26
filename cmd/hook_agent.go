package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jonasalessi/cdd-lint/internal/agenthook"
)

// newHookAgentCmd builds "cdd hook <id>" for one registered agent. The
// agent is a strategy: this command only knows where the settings file
// is, what command to put there and how to word the receipt.
func newHookAgentCmd(agent agenthook.Agent) *cobra.Command {
	var remove, local bool
	c := &cobra.Command{
		Use:   agent.ID(),
		Short: agent.Summary(),
		Long:  hookAgentLong(agent),
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if remove {
				return runHookAgentRemove(c, agent, local)
			}
			return runHookAgent(c, agent, local, configPath)
		},
	}
	c.Flags().BoolVar(&remove, "remove", false, "take the cdd entry out of the settings file")
	c.Flags().BoolVar(&local, "local", false, "use the personal, untracked settings file instead")
	return c
}

// hookAgentLong is the help of "cdd hook <id>", worded from what the agent
// knows about itself.
func hookAgentLong(agent agenthook.Agent) string {
	return fmt.Sprintf(`hook %[1]s writes an entry into %[2]s, the project settings of
the agent, that runs %[3]q after every file the agent edits.
A unit over its limit in that file is reported back to the agent to fix,
whatever the enforcement says; commits and CI keep their own rules.

The project is the working directory, which is where the agent reads its
settings and runs its hooks. Other settings and other hooks in the file are
preserved; running the command again replaces the entry, so an upgrade of
cdd refreshes it. A configuration away from cdd.config.yaml is baked in as
--config, relative to the project.

--local writes %[4]s, the personal file, instead.
--remove takes the entry out again and deletes the file when nothing else
is in it.`, agent.ID(), agent.Settings(false), agenthook.Command(agent.ID(), ""), agent.Settings(true))
}

// runHookAgent implements cdd hook <id>: the project is the working
// directory, the configuration must exist, then the entry is written.
func runHookAgent(c *cobra.Command, agent agenthook.Agent, local bool, path string) error {
	project, err := resolvedAbs(".")
	if err != nil {
		return err
	}
	baked, err := bakedConfig(project, path)
	if err != nil {
		return err
	}
	settings := filepath.Join(project, agent.Settings(local))
	updated, err := agent.Install(settings, agenthook.Command(agent.ID(), baked))
	if err != nil {
		return untouched(err)
	}
	verb := "installed"
	if updated {
		verb = "updated"
	}
	fmt.Fprintf(c.OutOrStdout(), "%s %s hook at %s\n", verb, agent.ID(), displayPath(settings))
	return nil
}

// runHookAgentRemove implements cdd hook <id> --remove.
func runHookAgentRemove(c *cobra.Command, agent agenthook.Agent, local bool) error {
	project, err := resolvedAbs(".")
	if err != nil {
		return err
	}
	settings := filepath.Join(project, agent.Settings(local))
	removed, err := agent.Remove(settings)
	if err != nil {
		return untouched(err)
	}
	if removed {
		fmt.Fprintf(c.OutOrStdout(), "removed cdd hook from %s\n", displayPath(settings))
	} else {
		fmt.Fprintf(c.OutOrStdout(), "no cdd hook found at %s\n", displayPath(settings))
	}
	return nil
}
