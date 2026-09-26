package agenthook

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryListsClaude(t *testing.T) { // TC-R1
	all := All()
	require.Len(t, all, 1)
	assert.Equal(t, "claude", all[0].ID())
	assert.Equal(t, []string{"claude"}, IDs())

	agent, ok := Lookup("claude")
	require.True(t, ok)
	assert.Equal(t, "claude", agent.ID())

	_, ok = Lookup("nope")
	assert.False(t, ok)
}

func TestCommand(t *testing.T) { // TC-R2
	assert.Equal(t, "cdd check --agent claude", Command("claude", ""))
	assert.Equal(t, "cdd check --agent claude --config 'sub/cdd.config.yaml'",
		Command("claude", "sub/cdd.config.yaml"))
	assert.Equal(t, `cdd check --agent claude --config 'it'\''s/cdd.config.yaml'`,
		Command("claude", "it's/cdd.config.yaml"))
}
