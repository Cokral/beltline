// Tests of which AFK decisions can be overruled. The
// linked decision is afkTestLinkedDecision.

package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// afkTestAt is the instant of seconds after the tests' t0.
func afkTestAt(seconds int) time.Time {
	return engineT0.Add(time.Duration(seconds) * time.Second)
}

// afkTestLinkedDecision builds an applied (or given) decision with the given status,
// reviewed flag, id and start time: on the run's current or latest
// attempt, which it links.
func afkTestLinkedDecision(run *Run, status AfkDecisionStatus, reviewed bool, id string, startedAt time.Time) AfkDecision {
	var host *Attempt
	if current := run.CurrentAttempt(); current != nil {
		host = current
	} else if len(run.Attempts) > 0 {
		host = &run.Attempts[len(run.Attempts)-1]
	}
	kind := AfkSituationKindApproval
	if stage := run.Stage(); host != nil && host.StageID == run.StageID && stage != nil && stage.Kind() == StageKindAgent {
		kind = AfkSituationKindFailure
	}
	decision := NewAfkDecision(AfkDecisionArgs{ID: id, Run: *run, Situation: AfkSituation{Kind: kind, Key: id},
		StartedAt: jsonx.Date{Time: startedAt}, Folder: "afk/01"})
	decision.Status = status
	decision.Reviewed = reviewed
	decision.Choice = new("decide approved")
	decision.Action = new("Chose “approved” at " + decision.StageName)
	decision.Message = new("Ship it.")
	decision.Why = new("The spec is complete.")
	if host != nil {
		for index := range run.Attempts {
			if run.Attempts[index].ID == host.ID {
				run.Attempts[index].AfkDecision = new(decision.ID)
			}
		}
	}
	return decision
}

// afkTestRefusal is the refusal AfkOfferFor returned, or nil for an offer.
func afkTestRefusal(err error) *AfkRefusal {
	if refusal, ok := err.(AfkRefusal); ok {
		return &refusal
	}
	return nil
}

// TestUnfinishedDecisionsAreIneligible
func TestUnfinishedDecisionsAreIneligible(t *testing.T) {
	t.Parallel()
	for _, status := range []AfkDecisionStatus{AfkDecisionStatusDeciding, AfkDecisionStatusFailed, AfkDecisionStatusLeftForYou, AfkDecisionStatusSuperseded} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			run := afkGateRun()
			decision := afkTestLinkedDecision(&run, status, false, "d", afkTestAt(30))
			_, err := AfkOfferFor(decision, &run)
			if refusal := afkTestRefusal(err); refusal == nil || refusal.Kind != AfkRefusalKindIneligible {
				t.Fatalf("offer error = %v, want ineligible", err)
			}
		})
	}
}

// TestAnOverruledDecisionCannotBeOverruledAgain
func TestAnOverruledDecisionCannotBeOverruledAgain(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	decision := afkTestLinkedDecision(&run, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	decision.Overrule = &AfkOverrule{Comments: "Already", StageID: "spec", StageName: "Spec", At: jsonx.Date{Time: engineT0},
		AttemptID: "approve-1", Phase: AfkOverrulePhaseRestarted}
	_, err := AfkOfferFor(decision, &run)
	if refusal := afkTestRefusal(err); refusal == nil || refusal.Kind != AfkRefusalKindIneligible {
		t.Fatalf("offer error = %v, want ineligible", err)
	}
}

// TestCheckedAndUncheckedAppliedDecisionsCanBeOverruled
func TestCheckedAndUncheckedAppliedDecisionsCanBeOverruled(t *testing.T) {
	t.Parallel()
	for _, reviewed := range []bool{false, true} {
		run := afkGateRun()
		decision := afkTestLinkedDecision(&run, AfkDecisionStatusApplied, reviewed, "d", afkTestAt(30))
		offer, err := AfkOfferFor(decision, &run)
		if err != nil {
			t.Fatalf("reviewed %v: an applied decision should be overridable: %v", reviewed, err)
		}
		if last := run.Attempts[len(run.Attempts)-1]; offer.AttemptID != last.ID {
			t.Fatalf("reviewed %v: attempt = %q, want %q", reviewed, offer.AttemptID, last.ID)
		}
		if offer.Previous == nil || offer.Previous.StageID != "spec" {
			t.Fatalf("reviewed %v: previous = %+v, want spec", reviewed, offer.Previous)
		}
		if offer.CurrentStage != run.Stage().Name {
			t.Fatalf("reviewed %v: current stage = %q, want %q", reviewed, offer.CurrentStage, run.Stage().Name)
		}
	}
}

// TestActiveRunsCanBeOverruled
func TestActiveRunsCanBeOverruled(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateQueued, RunStateRunning, RunStateWaiting, RunStateHeld, RunStateParked} {
		state := state
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			run := briefTestSampleRun(engineIssueBelt())
			switch state {
			case RunStateRunning:
				EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "bl-test", SessionID: "id", Folder: "f"}, engineT0)
			case RunStateWaiting:
				run = afkGateRun()
			case RunStateHeld:
				briefTestFinish(&run, "needs info")
			case RunStateParked:
				briefTestFinish(&run, "needs info")
				run.State = RunStateParked
			default:
				briefTestFinish(&run, "needs info")
				run.State = RunStateQueued
				run.StageID = "triage"
			}
			decision := afkTestLinkedDecision(&run, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
			if _, err := AfkOfferFor(decision, &run); err != nil {
				t.Fatalf("%s should be overridable: %v", state, err)
			}
		})
	}
}

