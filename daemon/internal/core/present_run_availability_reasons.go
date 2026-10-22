// RunAvailability: why a command
// is unavailable, the retry refusal and the copy for drop and abort. The table's entry
// points are in present_run_availability.go.

package core

import "slices"

// RunAvailabilityBaseReason is nil when the command may run; otherwise why not. Beltline's
// busy moments (an AFK decision, a Continue launch) do not refuse a command,
// which takes effect at once as the app's did, except the two that start a
// session: two browsers must not launch the same conversation twice.
func RunAvailabilityBaseReason(command RunCommand, context RunActionContext) *string {
	if context.Erasing && command != RunCommandErase {
		return new(RunAvailabilityErasing)
	}
	if context.Busy && command == RunCommandContinueSession {
		return new(RunAvailabilityBusy)
	}
	switch command {
	case RunCommandDecide:
		return runAvailabilityDecideReason(context)
	case RunCommandResume, RunCommandDrop, RunCommandAfk:
		// Send to works on any unfinished run, whatever its state or pause; drop and the AFK
		// switch need only an unfinished run too.
		if !context.Run.State.IsActive() {
			return new(runAvailabilityFinished(context.Run))
		}
		return nil
	case RunCommandAnswer:
		return runAvailabilityAnswerReason(context)
	case RunCommandAbort:
		return runAvailabilityAbortReason(context)
	case RunCommandPark:
		return runAvailabilityParkReason(context)
	case RunCommandUnpark:
		if context.Run.Parking == nil {
			return new("This run wasn't parked from the Run page.")
		}
		return nil
	case RunCommandPause:
		return runAvailabilityPauseReason(context)
	case RunCommandResumeRun:
		return runAvailabilityResumeRunReason(context)
	case RunCommandRetryContinuation:
		return RunAvailabilityRetryContinuationRefusal(context, nil)
	case RunCommandStartNow:
		return runAvailabilityStartNowReason(context)
	case RunCommandContinueSession:
		return runAvailabilityContinueReason(context)
	case RunCommandAdjustStage:
		return runAvailabilityAdjustReason(context)
	case RunCommandErase:
		if context.Run.State.IsActive() {
			return new("Drop the run before erasing it.")
		}
		return nil
	case RunCommandOverruleAfk:
		return runAvailabilityOverruleReason(context)
	}
	return nil
}

// runAvailabilityActiveRefusal is why a run that is still in progress cannot take a command:
// it has finished, it is parked, or its pause blocks progress.
func runAvailabilityActiveRefusal(run Run) *string {
	if !run.State.IsActive() {
		return new(runAvailabilityFinished(run))
	}
	if run.Parking != nil {
		return new(RunAvailabilityParked)
	}
	if run.Pause != nil && run.Pause.BlocksProgress() {
		return new(RunAvailabilityPauseBlocks)
	}
	return nil
}

// runAvailabilityDecideReason is why a decision on a gate is unavailable.
func runAvailabilityDecideReason(context RunActionContext) *string {
	if refusal := runAvailabilityActiveRefusal(context.Run); refusal != nil {
		return refusal
	}
	stage := context.Run.stageRef()
	if context.Run.State != RunStateWaiting || stage == nil || stage.Kind() != StageKindGate {
		return new("The run isn't waiting for a decision.")
	}
	return nil
}

// runAvailabilityAnswerReason is why an answer to a held run's question is unavailable.
func runAvailabilityAnswerReason(context RunActionContext) *string {
	if refusal := runAvailabilityActiveRefusal(context.Run); refusal != nil {
		return refusal
	}
	if context.Run.State != RunStateHeld || context.Run.Answerable() == nil {
		return new("The run isn't waiting for an answer.")
	}
	return nil
}

// runAvailabilityAbortReason is why stopping the run's sessions is unavailable.
func runAvailabilityAbortReason(context RunActionContext) *string {
	run := context.Run
	if run.Parking != nil {
		return new(RunAvailabilityParked)
	}
	for _, leg := range run.Legs {
		if leg.CanAbort(run) {
			return nil
		}
	}
	return new("Nothing is running or waiting to be stopped.")
}

// runAvailabilityParkReason is why parking the run from its button is unavailable.
func runAvailabilityParkReason(context RunActionContext) *string {
	run := context.Run
	if !run.State.IsActive() {
		return new(runAvailabilityFinished(run))
	}
	if run.Parking != nil {
		return new("This run is already parked.")
	}
	if !run.CanButtonPark() {
		return new("Every unfinished route is already parked.")
	}
	return nil
}

// runAvailabilityPauseReason is why pausing the run is unavailable.
func runAvailabilityPauseReason(context RunActionContext) *string {
	run := context.Run
	if context.GlobalPause {
		return new(RunAvailabilityGlobalPauseHelp)
	}
	if run.Pause != nil && run.Pause.Phase == RunPausePhasePausing {
		return new(RunAvailabilityPauseStopping)
	}
	if !RunCanPause(run) {
		if run.Pause == nil {
			return new(runAvailabilityFinished(run))
		}
		return new("This run is already paused.")
	}
	return nil
}

// runAvailabilityResumeRunReason is why resuming a paused run is unavailable.
func runAvailabilityResumeRunReason(context RunActionContext) *string {
	run := context.Run
	if context.GlobalPause {
		return new(RunAvailabilityGlobalPauseHelp)
	}
	if run.Pause == nil {
		return new("This run isn't paused.")
	}
	switch run.Pause.Phase {
	case RunPausePhasePausing:
		return new(RunAvailabilityPauseStopping)
	case RunPausePhaseIncomplete:
		return new("The pause didn't finish. Retry pause first.")
	default:
		return nil
	}
}

