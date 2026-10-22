// Engine tests of run pauses, the first half; the rest are in
// engine_run_pause_flow_test.go.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// engineHasInterrupt reports whether effects interrupt session's operation on leg.
func engineHasInterrupt(effects []Effect, session, operation, leg string) bool {
	return slices.ContainsFunc(effects, func(effect Effect) bool {
		return effect.Kind == EffectKindInterrupt && effect.Session == session && effect.Operation == operation && effect.Leg == leg
	})
}

// engineContinuation is the continuation of the attempt's interruption, or nil.
func engineContinuation(a Attempt) *AttemptInterruptionContinuation {
	if a.Interruption == nil {
		return nil
	}
	return a.Interruption.Continuation
}

// TestEveryActiveStateCanPauseAndATerminalRunCannot: an active run takes a pause and
// keeps its state; a shipped or dropped run takes nothing.
func TestEveryActiveStateCanPauseAndATerminalRunCannot(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateQueued, RunStateRunning, RunStateWaiting, RunStateHeld, RunStateParked} {
		run := engineSampleRun()
		run.State = state
		run.Legs[0].State = state
		if !RunCanPause(run) {
			t.Fatalf("%s: a run in this state cannot pause", state)
		}
		EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
		if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused || run.State != state {
			t.Errorf("%s: pause %+v state %q", state, run.Pause, run.State)
		}
	}
	for _, state := range []RunState{RunStateDone, RunStateShipped, RunStateDropped} {
		run := engineSampleRun()
		run.State = state
		before := run.Clone()
		if RunCanPause(run) {
			t.Errorf("%s: the run can pause", state)
		}
		if effects := EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0); len(effects) != 0 {
			t.Errorf("%s: effects = %+v", state, effects)
		}
		if !reflect.DeepEqual(run, before) {
			t.Errorf("%s: the run changed", state)
		}
	}
}

