package core

// engineNoteContinuation records the status of a Continue for one leg: launching,
// launched, failed or ambiguous (reason set for failed and ambiguous).
func engineNoteContinuation(run *Run, operation string, leg *string, status AttemptInterruptionContinuationStatus, reason *string) []Effect {
	index := engineContinuationIndex(*run, leg, operation)
	if index == nil {
		return nil
	}
	attempt := &run.Attempts[*index]
	if attempt.Interruption == nil || attempt.Interruption.Continuation == nil {
		return nil
	}
	continuation := attempt.Interruption.Continuation
	if continuation.Status == AttemptInterruptionContinuationStatusInvalidated {
		return nil
	}
	current := continuation.Status
	allowed := false
	switch {
	case current == AttemptInterruptionContinuationStatusQueued &&
		(status == AttemptInterruptionContinuationStatusLaunching || status == AttemptInterruptionContinuationStatusFailed):
		allowed = true
	case current == AttemptInterruptionContinuationStatusLaunching &&
		(status == AttemptInterruptionContinuationStatusLaunched || status == AttemptInterruptionContinuationStatusFailed ||
			status == AttemptInterruptionContinuationStatusAmbiguous):
		allowed = true
	}
	if !allowed {
		return nil
	}
	continuation.Status = status
	continuation.Reason = CopyPointer(reason)
	continuation.Prompt = RunPauseContinuePrompt
	return nil
}

// engineRetryContinuation retries a failed Continue (the Event's retry).
func engineRetryContinuation(run *Run, leg *string) []Effect {
	if run.Pause != nil && run.Pause.Phase != RunPausePhaseResuming {
		return nil
	}
	stored := LegFromID(leg)
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		attempt := &run.Attempts[index]
		if !attempt.IsOpen() || !SamePointee(LegFromID(attempt.Leg), stored) {
			continue
		}
		if attempt.Interruption == nil || attempt.Interruption.Continuation == nil ||
			attempt.Interruption.Continuation.Status != AttemptInterruptionContinuationStatusFailed {
			continue
		}
		attempt.Interruption.Continuation.Status = AttemptInterruptionContinuationStatusQueued
		attempt.Interruption.Continuation.Reason = nil
		return nil
	}
	return nil
}

// engineFailReconciliation is the source could not be read on Resume: the run
// stays stopped.
func engineFailReconciliation(run *Run, reason string) []Effect {
	if run.Pause == nil || (run.Pause.Phase != RunPausePhaseResuming && run.Pause.Phase != RunPausePhaseReconcileFailed) {
		return nil
	}
	run.Pause.Phase = RunPausePhaseReconcileFailed
	run.Pause.Reason = new(reason)
	return nil
}

// engineInvalidateContinuations forgets the Continues of the old attempt: a move
// or a drop throws them away.
func engineInvalidateContinuations(run *Run) {
	for index := range run.Attempts {
		attempt := &run.Attempts[index]
		if attempt.Interruption == nil || attempt.Interruption.Continuation == nil {
			continue
		}
		continuation := attempt.Interruption.Continuation
		switch continuation.Status {
		case AttemptInterruptionContinuationStatusQueued, AttemptInterruptionContinuationStatusLaunching:
			continuation.Status = AttemptInterruptionContinuationStatusInvalidated
		}
	}
}

// engineInvalidateContinuation invalidates the Continue of one leg.
func engineInvalidateContinuation(run *Run, leg *string) {
	stored := LegFromID(leg)
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		attempt := &run.Attempts[index]
		if !attempt.IsOpen() || !SamePointee(LegFromID(attempt.Leg), stored) {
			continue
		}
		if attempt.Interruption == nil || attempt.Interruption.Continuation == nil {
			return
		}
		continuation := attempt.Interruption.Continuation
		if continuation.Status == AttemptInterruptionContinuationStatusQueued ||
			continuation.Status == AttemptInterruptionContinuationStatusLaunching {
			continuation.Status = AttemptInterruptionContinuationStatusInvalidated
		}
		return
	}
}

// engineClearPauseIfTerminal clears the run's pause when the run is terminal.
func engineClearPauseIfTerminal(run *Run) {
	if run.State.IsActive() {
		return
	}
	engineInvalidateContinuations(run)
	run.Pause = nil
}

// EngineStaleInterruptedTurn is whether the interrupted turn's output on leg is
// not an outcome yet: Continue has not been delivered.
func EngineStaleInterruptedTurn(run Run, leg *string) bool {
	attempt := enginePausedAttempt(run, leg)
	if attempt == nil {
		return false
	}
	if attempt.Interruption == nil || attempt.Interruption.Confirmation != AttemptInterruptionConfirmationConfirmed {
		return false
	}
	return attempt.Interruption.Continuation == nil ||
		attempt.Interruption.Continuation.Status != AttemptInterruptionContinuationStatusLaunched
}

// EngineOccupiesSlot is whether leg holds a model or session slot on run.
func EngineOccupiesSlot(run Run, leg Leg) bool {
	if leg.State != RunStateRunning {
		return false
	}
	if stage := leg.Stage(run); stage == nil || stage.Kind() != StageKindAgent {
		return false
	}
	attempt := enginePausedAttempt(run, new(leg.ID))
	if attempt == nil {
		return true
	}
	if attempt.Interruption != nil && attempt.Interruption.Confirmation == AttemptInterruptionConfirmationConfirmed &&
		(attempt.Interruption.Continuation == nil || attempt.Interruption.Continuation.Status != AttemptInterruptionContinuationStatusLaunched) {
		return false
	}
	return true
}
