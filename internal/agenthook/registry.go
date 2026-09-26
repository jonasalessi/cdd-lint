package agenthook

import "slices"

// agents is the registry, in the order the help lists them. A new agent is
// appended here and nowhere else.
var agents = []Agent{claude{}}

// All lists the registered agents.
func All() []Agent {
	return slices.Clone(agents)
}

// Lookup finds the agent with the given id.
func Lookup(id string) (Agent, bool) {
	for _, agent := range agents {
		if agent.ID() == id {
			return agent, true
		}
	}
	return nil, false
}

// IDs lists the registered ids, for a usage message.
func IDs() []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID())
	}
	return ids
}
