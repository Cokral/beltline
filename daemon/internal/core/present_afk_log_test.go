package core

import (
	"reflect"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// afkLogDecision is a decision of the gate run in status, with reviewed set.
func afkLogDecision(id string, status AfkDecisionStatus, reviewed bool) AfkDecision {
	run := engineGateRun()
	decision := NewAfkDecision(AfkDecisionArgs{
		ID:        id,
		Run:       run,
		Situation: AfkSituation{Kind: AfkSituationKindApproval, Key: id},
		StartedAt: jsonx.Date{Time: testT0},
		Folder:    "",
	})
	decision.Status = status
	decision.Reviewed = reviewed
	return decision
}

// afkLogIDs is the ids of decisions, in order.
func afkLogIDs(decisions []AfkDecision) []string {
	ids := []string{}
	for _, decision := range decisions {
		ids = append(ids, decision.ID)
	}
	return ids
}

// TestToReviewShowsWhatNeedsYouNewestFirst
func TestToReviewShowsWhatNeedsYouNewestFirst(t *testing.T) {
	t.Parallel()
	all := []AfkDecision{
		afkLogDecision("a", AfkDecisionStatusApplied, false),
		afkLogDecision("b", AfkDecisionStatusSuperseded, false),
		afkLogDecision("c", AfkDecisionStatusApplied, true),
		afkLogDecision("d", AfkDecisionStatusLeftForYou, false),
		afkLogDecision("e", AfkDecisionStatusDeciding, false),
	}
	if got := afkLogIDs(AfkLogRows(all, AfkLogFilterReview)); !reflect.DeepEqual(got, []string{"e", "d", "a"}) {
		t.Fatalf("review rows = %v", got)
	}
	if got := afkLogIDs(AfkLogRows(all, AfkLogFilterAll)); !reflect.DeepEqual(got, []string{"e", "d", "c", "b", "a"}) {
		t.Fatalf("all rows = %v", got)
	}
}

// TestHeadlinesSayWhatHappened
func TestHeadlinesSayWhatHappened(t *testing.T) {
	t.Parallel()
	applied := afkLogDecision("a", AfkDecisionStatusApplied, false)
	applied.Action = new("Chose “approved” at Approve")
	if got := AfkLogHeadline(applied); got != "Chose “approved” at Approve" {
		t.Fatalf("applied headline = %q", got)
	}
	failed := afkLogDecision("b", AfkDecisionStatusFailed, false)
	failed.Detail = new("it took too long")
	if got := AfkLogHeadline(failed); got != "Could not decide at Approve: it took too long" {
		t.Fatalf("failed headline = %q", got)
	}
	if got := AfkLogHeadline(afkLogDecision("c", AfkDecisionStatusDeciding, false)); got != "Deciding at Approve…" {
		t.Fatalf("deciding headline = %q", got)
	}
}

// TestAnOverruleLeavesReviewAndKeepsTheOriginal
func TestAfkLogFactsReadTheRunAndTheFolder(t *testing.T) {
	t.Parallel()
	run := &Run{Stages: []Stage{{ID: "approve"}}}
	decision := AfkDecision{StageID: "approve"}
	if !AfkLogStageExists(decision, run) {
		t.Fatal("the run's stage is not found")
	}
	if AfkLogStageExists(AfkDecision{StageID: "spec"}, run) || AfkLogStageExists(decision, nil) {
		t.Fatal("a missing stage or run counts as present")
	}
	if reason := AfkLogRevealUnavailable(decision, true); reason != nil {
		t.Fatalf("reveal unavailable with its folder on disk: %q", *reason)
	}
	if reason := AfkLogRevealUnavailable(decision, false); reason == nil || *reason != "This decision kept no session files." {
		t.Fatalf("reason without a folder = %v", reason)
	}
	kept := AfkDecision{StageID: "approve", Folder: "afk/1-approve"}
	if reason := AfkLogRevealUnavailable(kept, false); reason == nil || *reason != "Its session files are no longer on disk." {
		t.Fatalf("reason for a gone folder = %v", reason)
	}
}

func TestAnOverruleLeavesReviewAndKeepsTheOriginal(t *testing.T) {
	t.Parallel()
	applied := afkLogDecision("a", AfkDecisionStatusApplied, false)
	applied.Action = new("Chose “approved” at Approve")
	applied.Choice = new("decide approved")
	applied.Message = new("Ship it.")
	applied.Why = new("The spec is complete.")
	applied.Overrule = &AfkOverrule{
		Comments:  "Cover the empty export.",
		StageID:   "spec",
		StageName: "Spec",
		At:        jsonx.Date{Time: testT0},
		AttemptID: "approve-1",
		Phase:     AfkOverrulePhaseRestarted,
	}
	if got := AfkLogLabel(applied); got != "OVERRULED" {
		t.Fatalf("label = %q", got)
	}
	if line := AfkLogOverruleLine(applied); line == nil || *line != "Overruled, restarting Spec" {
		t.Fatalf("overrule line = %v", line)
	}
	if got := AfkLogHeadline(applied); got != "Chose “approved” at Approve" {
		t.Fatalf("headline = %q", got)
	}
	if applied.NeedsReview() {
		t.Fatal("an overruled decision still needs review")
	}
	rows := []AfkDecision{applied, afkLogDecision("b", AfkDecisionStatusApplied, false)}
	if got := afkLogIDs(AfkLogRows(rows, AfkLogFilterReview)); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("review rows = %v", got)
	}
	if got := afkLogIDs(AfkLogRows(rows, AfkLogFilterAll)); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("all rows = %v", got)
	}
	held := applied
	heldOverrule := *applied.Overrule
	heldOverrule.Phase = AfkOverrulePhaseHeld
	heldOverrule.Detail = new("the session would not stop")
	held.Overrule = &heldOverrule
	if got := AfkLogLabel(held); got != "HELD" {
		t.Fatalf("held label = %q", got)
	}
	if line := AfkLogOverruleLine(held); line == nil || !strings.Contains(*line, "the session would not stop") {
		t.Fatalf("held overrule line = %v", line)
	}
}
