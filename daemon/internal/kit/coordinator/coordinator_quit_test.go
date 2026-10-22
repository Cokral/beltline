// Coordinator tests of the quit contract: the handler runs once, after the turn has ended, and an unset
// handler is a no-op.

package coordinator

import "testing"

// TestInvokeQuitHandlerRunsTheHandlerOutsideTheTurn: the handler may call back into the coordinator,
// which takes the lock, so the turn must already be over when it runs.
func TestInvokeQuitHandlerRunsTheHandlerOutsideTheTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	calls := 0
	c.SetQuitHandler(func() {
		calls++
		_ = c.Paths()
	})
	c.InvokeQuitHandler()
	if calls != 1 {
		t.Fatalf("quit handler ran %d times, want 1", calls)
	}
}

// TestInvokeQuitHandlerWithoutAHandlerDoesNothing: tests and demo mode set no handler.
func TestInvokeQuitHandlerWithoutAHandlerDoesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	h.Coord().InvokeQuitHandler()
}
