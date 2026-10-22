// Validation tests of belt draft requests: the rejected requests, the located problems,
// the failure without locations, and the goto picks that name a stage on the belt.
// The fixtures are the draft helpers of the sibling files (draftEditingBelt, draftFeedBelt, draftParityBelt).

package core

import (
	"reflect"
	"testing"
)

// vtFailure is the ValidationFailure an error holds, or a test failure.
func vtFailure(t *testing.T, err error) ValidationFailure {
	t.Helper()
	failure, ok := err.(ValidationFailure)
	if !ok {
		t.Fatalf("error %v (%T), want ValidationFailure", err, err)
	}
	return failure
}

// TestARejectedRequestReturnsItsOrderedProblemsAndLeavesTheInputDraftAlone
func TestARejectedRequestReturnsItsOrderedProblemsAndLeavesTheInputDraftAlone(t *testing.T) {
	t.Parallel()
	belt := draftEditingBelt()
	before := belt.Clone()
	request := BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageRemove, StageID: new("not-a-stage")}
	if got := BeltDraftEditingProblems(request); !reflect.DeepEqual(got, []string{"stage-remove needs a stage id that is on the belt."}) {
		t.Fatalf("problems %v", got)
	}
	if _, err := BeltDraftEditingApply(request); err == nil {
		t.Fatal("apply accepted the request")
	} else {
		_ = vtFailure(t, err) // asserts the error type; the failure value itself is not needed
	}
	if !reflect.DeepEqual(belt, before) {
		t.Fatal("the input draft changed")
	}
}

// TestRepositoryAddRefusesASecondRepositoryForASingleRepositoryKind
func TestRepositoryAddRefusesASecondRepositoryForASingleRepositoryKind(t *testing.T) {
	t.Parallel()
	schedule := DefaultScheduledTask()
	base := NewBeltFromTrigger(BeltFromTriggerArgs{
		ID: "sched", Name: "Sched", Trigger: Trigger{Kind: TriggerKindSchedule, Schedule: &schedule}, Repos: []string{"acme/one"},
	})
	// The app's form had no second field for these kinds.
	request := BeltDraftRequest{Draft: base, Op: BeltDraftOperationRepositoryAdd, EntryID: new(TriggerEntryLegacyID), Repository: new("acme/three")}
	if _, err := BeltDraftEditingApply(request); err == nil {
		t.Fatal("a second repository was accepted")
	} else {
		_ = vtFailure(t, err) // asserts the error type; the failure value itself is not needed
	}
}

// TestARejectedStageProblemIsLocatedAtThatStage
func TestARejectedStageProblemIsLocatedAtThatStage(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationStageRemove, StageID: new("not-a-stage")}
	located := BeltDraftEditingLocated(request)
	if len(located) != 1 || located[0].Message != "stage-remove needs a stage id that is on the belt." {
		t.Fatalf("located %+v", located)
	}
	if located[0].Location == nil || !reflect.DeepEqual(*located[0].Location, ProblemLocationStage("not-a-stage")) {
		t.Fatalf("location %+v, want the stage", located[0].Location)
	}
	failure := vtFailure(t, vtErrorOf(BeltDraftEditingApply(request)))
	if !reflect.DeepEqual(failure.Problems, []string{"stage-remove needs a stage id that is on the belt."}) {
		t.Fatalf("failure problems %v", failure.Problems)
	}
	if len(failure.Locations) != 1 || failure.Locations[0] == nil || !reflect.DeepEqual(*failure.Locations[0], ProblemLocationStage("not-a-stage")) {
		t.Fatalf("failure locations %+v", failure.Locations)
	}
}

// errorOf is the error of a call that returns a belt and an error.
func vtErrorOf(_ Belt, err error) error { return err }

// TestGotoPickNamesAStageOnTheBelt
func TestGotoPickNamesAStageOnTheBelt(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationGotoPick, StageID: new("triage"), OutcomeID: new("go"), GotoTarget: new("nowhere")}
	if _, err := BeltDraftEditingApply(request); err == nil {
		t.Fatal("a goto to a stage not on the belt was accepted")
	} else {
		_ = vtFailure(t, err) // asserts the error type; the failure value itself is not needed
	}
}
