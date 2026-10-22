// Engine tests of run pauses with routes: nested splits, reconciliation and
// ambiguous launches.

package core

import (
	"slices"
	"testing"
)

// TestTheFirstDurableTransitionWinsAndAbortOrDropStillMeanWhatTheyDid: whichever of a
// finish and a pause lands first decides; an abort or a drop during a pause still
// means what it did, and another run's events are its own.
func TestTheFirstDurableTransitionWinsAndAbortOrDropStillMeanWhatTheyDid(t *testing.T) {
	t.Parallel()
	finishedFirst := engineRunningAgent()
	EngineAdvance(&finishedFirst, Event{Kind: EventKindSessionFinished, Output: new("Outcome: ready to implement")}, engineT0)
	if finishedFirst.StageID != "spec" {
		t.Fatalf("stage = %q, want spec", finishedFirst.StageID)
	}
	EngineAdvance(&finishedFirst, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	if finishedFirst.Pause == nil || finishedFirst.Pause.Phase != RunPausePhasePaused || finishedFirst.StageID != "spec" {
		t.Errorf("paused after finish: pause %+v stage %q", finishedFirst.Pause, finishedFirst.StageID)
	}
	found := slices.ContainsFunc(finishedFirst.Attempts, func(a Attempt) bool {
		return engineStr(a.Outcome) == "ready to implement"
	})
	if !found {
		t.Error("the finished attempt lost its outcome")
	}

	pausedFirst := engineRunningAgent()
	EngineAdvance(&pausedFirst, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&pausedFirst, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&pausedFirst, Event{Kind: EventKindSessionFinished, Output: new("Outcome: ready to implement")}, engineT0)
	if pausedFirst.StageID != "triage" || pausedFirst.Attempts[len(pausedFirst.Attempts)-1].Outcome != nil {
		t.Errorf("paused first: stage %q outcome %q", pausedFirst.StageID, engineStr(pausedFirst.Attempts[len(pausedFirst.Attempts)-1].Outcome))
	}
	edited := pausedFirst.Subject
	edited.Title = "Edited while paused"
	EngineAdvance(&pausedFirst, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if pausedFirst.Subject.Title == "Edited while paused" {
		t.Error("an edit while paused reached the subject")
	}

	aborted := engineRunningAgent()
	EngineAdvance(&aborted, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&aborted, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&aborted, Event{Kind: EventKindAbort}, engineT0)
	if aborted.State != RunStateHeld || aborted.Pause == nil || !aborted.Pause.BlocksProgress() {
		t.Fatalf("aborted: state %q pause %+v", aborted.State, aborted.Pause)
	}
	EngineAdvance(&aborted, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	for _, a := range aborted.Attempts {
		if engineContinuation(a) != nil {
			t.Errorf("aborted attempt %s has a continuation", a.ID)
		}
	}

	dropped := engineRunningAgent()
	EngineAdvance(&dropped, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&dropped, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0)
	if dropped.State != RunStateDropped || dropped.Pause != nil {
		t.Errorf("dropped: state %q pause %+v", dropped.State, dropped.Pause)
	}

	other := engineSampleRun()
	other.ID = "other"
	EngineAdvance(&other, Event{Kind: EventKindSessionStarted, Session: "bl-other", SessionID: "o", Folder: "f"}, engineT0)
	if other.Pause != nil || other.State != RunStateRunning {
		t.Errorf("other: pause %+v state %q", other.Pause, other.State)
	}
}

// TestOnePauseCoversEveryUnfinishedRouteAndLeavesACompletedRouteCompleted: one pause
// stops every unfinished route; a route that finished stays finished, and a resume
// continues only the routes still unfinished.
func TestOnePauseCoversEveryUnfinishedRouteAndLeavesACompletedRouteCompleted(t *testing.T) {
	t.Parallel()
	run := engineStartRun(engineBothRoutes())
	engineFinishOutcome(&run, "ready to implement")
	complex, simple := new("r:complex"), new("r:simple")
	EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "complex", SessionID: "c", Folder: "c", Leg: complex}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "simple", SessionID: "s", Folder: "s", Leg: simple}, engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	if !engineHasInterrupt(effects, "complex", "p", "r:complex") || !engineHasInterrupt(effects, "simple", "p", "r:simple") {
		t.Errorf("effects = %+v, want an interrupt for each route", effects)
	}
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: complex, Conversation: new("c")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: simple, Conversation: new("s")}, engineT0)
	if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
		t.Fatalf("pause %+v, want paused", run.Pause)
	}
	EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: new("Outcome: done"), Leg: simple}, engineT0)
	if leg := run.LegOn(new("simple")); leg == nil || leg.State != RunStateRunning {
		t.Errorf("simple leg = %+v, want running", leg)
	}
	EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by you", Leg: complex}, engineT0)
	if leg := run.LegOn(new("simple")); leg == nil || leg.State != RunStateRunning {
		t.Errorf("simple leg after the drop = %+v, want running", leg)
	}
	if run.Pause == nil || !run.Pause.BlocksProgress() {
		t.Errorf("pause %+v, want blocking", run.Pause)
	}
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	complexQueued, simpleQueued := false, false
	for _, a := range run.Attempts {
		status := AttemptInterruptionContinuationStatus("")
		if c := engineContinuation(a); c != nil {
			status = c.Status
		}
		switch engineStr(a.Leg) {
		case "r:complex":
			if status == AttemptInterruptionContinuationStatusQueued {
				complexQueued = true
			}
		case "r:simple":
			if status == AttemptInterruptionContinuationStatusQueued {
				simpleQueued = true
			}
		}
	}
	if complexQueued {
		t.Error("the dropped route's continuation is queued")
	}
	if !simpleQueued {
		t.Error("the unfinished route has no queued continuation")
	}
}

