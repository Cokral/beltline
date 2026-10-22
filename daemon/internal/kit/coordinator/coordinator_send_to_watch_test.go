package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestAWatchLookupThatOutlivesSendToDoesNotHoldTheFreshWatch checks that Send to
// stops a displaced watch: its pull-request lookup, answering with nothing after the
// old attempt's grace ran out, leaves the fresh attempt at the same watch running.
func TestAWatchLookupThatOutlivesSendToDoesNotHoldTheFreshWatch(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordBuildThenWatch()}, OpensNoPulls: true})
	h.Until("the run is running at its watch", func() bool {
		r := h.nowRun()
		return r != nil && r.Stage() != nil && r.Stage().Kind() == core.StageKindWatch && r.State == core.RunStateRunning
	})
	displaced := h.Run().Attempts[len(h.Run().Attempts)-1]
	h.clock.Set(displaced.StartedAt.Add(watchesGrace + 5*time.Second))
	h.github.HoldPulls()
	checking := make(chan struct{})
	go func() {
		defer close(checking)
		h.coord.Lock()
		h.coord.watches.check(context.Background(), true)
		h.coord.EndTurn()
	}()
	coordWaits(t, "the pull request read waits", func() bool { return h.github.WaitingPulls() > 0 })
	id := h.Run().ID
	watch := h.Run().StageID
	h.coord.Resume(id, &watch, nil, false, nil)
	fresh := h.coord.Run(id).Attempts[len(h.coord.Run(id).Attempts)-1]
	if fresh.ID == displaced.ID || !fresh.IsOpen() {
		t.Fatalf("attempt after Send to = %+v, want a fresh open watch attempt", fresh)
	}
	h.github.ReleasePulls()
	<-checking
	run := h.coord.Run(id)
	last := run.Attempts[len(run.Attempts)-1]
	if run.State != core.RunStateRunning || last.ID != fresh.ID || !last.IsOpen() {
		t.Fatalf("run after the late read = %s (%v), last attempt %+v; want running with the fresh watch open", run.State, run.Reason, last)
	}
}
