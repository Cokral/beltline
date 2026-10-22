// Tests of the fork flow through the engine, and of aborting in a fork.
// The layout and validation tests are in model_fork_test.go.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// nfStartWork is a run of belt for the subject acme/billing#142,
// "Work"), started.
func nfStartWork(belt Belt) Run {
	run := NewRun(RunArgs{Belt: belt, Subject: Subject{Kind: SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Work"}, Now: engineT0})
	EngineStart(&run, engineT0)
	return run
}

// nfRoutesOf is the run's routes as a map of one fork.
func nfRoutesOf(fork string, routes ...string) map[string][]string {
	return map[string][]string{fork: routes}
}

// nfLastRoute is the route label of the run's last attempt, or "" when there is none.
func nfLastRoute(run Run) string {
	if len(run.Attempts) == 0 || run.Attempts[len(run.Attempts)-1].Route == nil {
		return ""
	}
	return *run.Attempts[len(run.Attempts)-1].Route
}

// nfReasonIs reports whether the run's reason is exactly reason.
func nfReasonIs(run Run, reason string) bool {
	return run.Reason != nil && *run.Reason == reason
}

// TestABeltStartingWithAForkStartsEveryRouteAndJoins
func TestABeltStartingWithAForkStartsEveryRouteAndJoins(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages = belt.Stages[1:] // Put the existing fork at the start of the belt.
	run := nfStartWork(belt)
	if got := run.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "complex", "simple")) {
		t.Errorf("routes = %v, want size: complex, simple", got)
	}
	if !StageKeysTakenTogether(belt.Forks[0], "complex", "simple", belt.Layout()) {
		t.Errorf("complex and simple are not taken together")
	}
	stages := map[string]struct{}{}
	for _, leg := range run.Legs {
		if leg.StageID != nil {
			stages[*leg.StageID] = struct{}{}
		}
	}
	if !reflect.DeepEqual(stages, map[string]struct{}{"spec": {}, "quick-build": {}}) {
		t.Errorf("leg stages = %v, want spec and quick-build", stages)
	}
	nfFinishLeg(&run, "complete", "r:complex")
	nfFinishLeg(&run, "done", "r:complex")
	nfFinishLeg(&run, "done", "r:simple")
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
	if got := run.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "complex", "simple")) {
		t.Errorf("routes = %v, want size: complex, simple", got)
	}
}

// TestAStartingForkTakesAnEmptyRouteAndHandlesNestedLayouts
func TestAStartingForkTakesAnEmptyRouteAndHandlesNestedLayouts(t *testing.T) {
	t.Parallel()
	flat := nfForkedBelt(nil)
	flat.Stages = flat.Stages[1:]
	flatRun := nfStartWork(flat)
	if got := flatRun.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "complex", "simple")) {
		t.Errorf("routes = %v, want size: complex, simple", got)
	}
	if !nfLegHas(flatRun, "complex", "spec") {
		t.Errorf("legs = %+v, want complex at spec", flatRun.Legs)
	}
	if !nfLegHas(flatRun, "simple", "verify") {
		t.Errorf("legs = %+v, want simple at verify", flatRun.Legs)
	}

	nested := nfBelt(false, true, true)
	nested.Stages = nested.Stages[1:] // The outer fork is now first; Build contains another fork.
	nestedRun := nfStartWork(nested)
	if got := nestedRun.Routes()["wave"]; !slices.Equal(got, []string{"build", "skip"}) {
		t.Errorf("wave = %v, want build and skip", got)
	}
	if !nfLegHas(nestedRun, "build", "assess") {
		t.Errorf("legs = %+v, want build at assess", nestedRun.Legs)
	}
	finished := slices.ContainsFunc(nestedRun.Legs, func(leg Leg) bool {
		return leg.Route != nil && *leg.Route == "skip" && leg.State == RunStateDone
	})
	if !finished {
		t.Errorf("legs = %+v, want skip finished", nestedRun.Legs)
	}
	joined := slices.ContainsFunc(nestedRun.Joins, func(join Join) bool { return join.ForkID == "wave" })
	if !joined {
		t.Errorf("joins = %+v, want the wave join", nestedRun.Joins)
	}
}

// nfLegHas reports whether run has a leg on route at stage.
func nfLegHas(run Run, route, stage string) bool {
	return slices.ContainsFunc(run.Legs, func(leg Leg) bool {
		return leg.Route != nil && *leg.Route == route && nfStageOf(&leg) == stage
	})
}

// TestTheNextStageAfterTriageIsTheFirstRoute
func TestTheNextStageAfterTriageIsTheFirstRoute(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&run, "ready to spec")
	if run.StageID != "spec" {
		t.Errorf("stage = %q, want spec", run.StageID)
	}
	if got := run.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "complex")) {
		t.Errorf("routes = %v, want size: complex", got)
	}
	if got := nfLastRoute(run); got != "Next: Spec" {
		t.Errorf("last attempt route = %q, want Next: Spec", got)
	}
}

// TestAnOutcomeTakesAnotherRoute
func TestAnOutcomeTakesAnotherRoute(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&run, "ready to implement")
	if run.StageID != "quick-build" {
		t.Errorf("stage = %q, want quick-build", run.StageID)
	}
	if got := run.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "simple")) {
		t.Errorf("routes = %v, want size: simple", got)
	}
	if StageKeysTakenTogether(run.Forks[0], "complex", "simple", run.Layout()) {
		t.Errorf("complex and simple are taken together")
	}
	if got := nfLastRoute(run); got != "Take the Simple route" {
		t.Errorf("last attempt route = %q, want Take the Simple route", got)
	}
}

