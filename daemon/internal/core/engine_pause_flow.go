// The whole-run pause, its
// confirmations and the continuations of interrupted turns.

package core

import (
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"slices"
	"strings"
	"time"
)

// engineInterceptPause applies the pause rules to event: events that must not
// move a paused run are answered here. handled is false when the event proceeds
// as usual. Abort, Drop, Send to and the pause operation itself still apply.

func engineInterceptPause(run *Run, event Event, now time.Time) (effects []Effect, handled bool) {
	if event.Kind == EventKindSessionFinished && EngineStaleInterruptedTurn(*run, event.Leg) {
		return nil, true
	}
	if run.Pause == nil || !run.Pause.BlocksProgress() {
		return nil, false
	}
	switch event.Kind {
	case EventKindPause, EventKindPauseConfirmed, EventKindPauseFailed, EventKindResumePaused,
		EventKindContinuationsAdmitted, EventKindRetryContinuation, EventKindReconcileFailed,
		EventKindAbort, EventKindDrop, EventKindResume:
		return nil, false
	case EventKindSubjectChanged, EventKindSubjectGone:
		if run.Pause.Phase == RunPausePhaseResuming {
			return nil, false
		}
		return nil, true
	case EventKindSessionFinished:
		if engineLaunchedContinuation(*run, event) {
			return nil, false
		}
		return nil, true
	case EventKindMergedLegs:
		run.Pause.Deferred = append(run.Pause.Deferred, RunPauseDeferred{Kind: RunPauseDeferredMerged, Values: event.Legs})
		return nil, true
	case EventKindMergeConflict:
		run.Pause.Deferred = append(run.Pause.Deferred, RunPauseDeferred{Kind: RunPauseDeferredConflict, Values: event.Files})
		return nil, true
	default:
		return nil, true
	}
}

// engineAcceptPause accepts a whole-run pause: unfinished routes keep their states.
func engineAcceptPause(run *Run, operation string, now time.Time) []Effect {
	if !run.State.IsActive() {
		return nil
	}
	if run.Pause != nil && run.Pause.Phase != RunPausePhaseIncomplete {
		return nil
	}
	retry := run.Pause != nil && run.Pause.Phase == RunPausePhaseIncomplete
	if retry {
		run.Pause.Phase = RunPausePhasePausing
		run.Pause.Operation = operation
		run.Pause.Reason = nil
	} else {
		snapshots := []RunPauseSnapshot{}
		for _, leg := range run.WorkingLegs() {
			snapshots = append(snapshots, RunPauseSnapshot{
				ID: leg.ID, State: leg.State, StageID: CopyPointer(leg.StageID), Reason: CopyPointer(leg.Reason),
			})
		}
		run.Pause = &RunPause{
			Phase: RunPausePhasePausing, Operation: operation, RequestedAt: jsonx.Date{Time: now},
			Snapshots: snapshots, Deferred: []RunPauseDeferred{},
		}
	}
	run.Operation++
	effects := []Effect{{Kind: EffectKindCancelAfk}}
	pending := 0
	for _, leg := range run.WorkingLegs() {
		if leg.State != RunStateRunning {
			continue
		}
		if stage := leg.Stage(*run); stage == nil || stage.Kind() != StageKindAgent {
			continue
		}
		index := engineOpenAttemptIndex(*run, new(leg.ID))
		if index == nil || run.Attempts[*index].Session == nil {
			continue
		}
		attempt := run.Attempts[*index]
		if retry && attempt.Interruption != nil && attempt.Interruption.Confirmation == AttemptInterruptionConfirmationConfirmed {
			continue
		}
		var record AttemptInterruption
		if attempt.Interruption != nil {
			record = cloneValue(*attempt.Interruption)
		} else {
			record = AttemptInterruption{Operation: operation, LegID: leg.ID, Confirmation: AttemptInterruptionConfirmationPending}
		}
		record.Operation = operation
		record.LegID = leg.ID
		record.Confirmation = AttemptInterruptionConfirmationPending
		record.Reason = nil
		record.TranscriptMark = CopyPointer(attempt.LogMark)
		record.UsageMark = CopyPointer(attempt.UsageMark)
		record.UsageCheckpoint = CopyPointer(attempt.UsageCheckpoint)
		run.Attempts[*index].Interruption = &record
		effects = append(effects, Effect{Kind: EffectKindInterrupt, Session: *attempt.Session, Operation: operation, Leg: leg.ID})
		pending++
	}
	if pending == 0 {
		run.Pause.Phase = RunPausePhasePaused
		run.Pause.ConfirmedAt = &jsonx.Date{Time: now}
	}
	return effects
}

