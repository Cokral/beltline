// The target variants and the parity copy of the run actions.
// See present_run_actions_test.go for the

package core

import (
	"strings"
	"testing"
)

// TestContinueAndRetryNameTheirSavedAttempts
func TestContinueAndRetryNameTheirSavedAttempts(t *testing.T) {
	t.Parallel()
	run := presState("running")
	context := RunActionContextDefault(run)
	offered := presContextual(context, RunCommandContinueSession)
	if len(offered) != 1 {
		t.Fatalf("continue offers = %d, want 1", len(offered))
	}
	if *offered[0].Attempt != run.Attempts[0].ID || *offered[0].Stage != "triage" || !offered[0].Enabled {
		t.Fatalf("continue = %+v", offered[0])
	}

	refusal := "the run's checkout is gone"
	context = RunActionContextDefault(run)
	context.ContinueRefusals = map[string]string{run.Attempts[0].ID: refusal}
	offered = presContextual(context, RunCommandContinueSession)
	if offered[0].Enabled || *offered[0].Reason != refusal {
		t.Fatalf("refused continue = %+v", offered[0])
	}
	if presAction(t, RunCommandContinueSession, context).Enabled || *presAction(t, RunCommandContinueSession, context).Reason != refusal {
		t.Fatal("unscoped continue does not carry the refusal")
	}

	failed := presState("running")
	failed.Attempts[0].Interruption = &AttemptInterruption{
		Operation: "op", LegID: "main", Confirmation: AttemptInterruptionConfirmationConfirmed,
		Continuation: presContinuation("op", AttemptInterruptionContinuationStatusFailed, "no"),
	}
	retryContext := RunActionContextDefault(failed)
	if !presAction(t, RunCommandRetryContinuation, retryContext).Enabled {
		t.Fatal("retry is not offered after a failed continue")
	}
	retry := presContextual(retryContext, RunCommandRetryContinuation)
	if len(retry) != 1 || *retry[0].Attempt != failed.Attempts[0].ID || *retry[0].Leg != "main" {
		t.Fatalf("retry = %+v", retry)
	}
	if RunAvailabilityRetryContinuationRefusal(retryContext, nil) != nil {
		t.Fatal("main-line retry refused")
	}

	routed := failed.Clone()
	routedLeg := "r:next"
	routedStage := "triage"
	routed.Legs = []Leg{{ID: "r:next", Route: new("next"), StageID: &routedStage, State: RunStateRunning}}
	routed.Attempts[0].Leg = &routedLeg
	routed.Attempts[0].Interruption.LegID = "r:next"
	routedContext := RunActionContextDefault(routed)
	routedAction := RunAvailabilityActionAt(RunCommandRetryContinuation, routedContext,
		RunActionTarget{Stage: new("triage"), Leg: new("r:next"), Attempt: new(routed.Attempts[0].ID)})
	if routedAction == nil || !routedAction.Enabled {
		t.Fatal("routed retry at r:next is not enabled")
	}
	if presAction(t, RunCommandRetryContinuation, routedContext).Enabled {
		t.Fatal("the main-line retry is enabled on a routed run")
	}
	if RunAvailabilityRetryContinuationRefusal(routedContext, new("r:next")) != nil {
		t.Fatal("r:next retry refused")
	}
	if got := RunAvailabilityRetryContinuationRefusal(routedContext, nil); got == nil || *got != RunAvailabilityNoFailed {
		t.Fatalf("main retry = %v, want no failed continuation", got)
	}
	if got := RunAvailabilityRetryContinuationRefusal(routedContext, new("r:missing")); got == nil || *got != RunAvailabilityRetryRouteMissing {
		t.Fatalf("missing route = %v", got)
	}
}

// presContinuation is an interruption continuation with the given status and reason.
func presContinuation(operation string, status AttemptInterruptionContinuationStatus, reason string) *AttemptInterruptionContinuation {
	continuation := NewAttemptInterruptionContinuation(operation, status, new(reason), nil)
	return &continuation
}

