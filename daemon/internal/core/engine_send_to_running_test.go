// Send to… and Drop… on routes that are doing work, not only held or parked ones.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// engineSendToStops reports whether effects stop an agent session.
func engineSendToStops(effects []Effect) bool {
	return slices.ContainsFunc(effects, func(effect Effect) bool {
		return effect.Kind == EffectKindStopSession || effect.Kind == EffectKindStop
	})
}

// TestSendToStopsARunningAgentWithoutAnAbort: a running agent is sent to another
// stage directly. Its session is stopped, its attempt closes as stopped, and the
// destination queues with no attempt until its own session starts.
func TestSendToStopsARunningAgentWithoutAnAbort(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	first := run.Attempts[0]
	effects := EngineAdvance(&run, EventResume(new("spec"), new("  Use the header.  "), nil), engineT0.Add(time.Second))
	if !engineSendToStops(effects) {
		t.Fatalf("effects = %+v, want the displaced session stopped", effects)
	}
	if run.StageID != "spec" || run.State != RunStateQueued {
		t.Fatalf("stage %q state %q, want spec queued", run.StageID, run.State)
	}
	closed := run.Attempts[0]
	if closed.ID != first.ID || closed.IsOpen() || engineStr(closed.Route) == "" || !strings.HasPrefix(engineStr(closed.Route), "Stopped: sent to ") {
		t.Errorf("displaced attempt = %+v, want closed as stopped", closed)
	}
	if len(run.Attempts) != 1 {
		t.Errorf("attempts = %d, want none for the destination until it starts", len(run.Attempts))
	}
	if leg := run.Focus(); leg == nil || engineStr(leg.Note) != "Use the header." {
		t.Errorf("route note = %v, want the trimmed message", leg)
	}
}

// TestSendToStopsAWatchOrAWaitingApprovalWithoutASession: a watch or an approval that
// is waiting closes its attempt as stopped, and no agent session is stopped.
func TestSendToStopsAWatchOrAWaitingApprovalWithoutASession(t *testing.T) {
	t.Parallel()
	watch := engineRunningWatch()
	effects := EngineAdvance(&watch, EventResume(new("spec"), nil, nil), engineT0.Add(time.Second))
	if engineSendToStops(effects) {
		t.Errorf("watch send to: effects %+v, want no session stop", effects)
	}
	if watch.StageID != "spec" || watch.State != RunStateQueued {
		t.Errorf("watch send to: stage %q state %q", watch.StageID, watch.State)
	}
	if last := watch.Attempts[len(watch.Attempts)-1]; last.IsOpen() || !strings.HasPrefix(engineStr(last.Route), "Stopped: sent to ") {
		t.Errorf("watch attempt = %+v, want closed as stopped", last)
	}

	gate := engineWaitingApproval()
	effects = EngineAdvance(&gate, EventResume(new("spec"), nil, nil), engineT0.Add(time.Second))
	if engineSendToStops(effects) {
		t.Errorf("approval send to: effects %+v, want no session stop", effects)
	}
	if gate.StageID != "spec" || gate.State != RunStateQueued {
		t.Errorf("approval send to: stage %q state %q", gate.StageID, gate.State)
	}
	if last := gate.Attempts[len(gate.Attempts)-1]; last.IsOpen() || !strings.HasPrefix(engineStr(last.Route), "Stopped: sent to ") {
		t.Errorf("approval attempt = %+v, want closed as stopped", last)
	}
}

// TestSendToQueuedWorkMovesWithoutAnAttempt: queued work moves to the chosen stage,
// and no attempt is created at the stage it left.
func TestSendToQueuedWorkMovesWithoutAnAttempt(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	effects := EngineAdvance(&run, EventResume(new("spec"), nil, nil), engineT0)
	if len(effects) != 0 {
		t.Errorf("effects = %+v, want none", effects)
	}
	if run.StageID != "spec" || run.State != RunStateQueued || len(run.Attempts) != 0 {
		t.Errorf("stage %q state %q attempts %d, want spec queued with none", run.StageID, run.State, len(run.Attempts))
	}
}

