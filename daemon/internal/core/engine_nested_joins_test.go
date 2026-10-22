// Engine tests of nested joins.

package core

import (
	"slices"
	"testing"
)

// nfTwoSided is the nested belt without its skip stage, with Triage
// taking Build and Skip, Assess taking Quick and Thorough, and Skip's Look taking
// One and Two.
func nfTwoSided(t *testing.T) Belt {
	t.Helper()
	belt := nfBelt(true, true, true)
	split := func(routes ...string) []Outcome {
		return []Outcome{nfOutcome("split", nfRoutesThen(routes...))}
	}
	belt.Stages[0].Outcomes = split("build", "skip")
	assess := nfStageIndex(t, belt, "assess")
	belt.Stages[assess].Outcomes = split("quick", "thorough")
	at := nfStageIndex(t, belt, "skip-build")
	belt.Stages[at].Route = new("one")
	look := belt.Stages[assess]
	look.ID = "look"
	look.Name = "Look"
	look.Route = new("skip")
	look.Outcomes = split("one", "two")
	two := belt.Stages[at]
	two.ID = "two-build"
	two.Route = new("two")
	nfInsertStage(&belt, at+1, two)
	nfInsertStage(&belt, at, look)
	belt.Forks = append(belt.Forks, Fork{ID: "later", Parent: new("skip"), Routes: []Route{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}})
	return belt
}

// nfIsolate gives every active leg a branch of its own, and the run a branch to
// merge into.
func nfIsolate(run *Run) {
	run.Branch = new("run-branch")
	for i := range run.Legs {
		if run.Legs[i].Branch != nil {
			continue
		}
		slug := run.Legs[i].ID
		if run.Legs[i].Route != nil {
			slug = *run.Legs[i].Route
		}
		if slug == "main" {
			run.Legs[i].Branch = new("run-branch")
		} else {
			run.Legs[i].Branch = new("run-branch-" + slug)
		}
	}
}

// nfFourWays splits a two-sided run into its four
// inner routes, each on a branch of its own.
func nfFourWays(t *testing.T, belt Belt) Run {
	t.Helper()
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	nfIsolate(&run)
	nfFinishLeg(&run, "split", "r:build")
	nfFinishLeg(&run, "split", "r:skip")
	nfIsolate(&run)
	return run
}

