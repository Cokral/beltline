package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// engineStr is a string pointer's text, "<nil>" for nil, for failure messages and
// comparisons that treat nil as a value of its own.
func engineStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// engineWaitingApproval is a run waiting at
// the approval stage.
func engineWaitingApproval() Run {
	run := engineSampleRun()
	engineFinishOutcome(&run, "ready to implement")
	engineFinishOutcome(&run, "complete")
	return run
}

// engineRunningWatch is a run at its watch.
func engineRunningWatch() Run {
	run := engineSampleRun()
	engineToWatch(&run)
	return run
}

// engineEndedAt reports whether the attempt ended at when.
func engineEndedAt(a Attempt, when time.Time) bool {
	return a.EndedAt != nil && a.EndedAt.Equal(when)
}

// TestAbortingQueuedWorkHoldsItWithoutAnAttemptOrStop: queued work is abortable. It
// is held where it is, with no attempt and no session to stop, and a personal pause stays.
func TestAbortingQueuedWorkHoldsItWithoutAnAttemptOrStop(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	if run.State != RunStateQueued || len(run.Attempts) != 0 {
		t.Fatalf("sample: state %q attempts %d", run.State, len(run.Attempts))
	}
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	if effects := EngineAdvance(&run, Event{Kind: EventKindAbort}, engineT0.Add(time.Second)); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none", effects)
	}
	if run.State != RunStateHeld || engineStr(run.Reason) != "stopped by you" || run.StageID != "triage" {
		t.Errorf("held: state %q reason %q stage %q", run.State, engineStr(run.Reason), run.StageID)
	}
	if len(run.Attempts) != 0 {
		t.Errorf("abort of queued work made %d attempts, want none", len(run.Attempts))
	}
	if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
		t.Errorf("pause = %+v, want the personal pause kept", run.Pause)
	}
}

// TestAStageThatIsNotRunningOrWaitingCannotBeAborted: only queued or running work, a
// running watch or a waiting approval takes an abort; every other state is left as it is.
func TestAStageThatIsNotRunningOrWaitingCannotBeAborted(t *testing.T) {
	t.Parallel()
	var runs []Run
	for _, state := range []RunState{RunStateHeld, RunStateParked, RunStateShipped, RunStateDropped} {
		run := engineSampleRun()
		run.State = state
		runs = append(runs, run)
	}
	heldWatch := engineSampleRun()
	engineToWatch(&heldWatch)
	EngineAdvance(&heldWatch, Event{Kind: EventKindWatched, Watch: WatchEventClosed, Detail: new("#201")}, engineT0)
	runs = append(runs, heldWatch)
	for _, start := range runs {
		run := start.Clone()
		before := start.Clone()
		if effects := EngineAdvance(&run, Event{Kind: EventKindAbort}, engineT0.Add(time.Second)); len(effects) != 0 {
			t.Errorf("abort on %s: effects %+v, want none", before.State, effects)
		}
		if !reflect.DeepEqual(run, before) {
			t.Errorf("abort on %s changed the run:\n%s", before.State, testkit.Diff(before, run))
		}
	}
}

