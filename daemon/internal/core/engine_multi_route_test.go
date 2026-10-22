// Tests of multi-route belts through the engine, the file format and validation.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestTakingOneRouteKeepsTheRunsOwnLeg
func TestTakingOneRouteKeepsTheRunsOwnLeg(t *testing.T) {
	t.Parallel()
	belt := nfForkedBelt([]string{"build"})
	belt.Stages[0].Outcomes[0].Then = nfRouteThen("simple")
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	if run.IsSplit() {
		t.Errorf("run is split, want one leg")
	}
	if len(run.Legs) != 1 || run.Legs[0].ID != "main" {
		t.Fatalf("legs = %+v, want main", run.Legs)
	}
	if run.Legs[0].Route == nil || *run.Legs[0].Route != "simple" {
		t.Errorf("leg route = %v, want simple", run.Legs[0].Route)
	}
	folder := EngineAttemptFolder(run, *run.Stage())
	if !strings.HasSuffix(folder, "-1") {
		t.Errorf("folder = %q, want a -1 suffix", folder)
	}
	if strings.Contains(folder, "simple-") {
		t.Errorf("folder = %q, want no simple- route part", folder)
	}
	EngineAdvance(&run, nfStarted("a", "f", nil), engineT0)
	last := run.Attempts[len(run.Attempts)-1]
	if last.Leg != nil {
		t.Errorf("attempt leg = %v, want none", *last.Leg)
	}
	if last.ID != run.StageID+"-1" {
		t.Errorf("attempt id = %q, want %q", last.ID, run.StageID+"-1")
	}
}

