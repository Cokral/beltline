package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// pauseTestRun is an active run paused in phase with operation p and an attempt
// whose interruption and continuation are given (nil for none).
func pauseTestRun(phase RunPausePhase, interruption *AttemptInterruption) Run {
	run := Run{ID: "run", StageID: "triage", State: RunStateRunning, Pause: &RunPause{
		Phase: phase, Operation: "p", RequestedAt: jsonx.Date{Time: parkTestNow},
		Snapshots: []RunPauseSnapshot{}, Deferred: []RunPauseDeferred{},
	}}
	if interruption != nil {
		run.Attempts = []Attempt{{ID: "triage-1", StageID: "triage", Number: 1,
			StartedAt: jsonx.Date{Time: parkTestNow}, Interruption: interruption}}
	}
	return run
}

// TestEngineInterceptPauseHoldsProgressButKeepsMergesForResume: a paused run answers
// most events as held, and keeps the merges that finished on disk for Resume.
func TestEngineInterceptPauseHoldsProgressButKeepsMergesForResume(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhasePaused, nil)
	if effects, handled := engineInterceptPause(&run, Event{Kind: EventKindDecided, Outcome: "approved"}, parkTestLater); !handled || len(effects) != 0 {
		t.Fatalf("decided while paused: handled %v, effects %+v; want held with none", handled, effects)
	}
	if _, handled := engineInterceptPause(&run, Event{Kind: EventKindPause, Operation: "q"}, parkTestLater); handled {
		t.Fatal("a pause operation must proceed as usual")
	}
	if _, handled := engineInterceptPause(&run, Event{Kind: EventKindAbort}, parkTestLater); handled {
		t.Fatal("abort must proceed as usual while paused")
	}
	if _, handled := engineInterceptPause(&run, Event{Kind: EventKindMergedLegs, Legs: []string{"a"}}, parkTestLater); !handled {
		t.Fatal("a merge while paused must be held")
	}
	if _, handled := engineInterceptPause(&run, Event{Kind: EventKindMergeConflict, Files: []string{"x.go"}}, parkTestLater); !handled {
		t.Fatal("a conflict while paused must be held")
	}
	if len(run.Pause.Deferred) != 2 {
		t.Fatalf("deferred = %+v, want the merge and the conflict", run.Pause.Deferred)
	}
	if d := run.Pause.Deferred[0]; d.Kind != RunPauseDeferredMerged || len(d.Values) != 1 || d.Values[0] != "a" {
		t.Fatalf("first deferred = %+v, want merged a", d)
	}
	if d := run.Pause.Deferred[1]; d.Kind != RunPauseDeferredConflict || d.Values[0] != "x.go" {
		t.Fatalf("second deferred = %+v, want conflict x.go", d)
	}
}

// TestEngineInterceptPauseLetsAPausedSubjectReachResume: a subject change holds a
// paused run, but proceeds once Resume is reconciling the source.
func TestEngineInterceptPauseLetsAPausedSubjectReachResume(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhasePaused, nil)
	if effects, handled := engineInterceptPause(&run, Event{Kind: EventKindSubjectChanged}, parkTestLater); !handled || len(effects) != 0 {
		t.Fatalf("subject changed while paused: handled %v, effects %+v", handled, effects)
	}
	run.Pause.Phase = RunPausePhaseResuming
	if _, handled := engineInterceptPause(&run, Event{Kind: EventKindSubjectGone}, parkTestLater); handled {
		t.Fatal("a subject closing while resuming must reach the reconciliation")
	}
	var nilRun Run
	if _, handled := engineInterceptPause(&nilRun, Event{Kind: EventKindDecided}, parkTestLater); handled {
		t.Fatal("a run that is not paused proceeds as usual")
	}
}

// TestEngineAcceptPauseWithNothingRunningIsPaused: a pause on a run with no agent
// sessions to stop is paused at once, and bumps the operation.
func TestEngineAcceptPauseWithNothingRunningIsPaused(t *testing.T) {
	t.Parallel()
	run := Run{ID: "run", StageID: "triage", State: RunStateQueued}
	effects := engineAcceptPause(&run, "p1", parkTestNow)
	if len(effects) != 1 || effects[0].Kind != EffectKindCancelAfk {
		t.Fatalf("effects = %+v, want cancelAfk only", effects)
	}
	if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused || run.Pause.Operation != "p1" {
		t.Fatalf("pause = %+v, want paused under p1", run.Pause)
	}
	if run.Pause.ConfirmedAt == nil || !run.Pause.ConfirmedAt.Equal(parkTestNow) {
		t.Fatalf("confirmed at = %v, want %s", run.Pause.ConfirmedAt, parkTestNow)
	}
	if run.Operation != 1 {
		t.Fatalf("operation = %d, want 1", run.Operation)
	}
}

