package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestRecoveryFinishesAFailedEraseAndThenSkipsTheErasuresFolder checks that recovery still retries an
// intent a failed erase left behind, even after an earlier pass found the folder clear, and that
// the recovery pass skips the folder once nothing is left in it.
func TestRecoveryFinishesAFailedEraseAndThenSkipsTheErasuresFolder(t *testing.T) {
	t.Parallel()
	simRunner := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
	h := newHarness(t, harnessOptions{})
	c := coordWith(t, h, func(s *Services) { s.Runner = simRunner })
	c.Lock()
	c.recoverErasures(bgCtx)
	clear := c.erasuresClear
	c.EndTurn()
	if !clear {
		t.Fatal("a pass over an empty erasures folder did not mark it clear")
	}
	run := addRun(c, c.Belts()[0].Belt, issue142())
	c.Lock()
	c.sessions.start(bgCtx, run.ID, nil)
	c.book.update(run.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	c.EndTurn()

	failure := "tmux did not stop"
	simRunner.StopError = &failure
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "tmux did not stop") {
		t.Fatalf("erase error = %v, want the stop failure", err)
	}
	c.Lock()
	clear = c.erasuresClear
	c.EndTurn()
	if clear {
		t.Fatal("the folder is still marked clear with an intent in it")
	}

	simRunner.StopError = nil
	c.Lock()
	c.recoverErasures(bgCtx)
	c.EndTurn()
	if c.Run(run.ID) != nil {
		t.Fatal("recovery did not finish the erasure")
	}
	if _, pending := h.storage.PendingErasure(run.ID); pending {
		t.Fatal("the erasure is still pending")
	}
	c.Lock()
	clear = c.erasuresClear
	c.EndTurn()
	if !clear {
		t.Fatal("the folder is not marked clear once the erasure finished")
	}
}
