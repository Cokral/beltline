// Tests of the AFK gate: the approval situation, the decision line and the brief.

package core

import (
	"slices"
	"strings"
	"testing"
)

// afkGateRun is a default-belt run waiting at its approval stage.
func afkGateRun() Run {
	run := briefTestSampleRun(engineIssueBelt())
	briefTestFinish(&run, "ready to spec")
	briefTestFinish(&run, "complete")
	return run
}

// TestAnApprovalOffersItsOutcomesAndLeavingItLast
func TestAnApprovalOffersItsOutcomesAndLeavingItLast(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	if run.State != RunStateWaiting {
		t.Fatalf("state = %v, want waiting", run.State)
	}
	situation := AfkSituationOf(run, afkOn())
	if situation == nil {
		t.Fatal("no situation for the approval")
	}
	if situation.Kind != AfkSituationKindApproval {
		t.Fatalf("kind = %v, want approval", situation.Kind)
	}
	ids := afkIDs(situation)
	if len(ids) == 0 || ids[0] != "decide approved" {
		t.Fatalf("ids = %v, want decide approved first", ids)
	}
	if !slices.Contains(ids, "decide sent back") {
		t.Fatalf("ids = %v, want decide sent back", ids)
	}
	if ids[len(ids)-1] != "leave" {
		t.Fatalf("ids = %v, want leave last", ids)
	}
	// Dropping is off by default.
	if slices.Contains(ids, "drop") {
		t.Fatalf("ids = %v, want no drop", ids)
	}
	approveOff := afkOn()
	approveOff.Approve = false
	if AfkSituationOf(run, approveOff) != nil {
		t.Fatal("an approval with approve off has a situation")
	}
}

// TestAnApprovalOutcomeThatDropsNeedsTheDropPermission
func TestAnApprovalOutcomeThatDropsNeedsTheDropPermission(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	index := run.StageIndex()
	if index == nil {
		t.Fatal("no current stage")
	}
	run.Stages[*index].Outcomes = append(run.Stages[*index].Outcomes, Outcome{ID: NewUUID(), Name: "rejected", When: "Reject", Tone: ToneProblem,
		Then: NewThen(ThenArgs{Action: ThenActionDrop})})
	without := AfkSituationOf(run, afkOn())
	if without == nil {
		t.Fatal("no situation without drop")
	}
	if slices.Contains(afkIDs(without), "decide rejected") {
		t.Fatalf("without drop permission the options are %v", afkIDs(without))
	}
	with := AfkSituationOf(run, afkEveryChoice())
	if with == nil {
		t.Fatal("no situation with drop")
	}
	if !slices.Contains(afkIDs(with), "decide rejected") || !slices.Contains(afkIDs(with), "drop") {
		t.Fatalf("with drop permission the options are %v", afkIDs(with))
	}
}

// TestTheLastDecisionLineDecides
func TestTheLastDecisionLineDecides(t *testing.T) {
	t.Parallel()
	text := "## Message\n\nShip it.\n\n## Why\n\nThe spec is complete.\n\nDecision: leave\n\n**Decision:** `Decide  Approved`\n"
	parsed := AfkParse(text, afkDecisionOptions())
	if parsed == nil {
		t.Fatal("no decision parsed")
	}
	if parsed.Option.ID != "decide approved" {
		t.Fatalf("option = %q, want decide approved", parsed.Option.ID)
	}
	if parsed.Message == nil || *parsed.Message != "Ship it." {
		t.Fatalf("message = %v, want Ship it.", parsed.Message)
	}
	if parsed.Why == nil || *parsed.Why != "The spec is complete." {
		t.Fatalf("why = %v, want The spec is complete.", parsed.Why)
	}
}

// TestAnOutcomeNameAloneChoosesThatOutcome
func TestAnOutcomeNameAloneChoosesThatOutcome(t *testing.T) {
	t.Parallel()
	parsed := AfkParse("## Why\n\nWave 2 is as planned.\n\nDecision: approved\n", afkDecisionOptions())
	if parsed == nil || parsed.Option.ID != "decide approved" {
		t.Fatalf("parsed = %+v, want decide approved", parsed)
	}
}

// TestDroppingSaysWhoDroppedIt
func TestDroppingSaysWhoDroppedIt(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by the AFK assistant"}, engineT0)
	if run.State != RunStateDropped {
		t.Fatalf("state = %v, want dropped", run.State)
	}
	if run.Reason == nil || *run.Reason != "dropped by the AFK assistant" {
		t.Fatalf("reason = %v, want dropped by the AFK assistant", run.Reason)
	}
}

// TestTheBriefListsTheOptionsAndWhereToWrite
func TestTheBriefListsTheOptionsAndWhereToWrite(t *testing.T) {
	t.Parallel()
	run := afkGateRun()
	settings := afkOn()
	settings.Guidance = "Never approve billing changes."
	situation := AfkSituationOf(run, settings)
	if situation == nil {
		t.Fatal("no situation")
	}
	latest := "# Spec\n\nThe goal."
	brief := AfkBriefRender(run, *situation, AfkBriefContext{
		RunFolder:    "/runs/x",
		Decision:     "/runs/x/afk/01-approve/decision.md",
		LatestOutput: &latest,
		Guidance:     "Never approve billing changes.",
	})
	for _, want := range []string{"- `decide approved`", "- `leave`", "/runs/x/afk/01-approve/decision.md",
		"Never approve billing changes.", "# Spec", "Export invoices as CSV"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
}
