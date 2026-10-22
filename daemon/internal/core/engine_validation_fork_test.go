// Validation tests of forks and of the routes that end at a stage.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// vtAskBelt is triage, then a fork
// before Spec with an empty Main route and a Needs details route that feeds in
// (Spec, its first stage); then Spec, Build and Ship. Needs info goes back to Ask.
func vtAskBelt() Belt {
	fork := Fork{ID: "details", Routes: []Route{vtRoute("main", "Main"), {ID: "asking", Name: "Needs details", Feeds: true}}}
	var stages []Stage
	add := func(agent string, route *string, id string) {
		stage := DefaultsStageForAgentAt(vtAgent(agent), DefaultsPlace{Before: stages, Forks: []Fork{fork}, Route: route}, stages, TriggerKindIssues)
		if id != "" {
			stage.ID = id
		}
		stages = append(stages, stage)
	}
	add("triage", nil, "")
	add("spec", new("asking"), "ask")
	add("spec", nil, "")
	add("build", nil, "")
	add("ship", nil, "")
	for index, outcome := range stages[0].Outcomes {
		if outcome.Name == "needs info" {
			stages[0].Outcomes[index].Then = vtThenGoto("ask")
		}
		if outcome.Name == "ready to implement" || outcome.Name == "ready to spec" {
			stages[0].Outcomes[index].Then = nfThenOf(ThenActionNext)
		}
	}
	return NewBeltFromTrigger(BeltFromTriggerArgs{
		ID: "asking", Name: "Asking", Trigger: vtIssuesTrigger(), Repos: []string{"acme/billing"}, Stages: stages, Forks: []Fork{fork},
	})
}

// vtOutcomeNamed is the index of the outcome named name on stage, or -1.
func vtOutcomeNamed(stage Stage, name string) int {
	return slices.IndexFunc(stage.Outcomes, func(outcome Outcome) bool { return outcome.Name == name })
}

// TestTheForkedBeltHasNoWarnings
func TestTheForkedBeltHasNoWarnings(t *testing.T) {
	t.Parallel()
	if got := vtWarnings(vtForkedBelt([]string{"build"})); len(got) != 0 {
		t.Fatalf("forked belt warnings %v", got)
	}
	// A route with no stages that skips Spec.
	belt := vtDefaultBelt("a/b")
	belt.ForkAround("spec")
	belt.Stages[0].Outcomes[0].Then = vtThenRoute(belt.Forks[1].Routes[1].ID)
	if got := vtWarnings(belt); len(got) != 0 {
		t.Fatalf("skipping fork warnings %v", got)
	}
}

// TestAStartingForkDoesNotNeedAnOutcomeToTakeItsOtherRoutes
func TestAStartingForkDoesNotNeedAnOutcomeToTakeItsOtherRoutes(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages = belt.Stages[1:]
	if nfContains(vtWarnings(belt), "Nothing takes the Simple route") {
		t.Fatalf("unexpected warning in %v", vtWarnings(belt))
	}
}

// TestARouteWithNoStagesStillNeedsWhatComesAfterIt
func TestARouteWithNoStagesStillNeedsWhatComesAfterIt(t *testing.T) {
	t.Parallel()
	if !slices.Contains(vtWarnings(vtForkedBelt(nil)), "Verify needs code to read, which the Simple route doesn't make.") {
		t.Fatalf("missing input warning in %v", vtWarnings(vtForkedBelt(nil)))
	}
}

// TestEveryRouteButTheFirstNeedsAnOutcomeThatTakesIt
func TestEveryRouteButTheFirstNeedsAnOutcomeThatTakesIt(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages[0].Outcomes[0].Then = nfThenOf(ThenActionNext)
	if !slices.Contains(vtWarnings(belt), "Nothing takes the Simple route. Give the stage before the fork an outcome that takes it, or make it feed in.") {
		t.Fatalf("missing take warning in %v", vtWarnings(belt))
	}
}