// TestAdjustOffersEveryAgentStageAndNamesTheMissingModelCase
func TestAdjustOffersEveryAgentStageAndNamesTheMissingModelCase(t *testing.T) {
	t.Parallel()
	run := presState("queued")
	context := RunActionContextDefault(run)
	offered := presContextual(context, RunCommandAdjustStage)
	var agents []string
	for _, stage := range run.Stages {
		if stage.Kind() == StageKindAgent {
			agents = append(agents, stage.ID)
		}
	}
	if len(offered) != len(agents) {
		t.Fatalf("adjust offers = %d, want %d", len(offered), len(agents))
	}
	for i, action := range offered {
		if *action.Stage != agents[i] || !action.Enabled {
			t.Fatalf("adjust %d = %+v", i, action)
		}
	}

	// One refused stage keeps its own record refused; the command stays
	// offered while another stage still has a model.
	refused := RunActionContextDefault(run)
	refused.AdjustRefusals = map[string]string{"triage": StageAdjustNoModelsAllowed}
	if !presAction(t, RunCommandAdjustStage, refused).Enabled {
		t.Fatal("adjust refused while another stage has a model")
	}
	target := RunAvailabilityActionAt(RunCommandAdjustStage, refused, RunActionTarget{Stage: new("triage")})
	if target == nil || target.Enabled || *target.Reason != StageAdjustNoModelsAllowed {
		t.Fatalf("triage adjust = %+v", target)
	}

	noStage := RunActionContextDefault(run)
	noStage.AdjustRefusals = map[string]string{}
	for _, id := range agents {
		noStage.AdjustRefusals[id] = StageAdjustNoModelsAllowed
	}
	none := presAction(t, RunCommandAdjustStage, noStage)
	if none.Enabled || *none.Reason != StageAdjustNoModelsAllowed {
		t.Fatalf("adjust with no models = %+v", none)
	}

	gateOnly := engineStartRun(gateBelt())
	gateOnly.Stages = gateOnly.Stages[1:]
	gateOnly.StageID = gateOnly.Stages[0].ID
	gateOnly.State = RunStateWaiting
	gateOnly.Legs = []Leg{{ID: "main", StageID: new(gateOnly.StageID), State: RunStateWaiting}}
	noAgent := presAction(t, RunCommandAdjustStage, RunActionContextDefault(gateOnly))
	if noAgent.Enabled || *noAgent.Reason != "This belt has no agent stage to adjust." {
		t.Fatalf("gate-only adjust = %+v", noAgent)
	}
}

// TestOverruleOffersEachAppliedDecisionAndDestination
func TestOverruleOffersEachAppliedDecisionAndDestination(t *testing.T) {
	t.Parallel()
	run := presState("held")
	offer := RunOverruleOffer{Decision: "decision-1", Attempt: run.Attempts[0].ID,
		Destinations: []RunOverruleDestination{{Stage: "triage", Label: "Triage"}}}
	context := RunActionContextDefault(run)
	context.Overrules = []RunOverruleOffer{offer}
	if !presAction(t, RunCommandOverruleAfk, context).Enabled {
		t.Fatal("overrule is not offered")
	}
	target := RunAvailabilityActionAt(RunCommandOverruleAfk, context, RunActionTarget{Stage: new("triage"), Decision: new("decision-1")})
	if target == nil || !target.Enabled || *target.Attempt != run.Attempts[0].ID {
		t.Fatalf("triage overrule = %+v", target)
	}
	if RunAvailabilityActionAt(RunCommandOverruleAfk, context, RunActionTarget{Stage: new("missing"), Decision: new("decision-1")}) != nil {
		t.Fatal("a missing stage is offered")
	}

	ineligible := RunActionContextDefault(run)
	ineligible.Overrules = []RunOverruleOffer{{Decision: "decision-2", Refusal: new("The run is no longer here.")}}
	if presAction(t, RunCommandOverruleAfk, ineligible).Enabled || *presAction(t, RunCommandOverruleAfk, ineligible).Reason != "The run is no longer here." {
		t.Fatal("an ineligible overrule is offered")
	}
	refused := RunAvailabilityActionAt(RunCommandOverruleAfk, ineligible, RunActionTarget{Decision: new("decision-2")})
	if refused == nil || refused.Enabled {
		t.Fatalf("refused decision = %+v", refused)
	}

	finished := presState("shipped")
	finishedContext := RunActionContextDefault(finished)
	finishedContext.Overrules = []RunOverruleOffer{offer}
	if presAction(t, RunCommandOverruleAfk, finishedContext).Enabled {
		t.Fatal("overrule offered on a shipped run")
	}
}

