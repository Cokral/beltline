package core

import (
	"reflect"
	"strings"
	"testing"
)

// approvalWillTexts flattens an approval's lines to their plain text, one string per line.
func approvalWillTexts(lines [][]InlineSpan) []string {
	texts := make([]string, 0, len(lines))
	for _, spans := range lines {
		var b strings.Builder
		for _, span := range spans {
			b.WriteString(span.Text)
		}
		texts = append(texts, b.String())
	}
	return texts
}

// approvalStageOf is the default issues belt's approval stage, and the belt's approval outcomes.
func approvalStageOf(t *testing.T, belt Belt) Stage {
	t.Helper()
	for _, stage := range belt.Stages {
		if stage.Type == StageTypeApprove {
			return stage
		}
	}
	t.Fatal("the default belt has no approval stage")
	return Stage{}
}

// TestApprovalWillListsWhatEachStageAfterTheApprovalDoes checks the lines under "Beltline will" for an approve
// that moves on: a worktree agent names the branch, a read-only one works on the change, and a watch waits
// for the pull request.
func TestApprovalWillListsWhatEachStageAfterTheApprovalDoes(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	run := engineStartRun(belt)
	stage := approvalStageOf(t, belt)

	will := ApprovalWillOf(run, stage, stage.Outcomes[0])
	if will.Heading != "Approve means Beltline will" {
		t.Errorf("heading = %q", will.Heading)
	}
	want := []string{
		"Create beltline/issue-142 and have Pi run Build.",
		"Have Pi run Verify on the change.",
		"Create beltline/issue-142 and have Pi run Ship.",
		"Wait for the pull request to be merged.",
		"Run Wrap up.",
	}
	if got := approvalWillTexts(will.Lines); !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

// TestApprovalWillUsesTheRunsBranchWhenItHasOne checks that a run's own branch replaces the default one in the
// lines that name it.
func TestApprovalWillUsesTheRunsBranchWhenItHasOne(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	run := engineStartRun(belt)
	run.Branch = new("feature/export")
	stage := approvalStageOf(t, belt)

	lines := approvalWillTexts(ApprovalWillOf(run, stage, stage.Outcomes[0]).Lines)
	if len(lines) == 0 || lines[0] != "Create feature/export and have Pi run Build." {
		t.Errorf("first line = %q", lines)
	}
}

// TestApprovalWillDescribesAnOutcomeThatDoesNotMoveOn checks that an outcome which parks the run gets one line
// describing its route, not the stages after the approval.
func TestApprovalWillDescribesAnOutcomeThatDoesNotMoveOn(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	run := engineStartRun(belt)
	stage := approvalStageOf(t, belt)
	parked := stage.Outcomes[0]
	parked.Then = NewThen(ThenArgs{Action: ThenActionPark})

	will := ApprovalWillOf(run, stage, parked)
	if want := []string{"Parked."}; !reflect.DeepEqual(approvalWillTexts(will.Lines), want) {
		t.Errorf("lines = %q, want %q", approvalWillTexts(will.Lines), want)
	}
}