// TestAMultiRouteOutcomeSelectsEveryNamedRoute
func TestAMultiRouteOutcomeSelectsEveryNamedRoute(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages[0].Outcomes[0].Then = nfRoutesThen("complex", "simple")
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	if !slices.Equal(run.Routes()["size"], []string{"complex", "simple"}) {
		t.Errorf("size routes = %v, want complex and simple", run.Routes()["size"])
	}
	if !StageKeysTakenTogether(belt.Forks[0], "complex", "simple", belt.Layout()) {
		t.Errorf("complex and simple are not taken together")
	}
}

// TestEachRouteEndsWhereTheRoutesMeet
func TestEachRouteEndsWhereTheRoutesMeet(t *testing.T) {
	t.Parallel()
	complex := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&complex, "ready to spec")
	nfFinish(&complex, "complete")
	if complex.StageID != "build" {
		t.Errorf("stage = %q, want build", complex.StageID)
	}
	nfFinish(&complex, "done")
	if complex.StageID != "verify" {
		t.Errorf("stage = %q, want verify", complex.StageID)
	}
	if n := len(nfAttemptsAt(complex, "quick-build")); n != 0 {
		t.Errorf("quick-build attempts = %d, want 0", n)
	}

	simple := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&simple, "ready to implement")
	nfFinish(&simple, "done")
	if simple.StageID != "verify" {
		t.Errorf("stage = %q, want verify", simple.StageID)
	}
	var stages []string
	for _, attempt := range simple.Attempts {
		stages = append(stages, attempt.StageID)
	}
	if !slices.Equal(stages, []string{"triage", "quick-build"}) {
		t.Errorf("attempt stages = %v, want triage and quick-build", stages)
	}
}

// TestARouteWithNoStagesSkipsToWhereTheyMeet
func TestARouteWithNoStagesSkipsToWhereTheyMeet(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfForkedBelt(nil))
	nfFinish(&run, "ready to implement")
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
	if got := run.Routes(); !reflect.DeepEqual(got, nfRoutesOf("size", "simple")) {
		t.Errorf("routes = %v, want size: simple", got)
	}
	if got := nfLastRoute(run); got != "Take the Simple route" {
		t.Errorf("last attempt route = %q, want Take the Simple route", got)
	}
}

// TestGoingBackIntoTheForkLandsOnTheRouteTheRunTook
func TestGoingBackIntoTheForkLandsOnTheRouteTheRunTook(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	if got := belt.Stage("verify").Outcome("code mismatch").Then.To; !slices.Equal(got, []string{"quick-build", "build"}) {
		t.Errorf("code mismatch goes to %v, want quick-build and build", got)
	}
	for _, verdict := range []struct{ verdict, build string }{{"ready to spec", "build"}, {"ready to implement", "quick-build"}} {
		run := engineStartRun(belt)
		nfFinish(&run, verdict.verdict)
		if verdict.verdict == "ready to spec" {
			nfFinish(&run, "complete")
		}
		nfFinish(&run, "done")
		nfFinishBody(&run, "code mismatch", "Rule 2 fails.")
		if run.StageID != verdict.build {
			t.Errorf("%s: stage = %q, want %s", verdict.verdict, run.StageID, verdict.build)
		}
		if run.Note == nil || !strings.Contains(*run.Note, "Rule 2 fails.") {
			t.Errorf("%s: note = %v, want the output", verdict.verdict, run.Note)
		}
		if got := nfLastRoute(run); got != "Back to Build with the output" {
			t.Errorf("%s: last attempt route = %q", verdict.verdict, got)
		}
		nfFinish(&run, "done")
		if run.StageID != "verify" {
			t.Errorf("%s: stage = %q, want verify", verdict.verdict, run.StageID)
		}
	}
}

// TestAGotoWithNoStageOnTheRunsRouteHolds
func TestAGotoWithNoStageOnTheRunsRouteHolds(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages[4].Outcomes[1].Then.To = []string{"build"}
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	nfFinish(&run, "done")
	nfFinish(&run, "code mismatch")
	if run.State != RunStateHeld {
		t.Errorf("state = %q, want held", run.State)
	}
	if !nfReasonIs(run, "code mismatch: its stage isn't on this run's route") {
		t.Errorf("reason = %v", run.Reason)
	}
}

// TestAMissingRouteHolds
func TestAMissingRouteHolds(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages[0].Outcomes[0].Then.SetRoute(new("gone"))
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	if run.State != RunStateHeld {
		t.Errorf("state = %q, want held", run.State)
	}
	if !nfReasonIs(run, "ready to implement: its route is missing") {
		t.Errorf("reason = %v", run.Reason)
	}
}

// TestAnEditBeforeTheRoutesBuildTriagesAgainAndForgetsTheRoute
func TestAnEditBeforeTheRoutesBuildTriagesAgainAndForgetsTheRoute(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&run, "ready to implement")
	edited := run.Subject.Clone()
	edited.Body = "Something else."
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
	if len(run.Routes()) != 0 {
		t.Errorf("routes = %v, want none", run.Routes())
	}
}

// TestAnEditWhileTheRoutesBuildRunsChangesNothing
func TestAnEditWhileTheRoutesBuildRunsChangesNothing(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfForkedBelt([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("s", "f", nil), engineT0)
	edited := run.Subject.Clone()
	edited.Body = "Something else."
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.StageID != "quick-build" {
		t.Errorf("stage = %q, want quick-build", run.StageID)
	}
	if run.State != RunStateRunning {
		t.Errorf("state = %q, want running", run.State)
	}
}

// TestARunEndingAtTheEndOfARouteIsDone
func TestARunEndingAtTheEndOfARouteIsDone(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages = belt.Stages[:len(belt.Stages)-2] // verify and ship go
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	nfFinish(&run, "done")
	if run.State != RunStateDone {
		t.Errorf("state = %q, want done", run.State)
	}
}