// TestShippedDroppedAbsentAndIncompleteHistoryCannot
func TestShippedDroppedAbsentAndIncompleteHistoryCannot(t *testing.T) {
	t.Parallel()
	shipped := afkGateRun()
	decision := afkTestLinkedDecision(&shipped, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	shipped.State = RunStateShipped
	if _, err := AfkOfferFor(decision, &shipped); !reflect.DeepEqual(err, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "This run has shipped and cannot be overruled."}) {
		t.Fatalf("shipped error = %v", err)
	}
	dropped := afkGateRun()
	dropping := afkTestLinkedDecision(&dropped, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	dropped.State = RunStateDropped
	if _, err := AfkOfferFor(dropping, &dropped); !reflect.DeepEqual(err, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "This run was dropped and cannot be overruled."}) {
		t.Fatalf("dropped error = %v", err)
	}
	if _, err := AfkOfferFor(decision, nil); !reflect.DeepEqual(err, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "The run is no longer here."}) {
		t.Fatalf("missing run error = %v", err)
	}
	missing := afkGateRun()
	unlinked := afkTestLinkedDecision(&missing, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	for index := range missing.Attempts {
		missing.Attempts[index].AfkDecision = nil
	}
	if _, err := AfkOfferFor(unlinked, &missing); !reflect.DeepEqual(err, AfkRefusal{Kind: AfkRefusalKindUnavailable,
		Message: "The original attempt is missing, so this decision cannot be overruled."}) {
		t.Fatalf("missing attempt error = %v", err)
	}
	bare := afkGateRun()
	bareDecision := afkTestLinkedDecision(&bare, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	var kept []Stage
	for _, stage := range bare.Stages {
		if stage.Kind() != StageKindAgent {
			kept = append(kept, stage)
		}
	}
	bare.Stages = kept
	refusal := afkTestRefusal(func() error { _, err := AfkOfferFor(bareDecision, &bare); return err }())
	if refusal == nil || refusal.Kind != AfkRefusalKindUnavailable || !nfContains([]string{refusal.Message}, "no agent stage") {
		t.Fatalf("a belt with no agent stage refused with %v", refusal)
	}
}

// TestOldDecisionsAndRunsDecodeWithoutAnOverrule
func TestOldDecisionsAndRunsDecodeWithoutAnOverrule(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	decision := afkTestLinkedDecision(&run, AfkDecisionStatusApplied, false, "d", afkTestAt(30))
	savedDecision, err := jsonx.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	members, err := jsonx.Members(savedDecision)
	if err != nil {
		t.Fatal(err)
	}
	delete(members, "overrule")
	oldDecisionData, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	var decoded AfkDecision
	if err := json.Unmarshal(oldDecisionData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Overrule != nil {
		t.Fatalf("overrule = %+v, want none", decoded.Overrule)
	}
	if decoded.Status != AfkDecisionStatusApplied {
		t.Fatalf("status = %q, want applied", decoded.Status)
	}

	savedRun, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	runMembers, err := jsonx.Members(savedRun)
	if err != nil {
		t.Fatal(err)
	}
	delete(runMembers, "restart")
	delete(runMembers, "operation")
	oldRunData, err := json.Marshal(runMembers)
	if err != nil {
		t.Fatal(err)
	}
	var oldRun Run
	if err := json.Unmarshal(oldRunData, &oldRun); err != nil {
		t.Fatal(err)
	}
	if oldRun.Restart != nil {
		t.Fatalf("restart = %+v, want none", oldRun.Restart)
	}
	if oldRun.Operation != 0 {
		t.Fatalf("operation = %d, want 0", oldRun.Operation)
	}

	run.Restart = &RestartIntent{DecisionID: decision.ID, AttemptID: "approve-1", StageID: "spec", Comments: "Again", At: jsonx.Date{Time: engineT0}, Phase: RestartIntentPhaseIntent}
	run.Operation = 4
	savedRun, err = jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var round Run
	if err := json.Unmarshal(savedRun, &round); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(round.Restart, run.Restart) {
		t.Fatalf("restart = %+v, want %+v", round.Restart, run.Restart)
	}
	if round.Operation != 4 {
		t.Fatalf("operation = %d, want 4", round.Operation)
	}
	savedDecision, err = jsonx.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	var decisionRound AfkDecision
	if err := json.Unmarshal(savedDecision, &decisionRound); err != nil {
		t.Fatal(err)
	}
	if d := testDiffEmpty(decision, decisionRound); d != "" {
		t.Fatalf("decision differs after a round trip:\n%s", d)
	}
}