// TestARunningAgentIsInterruptedWithoutAnOutcomeAndAQueuedWatchOrStopIsNot: a running
// agent is interrupted and pauses once confirmed; a queued run pauses at once; a
// watch keeps its place while paused, and a held or parked run stays so.
func TestARunningAgentIsInterruptedWithoutAnOutcomeAndAQueuedWatchOrStopIsNot(t *testing.T) {
	t.Parallel()
	running := engineRunningAgent()
	effects := EngineAdvance(&running, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	if !engineHasEffect(effects, EffectKindCancelAfk) {
		t.Errorf("effects = %+v, want cancelAfk", effects)
	}
	if !engineHasInterrupt(effects, "bl-test", "p", "main") {
		t.Errorf("effects = %+v, want interrupt bl-test p main", effects)
	}
	if engineHasEffect(effects, EffectKindComment) {
		t.Error("the pause posts a comment")
	}
	if running.Pause == nil || running.Pause.Phase != RunPausePhasePausing || running.StageID != "triage" {
		t.Fatalf("pausing: pause %+v stage %q", running.Pause, running.StageID)
	}
	if running.Attempts[len(running.Attempts)-1].Outcome != nil {
		t.Error("the interrupted attempt has an outcome")
	}
	EngineAdvance(&running, Event{Kind: EventKindSessionFinished, Output: new("partial\n\nOutcome: ready to implement")}, engineT0)
	if running.StageID != "triage" || !running.Attempts[len(running.Attempts)-1].IsOpen() {
		t.Errorf("after the outcome: stage %q, attempt open %v", running.StageID, running.Attempts[len(running.Attempts)-1].IsOpen())
	}
	EngineAdvance(&running, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	if running.Pause == nil || running.Pause.Phase != RunPausePhasePaused {
		t.Fatalf("confirmed: pause %+v", running.Pause)
	}
	if EngineOccupiesSlot(running, running.WorkingLegs()[0]) {
		t.Error("a paused leg still occupies its slot")
	}

	queued := engineSampleRun()
	if got := EngineAdvance(&queued, Event{Kind: EventKindPause, Operation: "p"}, engineT0); !reflect.DeepEqual(got, []Effect{{Kind: EffectKindCancelAfk}}) {
		t.Errorf("queued effects = %+v, want cancelAfk only", got)
	}
	if queued.Pause == nil || queued.Pause.Phase != RunPausePhasePaused || len(queued.Attempts) != 0 {
		t.Errorf("queued: pause %+v attempts %d", queued.Pause, len(queued.Attempts))
	}

	watch := engineSampleRun()
	engineToWatch(&watch)
	watchStage := watch.StageID
	EngineAdvance(&watch, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&watch, Event{Kind: EventKindWatched, Watch: WatchEventMerged, Detail: new("#1")}, engineT0)
	if watch.Pause == nil || watch.Pause.Phase != RunPausePhasePaused || watch.StageID != watchStage {
		t.Errorf("watch: pause %+v stage %q", watch.Pause, watch.StageID)
	}

	approval := engineSampleRun()
	engineFinishOutcome(&approval, "ready to implement")
	engineFinishOutcome(&approval, "complete")
	EngineAdvance(&approval, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&approval, Event{Kind: EventKindDecided, Outcome: "approved"}, engineT0)
	if approval.State != RunStateWaiting || approval.Pause == nil || approval.Pause.Phase != RunPausePhasePaused {
		t.Errorf("approval: state %q pause %+v", approval.State, approval.Pause)
	}

	// Send to moves a held route under a pause: the destination queues, and the pause
	// keeps it from starting until you resume.
	held := engineRunningAgent()
	EngineAdvance(&held, Event{Kind: EventKindAbort}, engineT0)
	EngineAdvance(&held, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&held, EventResume(nil, nil, nil), engineT0)
	if held.State != RunStateQueued || held.Pause == nil || held.Pause.Phase != RunPausePhasePaused {
		t.Errorf("held: state %q pause %+v", held.State, held.Pause)
	}

	parked := engineSampleRun()
	engineFinishOutcome(&parked, "ready to implement")
	engineFinishOutcome(&parked, "complete")
	EngineAdvance(&parked, Event{Kind: EventKindDecided, Outcome: "parked"}, engineT0)
	if parked.State != RunStateParked {
		t.Fatalf("parked: state %q", parked.State)
	}
	EngineAdvance(&parked, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&parked, EventResume(nil, nil, nil), engineT0)
	if parked.State != RunStateWaiting || parked.Parking != nil || parked.Pause == nil {
		t.Errorf("parked after send to: state %q parking %v pause %+v, want waiting, unparked and still paused", parked.State, parked.Parking != nil, parked.Pause)
	}
}

// TestResumeContinuesOnlyTheInterruptedAgentOnTheSameAttempt: a resume continues the
// interrupted session on the same attempt, keeping its settings and baseline, and the
// session's own outcome then moves the run.
func TestResumeContinuesOnlyTheInterruptedAgentOnTheSameAttempt(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	run.Attempts[0].Settings = &SessionSettings{Model: "kept/model", Thinking: new(ThinkingLow), Runner: new(BackendPi)}
	run.Attempts[0].ReadOnlyBaseline = new("original")
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0.Add(5*time.Second))
	attempt, folder := run.Attempts[0].ID, run.Attempts[0].Folder
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "resume-1"}, engineT0.Add(10*time.Second))
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseResuming {
		t.Fatalf("resuming: pause %+v", run.Pause)
	}
	first := run.Attempts[0]
	if first.ID != attempt || first.Folder != folder || engineStr(first.SessionID) != "sid" {
		t.Errorf("attempt changed: id %q folder %q session %q", first.ID, first.Folder, engineStr(first.SessionID))
	}
	if first.Interruption == nil || engineStr(first.Interruption.Conversation) != "sid" {
		t.Fatalf("interruption = %+v", first.Interruption)
	}
	if c := engineContinuation(first); c == nil || c.Prompt != RunPauseContinuePrompt || c.Status != AttemptInterruptionContinuationStatusQueued {
		t.Errorf("continuation = %+v", c)
	}
	if engineStr(first.ReadOnlyBaseline) != "original" || first.Settings == nil || first.Settings.Model != "kept/model" {
		t.Errorf("baseline %q settings %+v", engineStr(first.ReadOnlyBaseline), first.Settings)
	}
	EngineAdvance(&run, Event{Kind: EventKindContinuationsAdmitted}, engineT0.Add(12*time.Second))
	if run.Pause != nil || len(run.PauseTime) != 1 || !run.PauseTime[0].Start.Equal(engineT0) {
		t.Errorf("after admit: pause %+v pauseTime %+v", run.Pause, run.PauseTime)
	}
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Status != AttemptInterruptionContinuationStatusQueued {
		t.Errorf("after admit: continuation = %+v", c)
	}
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "resume-1", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunched, Operation: "resume-1", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: new("Outcome: ready to implement")}, engineT0)
	if run.StageID != "spec" || engineStr(run.Attempts[0].Outcome) != "ready to implement" {
		t.Errorf("after the continued session: stage %q outcome %q", run.StageID, engineStr(run.Attempts[0].Outcome))
	}
}

