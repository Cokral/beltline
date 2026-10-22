// Tests of the belt structure, including the two structural refusals:
// a duplicate stage id only warns, and a stage without an id is refused.

package core

import (
	"reflect"
	"testing"
)

// structureBelt is a belt with the given stages, forks and triggers: one
// manual entry "1" on acme/app unless triggers are given.
func structureBelt(stages []Stage, forks []Fork, triggers []TriggerEntry) Belt {
	if triggers == nil {
		triggers = []TriggerEntry{{ID: "1", Trigger: btManual(), Repos: []string{"acme/app"}}}
	}
	b := Belt{ID: "b", Name: "B", Version: 1, Triggers: triggers, Policies: DefaultPolicies(), Stages: stages, Forks: forks, Afk: AfkModeGlobal}
	return b
}

// structureProblemAt is the location of a problem, failing the test when it has none.
func structureProblemAt(t *testing.T, p LocatedProblem) ProblemLocation {
	t.Helper()
	if p.Location == nil {
		t.Fatalf("problem %q has no location", p.Message)
	}
	return *p.Location
}

// structureMessages lists the messages of problems in order.
func structureMessages(problems []LocatedProblem) []string {
	out := make([]string, len(problems))
	for i, p := range problems {
		out[i] = p.Message
	}
	return out
}

// TestAWellFormedBeltHasNoProblems
func TestAWellFormedBeltHasNoProblems(t *testing.T) {
	t.Parallel()
	stages := []Stage{
		draftTestStage("triage", StageTypeAgent, "", "", draftTestOutcome("go", Then{Action: ThenActionGoto, To: []string{"verify"}})),
		draftTestStage("build", StageTypeAgent, "", "simple", draftTestOutcome("ok", Then{Action: ThenActionNext})),
		draftTestStage("verify", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
	}
	forks := []Fork{{ID: "size", Routes: []Route{{ID: "simple", Name: "Simple"}}}}
	if got := BeltStructureLocated(structureBelt(stages, forks, nil)); len(got) != 0 {
		t.Fatalf("problems %v, want none", structureMessages(got))
	}
}

// TestADuplicateStageIdIsLocatedAtThatStage
func TestADuplicateStageIdIsLocatedAtThatStage(t *testing.T) {
	t.Parallel()
	stages := []Stage{
		draftTestStage("a", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
		draftTestStage("a", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
	}
	problems := BeltStructureLocated(structureBelt(stages, nil, nil))
	want := []string{"Stage id a is used by more than one stage."}
	if got := structureMessages(problems); !reflect.DeepEqual(got, want) {
		t.Fatalf("messages %v, want %v", got, want)
	}
	if got := structureProblemAt(t, problems[0]); !reflect.DeepEqual(got, ProblemLocationStage("a")) {
		t.Fatalf("location %+v, want stage a", got)
	}
}

// TestAStageOnAMissingRouteAndAGotoToAMissingStageAreLocatedAtTheirFields
func TestAStageOnAMissingRouteAndAGotoToAMissingStageAreLocatedAtTheirFields(t *testing.T) {
	t.Parallel()
	stages := []Stage{
		draftTestStage("a", StageTypeAgent, "", "gone",
			draftTestOutcome("jump", Then{Action: ThenActionGoto, To: []string{"nowhere"}})),
	}
	problems := BeltStructureLocated(structureBelt(stages, nil, nil))
	if len(problems) != 2 {
		t.Fatalf("problems %v, want 2", structureMessages(problems))
	}
	if got, want := structureProblemAt(t, problems[0]), (ProblemLocation{Kind: "stage", ID: new("a"), Field: new("route")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("route location %+v, want %+v", got, want)
	}
	if got, want := structureProblemAt(t, problems[1]), (ProblemLocation{Kind: "outcome", ID: new("a"), Field: new("jump")}); !reflect.DeepEqual(got, want) {
		t.Fatalf("goto location %+v, want %+v", got, want)
	}
}

// TestARepeatedRepositoryIsLocatedAtTheTriggerRepository
func TestARepeatedRepositoryIsLocatedAtTheTriggerRepository(t *testing.T) {
	t.Parallel()
	stages := []Stage{draftTestStage("a", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext}))}
	entries := []TriggerEntry{{ID: "1", Trigger: btManual(), Repos: []string{"acme/app", "acme/app"}}}
	problems := BeltStructureLocated(structureBelt(stages, nil, entries))
	if len(problems) == 0 {
		t.Fatal("no problems, want a repeated repository")
	}
	if got := structureProblemAt(t, problems[0]); !reflect.DeepEqual(got, ProblemLocationRepository("1", "acme/app")) {
		t.Fatalf("location %+v, want the repository", got)
	}
}

// TestARepeatedTriggerIdIsLocatedAtTheTrigger
func TestARepeatedTriggerIdIsLocatedAtTheTrigger(t *testing.T) {
	t.Parallel()
	stages := []Stage{draftTestStage("a", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext}))}
	entries := []TriggerEntry{{ID: "1", Trigger: btManual()}, {ID: "1", Trigger: btManual()}}
	problems := BeltStructureLocated(structureBelt(stages, nil, entries))
	if len(problems) == 0 {
		t.Fatal("no problems, want a repeated trigger id")
	}
	if got := structureProblemAt(t, problems[0]); !reflect.DeepEqual(got, ProblemLocationTrigger("1")) {
		t.Fatalf("location %+v, want the trigger", got)
	}
}

// TestAStructureWithDuplicateStageIdsWarnsButCanBeWritten
func TestAStructureWithDuplicateStageIdsWarnsButCanBeWritten(t *testing.T) {
	t.Parallel()
	draft := structureParityBelt()
	draft.Stages = append(draft.Stages, draftTestStage("verify", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})))
	if got := BeltStructureRefusals(draft); len(got) != 0 {
		t.Fatalf("refusals %v, want none", structureMessages(got))
	}
}

// TestAStageWithoutAnIdIsTheOnlyStructuralRefusal
func TestAStageWithoutAnIdIsTheOnlyStructuralRefusal(t *testing.T) {
	t.Parallel()
	draft := structureParityBelt()
	draft.Stages[1].ID = " "
	if got := BeltStructureRefusals(draft); len(got) != 1 {
		t.Fatalf("refusals %v, want exactly one", structureMessages(got))
	}
}

// structureParityBelt is the draft BeltDraftParityTests.belt() builds: a triage
// stage that goes to verify, then build and verify.
func structureParityBelt() Belt {
	return draftTestBelt([]Stage{
		draftTestStage("triage", StageTypeAgent, "", "", draftTestOutcome("go", Then{Action: ThenActionGoto, To: []string{"verify"}})),
		draftTestStage("build", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
		draftTestStage("verify", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
	})
}