// TestAbortingAWatchOrApprovalHoldsItWithoutStoppingASession: an abort at a watch
// or an approval holds the run in place, with the attempt closed as stopped.
func TestAbortingAWatchOrApprovalHoldsItWithoutStoppingASession(t *testing.T) {
	t.Parallel()
	stoppedAt := engineT0.Add(time.Second)
	watch := engineRunningWatch()
	watchAttempt := watch.Attempts[len(watch.Attempts)-1]
	if watch.State != RunStateRunning || watch.Stage().Kind() != StageKindWatch {
		t.Fatalf("watch: state %q stage kind %q", watch.State, watch.Stage().Kind())
	}
	if effects := EngineAdvance(&watch, Event{Kind: EventKindAbort}, stoppedAt); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none", effects)
	}
	last := watch.Attempts[len(watch.Attempts)-1]
	if watch.State != RunStateHeld || engineStr(watch.Reason) != "stopped by you" || watch.StageID != "watch" {
		t.Errorf("watch held: state %q reason %q stage %q", watch.State, engineStr(watch.Reason), watch.StageID)
	}
	if last.ID != watchAttempt.ID || engineStr(last.Route) != "Stopped: by you" || !engineEndedAt(last, stoppedAt) || last.IsOpen() {
		t.Errorf("watch attempt = %+v", last)
	}

	gate := engineWaitingApproval()
	approval := gate.Attempts[len(gate.Attempts)-1]
	if gate.State != RunStateWaiting || gate.Stage().Kind() != StageKindGate {
		t.Fatalf("gate: state %q stage kind %q", gate.State, gate.Stage().Kind())
	}
	if effects := EngineAdvance(&gate, Event{Kind: EventKindAbort}, stoppedAt); len(effects) != 0 {
		t.Fatalf("gate effects = %+v, want none", effects)
	}
	last = gate.Attempts[len(gate.Attempts)-1]
	if gate.State != RunStateHeld || engineStr(gate.Reason) != "stopped by you" || gate.StageID != "approve" {
		t.Errorf("gate held: state %q reason %q stage %q", gate.State, engineStr(gate.Reason), gate.StageID)
	}
	if last.ID != approval.ID || engineStr(last.Route) != "Stopped: by you" || !engineEndedAt(last, stoppedAt) {
		t.Errorf("gate attempt = %+v", last)
	}
}

// TestResumingAnAbortedWatchOrApprovalStartsAFreshAttempt: resuming a stopped watch
// or approval opens a new attempt; a dropped one is closed as stopped.
func TestResumingAnAbortedWatchOrApprovalStartsAFreshAttempt(t *testing.T) {
	t.Parallel()
	watch := engineRunningWatch()
	EngineAdvance(&watch, Event{Kind: EventKindAbort}, engineT0)
	stoppedWatch := watch.Attempts[len(watch.Attempts)-1]
	if effects := EngineAdvance(&watch, EventResume(nil, nil, nil), engineT0.Add(2*time.Second)); len(effects) != 0 {
		t.Fatalf("resume effects = %+v, want none", effects)
	}
	last := watch.Attempts[len(watch.Attempts)-1]
	if watch.State != RunStateRunning || watch.Stage().Kind() != StageKindWatch || watch.Continues != nil {
		t.Errorf("watch resumed: state %q kind %q continues %q", watch.State, watch.Stage().Kind(), engineStr(watch.Continues))
	}
	if !last.IsOpen() || last.ID == stoppedWatch.ID || last.Continues != nil || last.Number != stoppedWatch.Number+1 {
		t.Errorf("watch attempt = %+v, stopped was %+v", last, stoppedWatch)
	}
	if prev := watch.Attempts[len(watch.Attempts)-2]; engineStr(prev.Route) != "Stopped: by you" {
		t.Errorf("previous route = %q", engineStr(prev.Route))
	}

	gate := engineWaitingApproval()
	EngineAdvance(&gate, Event{Kind: EventKindAbort}, engineT0)
	stoppedGate := gate.Attempts[len(gate.Attempts)-1]
	resumed := EngineAdvance(&gate, EventResume(nil, nil, nil), engineT0.Add(2*time.Second))
	want := []Effect{{Kind: EffectKindNotify, Title: "Approve is waiting for you", Body: "#142 Export invoices as CSV"}}
	if !reflect.DeepEqual(resumed, want) {
		t.Errorf("resume effects = %+v, want %+v", resumed, want)
	}
	if engineHasEffect(resumed, EffectKindStopSession) {
		t.Errorf("resume effects stop a session: %+v", resumed)
	}
	last = gate.Attempts[len(gate.Attempts)-1]
	if gate.State != RunStateWaiting || gate.StageID != "approve" || gate.Continues != nil {
		t.Errorf("gate resumed: state %q stage %q continues %q", gate.State, gate.StageID, engineStr(gate.Continues))
	}
	if !last.IsOpen() || last.ID == stoppedGate.ID || last.Continues != nil || last.Number != stoppedGate.Number+1 {
		t.Errorf("gate attempt = %+v, stopped was %+v", last, stoppedGate)
	}
	if prev := gate.Attempts[len(gate.Attempts)-2]; engineStr(prev.Route) != "Stopped: by you" {
		t.Errorf("gate previous route = %q", engineStr(prev.Route))
	}

	dropped := engineRunningWatch()
	EngineAdvance(&dropped, Event{Kind: EventKindAbort}, engineT0)
	EngineAdvance(&dropped, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0)
	if dropped.State != RunStateDropped || engineStr(dropped.Attempts[len(dropped.Attempts)-1].Route) != "Stopped: by you" {
		t.Errorf("dropped: state %q attempt %+v", dropped.State, dropped.Attempts[len(dropped.Attempts)-1])
	}
}

