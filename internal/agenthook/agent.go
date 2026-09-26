// Package agenthook installs cdd into the hook system of a coding agent
// and reads the events that system sends, so the agent hears about a
// violation in the file it just edited.
//
// Each agent is a strategy behind the registry in registry.go: one file
// that knows the agent's settings file, the entry it takes and the event
// it sends. Adding an agent is that file plus one line in the registry;
// the command tree never names one.
package agenthook

import "strings"

// Agent is what differs from one coding agent to the next.
type Agent interface {
	// ID names the agent: the "cdd hook" subcommand and the --agent value.
	ID() string
	// Summary is the one-line help of the subcommand.
	Summary() string
	// Settings is the settings file the hook goes into, relative to the
	// project directory; local selects the personal, untracked variant.
	Settings(local bool) string
	// Install puts an entry running command into the settings file at
	// path, replacing the entry a previous run left; updated says which.
	Install(path, command string) (updated bool, err error)
	// Remove takes the entry out again and reports whether there was one.
	Remove(path string) (removed bool, err error)
	// EditedFile is the path of the file the event says the agent edited.
	EditedFile(event []byte) (string, error)
}

// Command is the command line an agent's hook runs: the check in hook
// mode, with --config when the configuration is away from the default
// place. configPath is relative to the project directory and is quoted
// for the POSIX shell the agent runs the command through.
func Command(id, configPath string) string {
	command := "cdd check --agent " + id
	if configPath != "" {
		command += " --config " + shellQuote(configPath)
	}
	return command
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