// TestANestedSplitPausesTogetherAndAContinuationFailureDoesNotRelaunchItsSibling: a
// pause inside a nested split pauses it together, and a failed continuation is retried
// by hand, not relaunched by itself.
func TestANestedSplitPausesTogetherAndAContinuationFailureDoesNotRelaunchItsSibling(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nestedFixtureBelt(false, true, true))
	engineFinishOutcome(&run, "go")
	EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "assess", SessionID: "a", Folder: "a"}, engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	if !engineHasInterrupt(effects, "assess", "p", "main") {
		t.Errorf("effects = %+v, want interrupt assess p main", effects)
	}
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("a")}, engineT0)
	if run.Pause == nil || !run.Pause.BlocksProgress() || run.StageID != "assess" {
		t.Fatalf("paused: pause %+v stage %q", run.Pause, run.StageID)
	}
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationsAdmitted}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationFailed, Operation: "r", Leg: new("main"), Reason: "could not start"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindRetryContinuation, Leg: new("main")}, engineT0)
	if c := engineContinuation(run.Attempts[len(run.Attempts)-1]); c == nil || c.Status != AttemptInterruptionContinuationStatusQueued {
		t.Fatalf("after retry: continuation %+v, want queued", c)
	}
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunched, Operation: "r", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindRetryContinuation, Leg: new("main")}, engineT0)
	if c := engineContinuation(run.Attempts[len(run.Attempts)-1]); c == nil || c.Status != AttemptInterruptionContinuationStatusLaunched {
		t.Errorf("after the launch: continuation %+v, want launched", c)
	}
}

// TestReconciliationFailureStaysStoppedAndAMoveInvalidatesContinue: a failed
// reconciliation stays stopped until resumed again, and a drop invalidates the
// continuation it would have sent.
func TestReconciliationFailureStaysStoppedAndAMoveInvalidatesContinue(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindReconcileFailed, Reason: "Could not read #142"}, engineT0)
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseReconcileFailed {
		t.Fatalf("pause %+v, want reconcileFailed", run.Pause)
	}
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusQueued {
		t.Errorf("continuation = %+v, want queued", c)
	}
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r", Leg: new("main")}, engineT0)
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusQueued {
		t.Errorf("continuation after launching = %+v, want queued", c)
	}
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseResuming {
		t.Errorf("pause %+v, want resuming", run.Pause)
	}

	moved := engineRunningAgent()
	EngineAdvance(&moved, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&moved, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&moved, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	EngineAdvance(&moved, Event{Kind: EventKindDrop, Reason: "the issue was closed"}, engineT0)
	if moved.State != RunStateDropped || moved.Pause != nil {
		t.Errorf("dropped: state %q pause %+v", moved.State, moved.Pause)
	}
	if c := engineContinuation(moved.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusInvalidated {
		t.Errorf("continuation = %+v, want invalidated", c)
	}
}

// TestAnAmbiguousLaunchIsNotSentAgain: a launch that may or may not have delivered
// Continue is ambiguous, and a retry does not send it again.
func TestAnAmbiguousLaunchIsNotSentAgain(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationsAdmitted}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationAmbiguous, Operation: "r", Leg: new("main"), Reason: "Beltline could not tell whether Continue was delivered"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindRetryContinuation, Leg: new("main")}, engineT0)
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusAmbiguous {
		t.Fatalf("continuation = %+v, want ambiguous", c)
	}
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r", Leg: new("main")}, engineT0)
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusAmbiguous {
		t.Errorf("continuation after launching = %+v, want ambiguous", c)
	}
}
