package core

import (
	"reflect"
	"testing"
)

// TestAttemptOutputIsNotApplicableToANonAgentStage: an approval writes no output, so it has no blocks or message.
func TestAttemptOutputIsNotApplicableToANonAgentStage(t *testing.T) {
	t.Parallel()
	stage := NewStage(StageArgs{ID: "approve", Type: StageTypeApprove, Outcomes: []Outcome{}})
	attempt := NewAttempt(AttemptArgs{StageID: "approve", Number: 1, Folder: "first"})
	got := AttemptOutputPresent(stage, attempt, new("ignored"))
	if got.Availability != AttemptOutputNotApplicable || got.Message != nil || got.Blocks == nil || len(got.Blocks) != 0 {
		t.Fatalf("presented = %+v, want notApplicable with no blocks or message", got)
	}
}

// TestAttemptOutputParsesTheTextAfterStrippingOutcomeLines: text that exists is available, with its outcome line removed.
func TestAttemptOutputParsesTheTextAfterStrippingOutcomeLines(t *testing.T) {
	t.Parallel()
	stage := NewStage(StageArgs{ID: "spec", Type: StageTypeAgent, Agent: &AgentRef{ID: "spec", Version: 1}, Key: new("spec"), Outcomes: []Outcome{}})
	attempt := NewAttempt(AttemptArgs{StageID: "spec", Number: 1, Folder: "first"})
	text := "# Plan\n\nDo the work.\n\nOutcome: complete"
	got := AttemptOutputPresent(stage, attempt, &text)
	if got.Availability != AttemptOutputAvailable || got.Message != nil {
		t.Fatalf("presented = %+v, want available with no message", got)
	}
	want := []MarkdownBlock{
		{Kind: MarkdownBlockKindHeading, Level: 1, Text: "Plan"},
		{Kind: MarkdownBlockKindParagraph, Text: "Do the work."},
	}
	if !reflect.DeepEqual(got.Blocks, want) {
		t.Fatalf("blocks = %+v, want %+v", got.Blocks, want)
	}
}

// TestAttemptOutputWithoutTextSaysWhatIsMissing: an open attempt is still working; a closed one names the file it wrote.
func TestAttemptOutputWithoutTextSaysWhatIsMissing(t *testing.T) {
	t.Parallel()
	stage := NewStage(StageArgs{ID: "spec", Type: StageTypeAgent, Agent: &AgentRef{ID: "spec", Version: 1}, Key: new("spec"), Outcomes: []Outcome{}})
	open := NewAttempt(AttemptArgs{StageID: "spec", Number: 1, Folder: "first"})
	got := AttemptOutputPresent(stage, open, nil)
	if got.Availability != AttemptOutputMissing || got.Blocks == nil || got.Message == nil ||
		*got.Message != "The agent is working. Its output appears here when it is done." {
		t.Fatalf("open attempt presented = %+v", got)
	}
	closed := open
	ended := open.StartedAt
	closed.EndedAt = &ended
	got = AttemptOutputPresent(stage, closed, nil)
	if got.Message == nil || *got.Message != "This attempt wrote no spec.md." {
		t.Fatalf("closed attempt message = %+v", got.Message)
	}
}