// TestAStageAfterTheForkNeedsWhatEveryRouteMakes
func TestAStageAfterTheForkNeedsWhatEveryRouteMakes(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"spec"})
	if !slices.Contains(vtWarnings(belt), "Verify needs code to read, which the Simple route doesn't make.") {
		t.Fatalf("missing input warning in %v", vtWarnings(belt))
	}
}

// TestGoingBackIntoTheForkNamesAStageOnEveryRoute
func TestGoingBackIntoTheForkNamesAStageOnEveryRoute(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages[4].Outcomes[1].Then = vtThenGoto("build")
	if !slices.Contains(vtWarnings(belt), "Verify: “code mismatch” goes back into a fork, but names no stage on the Simple route.") {
		t.Fatalf("missing back-into-fork warning in %v", vtWarnings(belt))
	}
}

// TestAGotoStaysOnItsRoute
func TestAGotoStaysOnItsRoute(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages[2].Outcomes = append(belt.Stages[2].Outcomes, vtOutcome("over", "x", ToneNeutral, vtThenGoto("quick-build")))
	if !slices.Contains(vtWarnings(belt), "Build: “over” goes from the Complex route into another route.") {
		t.Fatalf("missing route-crossing warning in %v", vtWarnings(belt))
	}
}

// TestARouteIsTakenRightBeforeItsFork
func TestARouteIsTakenRightBeforeItsFork(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages[4].Outcomes = append(belt.Stages[4].Outcomes, vtOutcome("again", "x", ToneNeutral, vtThenRoute("simple")))
	if !slices.Contains(vtWarnings(belt), "Verify: “again” takes a route of a fork that doesn't come right after Verify.") {
		t.Fatalf("missing fork-position warning in %v", vtWarnings(belt))
	}
}

// TestAForksStagesSitTogether
func TestAForksStagesSitTogether(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"build"})
	belt.Stages[2], belt.Stages[3] = belt.Stages[3], belt.Stages[2]
	if !slices.Contains(vtWarnings(belt), "The stages of the Complex and Simple routes are out of order. Keep each route's stages together.") {
		t.Fatalf("missing order warning in %v", vtWarnings(belt))
	}
}

// TestAForkAroundAStageAddsARouteThatSkipsIt checks the validation of a fork around a stage.
// The editing checks are in the model tests.
func TestAForkAroundAStageAddsARouteThatSkipsIt(t *testing.T) {
	t.Parallel()
	belt := vtDefaultBelt("a/b")
	belt.ForkAround("spec")
	if len(belt.Forks) != 2 {
		t.Fatalf("forks %d, want 2", len(belt.Forks))
	}
	var names []string
	for _, route := range belt.Forks[1].Routes {
		names = append(names, route.Name)
	}
	if !reflect.DeepEqual(names, []string{"Route 1", "Route 2"}) {
		t.Fatalf("route names %v, want [Route 1 Route 2]", names)
	}
	spec := belt.Stage("spec")
	if spec == nil || spec.Route == nil || *spec.Route != belt.Forks[1].Routes[0].ID {
		t.Fatalf("spec is on route %v, want the first route %q", spec, belt.Forks[1].Routes[0].ID)
	}
	if !slices.Contains(vtWarnings(belt), "Nothing takes the Route 2 route. Give the stage before the fork an outcome that takes it, or make it feed in.") {
		t.Fatalf("missing take warning in %v", vtWarnings(belt))
	}
}

// TestARouteThatFeedsInValidatesEvenWhenNothingGoesThere
func TestARouteThatFeedsInValidatesEvenWhenNothingGoesThere(t *testing.T) {
	t.Parallel()
	if vtAny(vtWarnings(vtAskBelt()), func(w string) bool { return strings.Contains(w, "route") }) {
		t.Fatalf("unexpected route warning in %v", vtWarnings(vtAskBelt()))
	}
	belt := vtAskBelt()
	needs := vtOutcomeNamed(belt.Stages[0], "needs info")
	belt.Stages[0].Outcomes[needs].Then = nfThenOf(ThenActionHold)
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "route") }) {
		t.Fatalf("unexpected route warning in %v", vtWarnings(belt))
	}
}

