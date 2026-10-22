// The agent commands the local API and the agent editor share: a new agent draft,
// saving, archiving and resetting an agent. They follow the belt commands' lock
// rules in coordinator_belt_commands.go.

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// NewAgentDraft returns a new unsaved agent from the existing library defaults.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) NewAgentDraft() core.Agent {
	c.Lock()
	defer c.EndTurn()
	return c.library.NewAgent()
}

// SaveAgentCommand saves an agent as a version only when its content changed.
// Rejected saves carry the individual problems and write nothing.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) SaveAgentCommand(agent core.Agent) (core.AgentRecord, error) {
	c.Lock()
	defer c.EndTurn()
	saved, err := c.library.SaveAgent(agent)
	if err != nil {
		return core.AgentRecord{}, err
	}
	for _, record := range c.library.agents {
		if record.Agent.ID == saved.ID {
			record.Agent = cloneAgent(record.Agent)
			return record, nil
		}
	}
	return core.AgentRecord{}, core.NewValidationFailure("The saved agent "+saved.ID+" could not be read back.", nil)
}

// ArchiveAgentCommand archives or restores an agent. A failed write leaves the record as it was.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ArchiveAgentCommand(id string, archived bool) (core.AgentRecord, error) {
	c.Lock()
	defer c.EndTurn()
	return c.library.ArchivingAgent(id, archived)
}

// ResetAgentCommand restores a built-in agent to its shipped content through the
// normal save checks. A non-built-in id is rejected.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ResetAgentCommand(id string) (core.AgentRecord, error) {
	c.Lock()
	defer c.EndTurn()
	shipped := core.DefaultsBuiltIn(id, nil)
	if shipped == nil {
		return core.AgentRecord{}, core.NewValidationFailure(core.AgentActionsResetNotBuiltIn, nil)
	}
	// The same rule the editor's Reset action follows: nothing to reset when the latest saved version already is the shipped one.
	if latest := c.library.LatestAgent(id); latest != nil && shipped.SameContent(latest.Agent) {
		return core.AgentRecord{}, core.NewValidationFailure(core.AgentActionsResetUnchanged, nil)
	}
	saved, err := c.library.ResetAgent(id)
	if err != nil {
		return core.AgentRecord{}, err
	}
	for _, record := range c.library.agents {
		if record.Agent.ID == saved.ID {
			record.Agent = cloneAgent(record.Agent)
			return record, nil
		}
	}
	return core.AgentRecord{}, core.NewValidationFailure("The reset agent "+saved.ID+" could not be read back.", nil)
}
