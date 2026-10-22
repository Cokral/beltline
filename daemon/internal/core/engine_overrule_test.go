package core

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// overruleTestNow is when the overruled decision was made, in the overrule tests.
var overruleTestNow = parkTestNow

// overruleTestBelt is triage (agent), approve (gate), build (agent, on the quick route).
func overruleTestBelt() []Stage {
	triage := draftTestStage("triage", StageTypeAgent, "triage", "")
	triage.Name = "Triage"
	approve := draftTestStage("approve", StageTypeApprove, "", "")
	approve.Name = "Approve"
	build := draftTestStage("build", StageTypeAgent, "build", "quick")
	build.Name = "Build"
	return []Stage{triage, approve, build}
}

// overruleTestForks is the one fork of the overrule belt: the Quick route that build sits on.
func overruleTestForks() []Fork {
	return []Fork{{ID: "size", Routes: []Route{{ID: "quick", Name: "Quick"}}}}
}

// overruleTestDecision is an applied AFK decision on stage approve, made at overruleTestNow.
func overruleTestDecision() AfkDecision {
	return AfkDecision{ID: "d1", Status: AfkDecisionStatusApplied, StageName: "Approve", SubjectRef: "#1",
		SubjectTitle: "Export", StartedAt: jsonx.Date{Time: overruleTestNow}}
}

// overruleTestRun is an active run on approve, with the decision's attempt and any earlier attempts.
func overruleTestRun(extra ...Attempt) Run {
	decided := "d1"
	attempts := []Attempt{{ID: "approve-1", StageID: "approve", Number: 1,
		StartedAt: jsonx.Date{Time: overruleTestNow}, AfkDecision: &decided}}
	attempts = append(attempts, extra...)
	return Run{ID: "run", StageID: "approve", State: RunStateWaiting, Stages: overruleTestBelt(), Forks: overruleTestForks(), Attempts: attempts}
}

// TestAfkOfferForRefusesEachDecisionThatCannotBeOverruled: the refusals name why, as
// the overrule dialog shows them.
func TestAfkOfferForRefusesEachDecisionThatCannotBeOverruled(t *testing.T) {
	t.Parallel()
	applied := overruleTestDecision()
	notApplied := applied
	notApplied.Status = AfkDecisionStatusLeftForYou
	overruled := applied
	overruled.Overrule = &AfkOverrule{Comments: "no"}
	shipped := overruleTestRun()
	shipped.State = RunStateShipped
	dropped := overruleTestRun()
	dropped.State = RunStateDropped
	missing := overruleTestRun()
	missing.Attempts[0].AfkDecision = nil
	noAgents := overruleTestRun()
	noAgents.Stages = []Stage{draftTestStage("approve", StageTypeApprove, "", "")}
	noAgents.Forks = nil

	cases := []struct {
		name     string
		decision AfkDecision
		run      *Run
		want     AfkRefusal
	}{
		{"not applied", notApplied, nil, AfkRefusal{Kind: AfkRefusalKindIneligible}},
		{"already overruled", overruled, nil, AfkRefusal{Kind: AfkRefusalKindIneligible}},
		{"no run", applied, nil, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "The run is no longer here."}},
		{"shipped", applied, &shipped, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "This run has shipped and cannot be overruled."}},
		{"dropped", applied, &dropped, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "This run was dropped and cannot be overruled."}},
		{"no attempt", applied, &missing, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "The original attempt is missing, so this decision cannot be overruled."}},
		{"no agent", applied, &noAgents, AfkRefusal{Kind: AfkRefusalKindUnavailable, Message: "This belt has no agent stage to restart."}},
	}
	for _, tc := range cases {
		_, err := AfkOfferFor(tc.decision, tc.run)
		refusal, ok := err.(AfkRefusal)
		if !ok {
			t.Errorf("%s: err = %v, want an AfkRefusal", tc.name, err)
			continue
		}
		if refusal != tc.want {
			t.Errorf("%s: refusal = %+v, want %+v", tc.name, refusal, tc.want)
		}
		if tc.want.Kind == AfkRefusalKindIneligible && refusal.Error() != "" {
			t.Errorf("%s: ineligible error = %q, want empty", tc.name, refusal.Error())
		}
	}
}

