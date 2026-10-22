// Tests of the task start kick: the intake's start-task call does not kick, so a new run waits for the
// next tick, while Coordinator.startTask kicks.

package coordinator

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

func TestStartTaskCommandDoesNotKickButStartTaskDoes(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Issues: []core.Subject{}, Belts: []core.Belt{manualBelt("tasks")}})
	c := h.coord

	c.Hold()
	if _, err := c.StartTaskCommand("tasks", "acme/web", "Command", "", nil); err != nil {
		t.Fatalf("StartTaskCommand: %v", err)
	}
	c.heldMu.Lock()
	spawned := len(c.held)
	c.heldMu.Unlock()
	if spawned != 0 {
		t.Fatalf("StartTaskCommand spawned %d work items, want none (no kick)", spawned)
	}

	if _, err := c.StartTask("tasks", "acme/web", "Kicked", "", nil); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	c.heldMu.Lock()
	spawned = len(c.held)
	c.heldMu.Unlock()
	if spawned != 1 {
		t.Fatalf("StartTask spawned %d work items, want the kick", spawned)
	}
	c.Release()
	c.Idle()
}