// TestOneOutcomeSplitsTheRunIntoOneLegPerRoute
func TestOneOutcomeSplitsTheRunIntoOneLegPerRoute(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	if !run.IsSplit() {
		t.Errorf("run is not split")
	}
	if len(run.Legs) != 2 {
		t.Errorf("legs = %d, want 2", len(run.Legs))
	}
	if got := run.Routes()["size"]; !slices.Equal(got, []string{"complex", "simple"}) {
		t.Errorf("size routes = %v, want complex and simple", got)
	}
	if got := nfStageOf(nfLegOn(run, "complex")); got != "spec" {
		t.Errorf("complex leg at %q, want spec", got)
	}
	if got := nfStageOf(nfLegOn(run, "simple")); got != "quick-build" {
		t.Errorf("simple leg at %q, want quick-build", got)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
}

// TestEachLegWalksItsOwnRouteAndFinishesOnItsOwn
func TestEachLegWalksItsOwnRouteAndFinishesOnItsOwn(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	// The Complex leg goes to its second stage; the Simple leg finishes.
	EngineAdvance(&run, nfStarted("a", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)
	if got := nfStageOf(nfLegOn(run, "complex")); got != "build" {
		t.Errorf("complex leg at %q, want build", got)
	}
	if nfLegOn(run, "complex").IsFinished() {
		t.Errorf("complex leg finished, want running")
	}
	EngineAdvance(&run, nfStarted("b", "f", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)
	if simple := nfLegOn(run, "simple"); simple == nil || !simple.IsFinished() {
		t.Errorf("simple leg = %+v, want finished", simple)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
	if run.Reason == nil || !strings.HasPrefix(*run.Reason, "WAITING FOR ROUTES") {
		t.Errorf("reason = %v, want WAITING FOR ROUTES", run.Reason)
	}
	if run.Reason == nil || !strings.Contains(*run.Reason, "Complex route") {
		t.Errorf("reason = %v, want it to name the Complex route", run.Reason)
	}
}

// TestTheJoinHoldsBackUntilEveryLegHasFinished
func TestTheJoinHoldsBackUntilEveryLegHasFinished(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	// Finish both legs: the run enters the stage where the routes meet.
	EngineAdvance(&run, nfStarted("s", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("t", "f", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)
	if run.StageID == "verify" {
		t.Errorf("stage = verify, want the join to wait")
	}
	EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("u", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:complex")), engineT0)
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
	if run.IsSplit() {
		t.Errorf("run is still split")
	}
}

// TestARouteWithNoStagesIsALegThatIsFinishedAtOnce
func TestARouteWithNoStagesIsALegThatIsFinishedAtOnce(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf(nil))
	nfFinish(&run, "ready to implement")
	if simple := nfLegOn(run, "simple"); simple == nil || !simple.IsFinished() {
		t.Errorf("simple leg = %+v, want finished", simple)
	}
	if got := nfStageOf(nfLegOn(run, "complex")); got != "spec" {
		t.Errorf("complex leg at %q, want spec", got)
	}
	if len(run.Legs) != 2 {
		t.Errorf("legs = %d, want 2", len(run.Legs))
	}
}

// TestALegThatHoldsLeavesTheOthersRunning
func TestALegThatHoldsLeavesTheOthersRunning(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)
	EngineAdvance(&run, nfStarted("b", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: open questions", new("r:complex")), engineT0)
	if run.State != RunStateHeld {
		t.Errorf("state = %q, want held", run.State)
	}
	if run.Reason == nil || !strings.Contains(*run.Reason, "Complex") {
		t.Errorf("reason = %v, want it to name Complex", run.Reason)
	}
	if state := nfStateOf(nfLegOn(run, "complex")); state != RunStateHeld {
		t.Errorf("complex state = %q, want held", state)
	}
	if simple := nfLegOn(run, "simple"); simple == nil || !simple.IsFinished() {
		t.Errorf("simple leg = %+v, want finished", simple)
	}
	// Finishing the held leg joins the routes.
	EngineAdvance(&run, EventResume(nil, nil, new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("c", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("d", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:complex")), engineT0)
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
}

// TestALegThatParksKeepsTheJoinWaitingAndADroppedLegCountsAsFinished
func TestALegThatParksKeepsTheJoinWaitingAndADroppedLegCountsAsFinished(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)
	EngineAdvance(&run, nfStarted("b", "f", new("r:complex")), engineT0)
	spec := nfIndexOfStage(t, run.Stages, "spec")
	run.Stages[spec].Outcomes[0].Then = NewThen(ThenArgs{Action: ThenActionPark})
	run.Stages[spec].Outcomes = append(run.Stages[spec].Outcomes, nfOutcome("later", nfThenOf(ThenActionPark)))
	EngineAdvance(&run, nfEnded("Outcome: later", new("r:complex")), engineT0)
	if run.State != RunStateParked {
		t.Errorf("state = %q, want parked", run.State)
	}
	if run.StageID == "verify" {
		t.Errorf("stage = verify, want the join to wait")
	}
	// Dropping the parked leg lets the run carry on.
	if state := nfStateOf(nfLegOn(run, "complex")); state != RunStateParked {
		t.Errorf("complex state = %q, want parked", state)
	}
	EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by you", Leg: new("r:complex")}, engineT0)
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
}

// TestRunningAgainAndGoingToAStageMoveOnlyThatLeg
func TestRunningAgainAndGoingToAStageMoveOnlyThatLeg(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:simple")), engineT0)
	if state := nfStateOf(nfLegOn(run, "simple")); state != RunStateRunning {
		t.Errorf("simple state = %q, want running", state)
	}
	if got := nfStageOf(nfLegOn(run, "complex")); got != "spec" {
		t.Errorf("complex leg at %q, want spec", got)
	}
	EngineAdvance(&run, Event{Kind: EventKindAbort, Leg: new("r:simple")}, engineT0)
	if state := nfStateOf(nfLegOn(run, "simple")); state != RunStateHeld {
		t.Errorf("simple state = %q, want held", state)
	}
	if state := nfStateOf(nfLegOn(run, "complex")); state != RunStateQueued {
		t.Errorf("complex state = %q, want queued", state)
	}
	if run.State != RunStateHeld {
		t.Errorf("state = %q, want held", run.State)
	}
}

// TestALegThatJumpsPastTheJoinRunsOnAlone
func TestALegThatJumpsPastTheJoinRunsOnAlone(t *testing.T) {
	t.Parallel()
	belt := nfBothRoutesOf([]string{"build"})
	spec := nfStageIndex(t, belt, "spec")
	belt.Stages[spec].Outcomes[0].Then = nfGotoThen("ship")
	belt.Stages[spec].Outcomes = append(belt.Stages[spec].Outcomes, nfOutcome("jump", nfGotoThen("ship")))
	run := engineStartRun(belt)
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: jump", new("r:complex")), engineT0)
	if got := nfStageOf(nfLegOn(run, "complex")); got != "ship" {
		t.Errorf("complex leg at %q, want ship", got)
	}
	if got := nfStageOf(nfLegOn(run, "simple")); got != "quick-build" {
		t.Errorf("simple leg at %q, want quick-build", got)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
}

// TestLegsGetTheirOwnAttemptFoldersAndIDs
func TestLegsGetTheirOwnAttemptFoldersAndIDs(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	complex := EngineAttemptFolderLeg(run, run.Stages[0], new("r:complex"))
	simple := EngineAttemptFolderLeg(run, run.Stages[0], new("r:simple"))
	if complex == simple {
		t.Errorf("folders are both %q", complex)
	}
	if !strings.Contains(complex, "complex") {
		t.Errorf("complex folder = %q, want it to name complex", complex)
	}
	if !strings.Contains(simple, "simple") {
		t.Errorf("simple folder = %q, want it to name simple", simple)
	}
	EngineAdvance(&run, nfStarted("a", complex, new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("b", simple, new("r:simple")), engineT0)
	if len(run.Attempts) != 3 {
		t.Errorf("attempts = %d, want 3", len(run.Attempts))
	}
	ids := map[string]struct{}{}
	for _, attempt := range run.Attempts {
		ids[attempt.ID] = struct{}{}
	}
	if len(ids) != 3 {
		t.Errorf("distinct attempt ids = %d, want 3", len(ids))
	}
}

// TestEditingTheSubjectStartsTheRunAgainAsOneLeg
func TestEditingTheSubjectStartsTheRunAgainAsOneLeg(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("b", "f", new("r:simple")), engineT0)
	subject := run.Subject.Clone()
	subject.Body = "Something else entirely."
	effects := EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: subject}, engineT0)
	stopped := map[string]struct{}{}
	for _, effect := range effects {
		if effect.Kind == EffectKindStop {
			stopped[effect.Session] = struct{}{}
		}
	}
	if !reflect.DeepEqual(stopped, map[string]struct{}{"a": {}, "b": {}}) {
		t.Errorf("stopped sessions = %v, want a and b", stopped)
	}
	if run.IsSplit() {
		t.Errorf("run is split")
	}
	if len(run.Legs) != 1 {
		t.Errorf("legs = %d, want 1", len(run.Legs))
	}
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
	if len(run.Routes()) != 0 {
		t.Errorf("routes = %v, want none", run.Routes())
	}
}

// TestDroppingOneLegStopsOnlyItsSession
func TestDroppingOneLegStopsOnlyItsSession(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:simple")), engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by you", Leg: new("r:simple")}, engineT0)
	if !reflect.DeepEqual(effects, []Effect{{Kind: EffectKindStop, Session: "a"}}) {
		t.Errorf("effects = %+v, want stop a", effects)
	}
	if state := nfStateOf(nfLegOn(run, "simple")); state != RunStateDropped {
		t.Errorf("simple state = %q, want dropped", state)
	}
	if state := nfStateOf(nfLegOn(run, "complex")); state != RunStateQueued {
		t.Errorf("complex state = %q, want queued", state)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
}

// TestDroppingTheRunEndsEveryLeg
func TestDroppingTheRunEndsEveryLeg(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("a", "f", new("r:simple")), engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0)
	if !nfContainsEffect(effects, Effect{Kind: EffectKindStop, Session: "a"}) {
		t.Errorf("effects = %+v, want stop a", effects)
	}
	if !engineHasEffect(effects, EffectKindCleanUp) {
		t.Errorf("effects = %+v, want cleanUp", effects)
	}
	if run.State != RunStateDropped {
		t.Errorf("state = %q, want dropped", run.State)
	}
}

// nfContainsEffect reports whether effects holds want.
func nfContainsEffect(effects []Effect, want Effect) bool {
	return slices.ContainsFunc(effects, func(effect Effect) bool { return reflect.DeepEqual(effect, want) })
}

// TestTheOutcomeLineNamesEveryRouteItTakes
func TestTheOutcomeLineNamesEveryRouteItTakes(t *testing.T) {
	t.Parallel()
	layout := nfBothRoutesOf([]string{"build"}).Layout()
	if got := EngineDescribe(nfRouteThen("simple"), layout, "triage", nil); got != "Take the Simple route" {
		t.Errorf("describe = %q, want Take the Simple route", got)
	}
	if got := EngineDescribe(nfRoutesThen("complex", "simple"), layout, "triage", nil); got != "Take the Complex and Simple routes" {
		t.Errorf("describe = %q, want Take the Complex and Simple routes", got)
	}
}

// TestTheJoinMergesTheLegBranchesIntoTheRunsBranch
func TestTheJoinMergesTheLegBranchesIntoTheRunsBranch(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	run.Branch = new("beltline/issue-142")
	run.Legs[0].Branch = new("beltline/issue-142-complex")
	run.Legs[1].Branch = new("beltline/issue-142-simple")
	EngineAdvance(&run, nfStarted("a", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("b", "f", new("r:simple")), engineT0)
	var effects []Effect
	effects = append(effects, EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)...)
	effects = append(effects, EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)...)
	EngineAdvance(&run, nfStarted("u", "f", new("r:complex")), engineT0)
	effects = append(effects, EngineAdvance(&run, nfEnded("Outcome: done", new("r:complex")), engineT0)...)
	merge := []string{"beltline/issue-142-complex", "beltline/issue-142-simple"}
	if !nfHasMergeLegs(effects, merge...) {
		t.Errorf("effects = %+v, want mergeLegs %v", effects, merge)
	}
	if !reflect.DeepEqual(run.MergeBranches, merge) {
		t.Errorf("merge branches = %v, want %v", run.MergeBranches, merge)
	}
	EngineAdvance(&run, Event{Kind: EventKindMergedLegs, Legs: merge}, engineT0)
	if len(run.MergeBranches) != 0 {
		t.Errorf("merge branches = %v, want none", run.MergeBranches)
	}
	if run.MergeNote == nil || !strings.Contains(*run.MergeNote, "Merged") {
		t.Errorf("merge note = %v, want Merged", run.MergeNote)
	}
	if run.MergeNote == nil || !strings.Contains(*run.MergeNote, "beltline/issue-142") {
		t.Errorf("merge note = %v, want the run branch", run.MergeNote)
	}
}

// TestAConflictAtTheJoinHoldsTheRunWithTheFilesNamed
func TestAConflictAtTheJoinHoldsTheRunWithTheFilesNamed(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutesOf([]string{"build"}))
	nfFinish(&run, "ready to implement")
	run.Branch = new("beltline/issue-142")
	run.Legs[0].Branch = new("beltline/issue-142-complex")
	run.Legs[1].Branch = new("beltline/issue-142-simple")
	EngineAdvance(&run, nfStarted("a", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("b", "f", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:simple")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)
	EngineAdvance(&run, nfStarted("u", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: done", new("r:complex")), engineT0)
	EngineAdvance(&run, Event{Kind: EventKindMergeConflict, Files: []string{"README.md", "Package.swift"}}, engineT0)
	if run.State != RunStateHeld {
		t.Errorf("state = %q, want held", run.State)
	}
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
	if run.Reason == nil || !strings.Contains(*run.Reason, "README.md") {
		t.Errorf("reason = %v, want README.md", run.Reason)
	}
	if run.Reason == nil || !strings.Contains(*run.Reason, "Package.swift") {
		t.Errorf("reason = %v, want Package.swift", run.Reason)
	}
}