// TestASecondPauseAndResumeKeepsTheSameAttemptAndCountsAFailedSiblingOnce: a second
// pause and resume reuse the attempt and replace the continuation they queue.
func TestASecondPauseAndResumeKeepsTheSameAttemptAndCountsAFailedSiblingOnce(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	confirm := func(op string, at time.Duration) {
		EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: op, Leg: new("main"), Conversation: new("sid")}, engineT0.Add(at))
	}
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	confirm("p", 0)
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r1"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationsAdmitted}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunching, Operation: "r1", Leg: new("main")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindContinuationLaunched, Operation: "r1", Leg: new("main")}, engineT0)
	if len(run.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(run.Attempts))
	}
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p2"}, engineT0.Add(time.Second))
	confirm("p2", time.Second)
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r2"}, engineT0.Add(2*time.Second))
	if c := engineContinuation(run.Attempts[0]); c == nil || c.Prompt != "Continue" || c.Operation != "r2" {
		t.Errorf("continuation = %+v, want Continue for r2", c)
	}
	if len(run.Attempts) != 1 {
		t.Errorf("attempts = %d, want 1", len(run.Attempts))
	}
}

// TestAnUnconfirmedInterruptStaysABarrierAndRetryDoesNotClaimSuccess: a pause that
// failed to stop the session is incomplete, names the session and the failure, and
// holds the run until the next pause interrupts again.
func TestAnUnconfirmedInterruptStaysABarrierAndRetryDoesNotClaimSuccess(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseFailed, Operation: "p", Leg: new("main"), Reason: "did not stop"}, engineT0)
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseIncomplete {
		t.Fatalf("pause %+v, want incomplete", run.Pause)
	}
	if reason := engineStr(run.Pause.Reason); !strings.Contains(reason, "bl-test") || !strings.Contains(reason, "did not stop") {
		t.Errorf("reason = %q", reason)
	}
	EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: new("Outcome: ready to implement")}, engineT0)
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
	effects := EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p2"}, engineT0)
	if !engineHasInterrupt(effects, "bl-test", "p2", "main") || run.Pause == nil || run.Pause.Phase != RunPausePhasePausing {
		t.Errorf("retry: effects %+v pause %+v", effects, run.Pause)
	}
}

// TestPausedTimeIsLeftOutOfWatchGraceAndTicketQuiet: time spent paused is not counted
// by the watch's grace period or the ticket's quiet period.
func TestPausedTimeIsLeftOutOfWatchGraceAndTicketQuiet(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	later := engineT0.Add(650 * time.Second)
	if got := RunPauseExcluded(run, engineT0, later); got != 650 {
		t.Errorf("excluded = %v, want 650", got)
	}
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, later)
	EngineAdvance(&run, Event{Kind: EventKindContinuationsAdmitted}, later)
	if got := RunPauseExcluded(run, engineT0, later.Add(10*time.Second)); got != 650 {
		t.Errorf("excluded after resume = %v, want 650", got)
	}

	seen := TicketObservation{Status: "To Do", PendingSince: &jsonx.Date{Time: engineT0}}
	state := TicketState{Status: "To Do", IsOpen: true, Comments: []TicketStateComment{{ID: "c", Author: "a", Body: "hi"}}}
	events := map[TicketEvent]struct{}{TicketEventNewComment: {}}
	gathering := seen
	if finding := TicketWatchReport(&gathering, state, events, engineT0.Add(400*time.Second), 200); finding != nil {
		t.Errorf("gathering: finding %+v, want none", finding)
	}
	due := seen
	if finding := TicketWatchReport(&due, state, events, engineT0.Add(400*time.Second), 0); finding == nil || finding.Event != TicketEventNewComment {
		t.Errorf("due: finding %+v, want newComment", finding)
	}
}

// TestAMergeInFlightWaitsUntilResumeAndDoesNotUndoTheResult: a merge that finishes
// while paused is held until resume, and the run's stage does not change.
func TestAMergeInFlightWaitsUntilResumeAndDoesNotUndoTheResult(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	run.MergeBranches = []string{"feature"}
	EngineAdvance(&run, Event{Kind: EventKindPause, Operation: "p"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindPauseConfirmed, Operation: "p", Leg: new("main"), Conversation: new("sid")}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindMergedLegs, Legs: []string{"feature"}}, engineT0)
	if !reflect.DeepEqual(run.MergeBranches, []string{"feature"}) {
		t.Errorf("merge branches = %v, want [feature]", run.MergeBranches)
	}
	if run.Pause == nil || !reflect.DeepEqual(run.Pause.Deferred, []RunPauseDeferred{{Kind: RunPauseDeferredMerged, Values: []string{"feature"}}}) {
		t.Errorf("deferred = %+v", run.Pause)
	}
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
	EngineAdvance(&run, Event{Kind: EventKindResumePaused, Operation: "r"}, engineT0)
	if len(run.MergeBranches) != 0 || !strings.Contains(engineStr(run.MergeNote), "Merged") {
		t.Errorf("after resume: branches %v note %q", run.MergeBranches, engineStr(run.MergeNote))
	}
}
