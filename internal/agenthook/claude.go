package agenthook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// claude is Claude Code: a PostToolUse hook on Edit and Write, listed
// under "hooks" in the project's .claude/settings.json. The command gets
// the event on stdin and its stderr reaches the agent on exit 2.
type claude struct{}

const (
	claudeID            = "claude"
	claudeSettings      = ".claude/settings.json"
	claudeLocalSettings = ".claude/settings.local.json"
	// The keys of the settings document the entry lives under.
	hooksKey    = "hooks"
	postToolUse = "PostToolUse"
	commandKey  = "command"
	// editMatcher selects the tools that write a file.
	editMatcher = "Edit|Write"
)

func (claude) ID() string { return claudeID }

func (claude) Summary() string {
	return "Install a Claude Code hook that checks every file the agent edits"
}

func (claude) Settings(local bool) string {
	if local {
		return claudeLocalSettings
	}
	return claudeSettings
}

// Install puts the cdd entry into hooks.PostToolUse, over the one a
// previous run left or at the end, and leaves everything else as it was.
func (claude) Install(path, command string) (bool, error) {
	s, err := readSettings(path)
	if err != nil {
		return false, err
	}
	if s == nil {
		s = &settingsFile{doc: map[string]any{}, mode: newSettingsMode}
	}
	events, err := s.events(path)
	if err != nil {
		return false, err
	}
	if events == nil {
		events = map[string]any{}
		s.doc[hooksKey] = events
	}
	entries, err := entriesOf(events, path)
	if err != nil {
		return false, err
	}
	entries, updated := placeEntry(entries, claudeEntry(command))
	events[postToolUse] = entries
	return updated, s.write(path)
}

// Remove drops every cdd entry, prunes what that leaves empty and deletes
// the file when nothing remains in it.
func (claude) Remove(path string) (bool, error) {
	s, err := readSettings(path)
	if err != nil || s == nil {
		return false, err
	}
	events, err := s.events(path)
	if err != nil || events == nil {
		return false, err
	}
	entries, err := entriesOf(events, path)
	if err != nil {
		return false, err
	}
	kept := slices.DeleteFunc(slices.Clone(entries), isClaudeEntry)
	if len(kept) == len(entries) {
		return false, nil
	}
	prune(s.doc, events, kept)
	if len(s.doc) == 0 {
		return true, os.Remove(path)
	}
	return true, s.write(path)
}

// EditedFile is tool_input.file_path, which Edit and Write both fill.
func (claude) EditedFile(event []byte) (string, error) {
	var e struct {
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(event, &e); err != nil {
		return "", fmt.Errorf("claude hook event: %w", err)
	}
	if e.ToolInput.FilePath == "" {
		return "", errors.New("claude hook event: no tool_input.file_path")
	}
	return e.ToolInput.FilePath, nil
}

// events is the "hooks" object of the document: nil when absent, an error
// when it is something else.
func (s *settingsFile) events(path string) (map[string]any, error) {
	v, ok := s.doc[hooksKey]
	if !ok {
		return nil, nil
	}
	events, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s is not an object", path, hooksKey)
	}
	return events, nil
}

// entriesOf is the PostToolUse list of the events object: nil when absent,
// an error when it is something else.
func entriesOf(events map[string]any, path string) ([]any, error) {
	v, ok := events[postToolUse]
	if !ok {
		return nil, nil
	}
	entries, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: %s.%s is not a list", path, hooksKey, postToolUse)
	}
	return entries, nil
}

// claudeEntry is the PostToolUse entry the hook is: one matcher, one
// command hook.
func claudeEntry(command string) map[string]any {
	return map[string]any{
		"matcher": editMatcher,
		hooksKey:  []any{map[string]any{"type": commandKey, commandKey: command}},
	}
}

// placeEntry puts entry over the first cdd entry, or at the end when there
// is none; the boolean says which.
func placeEntry(entries []any, entry map[string]any) ([]any, bool) {
	at := slices.IndexFunc(entries, isClaudeEntry)
	if at < 0 {
		return append(entries, entry), false
	}
	entries[at] = entry
	return entries, true
}

// isClaudeEntry recognizes the entry by its command, so a --config variant
// installed earlier is still ours.
func isClaudeEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, ok := m[hooksKey].([]any)
	return ok && slices.ContainsFunc(hooks, isClaudeHook)
}

func isClaudeHook(hook any) bool {
	m, ok := hook.(map[string]any)
	if !ok {
		return false
	}
	command, ok := m[commandKey].(string)
	return ok && strings.HasPrefix(command, Command(claudeID, ""))
}

// prune stores kept as the PostToolUse list and drops the list, then the
// events object, when they are empty.
func prune(doc, events map[string]any, kept []any) {
	events[postToolUse] = kept
	if len(kept) == 0 {
		delete(events, postToolUse)
	}
	if len(events) == 0 {
		delete(doc, hooksKey)
	}
}
