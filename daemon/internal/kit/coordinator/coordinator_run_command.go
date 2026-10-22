// The steps every run command takes: look the run up, ask the action table,
// apply what the command decided and answer with the run as it is after.
// coordinator_run_commands.go and coordinator_run_commands_settings.go hold
// the commands, each only its own checks and change.

package coordinator

import (
	"reflect"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// runGone is the refusal for a run that is not there.
const runGone = "This run is gone."

// runCommand is one command against one run, between looking the run up and
// answering: the action context it is decided with and the run as the command
// found it. It lives for one turn, with c.mu held.
type runCommand struct {
	c       *Coordinator
	command core.RunCommand
	context core.RunActionContext
}

// commandOn starts command against the run with id. A missing run is refused
// before any target is looked at.
// Lock: runs with c.mu held.
func (c *Coordinator) commandOn(id string, command core.RunCommand) (*runCommand, error) {
	context, err := c.requiredActionContext(id)
	if err != nil {
		return nil, err
	}
	return &runCommand{c: c, command: command, context: context}, nil
}

// run is the run as the command found it.
func (r *runCommand) run() core.Run { return r.context.Run }

// refuse is the action table's refusal of the command at target, as the error
// the API answers with, or nil when the table allows it. An empty target asks
// about the command itself.
// Lock: runs with c.mu held.
func (r *runCommand) refuse(target core.RunActionTarget) error {
	if refusal := core.RunAvailabilityRefusal(r.command, r.context, target); refusal != nil {
		return core.NewValidationFailure(*refusal, nil)
	}
	return nil
}

// apply applies event to the run and saves it; failure is the refusal when it
// could not be.
// Lock: runs with c.mu held.
func (r *runCommand) apply(event core.Event, failure string) error {
	if !r.c.book.apply(r.context.Run.ID, event, nil, nil, nil) {
		return core.NewValidationFailure(failure, nil)
	}
	return nil
}

// answer is the run after the command. A run that is gone by now is refused as
// gone; saved, when given, says whether the run shows the command's change,
// and failure is the refusal when it does not.
// Lock: runs with c.mu held.
func (r *runCommand) answer(failure string, saved func(before, after core.Run) bool) (core.Run, error) {
	after := r.c.book.Run(r.context.Run.ID)
	if after == nil {
		return core.Run{}, core.NewValidationFailure(runGone, nil)
	}
	if saved != nil && !saved(r.context.Run, *after) {
		return core.Run{}, core.NewValidationFailure(failure, nil)
	}
	return *after, nil
}

// changed is the saved check of a command that must change the run.
func changed(before, after core.Run) bool { return !reflect.DeepEqual(before, after) }