// TestAfkOfferForOffersThePreviousAgentAndEveryAgentStage: the offer names the
// latest agent before the decision as the default, and every agent stage as a choice.
func TestAfkOfferForOffersThePreviousAgentAndEveryAgentStage(t *testing.T) {
	t.Parallel()
	run := overruleTestRun(Attempt{ID: "triage-1", StageID: "triage", Number: 1,
		StartedAt: jsonx.Date{Time: overruleTestNow.Add(-60e9)}})
	offer, err := AfkOfferFor(overruleTestDecision(), &run)
	if err != nil {
		t.Fatalf("offer error = %v", err)
	}
	if offer.Previous == nil || offer.Previous.StageID != "triage" || offer.Previous.RouteName != nil {
		t.Fatalf("previous = %+v, want triage on the line", offer.Previous)
	}
	if len(offer.Choices) != 2 || offer.Choices[0].StageID != "triage" || offer.Choices[1].StageID != "build" {
		t.Fatalf("choices = %+v, want triage and build", offer.Choices)
	}
	if offer.Choices[1].RouteName == nil || *offer.Choices[1].RouteName != "Quick" {
		t.Fatalf("build target = %+v, want it on the Quick route", offer.Choices[1])
	}
	if offer.AttemptID != "approve-1" || offer.DecisionStage != "Approve" || offer.CurrentStage != "Approve" {
		t.Fatalf("offer = %+v, want the decision's attempt, stage and current stage", offer)
	}
	if offer.SubjectRef != "#1" || offer.SubjectTitle != "Export" {
		t.Fatalf("offer subject = %q %q", offer.SubjectRef, offer.SubjectTitle)
	}
}

// TestAfkPreviousAgentNeverChoosesAnAttemptThatStartedLater: attempts that start
// after the decision are not candidates, and the decision's own agent wins when it is one.
func TestAfkPreviousAgentNeverChoosesAnAttemptThatStartedLater(t *testing.T) {
	t.Parallel()
	later := Attempt{ID: "build-1", StageID: "build", Number: 1, StartedAt: jsonx.Date{Time: overruleTestNow.Add(60e9)}}
	earlier := Attempt{ID: "triage-1", StageID: "triage", Number: 1, StartedAt: jsonx.Date{Time: overruleTestNow.Add(-60e9)}}
	run := overruleTestRun(later, earlier)
	previous := AfkPreviousAgent(overruleTestDecision(), run)
	if previous == nil || previous.StageID != "triage" {
		t.Fatalf("previous = %+v, want triage, the latest agent before the decision", previous)
	}

	own := overruleTestRun()
	own.Attempts[0].StageID = "build"
	own.Attempts[0].ID = "build-1"
	previous = AfkPreviousAgent(overruleTestDecision(), own)
	if previous == nil || previous.StageID != "build" {
		t.Fatalf("previous = %+v, want the decision's own agent build", previous)
	}

	none := overruleTestRun(later)
	if previous := AfkPreviousAgent(overruleTestDecision(), none); previous != nil {
		t.Fatalf("previous = %+v, want none when only later agents ran", previous)
	}
}

// TestAfkAgentTargetsAreTheAgentStagesInBeltOrder: targets include the stages on
// other routes, and their label names the route.
func TestAfkAgentTargetsAreTheAgentStagesInBeltOrder(t *testing.T) {
	t.Parallel()
	run := overruleTestRun()
	targets := AfkAgentTargets(run)
	if len(targets) != 2 || targets[0].StageID != "triage" || targets[1].StageID != "build" {
		t.Fatalf("targets = %+v, want triage then build", targets)
	}
	if got := targets[1].Label(); got != "Build · Quick" {
		t.Fatalf("label = %q, want %q", got, "Build · Quick")
	}
	if got := targets[0].Label(); got != "Triage" {
		t.Fatalf("label on the line = %q, want the stage name", got)
	}
}

// TestAfkTargetLabelNamesItsRoute: the label adds the route after a middle dot.
func TestAfkTargetLabelNamesItsRoute(t *testing.T) {
	t.Parallel()
	onRoute := AfkTarget{StageID: "b", StageName: "Build", RouteName: new("Quick")}
	if onRoute.StageID != "b" || onRoute.Label() != "Build · Quick" {
		t.Fatalf("target = %q %q, want b and Build · Quick", onRoute.StageID, onRoute.Label())
	}
	if (AfkTarget{StageID: "b", StageName: "Build"}).Label() != "Build" {
		t.Fatal("a stage on the line is labelled by its name alone")
	}
}