// TestSendToTheCurrentStageRestartsARunningAgent: sending a running route back to the
// stage it is at ends that session, and with earlier work the new attempt replays the stopped one.
func TestSendToTheCurrentStageRestartsARunningAgent(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	stopped := run.Attempts[0].ID
	EngineAdvance(&run, EventResume(new("triage"), nil, nil), engineT0.Add(time.Second))
	if run.State != RunStateQueued || run.StageID != "triage" || len(run.Attempts) != 1 {
		t.Fatalf("state %q stage %q attempts %d, want triage queued with no new attempt", run.State, run.StageID, len(run.Attempts))
	}
	ev := EventResume(new("triage"), nil, nil)
	ev.Earlier = true
	restarted := engineRunningAgent()
	EngineAdvance(&restarted, ev, engineT0.Add(time.Second))
	EngineAdvance(&restarted, Event{Kind: EventKindSessionStarted, Session: "fresh", SessionID: "fresh-id", Folder: "01-triage-2"}, engineT0.Add(2*time.Second))
	last := restarted.Attempts[len(restarted.Attempts)-1]
	if last.ID == stopped || !engineIDIs(last.Replays, stopped) {
		t.Errorf("fresh attempt replays %q, want the stopped attempt %q", engineStr(last.Replays), stopped)
	}
}

// TestASendToOnAPausedRunStaysPausedAndInvalidatesTheDisplacedAttempt: the personal
// pause still blocks the destination, late events for the displaced attempt do not
// move the run, and the pause settles once the displaced session is gone.
func TestASendToOnAPausedRunStaysPausedAndInvalidatesTheDisplacedAttempt(t *testing.T) {
	t.Parallel()
	for _, destination := range []string{"spec", "watch", "approve"} {
		t.Run(destination, func(t *testing.T) {
			t.Parallel()
			run := engineRunningAgent()
			EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "op"}, engineT0)
			if run.Pause == nil || run.Pause.Phase != RunPausePhasePausing {
				t.Fatalf("pause = %+v, want pausing", run.Pause)
			}
			displaced := run.Attempts[0].ID
			EngineAdvance(&run, EventResume(new(destination), nil, nil), engineT0.Add(time.Second))
			if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
				t.Fatalf("pause = %+v, want paused once the displaced session is gone", run.Pause)
			}
			if run.StageID != destination {
				t.Fatalf("stage %q, want %q", run.StageID, destination)
			}
			if run.Attempts[0].ID != displaced || run.Attempts[0].IsOpen() {
				t.Fatalf("displaced attempt = %+v, want closed", run.Attempts[0])
			}
			before := run.Clone()
			// A late result, a late pause confirmation and a stray session start change nothing.
			EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Session: "bl-test", Output: new("Outcome: complete")}, engineT0.Add(2*time.Second))
			EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "op"}, engineT0.Add(2*time.Second))
			EngineAdvance(&run, Event{Kind: EventKindPauseFailed, Operation: "op", Reason: "late"}, engineT0.Add(2*time.Second))
			if !reflect.DeepEqual(run.StageID, before.StageID) || run.State != before.State || len(run.Attempts) != len(before.Attempts) {
				t.Errorf("late events moved the run: stage %q state %q attempts %d", run.StageID, run.State, len(run.Attempts))
			}
			if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
				t.Errorf("late events changed the pause: %+v", run.Pause)
			}
		})
	}
}

