// Tests of the none outcome and polling on a watch: a configured signal beats the quiet outcome, a
// quiet pull holds the run, a stale quiet result is dropped when the run moves on, and a poll inside a
// minute is skipped.

package coordinator

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// sameJSON fails unless a and b encode to the same JSON.
func sameJSON(t *testing.T, what string, a, b any) {
	t.Helper()
	left, err := jsonx.Marshal(a)
	if err != nil {
		t.Fatalf("%s: encode: %v", what, err)
	}
	right, err := jsonx.Marshal(b)
	if err != nil {
		t.Fatalf("%s: encode: %v", what, err)
	}
	if string(left) != string(right) {
		t.Fatalf("%s differs:\n got %s\nwant %s", what, left, right)
	}
}

// lastOutcome is the outcome of the stage's last attempt, or "(nil)".
func lastOutcome(run core.Run, stage string) string {
	attempt := run.LastAttempt(stage)
	if attempt == nil || attempt.Outcome == nil {
		return "(nil)"
	}
	return *attempt.Outcome
}

// TestAPollInsideAMinuteIsSkippedUnlessForced
func TestAPollInsideAMinuteIsSkippedUnlessForced(t *testing.T) {
	t.Parallel()
	quiet := coordQuiet("quiet", core.ToneNeutral, holdThen())
	f := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{}, none: &quiet})
	f.checkQuietly()
	coordCheck(t, f.run().State == core.RunStateRunning, "state after first unforced poll = %v", f.run().State)
	coordCheck(t, len(f.watchOutcomes()) == 0, "outcomes after first unforced poll = %v", f.watchOutcomes())
	f.open()
	f.h.clock.Set(sessionSampleT0.Add(59 * time.Second))
	f.checkQuietly()
	coordCheck(t, f.run().State == core.RunStateRunning, "state inside a minute = %v", f.run().State)
	coordCheck(t, len(f.watchOutcomes()) == 0, "outcomes inside a minute = %v", f.watchOutcomes())
	f.check()
	coordEqual(t, "forced outcomes", f.watchOutcomes(), []string{"quiet"})
	coordCheck(t, f.run().State == core.RunStateHeld, "state after forced poll = %v, want held", f.run().State)
}

// TestAStaleNoneIsDiscardedWhenTheRunMovesOrStops
func TestAStaleNoneIsDiscardedWhenTheRunMovesOrStops(t *testing.T) {
	t.Parallel()
	quiet := coordQuiet("quiet", core.ToneNeutral, holdThen())

	stopped := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{}, none: &quiet})
	stopped.open()
	done := stopped.checking()
	stopped.apply(core.Event{Kind: core.EventKindDrop, Reason: "dropped by you"})
	stopped.gh.ReleasePulls()
	<-done
	stopped.h.coord.Idle()
	coordCheck(t, stopped.run().State == core.RunStateDropped, "stopped state = %v, want dropped", stopped.run().State)
	coordCheck(t, stopped.run().Reason != nil && *stopped.run().Reason == "dropped by you", "stopped reason = %v", stopped.run().Reason)
	coordCheck(t, len(stopped.watchOutcomes()) == 0, "stopped outcomes = %v, want none", stopped.watchOutcomes())
	stored := stopped.stored()
	coordCheck(t, len(stored) == 1, "stored runs = %d", len(stored))
	sameJSON(t, "stopped stored run", stored[0], stopped.run())

	moved := newH5m3Watch(t, watchOptions{events: []core.WatchEvent{}, none: &quiet})
	moved.open()
	movingDone := moved.checking()
	attemptID := moved.run().CurrentAttempt().ID
	moved.update(func(run *core.Run) {
		run.StageID = "build"
		run.State = core.RunStateQueued
	})
	moved.gh.ReleasePulls()
	<-movingDone
	moved.h.coord.Idle()
	coordCheck(t, moved.run().StageID == "build", "moved stage = %s, want build", moved.run().StageID)
	coordCheck(t, moved.run().State == core.RunStateQueued, "moved state = %v, want queued", moved.run().State)
	coordCheck(t, len(moved.watchOutcomes()) == 0, "moved outcomes = %v, want none", moved.watchOutcomes())
	attempts := watchAttempts(moved.run())
	coordCheck(t, len(attempts) > 0, "no watch attempts")
	left := attempts[len(attempts)-1]
	coordCheck(t, left.ID == attemptID, "left attempt is not the open one")
	coordCheck(t, left.Outcome == nil, "left attempt has an outcome")
	coordCheck(t, left.EndedAt == nil, "left attempt has an end")
	stored = moved.stored()
	coordCheck(t, len(stored) == 1, "stored runs = %d", len(stored))
	sameJSON(t, "moved stored run", stored[0], moved.run())
}
