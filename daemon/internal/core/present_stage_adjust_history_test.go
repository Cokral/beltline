package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestAdjustmentLabelsJoinInOrderWithEffortForThinking pins the labels the stage panel
// history and the run's note both show: the fields in the order they were changed, with
// thinking named Effort.
func TestAdjustmentLabelsJoinInOrderWithEffortForThinking(t *testing.T) {
	t.Parallel()
	fields := []AdjustmentField{AdjustmentFieldModel, AdjustmentFieldThinking, AdjustmentFieldBrief}
	if got, want := AdjustmentLabels(fields), "Model, Effort, Brief"; got != want {
		t.Fatalf("AdjustmentLabels = %q, want %q", got, want)
	}
	if got := AdjustmentLabels(nil); got != "" {
		t.Fatalf("AdjustmentLabels(nil) = %q, want empty", got)
	}
}

// TestAdjustmentHistoryLineNamesTheStageAndWhen pins the stage panel's history line for one
// adjustment: the stage's name, how long ago it was saved, and what changed.
func TestAdjustmentHistoryLineNamesTheStageAndWhen(t *testing.T) {
	t.Parallel()
	now := time.Unix(100_000, 0)
	adjustment := Adjustment{
		StageID: "build",
		Date:    jsonx.Date{Time: now.Add(-240 * time.Second)},
		Fields:  []AdjustmentField{AdjustmentFieldModel, AdjustmentFieldThinking},
	}
	if got, want := AdjustmentHistoryLine("Build", adjustment, now), "Build adjusted 4 min ago · Model, Effort"; got != want {
		t.Fatalf("AdjustmentHistoryLine = %q, want %q", got, want)
	}
}

// TestRunPresentationLookingNamesTheRunBranchUntilAPullRequestIsKnown pins the watch line
// shown while no pull request is known: the run's branch, or the fallback when it has none.
// Once a pull request is known the line is gone.
func TestRunPresentationLookingNamesTheRunBranchUntilAPullRequestIsKnown(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	branch := "beltline/issue-142"
	run.Branch = &branch
	run.PullRequests = nil
	if got := RunPresentationLooking(run); got == nil || *got != "Looking for the pull request from beltline/issue-142." {
		t.Fatalf("RunPresentationLooking with a branch = %v", got)
	}
	run.Branch = nil
	if got := RunPresentationLooking(run); got == nil || *got != "Looking for the pull request from the run's branch." {
		t.Fatalf("RunPresentationLooking without a branch = %v", got)
	}
	run.PullRequests = []PullRequest{{Repo: "owner/repo", Number: 12, URL: "https://example.com/12", State: PullRequestStateOpen}}
	if got := RunPresentationLooking(run); got != nil {
		t.Fatalf("RunPresentationLooking with a pull request = %q, want nil", *got)
	}
}
