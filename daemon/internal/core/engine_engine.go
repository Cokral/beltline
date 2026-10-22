// The start, the event step and
// the overrule restart. The rest of the namespace is in engine_engine_apply.go,
// engine_engine_moves.go and engine_engine_attempts.go.

package core

import (
	"bytes"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// EngineStart starts a new run at its first stage. A run with no stages is held.
// Mutates run.
func EngineStart(run *Run, now time.Time) []Effect {
	if len(run.Stages) == 0 {
		run.State = RunStateHeld
		run.Reason = new("the belt has no stages")
		return nil
	}
	effects := engineStartAtBeginning(run, nil, now)
	engineRefresh(run)
	return effects
}

// EngineAdvance applies event to run and returns the effects the executor must
// carry out. It is pure: it changes run and returns effects, and never touches
// GitHub, git, tmux or the clock. Only an event that changed the run settles the
// legs and reads back the run's own state, so an event that does nothing
// changes nothing; run.updatedAt is set to now only when run changed.
func EngineAdvance(run *Run, event Event, now time.Time) []Effect {
	before, beforeErr := engineSaved(*run)
	completed := run.State.Completed()
	effects := engineStep(run, event, now)
	// Only an event that changed the run settles the legs and reads back
	// the run's own state, so an event that does nothing changes nothing.
	// A button-parked run is frozen again after the step, so a merge that
	// landed while it was parked cannot join or leave a route running.
	if engineChanged(before, beforeErr, run) {
		if run.Parking != nil {
			engineFreezeParked(run)
		}
		effects = append(effects, engineSettle(run, now)...)
		if run.Parking != nil {
			engineFreezeParked(run)
		}
		engineRefresh(run)
	}
	// Work that starts again on a run that had finished is a new trip, so its
	// shipping intent ends and a later completion is Done unless it ships again.
	if completed && run.State.IsActive() {
		run.Shipping = false
	}
	if engineChanged(before, beforeErr, run) {
		run.UpdatedAt = jsonx.Date{Time: now}
	}
	return effects
}

// engineSaved is the run as it is saved, and the error when it cannot be saved.
// Two runs that save the same are the same run for Engine.advance.
func engineSaved(run Run) ([]byte, error) {
	return jsonx.Marshal(run)
}

// engineChanged reports whether run saves differently from before, which is what
// EngineAdvance takes as a change. A run that cannot be saved on either side
// counts as changed, so an error never looks like a no-op. The comparison is of
// the saved JSON, so a change below one second is invisible here, as it is in the
// saved file (jsonx.Date writes whole seconds).
func engineChanged(before []byte, beforeErr error, run *Run) bool {
	after, err := engineSaved(*run)
	if beforeErr != nil || err != nil {
		return true
	}
	return !bytes.Equal(before, after)
}

// engineStep applies one event before settling. Pause interception and button
// parking fence it first; then each event kind's case.
// engineStep applies one event before settling. Pause interception and button
// parking fence it first; then each event kind's case, with the bodies that need
// more than one step in their own helpers.
func engineStep(run *Run, event Event, now time.Time) []Effect {
	if effects, handled := engineInterceptPause(run, event, now); handled {
		return effects
	}
	// Button parking also fences late events while preserving explicit unpark actions.
	if run.Parking != nil && engineBlocksWhileParked(event) {
		return nil
	}
	switch event.Kind {
	case EventKindSessionStarted:
		return engineSessionStarted(run, event, now)

	case EventKindSessionFinished:
		return engineSessionFinished(run, event, now)

	case EventKindDecided:
		return engineDecided(run, event, now)

	case EventKindWatched:
		return engineWatchedEvent(run, event, now)

	case EventKindTicketWatched:
		return engineTicketWatchedEvent(run, event, now)

	case EventKindSubjectChanged:
		return engineSubjectChanged(run, event, now)

	case EventKindSubjectGone:
		return engineSubjectGone(run, event, now)

	case EventKindAdmissionBlocked:
		return engineAdmissionBlocked(run, event)

	case EventKindBlocked:
		return engineWatchBlocked(run, event, now)

	case EventKindResume:
		return engineResume(run, event, now)

	case EventKindAnswered:
		return engineAnswered(run, event, now)

	case EventKindMergedLegs:
		return engineMergedLegs(run, event.Legs, now)

	case EventKindMergeConflict:
		return engineMergeConflict(run, event.Files, now)

	case EventKindAbort:
		return engineAbortEvent(run, event, now)

	case EventKindDrop:
		return engineDropEvent(run, event, now)

	case EventKindOverrule:
		return engineOverrule(run, event.RestartStage, event.Comments, event.Decision, event.AttemptID, now)
	}
	if effects, handled := enginePauseEvent(run, event, now); handled {
		return effects
	}
	return nil
}

// engineWatchedEvent applies a pull request watch report to a leg that waits on
// a pull request; any other leg ignores it.
func engineWatchedEvent(run *Run, event Event, now time.Time) []Effect {
	if stage := engineLeg(*run, event.Leg).Stage(*run); !engineIsWatchTarget(stage, WatchTargetPullRequest) {
		return nil
	}
	return engineWatched(run, event.Leg, string(event.Watch), event.Watch.Label(), event.Detail, now)
}

// engineTicketWatchedEvent applies a ticket watch report to a leg that waits on
// its ticket; any other leg ignores it.
func engineTicketWatchedEvent(run *Run, event Event, now time.Time) []Effect {
	if stage := engineLeg(*run, event.Leg).Stage(*run); !engineIsWatchTarget(stage, WatchTargetTicket) {
		return nil
	}
	return engineWatched(run, event.Leg, string(event.Ticket), event.Ticket.Label(), event.Detail, now)
}

// engineSubjectGone drops a run whose subject closed, unless the run has
// reached its watch: merging closes the issue or finishes the ticket, and the
// watch sees that through.
func engineSubjectGone(run *Run, event Event, now time.Time) []Effect {
	if !run.State.IsActive() || !run.Policies.StopOnClose || run.ReachedWatch() {
		return nil
	}
	return engineDropRun(run, event.Reason, now)
}

// engineAdmissionBlocked holds a queued agent leg that could not start. Before
// allocation there is no attempt and no policy comment, as there is for a run;
// only the leg that could not start is held.
func engineAdmissionBlocked(run *Run, event Event) []Effect {
	at := engineLeg(*run, event.Leg)
	if stage := at.Stage(*run); at.State != RunStateQueued || stage == nil || stage.Kind() != StageKindAgent {
		return nil
	}
	engineSet(run, &at.ID, func(l *Leg) {
		l.State = RunStateHeld
		l.Reason = new(event.Reason)
	})
	return nil
}

// engineWatchBlocked closes a running watch's attempt as held for you and holds
// its leg, when the leg is a running watch stage.
func engineWatchBlocked(run *Run, event Event, now time.Time) []Effect {
	at := engineLeg(*run, event.Leg)
	if stage := at.Stage(*run); at.State != RunStateRunning || stage == nil || stage.Kind() != StageKindWatch {
		return nil
	}
	engineCloseAttempt(run, &at.ID, now, func(a *Attempt) {
		a.Tone = new(ToneProblem)
		a.Detail = new(event.Reason)
		a.Route = new("Held for you")
	})
	return engineHoldLeg(run, &at.ID, event.Reason, nil, now, true)
}

// engineAbortEvent aborts the leg when it can still be aborted.
func engineAbortEvent(run *Run, event Event, now time.Time) []Effect {
	at := engineLeg(*run, event.Leg)
	if !at.CanAbort(*run) {
		return nil
	}
	return engineAbortLeg(run, &at.ID, now)
}

// engineDropEvent drops the leg's route when the run is split and the event names
// a leg; without a leg the run and everything on it goes at once.
func engineDropEvent(run *Run, event Event, now time.Time) []Effect {
	at := engineLeg(*run, event.Leg)
	if !at.State.IsActive() {
		return nil
	}
	if !run.IsSplit() || event.Leg == nil {
		return engineDropRun(run, event.Reason, now)
	}
	return engineDropLeg(run, event.Leg, event.Reason, now)
}

// enginePauseEvent applies the pause, continuation, park and restart events. It
// reports false for any other kind, which engineStep then ignores.
func enginePauseEvent(run *Run, event Event, now time.Time) ([]Effect, bool) {
	switch event.Kind {
	case EventKindPause:
		return engineAcceptPause(run, event.Operation, now), true
	case EventKindPauseConfirmed:
		return engineConfirmPause(run, event.Operation, event.Leg, event.Conversation, now), true
	case EventKindPauseFailed:
		return engineFailPause(run, event.Operation, event.Leg, event.Reason, now), true
	case EventKindResumePaused:
		return engineResumePaused(run, event.Operation, now), true
	case EventKindContinuationsAdmitted:
		return engineAdmitContinuations(run, now), true
	case EventKindContinuationLaunching:
		return engineNoteContinuation(run, event.Operation, event.Leg, AttemptInterruptionContinuationStatusLaunching, nil), true
	case EventKindContinuationLaunched:
		return engineNoteContinuation(run, event.Operation, event.Leg, AttemptInterruptionContinuationStatusLaunched, nil), true
	case EventKindContinuationFailed:
		return engineNoteContinuation(run, event.Operation, event.Leg, AttemptInterruptionContinuationStatusFailed, new(event.Reason)), true
	case EventKindContinuationAmbiguous:
		return engineNoteContinuation(run, event.Operation, event.Leg, AttemptInterruptionContinuationStatusAmbiguous, new(event.Reason)), true
	case EventKindRetryContinuation:
		return engineRetryContinuation(run, event.Leg), true
	case EventKindReconcileFailed:
		return engineFailReconciliation(run, event.Reason), true
	case EventKindParkRun:
		return engineParkRun(run, now), true
	case EventKindUnparkRun:
		return engineUnparkRun(run, now), true
	case EventKindParkStopped:
		return engineParkStopped(run, event.Session, event.Error, now), true
	case EventKindRestartHeld:
		return engineRestartHeld(run, event.Reason, now), true
	}
	return nil, false
}