// nfSameSet reports whether list holds exactly the values in want, each once.
func nfSameSet(list []string, want ...string) bool {
	set := map[string]struct{}{}
	for _, s := range list {
		set[s] = struct{}{}
	}
	if len(set) != len(want) {
		return false
	}
	for _, s := range want {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// nfHasMergeLegs reports whether effects holds a mergeLegs effect of exactly legs.
func nfHasMergeLegs(effects []Effect, legs ...string) bool {
	return slices.ContainsFunc(effects, func(effect Effect) bool {
		return effect.Kind == EffectKindMergeLegs && slices.Equal(effect.Legs, legs)
	})
}

// TestAJoinWithNoBranchToMergeIntoCarriesOnOnTheRunsBranch
func TestAJoinWithNoBranchToMergeIntoCarriesOnOnTheRunsBranch(t *testing.T) {
	t.Parallel()
	belt := nfBelt(true, true, true)
	belt.Stages[0].Outcomes = []Outcome{nfOutcome("split", nfRoutesThen("build", "skip"))}
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	for i := range run.Legs {
		run.Legs[i].Branch = new("b-" + *run.Legs[i].Route)
	}
	nfFinishLeg(&run, "go", "r:build")
	nfFinishLeg(&run, "done", "r:build")
	nfFinishLeg(&run, "done", "r:build")
	nfFinishLeg(&run, "done", "r:skip")
	var shipping *Leg
	for i := range run.Legs {
		if nfStageOf(&run.Legs[i]) == "ship" {
			shipping = &run.Legs[i]
			break
		}
	}
	if shipping == nil || shipping.State != RunStateQueued {
		t.Fatalf("shipping leg = %+v, want queued", shipping)
	}
	// Nil is the run's own branch, the one the Coordinator merges into.
	if shipping.Branch != nil {
		t.Errorf("shipping branch = %v, want nil", *shipping.Branch)
	}
	if run.MergeInto != nil {
		t.Errorf("merge into = %v, want nil", *run.MergeInto)
	}
	if !nfSameSet(run.MergeBranches, "b-build", "b-skip") {
		t.Errorf("merge branches = %v, want b-build and b-skip", run.MergeBranches)
	}
}

// TestTwoJoinsMergeOneAfterTheOther
func TestTwoJoinsMergeOneAfterTheOther(t *testing.T) {
	t.Parallel()
	run := nfFourWays(t, nfTwoSided(t))
	nfFinishLeg(&run, "done", "r:quick")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	if !nfIsString(run.MergeInto, "run-branch-build") {
		t.Errorf("merge into = %v, want run-branch-build", run.MergeInto)
	}
	if !nfIsString(run.MergeLeg, "r:build") {
		t.Errorf("merge leg = %v, want r:build", run.MergeLeg)
	}
	nfFinishLeg(&run, "done", "r:one")
	nfFinishLeg(&run, "done", "r:two")
	// The second join waits for the first merge instead of replacing it.
	if !nfIsString(run.MergeInto, "run-branch-build") {
		t.Errorf("merge into = %v, want run-branch-build", run.MergeInto)
	}
	if !nfIsString(run.MergeLeg, "r:build") {
		t.Errorf("merge leg = %v, want r:build", run.MergeLeg)
	}
	if !nfSameSet(run.MergeBranches, "run-branch-quick", "run-branch-thorough") {
		t.Errorf("merge branches = %v, want run-branch-quick and run-branch-thorough", run.MergeBranches)
	}
	if len(run.MergesWaiting) != 1 || !nfIsString(run.MergesWaiting[0].Leg, "r:skip") {
		t.Errorf("merges waiting = %+v, want one for r:skip", run.MergesWaiting)
	}
	next := EngineAdvance(&run, Event{Kind: EventKindMergedLegs, Legs: run.MergeBranches}, engineT0)
	if !nfHasMergeLegs(next, "run-branch-one", "run-branch-two") {
		t.Errorf("effects = %+v, want mergeLegs one and two", next)
	}
	if !nfIsString(run.MergeInto, "run-branch-skip") {
		t.Errorf("merge into = %v, want run-branch-skip", run.MergeInto)
	}
	if !nfIsString(run.MergeLeg, "r:skip") {
		t.Errorf("merge leg = %v, want r:skip", run.MergeLeg)
	}
	if len(run.MergesWaiting) != 0 {
		t.Errorf("merges waiting = %+v, want none", run.MergesWaiting)
	}
	EngineAdvance(&run, Event{Kind: EventKindMergedLegs, Legs: run.MergeBranches}, engineT0)
	if len(run.MergeBranches) != 0 {
		t.Errorf("merge branches = %v, want none", run.MergeBranches)
	}
	if run.MergeLeg != nil {
		t.Errorf("merge leg = %v, want nil", *run.MergeLeg)
	}
}

// TestAConflictStillStartsTheNextMerge
func TestAConflictStillStartsTheNextMerge(t *testing.T) {
	t.Parallel()
	run := nfFourWays(t, nfTwoSided(t))
	for _, leg := range []string{"r:quick", "r:thorough", "r:thorough", "r:one", "r:two"} {
		nfFinishLeg(&run, "done", leg)
	}
	next := EngineAdvance(&run, Event{Kind: EventKindMergeConflict, Files: []string{"a.swift"}}, engineT0)
	if state := nfStateOf(nfLegByID(run, "r:build")); state != RunStateHeld {
		t.Errorf("r:build state = %q, want held", state)
	}
	if !nfHasMergeLegs(next, "run-branch-one", "run-branch-two") {
		t.Errorf("effects = %+v, want mergeLegs one and two", next)
	}
	if !nfIsString(run.MergeLeg, "r:skip") {
		t.Errorf("merge leg = %v, want r:skip", run.MergeLeg)
	}
}

// TestEditingTheIssueForgetsTheOpenJoins
func TestEditingTheIssueForgetsTheOpenJoins(t *testing.T) {
	t.Parallel()
	belt := nfBelt(false, true, true)
	nfSetOutcomes(t, &belt, "assess", nfOutcome("split", nfRoutesThen("quick", "thorough")))
	run := engineStartRun(belt)
	nfFinish(&run, "go")
	nfFinish(&run, "split")
	if len(run.Joins) == 0 {
		t.Fatalf("joins = empty, want the pace join")
	}
	edited := run.Subject.Clone()
	edited.Body += " And a PDF."
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
	if len(run.Joins) != 0 {
		t.Errorf("joins = %+v, want none", run.Joins)
	}
	nfFinish(&run, "go")
	nfFinish(&run, "split")
	nfFinishLeg(&run, "done", "r:quick")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	queued := 0
	for _, leg := range nfLegsAt(run, "verify") {
		if leg.State == RunStateQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Errorf("queued verify legs = %d, want 1", queued)
	}
	for _, join := range run.Joins {
		if !join.Joined {
			t.Errorf("join %q not joined", join.ID)
		}
	}
}

// TestEveryInnerSplitJumpingOutStillLeavesTheOuterSplit
func TestEveryInnerSplitJumpingOutStillLeavesTheOuterSplit(t *testing.T) {
	t.Parallel()
	belt := nfTwoSided(t)
	out := nfOutcome("ship-it", nfGotoThen("ship"))
	for _, id := range []string{"quick-build", "thorough-spec", "skip-build", "two-build"} {
		nfSetOutcomes(t, &belt, id, out)
	}
	run := nfFourWays(t, belt)
	for _, leg := range []string{"r:quick", "r:thorough", "r:one", "r:two"} {
		nfFinishLeg(&run, "ship-it", leg)
	}
	for _, join := range run.Joins {
		if !join.Joined {
			t.Errorf("join %q not joined", join.ID)
		}
	}
	for _, leg := range run.Legs {
		if leg.Depart != nil {
			t.Errorf("leg %q departs to %q, want none", leg.ID, *leg.Depart)
		}
	}
	if len(run.Legs) == 0 {
		t.Fatalf("legs = empty")
	}
	for _, leg := range run.Legs {
		if nfStageOf(&leg) != "ship" || leg.State != RunStateQueued {
			t.Errorf("leg %q at %q in %q, want ship queued", leg.ID, nfStageOf(&leg), leg.State)
		}
	}
}

// TestResumingAtAStageOutsideTheSplitWaitsAndGoesThere
func TestResumingAtAStageOutsideTheSplitWaitsAndGoesThere(t *testing.T) {
	t.Parallel()
	_, run := nfJumped(t)
	EngineAdvance(&run, nfStarted("q", "f", new("r:quick")), engineT0)
	EngineAdvance(&run, Event{Kind: EventKindAbort, Leg: new("r:quick")}, engineT0)
	if state := nfStateOf(nfLegByID(run, "r:quick")); state != RunStateHeld {
		t.Fatalf("r:quick state = %q, want held", state)
	}
	EngineAdvance(&run, Event{Kind: EventKindResume, Stage: new("ship"), Note: new("go on"), Earlier: false, Leg: new("r:quick")}, engineT0)
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("r:quick depart = %v, want ship", d)
	}
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:skip")
	if n := len(nfLegsAt(run, "verify")); n != 0 {
		t.Errorf("verify legs = %d, want 0", n)
	}
	quick := nfLegByID(run, "r:quick")
	if got := nfStageOf(quick); got != "ship" {
		t.Errorf("r:quick at %q, want ship", got)
	}
	if quick.State != RunStateQueued {
		t.Errorf("r:quick state = %q, want queued", quick.State)
	}
	if quick.Note == nil || *quick.Note != "go on" {
		t.Errorf("r:quick note = %v, want go on", quick.Note)
	}
}

// TestAGotoOnANestedBeltReplaysTheEarlierSession
func TestAGotoOnANestedBeltReplaysTheEarlierSession(t *testing.T) {
	t.Parallel()
	belt := nfBelt(false, true, true)
	verify := nfStageIndex(t, belt, "verify")
	belt.Stages[verify].Outcomes = append(belt.Stages[verify].Outcomes, nfOutcome("again", nfGotoThen("assess")))
	run := engineStartRun(belt)
	nfFinish(&run, "go")
	nfFinish(&run, "go")
	nfFinish(&run, "done")
	nfFinish(&run, "again")
	if run.StageID != "assess" {
		t.Errorf("stage = %q, want assess", run.StageID)
	}
	if run.Replays == nil {
		t.Fatalf("replays = nil, want the earlier assess attempt")
	}
	var last *Attempt
	for i := range run.Attempts {
		if run.Attempts[i].StageID == "assess" {
			last = &run.Attempts[i]
		}
	}
	if last == nil || *run.Replays != last.ID {
		t.Errorf("replays = %v, want the last assess attempt", *run.Replays)
	}
}

// TestARouteAtTheEndOfARouteSeesTheForkAfterTheRoutesMeet
func TestARouteAtTheEndOfARouteSeesTheForkAfterTheRoutesMeet(t *testing.T) {
	t.Parallel()
	belt := nfBelt(false, false, true)
	belt.Forks = append(belt.Forks, Fork{ID: "after", Routes: []Route{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}})
	ship := nfStageIndex(t, belt, "ship")
	polish := belt.Stages[ship]
	polish.ID = "polish"
	polish.Name = "Polish"
	polish.Route = new("a")
	polish.Outcomes = []Outcome{nfOutcome("done", nfThenOf(ThenActionNext))}
	nfInsertStage(&belt, ship, polish)
	layout := belt.Layout()
	if fork := layout.ForkAfter("thorough-build"); fork == nil || fork.ID != "after" {
		t.Errorf("fork after thorough-build = %+v, want after", fork)
	}
	if fork := layout.ForkAfter("quick-build"); fork == nil || fork.ID != "after" {
		t.Errorf("fork after quick-build = %+v, want after", fork)
	}
	if fork := layout.ForkAfter("thorough-spec"); fork != nil {
		t.Errorf("fork after thorough-spec = %+v, want none", fork)
	}
	if fork := layout.ForkAfter("triage"); fork == nil || fork.ID != "wave" {
		t.Errorf("fork after triage = %+v, want wave", fork)
	}
	if fork := layout.ForkAfter("assess"); fork == nil || fork.ID != "pace" {
		t.Errorf("fork after assess = %+v, want pace", fork)
	}
	build := nfStageIndex(t, belt, "thorough-build")
	belt.Stages[build].Outcomes = append(belt.Stages[build].Outcomes, nfOutcome("polish", nfRouteThen("b")))
	for _, warning := range nfWarnings(belt) {
		if nfContains([]string{warning}, "doesn't come right after") {
			t.Errorf("warning %q, want none", warning)
		}
	}
}

// TestTheMergeNoteGoesOnTheMergedRoutesAttempt
func TestTheMergeNoteGoesOnTheMergedRoutesAttempt(t *testing.T) {
	t.Parallel()
	run := nfFourWays(t, nfTwoSided(t))
	EngineAdvance(&run, nfStarted("one", "f", new("r:one")), engineT0)
	EngineAdvance(&run, Event{Kind: EventKindAbort, Leg: new("r:one")}, engineT0)
	nfFinishLeg(&run, "done", "r:quick")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	// The held route is in front while Build's routes merge.
	if focus := run.Focus(); focus == nil || focus.ID != "r:one" {
		t.Errorf("focus = %+v, want r:one", focus)
	}
	EngineAdvance(&run, Event{Kind: EventKindMergedLegs, Legs: run.MergeBranches}, engineT0)
	if nfHasMergedAttempt(run, "r:one") {
		t.Errorf("r:one has a merged attempt, want none")
	}
	EngineAdvance(&run, Event{Kind: EventKindResume, Earlier: false, Leg: new("r:one")}, engineT0)
	EngineAdvance(&run, nfStarted("one", "f", new("r:one")), engineT0)
	if nfHasMergedAttempt(run, "r:one") {
		t.Errorf("r:one has a merged attempt after resume, want none")
	}
	EngineAdvance(&run, nfStarted("v", "f", new("r:build")), engineT0)
	var last *Attempt
	for i := range run.Attempts {
		if nfIsString(run.Attempts[i].Leg, "r:build") && run.Attempts[i].StageID == "verify" {
			last = &run.Attempts[i]
		}
	}
	if last == nil || last.Detail == nil || !nfContains([]string{*last.Detail}, "Merged") {
		t.Errorf("last verify attempt on r:build = %+v, want the merge note", last)
	}
}

// nfHasMergedAttempt reports whether an attempt on leg has a merge note.
func nfHasMergedAttempt(run Run, leg string) bool {
	return slices.ContainsFunc(run.Attempts, func(attempt Attempt) bool {
		return nfIsString(attempt.Leg, leg) && attempt.Detail != nil && nfContains([]string{*attempt.Detail}, "Merged")
	})
}