// TestSendToDoesNotRunAWatchOrApprovalUnderAPause: a destination watch or approval
// does not advance on its own events while the personal pause holds the run.
func TestSendToDoesNotRunAWatchOrApprovalUnderAPause(t *testing.T) {
	t.Parallel()
	gate := engineWaitingApproval()
	EngineAdvance(&gate, Event{Kind: EventKindPause, Operation: "op"}, engineT0)
	EngineAdvance(&gate, Event{Kind: EventKindPauseConfirmed, Operation: "op"}, engineT0)
	EngineAdvance(&gate, EventResume(new("approve"), nil, nil), engineT0.Add(time.Second))
	EngineAdvance(&gate, Event{Kind: EventKindDecided, Outcome: "approved"}, engineT0.Add(2*time.Second))
	if gate.State != RunStateWaiting || gate.Pause == nil || gate.Pause.Phase != RunPausePhasePaused {
		t.Errorf("approval under pause: state %q pause %+v", gate.State, gate.Pause)
	}
}

// TestDropEndsAPausedOrParkedRunWithoutResumingIt: drop clears the pause and the park,
// stops the running session and keeps the attempts it had.
func TestDropEndsAPausedOrParkedRunWithoutResumingIt(t *testing.T) {
	t.Parallel()
	paused := engineRunningAgent()
	EngineAdvance(&paused, Event{Kind: EventKindPause, Operation: "op"}, engineT0)
	attempts := len(paused.Attempts)
	effects := EngineAdvance(&paused, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0.Add(time.Second))
	if !engineSendToStops(effects) {
		t.Errorf("paused drop effects = %+v, want the session stopped", effects)
	}
	if paused.State != RunStateDropped || paused.Pause != nil || paused.Parking != nil || len(paused.Attempts) != attempts {
		t.Errorf("paused drop: state %q pause %+v parking %v attempts %d", paused.State, paused.Pause, paused.Parking != nil, len(paused.Attempts))
	}
	if engineStr(paused.Reason) != "dropped by you" {
		t.Errorf("reason = %q", engineStr(paused.Reason))
	}

	parked := engineRunningAgent()
	EngineAdvance(&parked, Event{Kind: EventKindParkRun}, engineT0)
	EngineAdvance(&parked, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0.Add(time.Second))
	if parked.State != RunStateDropped || parked.Parking != nil {
		t.Errorf("parked drop: state %q parking %v", parked.State, parked.Parking != nil)
	}
}

// TestSendToEndsAParkAndKeepsAConcurrentPause: Send to ends a whole-run park, and a
// personal pause that is in place stays, so the destination still waits.
func TestSendToEndsAParkAndKeepsAConcurrentPause(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	EngineAdvance(&run, Event{Kind: EventKindParkRun}, engineT0)
	if run.Parking == nil {
		t.Fatal("the run did not park")
	}
	run.Pause = &RunPause{Phase: RunPausePhasePaused, Operation: "op", RequestedAt: jsonx.Date{Time: engineT0}}
	EngineAdvance(&run, EventResume(new("spec"), nil, nil), engineT0.Add(time.Second))
	if run.Parking != nil || run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
		t.Fatalf("parking %v pause %+v, want unparked and still paused", run.Parking != nil, run.Pause)
	}
	if run.StageID != "spec" || run.State != RunStateQueued {
		t.Errorf("stage %q state %q, want spec queued", run.StageID, run.State)
	}
}

// TestSendToDiscardsTheDisplacedContinue: a Continue queued for the displaced attempt
// when its pause lifts is discarded by the move, so the stopped turn is not continued.
func TestSendToDiscardsTheDisplacedContinue(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "op"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "op"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "op"}, engineT0)
	continuation := run.Attempts[0].Interruption
	if continuation == nil || continuation.Continuation == nil || continuation.Continuation.Status != AttemptInterruptionContinuationStatusQueued {
		t.Fatalf("interruption = %+v, want a queued Continue before the move", continuation)
	}
	EngineAdvance(&run, EventResume(new("spec"), nil, nil), engineT0.Add(time.Second))
	if got := run.Attempts[0].Interruption.Continuation.Status; got != AttemptInterruptionContinuationStatusInvalidated {
		t.Errorf("continue status = %q, want invalidated", got)
	}
	if run.StageID != "spec" || run.Attempts[0].IsOpen() {
		t.Errorf("stage %q, displaced attempt open %v", run.StageID, run.Attempts[0].IsOpen())
	}
}
