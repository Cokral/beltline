// The coordinator's reads for the terminal route: the runner's attach command, which the route calls on
// the coordinator's runner, and the configuration assistant's session name.

package coordinator

import "context"

// TerminalAttachCommand is the command that attaches a terminal to session,
// read-only when asked; nil when the session is not running. The runner call runs without the
// lock, since it starts a process.
// Lock: exported entry point; takes c.mu. Suspends: runner.AttachCommand.
func (c *Coordinator) TerminalAttachCommand(ctx context.Context, session string, readOnly bool) []string {
	c.Lock()
	defer c.EndTurn()
	var command []string
	c.Suspend(func() { command = c.runner.AttachCommand(ctx, session, readOnly) })
	return command
}

// AssistantSessionName is the configuration assistant's tmux session, one per
// data folder.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AssistantSessionName() string {
	c.Lock()
	defer c.EndTurn()
	return c.assistant.session
}