// TestMenuCopyComesFromTheDaemonAsTheAppWordedIt
func TestMenuCopyComesFromTheDaemonAsTheAppWordedIt(t *testing.T) {
	t.Parallel()
	run := presState("running")
	context := RunActionContextDefault(run)
	help := func(command RunCommand) string {
		t.Helper()
		h := presAction(t, command, context).Help
		if h == nil {
			return ""
		}
		return *h
	}
	checks := []struct{ name, got, want string }{
		{"pause", help(RunCommandPause), "Pause this run. Its place and conversations stay."},
		{"park", help(RunCommandPark), "Park this run where it is. Nothing is posted."},
		{"drop", help(RunCommandDrop), "Its session stops and its checkouts are removed. The " + run.Subject.Kind.Noun() + " itself is not changed."},
		{"abort", help(RunCommandAbort), RunAvailabilityAbortHelp(run)},
		{"abort text", help(RunCommandAbort), "The agent stops and the run waits for you at this stage. Nothing is added to the issue, pull request or ticket."},
		{"resume", help(RunCommandResume), "Gates and watches only record it."},
		{"adjust-stage", help(RunCommandAdjustStage), "Uses models allowed when this run started."},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Fatalf("%s help = %q, want %q", check.name, check.got, check.want)
		}
	}
	erase := presAction(t, RunCommandErase, RunActionContextDefault(presState("shipped")))
	if erase.Help == nil || *erase.Help != "Erase this run so its work can be picked up again" {
		t.Fatalf("erase help = %v", erase.Help)
	}
}

// TestABusyRunStillAcceptsTheCommandsTheAppAccepted
func TestABusyRunStillAcceptsTheCommandsTheAppAccepted(t *testing.T) {
	t.Parallel()
	context := RunActionContextDefault(presState("waiting"))
	context.Busy = true
	for _, command := range []RunCommand{RunCommandDecide, RunCommandDrop, RunCommandAfk} {
		if !presAction(t, command, context).Enabled {
			t.Fatalf("%s refused while busy", command)
		}
	}
	if presAction(t, RunCommandDrop, context).Reason != nil {
		t.Fatal("drop has a reason while busy")
	}
}

// TestEraseWaitsForABusyOrErasingRunInsteadOfRefusingIt
func TestEraseWaitsForABusyOrErasingRunInsteadOfRefusingIt(t *testing.T) {
	t.Parallel()
	shipped := presState("shipped")
	busy := RunActionContextDefault(shipped)
	busy.Busy = true
	erasing := RunActionContextDefault(shipped)
	erasing.Erasing = true
	if !presAction(t, RunCommandErase, busy).Enabled || !presAction(t, RunCommandErase, erasing).Enabled {
		t.Fatal("erase refused while busy or erasing")
	}
	// Everything else waits out an erasure.
	running := RunActionContextDefault(presState("running"))
	running.Erasing = true
	drop := presAction(t, RunCommandDrop, running)
	if drop.Enabled || *drop.Reason != RunAvailabilityErasing {
		t.Fatalf("drop during erasure = %+v", drop)
	}
}

// TestASendToEndsAButtonParkOnAnyStage: Send to is open on the parked stage too, since
// it ends the park; Unpark still restarts the run where it was.
func TestASendToEndsAButtonParkOnAnyStage(t *testing.T) {
	t.Parallel()
	run := presState("parked")
	context := RunActionContextDefault(run)
	for _, stage := range run.Stages {
		if RunAvailabilityRefusal(RunCommandResume, context, RunActionTarget{Stage: new(stage.ID)}) != nil {
			t.Fatalf("send to %s is refused while parked", stage.ID)
		}
	}
	if RunAvailabilityRefusal(RunCommandUnpark, context, RunActionTarget{}) != nil {
		t.Fatal("unpark is refused while parked")
	}
}

// TestSendToAndDropAreOpenOnEveryUnfinishedState: whatever the route is doing, Send to
// and Drop are enabled, and they are refused only once the run has finished.
func TestSendToAndDropAreOpenOnEveryUnfinishedState(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"queued", "running", "held", "waiting", "parked", "outcome-parked"} {
		context := RunActionContextDefault(presState(name))
		for _, command := range []RunCommand{RunCommandResume, RunCommandDrop} {
			if action := presAction(t, command, context); !action.Enabled {
				t.Errorf("%s on %s is refused: %v", command, name, action.Reason)
			}
		}
	}
	for _, name := range []string{"shipped", "dropped"} {
		context := RunActionContextDefault(presState(name))
		for _, command := range []RunCommand{RunCommandResume, RunCommandDrop} {
			if action := presAction(t, command, context); action.Enabled || action.Reason == nil {
				t.Errorf("%s on %s = %+v, want refused with a reason", command, name, action)
			}
		}
	}
}

