package core

import (
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"time"
)

// engineReplayDeferred replays the deferred work of a pause.
func engineReplayDeferred(run *Run, now time.Time) []Effect {
	pending := []RunPauseDeferred{}
	if run.Pause != nil {
		pending = run.Pause.Deferred
	}
	if len(pending) == 0 {
		return nil
	}
	var saved *RunPause
	if run.Pause != nil {
		copied := cloneValue(*run.Pause)
		saved = &copied
	}
	run.Pause = nil
	var effects []Effect
	for _, item := range pending {
		switch item.Kind {
		case RunPauseDeferredMerged:
			effects = append(effects, engineStep(run, Event{Kind: EventKindMergedLegs, Legs: item.Values}, now)...)
		case RunPauseDeferredConflict:
			effects = append(effects, engineStep(run, Event{Kind: EventKindMergeConflict, Files: item.Values}, now)...)
		}
	}
	if run.State.IsActive() && saved != nil {
		saved.Deferred = []RunPauseDeferred{}
		run.Pause = saved
	} else {
		engineInvalidateContinuations(run)
		run.Pause = nil
	}
	return effects
}

// enginePauseSettle moves the pause to paused once nothing is pending, or to
// incomplete when a session failed to confirm. Shared by confirmPause and
// settlePausePhase.
func enginePauseSettle(run *Run, now time.Time) {
	pending := false
	failed := false
	for index := range run.Attempts {
		attempt := run.Attempts[index]
		if !attempt.IsOpen() || attempt.Interruption == nil || run.Pause == nil || attempt.Interruption.Operation != run.Pause.Operation {
			continue
		}
		switch attempt.Interruption.Confirmation {
		case AttemptInterruptionConfirmationPending:
			pending = true
		case AttemptInterruptionConfirmationFailed:
			failed = true
		case AttemptInterruptionConfirmationConfirmed:
		}
	}
	if failed {
		if run.Pause != nil {
			run.Pause.Phase = RunPausePhaseIncomplete
		}
	} else if !pending {
		if run.Pause != nil {
			run.Pause.Phase = RunPausePhasePaused
			run.Pause.ConfirmedAt = &jsonx.Date{Time: now}
			run.Pause.Reason = nil
		}
	}
}

// engineLaunchedContinuation reports whether a launched session continues the run's pause.
func engineLaunchedContinuation(run Run, event Event) bool {
	if event.Kind != EventKindSessionFinished {
		return false
	}
	attempt := enginePausedAttempt(run, event.Leg)
	if attempt == nil {
		return false
	}
	return attempt.Interruption != nil && attempt.Interruption.Continuation != nil &&
		attempt.Interruption.Continuation.Status == AttemptInterruptionContinuationStatusLaunched
}

// enginePausedAttempt is the attempt a pause holds open on leg, or nil.
func enginePausedAttempt(run Run, leg *string) *Attempt {
	index := engineOpenAttemptIndex(run, leg)
	if index == nil {
		return nil
	}
	attempt := run.Attempts[*index]
	return &attempt
}

// engineOpenAttemptIndex
func engineOpenAttemptIndex(run Run, leg *string) *int {
	at := engineLeg(run, leg)
	stageID := ValueOr(at.StageID, run.StageID)
	key := AttemptKey(stageID, new(at.ID))
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		if AttemptKey(run.Attempts[index].StageID, run.Attempts[index].Leg) == key && run.Attempts[index].IsOpen() {
			found := index
			return &found
		}
	}
	return nil
}

// engineInterruptionIndex is the index of the interruption of leg for operation.
func engineInterruptionIndex(run Run, leg *string, operation string) *int {
	index := engineOpenAttemptIndex(run, leg)
	if index == nil {
		return nil
	}
	interruption := run.Attempts[*index].Interruption
	if interruption == nil || interruption.Operation != operation {
		return nil
	}
	return index
}

// engineContinuationIndex is the index of the continuation of leg for operation.
func engineContinuationIndex(run Run, leg *string, operation string) *int {
	index := engineOpenAttemptIndex(run, leg)
	if index == nil {
		return nil
	}
	interruption := run.Attempts[*index].Interruption
	if interruption == nil || interruption.Continuation == nil || interruption.Continuation.Operation != operation {
		return nil
	}
	return index
}

// engineRouteLabel is the label of leg's route.
func engineRouteLabel(run Run, leg *string) string {
	at := engineLeg(run, leg)
	if name := at.RouteName(run.Layout()); name != nil {
		return *name
	}
	if stage := at.Stage(run); stage != nil {
		return stage.Name
	}
	if stage := run.Stage(); stage != nil {
		return stage.Name
	}
	return "this route"
}