// TestTakingARouteThatFeedsInWarns
func TestTakingARouteThatFeedsInWarns(t *testing.T) {
	t.Parallel()
	belt := vtAskBelt()
	needs := vtOutcomeNamed(belt.Stages[0], "needs info")
	belt.Stages[0].Outcomes[needs].Then = vtThenRoute("asking")
	if !slices.Contains(vtWarnings(belt), "Triage: “needs info” takes the Needs details route, which feeds in. Go to one of its stages instead.") {
		t.Fatalf("missing feed-in warning in %v", vtWarnings(belt))
	}
}

// TestAForksFirstRouteCannotFeedIn
func TestAForksFirstRouteCannotFeedIn(t *testing.T) {
	t.Parallel()
	belt := vtAskBelt()
	belt.Forks[0].Routes[0].Feeds = true
	if !slices.Contains(vtWarnings(belt), "The Main route is a fork's first route, so it can't feed in.") {
		t.Fatalf("missing first-route warning in %v", vtWarnings(belt))
	}
}

// TestAGotoBackIntoAForkNeverAsksForARouteThatFeedsIn
func TestAGotoBackIntoAForkNeverAsksForARouteThatFeedsIn(t *testing.T) {
	t.Parallel()
	belt := vtAskBelt()
	belt.Forks[0].Routes = append(belt.Forks[0].Routes[:1:1], append([]Route{vtRoute("other", "Other")}, belt.Forks[0].Routes[1:]...)...)
	quick := DefaultsStageForAgentAt(vtAgent("build"), DefaultsPlace{Before: []Stage{belt.Stages[0]}, Forks: belt.Forks, Route: new("other")}, belt.Stages, TriggerKindIssues)
	quick.ID = "quick"
	belt.Stages = append(belt.Stages[:1:1], append([]Stage{quick}, belt.Stages[1:]...)...)
	if got := belt.Layout().Counterparts("quick", "ship"); len(got) != 0 {
		t.Fatalf("counterparts %v, want none", got)
	}
	ship := vtStageIndexByID(belt.Stages, "ship")
	failed := vtOutcomeNamed(belt.Stages[ship], "failed")
	belt.Stages[ship].Outcomes[failed].Then = vtThenGoto("ask")
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "goes back into a fork") }) {
		t.Fatalf("unexpected back-into-fork warning in %v", vtWarnings(belt))
	}
	belt.Stages[ship].Outcomes[failed].Then = vtThenGoto("quick")
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "names no stage on") }) {
		t.Fatalf("unexpected names-no-stage warning in %v", vtWarnings(belt))
	}
}

// TestARouteThatDoesNotJoinHandsNothingOn
func TestARouteThatDoesNotJoinHandsNothingOn(t *testing.T) {
	t.Parallel()
	belt := vtForkedBelt([]string{"triage"})
	if !vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "which the Simple route doesn't make") }) {
		t.Fatalf("missing input warning in %v", vtWarnings(belt))
	}
	quick := vtStageIndexByID(belt.Stages, "quick-triage")
	for index := range belt.Stages[quick].Outcomes {
		if belt.Stages[quick].Outcomes[index].Then.Action.MovesOn() {
			belt.Stages[quick].Outcomes[index].Then = vtThenGoto("triage")
		}
	}
	if belt.Layout().Joins("simple") {
		t.Fatal("simple route joins")
	}
	if vtAny(vtWarnings(belt), func(w string) bool { return strings.Contains(w, "which the Simple route doesn't make") }) {
		t.Fatalf("unexpected input warning in %v", vtWarnings(belt))
	}
}

// vtStageIndexByID is the index of the stage with id, or -1.
func vtStageIndexByID(stages []Stage, id string) int {
	return slices.IndexFunc(stages, func(stage Stage) bool { return stage.ID == id })
}