// TestRetryOnTheMainRouteNeedsOnlyAnUnpausedRunWithAFailedContinue
func TestRetryOnTheMainRouteNeedsOnlyAnUnpausedRunWithAFailedContinue(t *testing.T) {
	t.Parallel()
	run := presState("running")
	run.Parking = presState("parked").Parking
	if got := RunAvailabilityRetryContinuationRefusal(RunActionContextDefault(run), nil); got == nil || *got != RunAvailabilityNoFailed {
		t.Fatalf("before a failed continue = %v", got)
	}
	run.Attempts[0].Interruption = &AttemptInterruption{Operation: "op", LegID: "main", Confirmation: AttemptInterruptionConfirmationConfirmed,
		Continuation: presContinuation("op", AttemptInterruptionContinuationStatusFailed, "no")}
	parked := RunActionContextDefault(run)
	if RunAvailabilityRetryContinuationRefusal(parked, nil) != nil {
		t.Fatal("main-line retry refused after a failed continue")
	}
	if got := RunAvailabilityRetryContinuationRefusal(parked, new("missing")); got == nil || *got != RunAvailabilityRetryRouteMissing {
		t.Fatalf("missing route = %v", got)
	}
	paused := RunActionContextDefault(presState("running"))
	paused.GlobalPause = true
	if got := RunAvailabilityRetryContinuationRefusal(paused, nil); got == nil || *got != RunPauseGlobalPauseHelp {
		t.Fatalf("global pause = %v", got)
	}
}

// TestOverruleDestinationsCarryThePreviousAgentAndTheCommentsNote
func TestOverruleDestinationsCarryThePreviousAgentAndTheCommentsNote(t *testing.T) {
	t.Parallel()
	offer := RunOverruleOffer{Decision: "d1", Attempt: "a1",
		Destinations: []RunOverruleDestination{{Stage: "triage", Label: "Triage"}, {Stage: "build", Label: "Build"}},
		Previous:     new("build")}
	context := RunActionContextDefault(presState("shipped"))
	context.Overrules = []RunOverruleOffer{offer}
	destinations := presContextual(context, RunCommandOverruleAfk)
	var stages []string
	for _, action := range destinations {
		if action.Stage != nil {
			stages = append(stages, *action.Stage)
			if action.Help == nil || *action.Help != RunAvailabilityOverruleHelp {
				t.Fatalf("destination help = %v", action.Help)
			}
		}
	}
	if strings.Join(stages, ",") != "triage,build" {
		t.Fatalf("destinations = %v", stages)
	}
	if destinations[1].Previous != nil || destinations[2].Previous == nil || !*destinations[2].Previous {
		t.Fatalf("previous flags = %v, %v", destinations[1].Previous, destinations[2].Previous)
	}
}

// TestContinueIsRefusedWhenTheRunnerHasNoTerminal
func TestContinueIsRefusedWhenTheRunnerHasNoTerminal(t *testing.T) {
	t.Parallel()
	run := presState("running")
	headless := RunActionContextDefault(run)
	headless.TmuxRunner = false
	offered := presContextual(headless, RunCommandContinueSession)
	if len(offered) != 1 || offered[0].Enabled || *offered[0].Reason != RunAvailabilityContinueNeedsTmux {
		t.Fatalf("headless continue = %+v", offered)
	}
	tmux := presContextual(RunActionContextDefault(run), RunCommandContinueSession)
	if len(tmux) != 1 || !tmux[0].Enabled {
		t.Fatalf("tmux continue = %+v", tmux)
	}
}

// TestDecideActionsCarryTheNoteRuleAndTheHelpForEachOutcome
func TestDecideActionsCarryTheNoteRuleAndTheHelpForEachOutcome(t *testing.T) {
	t.Parallel()
	run := presState("waiting")
	context := RunActionContextDefault(run)
	stage := run.Stage()
	for _, outcome := range stage.Outcomes {
		decision := RunAvailabilityActionAt(RunCommandDecide, context, RunActionTarget{Stage: new(stage.ID), Outcome: new(outcome.Name)})
		if decision == nil {
			t.Fatalf("no decide action for %s", outcome.Name)
		}
		if decision.NeedsNote == nil || *decision.NeedsNote != outcome.Then.WithOutput {
			t.Fatalf("%s needsNote = %v", outcome.Name, decision.NeedsNote)
		}
		if decision.NoteLabel == nil || !strings.HasPrefix(*decision.NoteLabel, "Note for ") {
			t.Fatalf("%s noteLabel = %v", outcome.Name, decision.NoteLabel)
		}
		if decision.ThenHelp == nil || !strings.HasPrefix(*decision.ThenHelp, "Then: ") {
			t.Fatalf("%s thenHelp = %v", outcome.Name, decision.ThenHelp)
		}
	}
}
