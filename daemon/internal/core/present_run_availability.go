// RunAvailability: the
// availability table. The types it returns are in present_run_actions.go.

package core

// Shared references to the model's pause copy, as RunAvailability names them.
const (
	// RunAvailabilityGlobalPauseHelp is the app's one global pause reason, shared with the toolbar.
	RunAvailabilityGlobalPauseHelp = RunPauseGlobalPauseHelp
	// RunAvailabilityPauseBlocks is what a whole-run pause that blocks progress says. A whole-run pause blocks progress.
	RunAvailabilityPauseBlocks = RunPauseExplanation
)

// RunAvailabilityActions is every action, unscoped records first in command order, then
// contextual records. Unchanged inputs always produce the same order.
func RunAvailabilityActions(context RunActionContext) []RunAction {
	var list []RunAction
	for _, command := range AllRunCommand {
		list = append(list, runAvailabilityBase(command, context))
	}
	return append(list, runAvailabilityContextual(context)...)
}

// RunActionTarget names the concrete target of a contextual action. A nil field is not
// matched.
type RunActionTarget struct {
	Stage    *string
	Leg      *string
	Outcome  *string
	Attempt  *string
	Decision *string
}

// RunAvailabilityActionAt is the contextual action for one command and target. Nil when
// no record describes that choice.
func RunAvailabilityActionAt(command RunCommand, context RunActionContext, target RunActionTarget) *RunAction {
	wantsTarget := target.Stage != nil || target.Leg != nil || target.Outcome != nil || target.Attempt != nil || target.Decision != nil
	for _, action := range context.Actions() {
		if action.ID != string(command) {
			continue
		}
		var matches bool
		if !wantsTarget {
			matches = action.IsUnscoped()
		} else {
			matches = runAvailabilityFieldMatches(target.Stage, action.Stage) &&
				runAvailabilityFieldMatches(target.Leg, action.Leg) &&
				runAvailabilityFieldMatches(target.Outcome, action.Outcome) &&
				runAvailabilityFieldMatches(target.Attempt, action.Attempt) &&
				runAvailabilityFieldMatches(target.Decision, action.Decision)
		}
		if matches {
			found := action
			return &found
		}
	}
	// An applied decision that cannot be overruled at all carries one
	// refused record without a destination; a named stage falls back to it.
	if command == RunCommandOverruleAfk && target.Decision != nil && target.Stage != nil {
		for _, action := range context.Actions() {
			if action.ID == string(command) && action.Decision != nil && *action.Decision == *target.Decision &&
				action.Stage == nil && !action.Enabled {
				found := action
				return &found
			}
		}
	}
	return nil
}

// RunAvailabilityRefusal is a command the daemon refuses with 422, with the same reason the
// resource carried. Nil when the command is available.
func RunAvailabilityRefusal(command RunCommand, context RunActionContext, target RunActionTarget) *string {
	action := RunAvailabilityActionAt(command, context, target)
	if action == nil || action.Enabled {
		return nil
	}
	return action.Reason
}

// RunAvailabilityHelp is the copy a command's record carries, whether or not it is
// available now.
func RunAvailabilityHelp(command RunCommand, context RunActionContext) *string {
	run := context.Run
	var text string
	switch command {
	case RunCommandDrop:
		text = RunAvailabilityDropHelp(run)
	case RunCommandAbort:
		text = RunAvailabilityAbortHelp(run)
	case RunCommandPause:
		text = "Pause this run. Its place and conversations stay."
	case RunCommandPark:
		text = "Park this run where it is. Nothing is posted."
	case RunCommandUnpark:
		text = "Continue this run where it stopped"
	case RunCommandErase:
		text = "Erase this run so its work can be picked up again"
	case RunCommandResume:
		text = "Gates and watches only record it."
	case RunCommandRetryContinuation:
		text = RunAvailabilityRetryHelp
	case RunCommandStartNow:
		name := "this stage"
		if stage := run.stageRef(); stage != nil {
			name = stage.Name
		}
		text = "Start " + name + " now, even when it is full or the belt is paused"
	case RunCommandAdjustStage:
		text = RunAvailabilityAdjustHelp
	case RunCommandOverruleAfk:
		text = RunAvailabilityOverruleHelp
	default:
		return nil
	}
	return &text
}

// runAvailabilityBase is the unscoped action for one command.
func runAvailabilityBase(command RunCommand, context RunActionContext) RunAction {
	reason := RunAvailabilityBaseReason(command, context)
	return NewRunAction(RunActionArgs{ID: string(command), Enabled: reason == nil, Reason: reason, Help: RunAvailabilityHelp(command, context)})
}

// runAvailabilityFieldMatches is true when want is unset, or got equals it. A nil want is not matched.
func runAvailabilityFieldMatches(want *string, got *string) bool {
	return want == nil || (got != nil && *got == *want)
}

// runAvailabilityAttemptLeg is the attempt's leg, with the main line named "main".
func runAvailabilityAttemptLeg(attempt Attempt) string {
	if attempt.Leg == nil {
		return "main"
	}
	return *attempt.Leg
}

// runAvailabilityContinuationFailed reports whether the attempt's Continue has failed.
func runAvailabilityContinuationFailed(attempt Attempt) bool {
	return attempt.Interruption != nil && attempt.Interruption.Continuation != nil &&
		attempt.Interruption.Continuation.Status == AttemptInterruptionContinuationStatusFailed
}
