package runner

import (
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// CursorAdapter runs a stage in its interactive interface on the chat it
// allocated, which stays open until the agent hands the work back; attempts
// launched before that ran headless with `--print` and exited when done.
// Continue and the assistant open the interactive interface too. Like the other
// runners it never stops to ask (`--force`). Only the output file is
// authoritative; its private chat storage is deliberately not inspected.
type CursorAdapter struct {
	adapterDefaults
}

var _ AgentAdapter = CursorAdapter{}

// Backend is the Cursor CLI.
func (CursorAdapter) Backend() core.Backend { return core.BackendCursor }

// RunningNote says a headless Cursor attempt, so nothing is drawn until the attempt ends.
func (CursorAdapter) RunningNote() *string {
	note := core.BackendCursor.Label() + " runs headless: nothing is drawn until the attempt ends."
	return &note
}

// CreateSessionArguments is `create-chat`: Cursor makes the chat id, saved on the attempt before it starts.
func (CursorAdapter) CreateSessionArguments() []string { return []string{"create-chat"} }

// cursorModelArguments is `--model` for a non-blank model, or nothing.
func cursorModelArguments(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	return []string{"--model", model}
}

// Arguments is a headless `--print` turn that resumes the chat.
func (CursorAdapter) Arguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := []string{"--print", "--output-format", "text", "--force", "--trust", "--resume", sessionID}
	args = append(args, cursorModelArguments(model)...)
	return append(args, "$PROMPT")
}

// ResumeArguments is Arguments: the chat is resumed by its id either way.
func (c CursorAdapter) ResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	return c.Arguments(sessionID, name, model, thinking)
}

// InteractiveArguments resumes the allocated chat in Cursor's interactive
// interface with the brief, the model and `--force`: Cursor takes no thinking setting.
func (c CursorAdapter) InteractiveArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	return c.ConversationArguments(sessionID, name, model, thinking)
}

// InteractiveResumeArguments is InteractiveArguments: the chat is resumed by its id either way.
func (c CursorAdapter) InteractiveResumeArguments(sessionID, name, model string, thinking *core.Thinking, token *string) []string {
	return c.InteractiveArguments(sessionID, name, model, thinking)
}

// ContinueArguments resumes the chat in Cursor's interactive interface.
func (CursorAdapter) ContinueArguments(sessionID, name string, token *string) []string {
	return []string{"--force", "--trust", "--resume", sessionID}
}

// ConversationArguments is an interactive conversation with the prompt.
func (c CursorAdapter) ConversationArguments(sessionID, name, model string, thinking *core.Thinking) []string {
	args := append(c.ContinueArguments(sessionID, name, nil), cursorModelArguments(model)...)
	return append(args, "$PROMPT")
}

// Ending is nil: only the output file says how a Cursor turn ended.
func (CursorAdapter) Ending(directory, sessionID string, after int) *LogEnding { return nil }

// Mark is zero: Cursor keeps no message count.
func (CursorAdapter) Mark(directory, sessionID string) int { return 0 }

// Reply is nil: Cursor's replies are not read from its storage.
func (CursorAdapter) Reply(directory, sessionID string, after int) *string { return nil }

// Usage is nil: the documented output has no usage fields.
func (CursorAdapter) Usage(directory, sessionID, name string, after, marked int) *core.Usage {
	return nil
}

// UsageWithCheckpoint is Usage: Cursor has no usage to count from a checkpoint.
func (c CursorAdapter) UsageWithCheckpoint(directory, sessionID, name string, after, marked int, checkpoint *core.UsageCheckpoint) *core.Usage {
	return c.Usage(directory, sessionID, name, after, marked)
}

// UsageMark is zero.
func (CursorAdapter) UsageMark(directory, sessionID, name string) int { return 0 }
