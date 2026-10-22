package coordinator

import (
	"errors"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// commandMessage is the message of a command's validation failure, or "".
func commandMessage(err error) string {
	var failure core.Failure
	if errors.As(err, &failure) {
		return failure.Message
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// TestRunCommandSteps covers the steps every run command shares: a missing run
// is refused as gone, the action table refuses through refuse, answer refuses
// a run that shows no change when the command needs one, and a run erased
// under the command is answered as gone.
func TestRunCommandSteps(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	c := h.coord
	c.Lock()
	defer c.EndTurn()

	if _, err := c.commandOn("missing", core.RunCommandDrop); commandMessage(err) != runGone {
		t.Fatalf("missing run: got %q, want %q", commandMessage(err), runGone)
	}
	command, err := c.commandOn(id, core.RunCommandUnpark)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.refuse(core.RunActionTarget{}); err == nil {
		t.Fatal("Unpark on a run that isn't parked was not refused")
	}
	command, err = c.commandOn(id, core.RunCommandDrop)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.refuse(core.RunActionTarget{}); err != nil {
		t.Fatalf("Drop on a running run was refused: %v", err)
	}
	if _, err := command.answer("Could not save the drop.", changed); commandMessage(err) != "Could not save the drop." {
		t.Fatalf("unchanged run: got %q", commandMessage(err))
	}
	if after, err := command.answer("", nil); err != nil || after.ID != id {
		t.Fatalf("answer without a saved check: %v, %v", after.ID, err)
	}
	c.book.removeErased(id)
	if _, err := command.answer("Could not save the drop.", changed); commandMessage(err) != runGone {
		t.Fatalf("erased run: got %q, want %q", commandMessage(err), runGone)
	}
}