// TestEngineAcceptPauseRetriesAnIncompletePause: an incomplete pause is accepted
// again under the new operation, and a finished pause is left alone.
func TestEngineAcceptPauseRetriesAnIncompletePause(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhaseIncomplete, nil)
	run.Pause.Reason = new("old")
	engineAcceptPause(&run, "p2", parkTestLater)
	if run.Pause.Phase != RunPausePhasePaused || run.Pause.Operation != "p2" || run.Pause.Reason != nil {
		t.Fatalf("pause = %+v, want paused under p2 with the reason cleared", run.Pause)
	}
	paused := pauseTestRun(RunPausePhasePaused, nil)
	if effects := engineAcceptPause(&paused, "p3", parkTestLater); len(effects) != 0 || paused.Pause.Operation != "p" {
		t.Fatalf("a paused run changed on a second pause: effects %+v, pause %+v", effects, paused.Pause)
	}
}

// TestEngineResumeAdmitsTheRunAndRecordsItsPauseTime: Resume moves a paused run to
// resuming, and admitting it closes the stretch it was paused for.
func TestEngineResumeAdmitsTheRunAndRecordsItsPauseTime(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhasePaused, nil)
	if effects := engineResumePaused(&run, "r1", parkTestLater); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none", effects)
	}
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseResuming || run.Pause.Operation != "r1" {
		t.Fatalf("pause = %+v, want resuming under r1", run.Pause)
	}
	engineAdmitContinuations(&run, parkTestLater)
	if run.Pause != nil {
		t.Fatalf("pause = %+v, want it admitted and cleared", run.Pause)
	}
	if len(run.PauseTime) != 1 || !run.PauseTime[0].Start.Equal(parkTestNow) || !run.PauseTime[0].End.Equal(parkTestLater) {
		t.Fatalf("pause time = %+v, want one interval from %s to %s", run.PauseTime, parkTestNow, parkTestLater)
	}
}

// TestEngineResumeRetriesAFailedReconciliation: a source that could not be read is
// tried again when Resume is asked again; only a resuming pause can fail to read.
func TestEngineResumeRetriesAFailedReconciliation(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhaseResuming, nil)
	engineFailReconciliation(&run, "no source")
	if run.Pause.Phase != RunPausePhaseReconcileFailed || run.Pause.Reason == nil || *run.Pause.Reason != "no source" {
		t.Fatalf("pause = %+v, want reconcileFailed with the reason", run.Pause)
	}
	engineResumePaused(&run, "r2", parkTestLater)
	if run.Pause.Phase != RunPausePhaseResuming || run.Pause.Reason != nil {
		t.Fatalf("pause = %+v, want resuming with no reason", run.Pause)
	}
	paused := pauseTestRun(RunPausePhasePaused, nil)
	engineFailReconciliation(&paused, "ignored")
	if paused.Pause.Phase != RunPausePhasePaused || paused.Pause.Reason != nil {
		t.Fatalf("a paused run took a reconciliation failure: %+v", paused.Pause)
	}
}

// TestEngineRetryContinuationRequeuesAFailedContinue: a Continue that failed to
// launch is queued again, and only while the run is not paused.
func TestEngineRetryContinuationRequeuesAFailedContinue(t *testing.T) {
	t.Parallel()
	failed := NewAttemptInterruptionContinuation("p", AttemptInterruptionContinuationStatusFailed, new("gone"), nil)
	interruption := &AttemptInterruption{Operation: "p", LegID: "main", Confirmation: AttemptInterruptionConfirmationConfirmed, Continuation: &failed}
	run := Run{ID: "run", StageID: "triage", State: RunStateQueued, Attempts: []Attempt{{ID: "triage-1", StageID: "triage", Interruption: interruption, StartedAt: jsonx.Date{Time: parkTestNow}}}}
	engineRetryContinuation(&run, nil)
	got := run.Attempts[0].Interruption.Continuation
	if got.Status != AttemptInterruptionContinuationStatusQueued || got.Reason != nil {
		t.Fatalf("continuation = %+v, want queued with no reason", got)
	}
	paused := pauseTestRun(RunPausePhasePaused, nil)
	paused.Attempts = run.Attempts
	engineRetryContinuation(&paused, nil)
	if paused.Attempts[0].Interruption.Continuation.Status != AttemptInterruptionContinuationStatusQueued {
		t.Fatal("a paused run must not change the continuation")
	}
}