// TestALateWatchApprovalOrAfkDecisionCannotMoveAnAbortedRun: a watch event, a
// blocked report, or an AFK decision that arrives after a stop changes nothing.
func TestALateWatchApprovalOrAfkDecisionCannotMoveAnAbortedRun(t *testing.T) {
	t.Parallel()
	watch := engineRunningWatch()
	EngineAdvance(&watch, Event{Kind: EventKindAbort}, engineT0)
	heldWatch := watch.Clone()
	if effects := EngineAdvance(&watch, Event{Kind: EventKindWatched, Watch: WatchEventMerged, Detail: new("#201")}, engineT0.Add(time.Second)); len(effects) != 0 {
		t.Errorf("late merge effects = %+v", effects)
	}
	if effects := EngineAdvance(&watch, Event{Kind: EventKindBlocked, Reason: "no pull request from beltline/issue-142"}, engineT0.Add(2*time.Second)); len(effects) != 0 {
		t.Errorf("late blocked effects = %+v", effects)
	}
	if !reflect.DeepEqual(watch, heldWatch) {
		t.Errorf("late watch events moved the run:\n%s", testkit.Diff(heldWatch, watch))
	}

	gate := engineWaitingApproval()
	EngineAdvance(&gate, Event{Kind: EventKindAbort}, engineT0)
	heldGate := gate.Clone()
	if effects := EngineAdvance(&gate, Event{Kind: EventKindDecided, Outcome: "approved"}, engineT0.Add(time.Second)); len(effects) != 0 {
		t.Errorf("late decision effects = %+v", effects)
	}
	afk := AfkOption{ID: "decide approved", Action: AfkAction{Kind: AfkActionKindDecide, Outcome: "approved"}, Label: "Choose “approved”"}
	decision := AfkEventFor(afk, new("Looks good."))
	if decision == nil {
		t.Fatal("AfkEventFor returned no event")
	}
	if effects := EngineAdvance(&gate, *decision, engineT0.Add(2*time.Second)); len(effects) != 0 {
		t.Errorf("late AFK effects = %+v", effects)
	}
	if !reflect.DeepEqual(gate, heldGate) {
		t.Errorf("late AFK decision moved the run:\n%s", testkit.Diff(heldGate, gate))
	}
	if AfkSituationOf(gate, AfkSettings{Enabled: true, Approve: true}) != nil {
		t.Error("a stopped approval still has an AFK situation")
	}
}

// TestAbortHoldsInPlaceAndOnlyStopsTheSession: an abort on a running agent stops
// its session and holds the run where it is.
func TestAbortHoldsInPlaceAndOnlyStopsTheSession(t *testing.T) {
	t.Parallel()
	run := engineRunningRun()
	before := run.Clone()
	stoppedAt := engineT0.Add(time.Second)
	if got := EngineAdvance(&run, Event{Kind: EventKindAbort}, stoppedAt); !reflect.DeepEqual(got, []Effect{{Kind: EffectKindStopSession}}) {
		t.Fatalf("effects = %+v, want stopSession", got)
	}
	if run.State != RunStateHeld || !run.State.NeedsYou() || engineStr(run.Reason) != "stopped by you" {
		t.Errorf("held: state %q reason %q", run.State, engineStr(run.Reason))
	}
	if run.StageID != before.StageID || !reflect.DeepEqual(run.Stages, before.Stages) || !reflect.DeepEqual(run.Branch, before.Branch) {
		t.Errorf("stage, stages or branch changed")
	}
	last := run.Attempts[len(run.Attempts)-1]
	if last.Folder != before.Attempts[len(before.Attempts)-1].Folder || engineStr(last.Route) != "Stopped: by you" || !engineEndedAt(last, stoppedAt) {
		t.Errorf("attempt = %+v", last)
	}
	if !run.UpdatedAt.Equal(stoppedAt) {
		t.Errorf("updatedAt = %v, want %v", run.UpdatedAt.Time, stoppedAt)
	}
	if answerable := run.Answerable(); answerable == nil || answerable.ID != before.Attempts[len(before.Attempts)-1].ID {
		t.Errorf("answerable = %+v", answerable)
	}
}

