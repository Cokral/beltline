// Validation tests of nested forks: a stale goto warns and is not removed, and
// a sibling route inside an active fork is refused with a warning.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// vtNestedBelt is triage, then an
// outer fork (Build / Skip). Build holds Assess, an inner fork (Quick / Thorough)
// and Verify. Ship follows the outer fork. Skip is empty unless skipStage.
func vtNestedBelt(skipStage, verify, quick bool) Belt {
	outer := Fork{ID: "wave", Routes: []Route{vtRoute("build", "Build"), vtRoute("skip", "Skip")}}
	inner := Fork{ID: "pace", Routes: []Route{vtRoute("quick", "Quick"), vtRoute("thorough", "Thorough")}, Parent: new("build")}
	stage := func(id, agent string, route *string, outcome Outcome) Stage {
		s := DefaultsStageForAgentAt(vtAgent(agent), DefaultsPlace{Route: route}, nil, TriggerKindIssues)
		s.ID = id
		switch id {
		case "quick-build", "thorough-build", "skip-build":
			s.Name = "Build"
		case "thorough-spec":
			s.Name = "Spec"
		case "assess":
			s.Name = "Assess"
		default:
			s.Name = strings.ToUpper(id[:1]) + id[1:]
		}
		s.Route = route
		s.Outcomes = []Outcome{outcome}
		return s
	}
	goOutcome := vtOutcome("go", "go", ToneNeutral, nfThenOf(ThenActionNext))
	doneOutcome := vtOutcome("done", "done", ToneNeutral, nfThenOf(ThenActionNext))
	stages := []Stage{
		stage("triage", "triage", nil, goOutcome),
		stage("assess", "spec", new("build"), goOutcome),
	}
	if quick {
		stages = append(stages, stage("quick-build", "build", new("quick"), doneOutcome))
	}
	stages = append(stages,
		stage("thorough-spec", "spec", new("thorough"), doneOutcome),
		stage("thorough-build", "build", new("thorough"), doneOutcome),
	)
	if verify {
		stages = append(stages, stage("verify", "verify", new("build"), doneOutcome))
	}
	if skipStage {
		stages = append(stages, stage("skip-build", "build", new("skip"), doneOutcome))
	}
	shipOutcome := vtOutcome("opened", "opened", ToneNeutral, nfThenOf(ThenActionDone))
	stages = append(stages, stage("ship", "ship", nil, shipOutcome))
	return NewBeltFromTrigger(BeltFromTriggerArgs{
		ID: "nested", Name: "Nested", Trigger: vtIssuesTrigger(), Repos: []string{"acme/billing"}, Stages: stages, Forks: []Fork{outer, inner},
	})
}

// TestAStaleGotoWarnsAndIsNotRemoved
func TestAStaleGotoWarnsAndIsNotRemoved(t *testing.T) {
	t.Parallel()
	belt := vtNestedBelt(false, true, true)
	belt.Stages[1].Outcomes = append(belt.Stages[1].Outcomes, vtOutcome("out", "out", ToneNeutral, vtThenGoto("ship")))
	belt.Remove("ship")
	if vtOutcomeNamed(belt.Stages[1], "out") < 0 {
		t.Fatal("the stale goto was removed")
	}
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "goes to a stage that isn't on this belt anymore") }) {
		t.Fatalf("missing stale goto warning in %v", vtWarnings(belt))
	}
}

