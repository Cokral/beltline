package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// AssistantAction is the core command the assistant offers; see core.AssistantActionsOf.
type AssistantAction = core.AssistantAction

// AssistantResource is the configuration assistant as a resource: its reconciled
// running state, the session to attach to, the selection the next session uses
// and what is wrong in the files it changed.
type AssistantResource struct {
	Running       bool
	Busy          bool
	Session       string
	Command       []string
	Configuration AssistantConfiguration
	CheckCommand  []string
	Problems      []string
	Actions       []AssistantAction
}

// ReadAssistant reads the assistant resource. It is read only: it never launches,
// stops or reconciles a conversation.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReadAssistant() AssistantResource {
	c.Lock()
	defer c.EndTurn()
	a := &c.assistant
	return AssistantResource{
		Running: a.running,
		Busy:    a.busy,
		Session: a.session,
		Command: assistantCloneStrings(a.command),
		Configuration: AssistantConfiguration{
			Backend:  c.settings.ConfigurationBackend,
			Model:    c.settings.ConfigurationModel,
			Thinking: core.CopyPointer(c.settings.ConfigurationThinking),
		},
		CheckCommand: assistantCloneStrings(a.checkCommand),
		Problems:     assistantCloneStrings(a.problems),
		Actions: core.AssistantActionsOf(a.busy, a.running, c.settings.ConfigurationModel, func() *string {
			return c.modelWarning(c.settings.ConfigurationModel, c.settings.ConfigurationBackend, "Assistant")
		}),
	}
}

// assistantCloneStrings copies a string slice, keeping nil as nil.
func assistantCloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string{}, values...)
}