// TestEngineInvalidateContinuationsForgetsOnlyTheQueuedAndLaunchingOnes: a move or a
// drop throws away Continues that have not been delivered, and keeps the rest.
func TestEngineInvalidateContinuationsForgetsOnlyTheQueuedAndLaunchingOnes(t *testing.T) {
	t.Parallel()
	statuses := []AttemptInterruptionContinuationStatus{
		AttemptInterruptionContinuationStatusQueued, AttemptInterruptionContinuationStatusLaunching,
		AttemptInterruptionContinuationStatusLaunched, AttemptInterruptionContinuationStatusFailed,
	}
	run := Run{ID: "run", StageID: "triage", State: RunStateQueued}
	for i, status := range statuses {
		continuation := NewAttemptInterruptionContinuation("p", status, nil, nil)
		run.Attempts = append(run.Attempts, Attempt{ID: "a" + string(rune('0'+i)), StageID: "triage",
			StartedAt: jsonx.Date{Time: parkTestNow}, Interruption: &AttemptInterruption{
				Operation: "p", LegID: "main", Confirmation: AttemptInterruptionConfirmationConfirmed, Continuation: &continuation,
			}})
	}
	engineInvalidateContinuations(&run)
	want := []AttemptInterruptionContinuationStatus{
		AttemptInterruptionContinuationStatusInvalidated, AttemptInterruptionContinuationStatusInvalidated,
		AttemptInterruptionContinuationStatusLaunched, AttemptInterruptionContinuationStatusFailed,
	}
	for i := range want {
		if got := run.Attempts[i].Interruption.Continuation.Status; got != want[i] {
			t.Errorf("attempt %d status = %s, want %s", i, got, want[i])
		}
	}
}

// TestEngineClearPauseIfTerminalEndsOnlyWithTheRun: a shipped or dropped run loses its
// pause; an active run keeps it.
func TestEngineClearPauseIfTerminalEndsOnlyWithTheRun(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhasePaused, nil)
	engineClearPauseIfTerminal(&run)
	if run.Pause == nil {
		t.Fatal("an active run lost its pause")
	}
	run.State = RunStateDropped
	engineClearPauseIfTerminal(&run)
	if run.Pause != nil {
		t.Fatalf("a dropped run kept its pause: %+v", run.Pause)
	}
}

// TestEngineSettlePausePhaseFollowsEverySession: the pause is paused once no
// generating session is pending, and incomplete while one failed to confirm.
func TestEngineSettlePausePhaseFollowsEverySession(t *testing.T) {
	t.Parallel()
	pending := &AttemptInterruption{Operation: "p", LegID: "main", Confirmation: AttemptInterruptionConfirmationPending}
	run := pauseTestRun(RunPausePhasePausing, pending)
	enginePauseSettle(&run, parkTestLater)
	if run.Pause.Phase != RunPausePhasePausing {
		t.Fatalf("phase = %s with a session pending, want pausing", run.Pause.Phase)
	}
	run.Attempts[0].Interruption.Confirmation = AttemptInterruptionConfirmationFailed
	enginePauseSettle(&run, parkTestLater)
	if run.Pause.Phase != RunPausePhaseIncomplete {
		t.Fatalf("phase = %s with a session that failed, want incomplete", run.Pause.Phase)
	}
	run.Attempts[0].Interruption.Confirmation = AttemptInterruptionConfirmationConfirmed
	enginePauseSettle(&run, parkTestLater)
	if run.Pause.Phase != RunPausePhasePaused || run.Pause.ConfirmedAt == nil || !run.Pause.ConfirmedAt.Equal(parkTestLater) {
		t.Fatalf("pause = %+v, want paused and confirmed at %s", run.Pause, parkTestLater)
	}
}

// TestEngineClosePauseIntervalNeverRunsBackwards: an interval that would end before
// it starts ends where it starts.
func TestEngineClosePauseIntervalNeverRunsBackwards(t *testing.T) {
	t.Parallel()
	run := pauseTestRun(RunPausePhasePaused, nil)
	engineClosePauseInterval(&run, parkTestNow.Add(-time.Minute))
	if len(run.PauseTime) != 1 || !run.PauseTime[0].End.Equal(parkTestNow) {
		t.Fatalf("pause time = %+v, want an interval ending at its start", run.PauseTime)
	}
	empty := Run{ID: "run"}
	engineClosePauseInterval(&empty, parkTestLater)
	if len(empty.PauseTime) != 0 {
		t.Fatal("a run that is not paused records no interval")
	}
}