// TestTheRouteEditorExposesStaleSelectionsWithoutChangingValidChoices
func TestTheRouteEditorExposesStaleSelectionsWithoutChangingValidChoices(t *testing.T) {
	t.Parallel()
	belt := vtNestedBelt(false, true, true)
	selected := []string{"build", "quick", "skip", "thorough", "deleted"}
	if got := belt.Layout().UnavailableRoutes(selected, "assess"); !reflect.DeepEqual(got, []string{"build", "skip", "deleted"}) {
		t.Fatalf("unavailable after assess %v", got)
	}
	if got := belt.Layout().UnavailableRoutes([]string{"build", "skip"}, "triage"); len(got) != 0 {
		t.Fatalf("unavailable after triage %v, want none", got)
	}
	if got := belt.Layout().UnavailableRoutes(selected, "ship"); !reflect.DeepEqual(got, selected) {
		t.Fatalf("unavailable after ship %v, want %v", got, selected)
	}

	belt.Stages[1].Outcomes[0].Then = nfRoutesThen("build", "quick")
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "doesn't come right after Assess") }) {
		t.Fatalf("missing position warning in %v", vtWarnings(belt))
	}
	stale := belt.Layout().UnavailableRoutes(belt.Stages[1].Outcomes[0].Then.Routes, "assess")
	var kept []string
	for _, route := range belt.Stages[1].Outcomes[0].Then.Routes {
		if !slices.Contains(stale, route) {
			kept = append(kept, route)
		}
	}
	belt.Stages[1].Outcomes[0].Then.Routes = kept
	if !reflect.DeepEqual(belt.Stages[1].Outcomes[0].Then.Routes, []string{"quick"}) {
		t.Fatalf("routes %v, want [quick]", belt.Stages[1].Outcomes[0].Then.Routes)
	}
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "doesn't come right after Assess") }) {
		t.Fatalf("unexpected position warning in %v", vtWarnings(belt))
	}
}

// TestLeavingANestedRouteForAStageOutsideIsAllowed
func TestLeavingANestedRouteForAStageOutsideIsAllowed(t *testing.T) {
	t.Parallel()
	belt := vtNestedBelt(false, true, true)
	quick := vtStageIndexByID(belt.Stages, "quick-build")
	belt.Stages[quick].Outcomes = append(belt.Stages[quick].Outcomes, vtOutcome("ship-it", "ship-it", ToneNeutral, vtThenGoto("ship")))
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "ship-it") }) {
		t.Fatalf("unexpected warning in %v", vtWarnings(belt))
	}
}

// TestMalformedNestingWarns
func TestMalformedNestingWarns(t *testing.T) {
	t.Parallel()
	belt := vtNestedBelt(false, true, true)
	belt.Forks[1].Parent = new("missing")
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "isn't on this belt") }) {
		t.Fatalf("missing parent warning in %v", vtWarnings(belt))
	}

	belt = vtNestedBelt(false, true, true)
	belt.Forks[0].Parent = new("quick")
	belt.Forks[1].Parent = new("build")
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "circle") }) {
		t.Fatalf("missing circle warning in %v", vtWarnings(belt))
	}

	belt = vtNestedBelt(false, true, true)
	belt.Forks = append(belt.Forks, belt.Forks[0])
	if !slices.Contains(vtWarnings(belt), "Two forks have the same id.") {
		t.Fatalf("missing duplicate fork warning in %v", vtWarnings(belt))
	}

	belt = vtNestedBelt(false, true, true)
	belt.Stages[2].Route = new("gone")
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "isn't on this belt anymore") }) {
		t.Fatalf("missing route warning in %v", vtWarnings(belt))
	}

	belt = vtNestedBelt(false, true, true)
	belt.Stages[2], belt.Stages[5] = belt.Stages[5], belt.Stages[2]
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "out of order") }) {
		t.Fatalf("missing order warning in %v", vtWarnings(belt))
	}

	belt = vtNestedBelt(false, true, true)
	belt.Stages[0].Outcomes = []Outcome{vtOutcome("early", "early", ToneNeutral, vtThenRoute("quick"))}
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "doesn't come right after") }) {
		t.Fatalf("missing position warning in %v", vtWarnings(belt))
	}
}

// TestInputsUseOnlyWhatEveryRouteGuarantees
func TestInputsUseOnlyWhatEveryRouteGuarantees(t *testing.T) {
	t.Parallel()
	belt := vtNestedBelt(false, true, false)
	belt.Stages[0].Outcomes[0].Then = nfRoutesThen("build", "skip")
	warnings := vtWarnings(belt)
	if !vtAny(warnings, func(w string) bool {
		return strings.Contains(w, "Verify") && strings.Contains(w, "code to read") && strings.Contains(w, "Quick")
	}) {
		t.Fatalf("missing Quick input warning in %v", warnings)
	}
	if vtAny(warnings, func(w string) bool {
		return strings.Contains(w, "Thorough") && strings.Contains(w, "doesn't make") && strings.Contains(w, "Verify")
	}) {
		t.Fatalf("unexpected Thorough warning in %v", warnings)
	}
}