// TestAnsweringContinuesTheStoppedSession: answering a stopped session queues the
// stage to continue the same agent session.
func TestAnsweringContinuesTheStoppedSession(t *testing.T) {
	t.Parallel()
	run := engineRunningRun()
	closed := run.Attempts[0]
	EngineAdvance(&run, Event{Kind: EventKindAbort}, engineT0)
	if effects := EngineAdvance(&run, Event{Kind: EventKindAnswered, Answer: new("Use the invoices page.")}, engineT0); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none", effects)
	}
	if run.State != RunStateQueued || engineStr(run.Continues) != closed.ID || engineStr(run.Note) != "Use the invoices page." {
		t.Errorf("answered: state %q continues %q note %q", run.State, engineStr(run.Continues), engineStr(run.Note))
	}
	EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "bl-test-2", SessionID: ValueOf(closed.SessionID), Folder: "01-triage-2"}, engineT0)
	second := run.Attempts[len(run.Attempts)-1]
	if engineStr(second.Continues) != closed.ID || engineStr(second.SessionID) != engineStr(closed.SessionID) || engineStr(second.AgentSessionName()) != engineStr(closed.AgentSessionName()) {
		t.Errorf("second attempt = %+v", second)
	}
	if engineStr(second.Note) != "Use the invoices page." || run.Continues != nil {
		t.Errorf("second note %q, run continues %q", engineStr(second.Note), engineStr(run.Continues))
	}
}

// TestRunningAgainStartsAFreshAttemptAndSendingCanChangeStage: a resume with no
// stage, or with a stage, starts a new session rather than continuing the old one.
func TestRunningAgainStartsAFreshAttemptAndSendingCanChangeStage(t *testing.T) {
	t.Parallel()
	for _, target := range []*string{nil, new("spec")} {
		run := engineRunningRun()
		EngineAdvance(&run, Event{Kind: EventKindAbort}, engineT0)
		EngineAdvance(&run, EventResume(target, nil, nil), engineT0)
		want := ValueOr(target, "triage")
		if run.State != RunStateQueued || run.StageID != want || run.Continues != nil {
			t.Errorf("resume to %s: state %q stage %q continues %q", engineStr(target), run.State, run.StageID, engineStr(run.Continues))
		}
		EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "bl-new", SessionID: "new", Folder: "new"}, engineT0)
		last := run.Attempts[len(run.Attempts)-1]
		if engineStr(last.SessionID) != "new" || last.Continues != nil {
			t.Errorf("fresh attempt = %+v", last)
		}
		if engineStr(run.Attempts[0].Route) != "Stopped: by you" {
			t.Errorf("first route = %q", engineStr(run.Attempts[0].Route))
		}
	}
}

// TestALateOutcomeCannotMoveAnAbortedRun: a session that finishes after its stop
// with an outcome changes nothing.
func TestALateOutcomeCannotMoveAnAbortedRun(t *testing.T) {
	t.Parallel()
	run := engineRunningRun()
	EngineAdvance(&run, Event{Kind: EventKindAbort}, engineT0)
	held := run.Clone()
	text := "Outcome: ready to spec"
	if effects := EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: &text}, engineT0.Add(time.Second)); len(effects) != 0 {
		t.Errorf("late outcome effects = %+v", effects)
	}
	if !reflect.DeepEqual(run, held) {
		t.Errorf("late outcome moved the run:\n%s", testkit.Diff(held, run))
	}
}