// engineConfirmPause records that one generating session confirmed it stopped.
func engineConfirmPause(run *Run, operation string, leg *string, conversation *string, now time.Time) []Effect {
	if !nestedPauseIs(run, operation, RunPausePhasePausing, RunPausePhaseIncomplete) {
		return nil
	}
	index := engineInterruptionIndex(*run, leg, operation)
	if index == nil {
		return nil
	}
	attempt := &run.Attempts[*index]
	if attempt.Interruption != nil && attempt.Interruption.Confirmation == AttemptInterruptionConfirmationConfirmed {
		return nil
	}
	if attempt.Interruption != nil {
		attempt.Interruption.Confirmation = AttemptInterruptionConfirmationConfirmed
		attempt.Interruption.Reason = nil
		if conversation != nil {
			attempt.Interruption.Conversation = CopyPointer(conversation)
		}
	}
	enginePauseSettle(run, now)
	return nil
}

// engineFailPause records that one generating session could not be confirmed
// stopped.
func engineFailPause(run *Run, operation string, leg *string, reason string, now time.Time) []Effect {
	if !nestedPauseIs(run, operation, RunPausePhasePausing, RunPausePhaseIncomplete) {
		return nil
	}
	index := engineInterruptionIndex(*run, leg, operation)
	if index == nil {
		return nil
	}
	attempt := &run.Attempts[*index]
	if attempt.Interruption == nil || attempt.Interruption.Confirmation != AttemptInterruptionConfirmationPending {
		return nil
	}
	attempt.Interruption.Confirmation = AttemptInterruptionConfirmationFailed
	attempt.Interruption.Reason = new(reason)
	name := engineRouteLabel(*run, leg)
	session := ValueOr(attempt.Session, "session")
	line := name + " (" + session + "): " + reason
	if existing := run.Pause.Reason; existing != nil && *existing != "" && !strings.Contains(*existing, line) {
		run.Pause.Reason = new(*existing + "\n" + line)
	} else {
		run.Pause.Reason = new(line)
	}
	run.Pause.Phase = RunPausePhaseIncomplete
	return nil
}

// nestedPauseIs reports whether the run has a pause for operation in one of phases.
func nestedPauseIs(run *Run, operation string, phases ...RunPausePhase) bool {
	if run.Pause == nil || run.Pause.Operation != operation {
		return false
	}
	return slices.Contains(phases, run.Pause.Phase)
}

// engineResumePaused accepts Resume. Continue is queued only for turns this pause
// interrupted.
func engineResumePaused(run *Run, operation string, now time.Time) []Effect {
	if run.Pause != nil && run.Pause.Phase == RunPausePhaseReconcileFailed {
		run.Pause.Phase = RunPausePhaseResuming
		run.Pause.Reason = nil
		return nil
	}
	if run.Pause == nil || run.Pause.Phase != RunPausePhasePaused {
		return nil
	}
	effects := engineReplayDeferred(run, now)
	if !run.State.IsActive() || run.Pause == nil {
		engineClosePauseInterval(run, now)
		engineInvalidateContinuations(run)
		run.Pause = nil
		return effects
	}
	run.Pause.Phase = RunPausePhaseResuming
	run.Pause.Operation = operation
	run.Pause.Reason = nil
	for index := range run.Attempts {
		attempt := &run.Attempts[index]
		if !attempt.IsOpen() || attempt.Interruption == nil || attempt.Interruption.Confirmation != AttemptInterruptionConfirmationConfirmed {
			continue
		}
		leg := nestedLegOf(*run, attempt.Interruption.LegID)
		if leg == nil || !leg.State.IsActive() || leg.IsFinished() {
			continue
		}
		if stage := leg.Stage(*run); stage == nil || stage.Kind() != StageKindAgent {
			continue
		}
		continuation := NewAttemptInterruptionContinuation(operation, AttemptInterruptionContinuationStatusQueued, nil, nil)
		attempt.Interruption.Continuation = &continuation
	}
	return effects
}

// engineAdmitContinuations runs once source reconciliation has finished: queued
// work and watches may run again.
func engineAdmitContinuations(run *Run, now time.Time) []Effect {
	if run.Pause == nil || run.Pause.Phase != RunPausePhaseResuming {
		return nil
	}
	engineClosePauseInterval(run, now)
	run.Pause = nil
	return nil
}

// engineClosePauseInterval closes the open pause interval at now.
func engineClosePauseInterval(run *Run, now time.Time) {
	if run.Pause == nil {
		return
	}
	start := run.Pause.RequestedAt.Time
	end := start
	if now.After(start) {
		end = now
	}
	run.PauseTime = append(run.PauseTime, RunPauseInterval{Start: jsonx.Date{Time: start}, End: jsonx.Date{Time: end}})
}