// runAvailabilityStartNowReason is why starting a queued run now is unavailable.
func runAvailabilityStartNowReason(context RunActionContext) *string {
	run := context.Run
	if context.GlobalPause {
		return new(RunAvailabilityGlobalPauseHelp)
	}
	if run.Parking != nil {
		return new(RunAvailabilityParked)
	}
	if run.Pause != nil && run.Pause.BlocksProgress() {
		return new(RunAvailabilityPauseBlocks)
	}
	stage := run.stageRef()
	if run.State != RunStateQueued || stage == nil || stage.Kind() != StageKindAgent {
		return new("Only a queued agent stage can start now.")
	}
	return nil
}

// runAvailabilityContinueReason is why continuing a session is unavailable: every saved
// session must be refused for the run to refuse, and the first session's reason is shown.
func runAvailabilityContinueReason(context RunActionContext) *string {
	if refusal := runAvailabilityActiveRefusal(context.Run); refusal != nil {
		return refusal
	}
	var sessions []Attempt
	for _, attempt := range context.Run.Attempts {
		if attempt.Session != nil {
			sessions = append(sessions, attempt)
		}
	}
	if len(sessions) == 0 {
		return new("This run has no saved sessions.")
	}
	for _, attempt := range sessions {
		if _, refused := context.ContinueRefusals[attempt.ID]; !refused {
			return nil
		}
	}
	if refusal, ok := context.ContinueRefusals[sessions[0].ID]; ok {
		return new(refusal)
	}
	return new(RunAvailabilitySessionGone)
}

// runAvailabilityAdjustReason is why changing a stage's model is unavailable.
func runAvailabilityAdjustReason(context RunActionContext) *string {
	run := context.Run
	if !run.State.IsActive() {
		return new(runAvailabilityFinished(run))
	}
	if run.Parking != nil {
		return new(RunAvailabilityParked)
	}
	hasAgent := false
	allowed := false
	for _, stage := range run.Stages {
		if stage.Kind() != StageKindAgent {
			continue
		}
		hasAgent = true
		if _, refused := context.AdjustRefusals[stage.ID]; !refused {
			allowed = true
		}
	}
	if !hasAgent {
		return new("This belt has no agent stage to adjust.")
	}
	if !allowed {
		return new(StageAdjustNoModelsAllowed)
	}
	return nil
}

// runAvailabilityOverruleReason is why overruling the AFK decision is unavailable.
func runAvailabilityOverruleReason(context RunActionContext) *string {
	if !context.Run.State.IsActive() {
		return new(runAvailabilityFinished(context.Run))
	}
	for _, offer := range context.Overrules {
		if len(offer.Destinations) > 0 {
			return nil
		}
	}
	if len(context.Overrules) > 0 && context.Overrules[0].Refusal != nil {
		return context.Overrules[0].Refusal
	}
	return new("There is no applied AFK decision to overrule.")
}

// RunAvailabilityRetryContinuationRefusal is why a retry is unavailable. An omitted route
// is the main line, which the app retried whenever the run was not paused and some
// attempt's Continue had failed; a named route also has to exist and be the one whose
// Continue failed.
func RunAvailabilityRetryContinuationRefusal(context RunActionContext, leg *string) *string {
	if context.Erasing {
		return new(RunAvailabilityErasing)
	}
	if context.Busy {
		return new(RunAvailabilityBusy)
	}
	if context.GlobalPause {
		return new(RunAvailabilityGlobalPauseHelp)
	}
	run := context.Run
	if run.Pause != nil && run.Pause.BlocksProgress() {
		return new(RunAvailabilityPauseBlocks)
	}
	// The app offered Retry only while some open attempt's Continue had failed.
	if leg == nil {
		for _, attempt := range run.Attempts {
			if attempt.IsOpen() && runAvailabilityAttemptLeg(attempt) == "main" && runAvailabilityContinuationFailed(attempt) {
				return nil
			}
		}
		return new(RunAvailabilityNoFailed)
	}
	known := slices.ContainsFunc(run.Legs, func(candidate Leg) bool { return candidate.ID == *leg })
	if !known {
		return new(RunAvailabilityRetryRouteMissing)
	}
	for _, attempt := range run.Attempts {
		if attempt.IsOpen() && runAvailabilityAttemptLeg(attempt) == *leg && runAvailabilityContinuationFailed(attempt) {
			return nil
		}
	}
	return new(RunAvailabilityNoFailed)
}

// runAvailabilityFinished is why a finished run takes no command.
func runAvailabilityFinished(run Run) string {
	switch run.State {
	case RunStateDropped:
		return "This run was dropped."
	case RunStateDone:
		return "This run is done."
	}
	return "This run has shipped."
}

// RunAvailabilityDropHelp is what Drop says it does, for this run's kind of subject.
func RunAvailabilityDropHelp(run Run) string {
	return "Its session stops and its checkouts are removed. The " + run.Subject.Kind.Noun() + " itself is not changed."
}

// RunAvailabilityAbortHelp is what Abort does here. A split run stops every route that is
// still going; one route says what happens at the stage in front.
func RunAvailabilityAbortHelp(run Run) string {
	if run.IsSplit() {
		return "Running agents and watches on every route stop, and waiting approvals are held. Those routes wait for you at their current stages. Nothing is added to the issue, pull request or ticket."
	}
	stopped := "The run waits for you at this stage."
	if stage := run.stageRef(); stage != nil {
		switch stage.Kind() {
		case StageKindAgent:
			stopped = "The agent stops and the run waits for you at this stage."
		case StageKindWatch:
			stopped = "Watching stops and the run waits for you at this stage."
		}
	}
	return stopped + " Nothing is added to the issue, pull request or ticket."
}
