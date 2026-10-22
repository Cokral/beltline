package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestStagePanelAttemptOfShowsAGateDecisionAndItsAfkNote checks a gate's attempt line: the chosen outcome, who
// decided and how long ago, the duration, and the AFK note when an AFK decision ended the wait.
func TestStagePanelAttemptOfShowsAGateDecisionAndItsAfkNote(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	var gate Stage
	for _, stage := range run.Stages {
		if stage.Kind() == StageKindGate {
			gate = stage
			break
		}
	}
	if gate.ID == "" {
		t.Fatal("the run has no gate stage")
	}
	now := testT0.Add(10 * time.Minute)
	ended := testT0.Add(4 * time.Minute)
	attempt := Attempt{
		ID:        "a1",
		StageID:   gate.ID,
		StartedAt: jsonx.Date{Time: testT0},
		EndedAt:   &jsonx.Date{Time: ended},
		Outcome:   new("approved"),
		Folder:    "1-" + gate.ID,
	}
	noLabel := func(string, Backend) *string { return nil }

	facts := StagePanelAttemptOf(attempt, run, gate, noLabel, now, nil)
	if facts.Decision == nil || *facts.Decision != CapitalizedFirst("approved") {
		t.Errorf("decision = %v", facts.Decision)
	}
	if facts.DecidedBy == nil || *facts.DecidedBy != "You" {
		t.Errorf("decided by = %v, want You", facts.DecidedBy)
	}
	if facts.DecisionLine == nil || *facts.DecisionLine != StageCopyDecidedLine("You", ended, now) {
		t.Errorf("decision line = %v", facts.DecisionLine)
	}
	if facts.DecidedAt == nil || !facts.DecidedAt.Equal(ended) {
		t.Errorf("decided at = %v, want %v", facts.DecidedAt, ended)
	}
	if facts.Duration == nil || *facts.Duration != StatusDuration(240) {
		t.Errorf("duration = %v", facts.Duration)
	}
	if facts.File != nil {
		t.Errorf("a gate has no output file, got %q", *facts.File)
	}
	if facts.Result == nil || facts.Result.Outcome == nil || *facts.Result.Outcome != "approved" || facts.Result.Problem {
		t.Errorf("result = %+v", facts.Result)
	}
	if facts.Afk != nil {
		t.Errorf("no AFK decision ended the wait, got %+v", facts.Afk)
	}

	attempt.AfkDecision = new("d1")
	decision := afkLogDecision("d1", AfkDecisionStatusApplied, false)
	decision.Action = new("Chose “approved” at Approve")
	decision.Why = new("The spec is complete.")
	facts = StagePanelAttemptOf(attempt, run, gate, noLabel, now, &decision)
	if facts.DecidedBy == nil || *facts.DecidedBy != "The "+AfkName {
		t.Errorf("decided by after AFK = %v", facts.DecidedBy)
	}
	if facts.Afk == nil || facts.Afk.Title != AfkName+" · "+AfkLogHeadline(decision) {
		t.Fatalf("afk note = %+v", facts.Afk)
	}
	if facts.Afk.Why == nil || *facts.Afk.Why != "The spec is complete." || facts.Afk.Comments != nil {
		t.Errorf("afk note why and comments = %v, %v", facts.Afk.Why, facts.Afk.Comments)
	}
}

// TestStagePanelAttemptOfFailureWithoutOutcome checks that a problem tone with no outcome shows the failed
// outcome row and is flagged as a problem.
func TestStagePanelAttemptOfFailureWithoutOutcome(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	stage := run.Stages[0]
	problem := ToneProblem
	attempt := Attempt{ID: "a1", StageID: stage.ID, StartedAt: jsonx.Date{Time: testT0}, Tone: &problem, Folder: "1-" + stage.ID}

	facts := StagePanelAttemptOf(attempt, run, stage, func(string, Backend) *string { return nil }, testT0, nil)
	if facts.Result == nil || !facts.Result.Problem || facts.Result.Outcome == nil || *facts.Result.Outcome != StageCopyFailedOutcome {
		t.Fatalf("result = %+v", facts.Result)
	}
	if facts.Result.Tone == nil || *facts.Result.Tone != string(ToneProblem) {
		t.Errorf("tone = %v", facts.Result.Tone)
	}
	if facts.Duration != nil {
		t.Errorf("an attempt without an end has no duration, got %v", facts.Duration)
	}
	if facts.File == nil || *facts.File != "runs/"+run.Folder()+"/1-"+stage.ID+"/"+stage.Writes() {
		t.Errorf("file = %v", facts.File)
	}
}

// TestStagePanelAfkNoteOfKeepsOnlyNonEmptyWords checks that an empty why is left out of the note, and that
// the deciding note comes from the last decision still being made.
func TestStagePanelAfkNoteOfKeepsOnlyNonEmptyWords(t *testing.T) {
	t.Parallel()
	applied := afkLogDecision("a", AfkDecisionStatusApplied, false)
	applied.Why = new("")
	if note := StagePanelAfkNoteOf(applied); note.Why != nil || note.Deciding {
		t.Errorf("note = %+v", note)
	}

	deciding := afkLogDecision("e", AfkDecisionStatusDeciding, false)
	decisions := []AfkDecision{applied, deciding}
	note := StagePanelDecidingNote(decisions)
	if note == nil || !note.Deciding {
		t.Fatalf("deciding note = %+v", note)
	}
	if got := StagePanelDecidingNote([]AfkDecision{applied}); got != nil {
		t.Errorf("no decision is deciding, got %+v", got)
	}
}
