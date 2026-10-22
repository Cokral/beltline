// Tests of escaping a nested fork: a sibling route inside an active fork is
// refused with its warning, and a nested run restores its joins, departures and merges.

package core

import (
	"reflect"
	"slices"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// nfJumped is the nested belt with quick-build
// jumping to ship, verify and triage, and a run that has split into Build and
// Skip, then Build into Quick and Thorough, each leg on its own branch.
func nfJumped(t *testing.T) (Belt, Run) {
	t.Helper()
	belt := nfBelt(true, true, true)
	nfSetOutcomes(t, &belt, "triage", nfOutcome("split", nfRoutesThen("build", "skip")))
	nfSetOutcomes(t, &belt, "assess", nfOutcome("split", nfRoutesThen("quick", "thorough")))
	nfSetOutcomes(t, &belt, "quick-build",
		nfOutcome("ship-it", nfGotoThen("ship")),
		nfOutcome("check", nfGotoThen("verify")),
		nfOutcome("back", nfGotoThen("triage")),
		nfOutcome("done", nfThenOf(ThenActionNext)),
	)
	run := engineStartRun(belt)
	run.Branch = new("run-branch")
	nfFinish(&run, "split")
	for i := range run.Legs {
		run.Legs[i].Branch = new("outer-" + *run.Legs[i].Route)
	}
	nfFinishLeg(&run, "split", "r:build")
	for i := range run.Legs {
		if run.Legs[i].Branch == nil {
			run.Legs[i].Branch = new("inner-" + *run.Legs[i].Route)
		}
	}
	return belt, run
}

// TestLeavingNestedForksWaitsAndSkipsTheJoin
func TestLeavingNestedForksWaitsAndSkipsTheJoin(t *testing.T) {
	t.Parallel()
	_, run := nfJumped(t)
	before := make([]string, 0, len(run.Attempts))
	for _, attempt := range run.Attempts {
		before = append(before, attempt.ID)
	}
	nfFinishLeg(&run, "ship-it", "r:quick")
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("quick depart = %v, want ship", d)
	}
	if got := nfStageOf(nfLegOn(run, "thorough")); got != "thorough-spec" {
		t.Errorf("thorough leg at %q, want thorough-spec", got)
	}
	if got := nfStageOf(nfLegOn(run, "skip")); got != "skip-build" {
		t.Errorf("skip leg at %q, want skip-build", got)
	}
	if n := len(nfAttemptsAt(run, "ship")); n != 0 {
		t.Errorf("ship attempts = %d, want 0", n)
	}
	if len(run.MergeBranches) != 0 {
		t.Errorf("merge branches = %v, want none", run.MergeBranches)
	}
	nfFinishLeg(&run, "done", "r:thorough")
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("quick depart = %v, want ship", d)
	}
	nfFinishLeg(&run, "done", "r:thorough")
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("quick depart = %v, want ship", d)
	}
	if n := len(nfLegsAt(run, "verify")); n != 0 {
		t.Errorf("verify legs = %d, want 0", n)
	}
	if len(run.MergeBranches) != 0 {
		t.Errorf("merge branches = %v, want none", run.MergeBranches)
	}
	nfFinishLeg(&run, "done", "r:skip")
	left := nfLegByID(run, "r:quick")
	if left.Depart != nil {
		t.Errorf("quick depart = %v, want none once it has left", *left.Depart)
	}
	if got := nfStageOf(left); got != "ship" {
		t.Errorf("quick leg at %q, want ship", got)
	}
	if left.Branch == nil || *left.Branch != "inner-quick" {
		t.Errorf("quick branch = %v, want inner-quick", left.Branch)
	}
	ids := make([]string, 0, len(run.Attempts))
	for _, attempt := range run.Attempts {
		ids = append(ids, attempt.ID)
	}
	if !nfStartsWith(ids, before) && len(nfAttemptsAt(run, "quick-build")) == 0 {
		t.Errorf("attempts = %v, want the earlier ones kept", ids)
	}
	if len(run.MergeBranches) != 0 {
		t.Errorf("merge branches = %v, want none", run.MergeBranches)
	}
}

// TestAHeldSiblingDelaysTheEscapeUntilItIsDropped
func TestAHeldSiblingDelaysTheEscapeUntilItIsDropped(t *testing.T) {
	t.Parallel()
	belt, _ := nfJumped(t)
	nfSetOutcomes(t, &belt, "thorough-spec", nfOutcome("ask", nfThenOf(ThenActionHold)))
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	nfFinishLeg(&run, "split", "r:build")
	nfFinishLeg(&run, "ship-it", "r:quick")
	nfFinishLeg(&run, "ask", "r:thorough")
	nfFinishLeg(&run, "done", "r:skip")
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("quick depart = %v, want ship", d)
	}
	EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "leave it", Leg: new("r:thorough")}, engineT0)
	left := nfLegByID(run, "r:quick")
	if got := nfStageOf(left); got != "ship" {
		t.Errorf("quick leg at %q, want ship", got)
	}
	if left.Depart != nil {
		t.Errorf("quick depart = %v, want none", *left.Depart)
	}
}

// TestAnInParentJumpDoesNotWaitForTheOuterSibling
func TestAnInParentJumpDoesNotWaitForTheOuterSibling(t *testing.T) {
	t.Parallel()
	_, run := nfJumped(t)
	nfFinishLeg(&run, "check", "r:quick")
	if d := nfLegByID(run, "r:quick").Depart; d == nil || *d != "verify" {
		t.Errorf("quick depart = %v, want verify", d)
	}
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	if got := nfStageOf(nfLegByID(run, "r:quick")); got != "verify" {
		t.Errorf("quick leg at %q, want verify", got)
	}
	if got := nfStageOf(nfLegOn(run, "skip")); got != "skip-build" {
		t.Errorf("skip leg at %q, want skip-build", got)
	}
	if branch := nfLegByID(run, "r:quick").Branch; branch == nil || *branch != "inner-quick" {
		t.Errorf("quick branch = %v, want inner-quick", branch)
	}
}

// TestTwoDeparturesStaySeparate
func TestTwoDeparturesStaySeparate(t *testing.T) {
	t.Parallel()
	belt := nfBelt(true, true, true)
	nfSetOutcomes(t, &belt, "triage", nfOutcome("split", nfRoutesThen("build", "skip")))
	nfSetOutcomes(t, &belt, "assess", nfOutcome("split", nfRoutesThen("quick", "thorough")))
	leave := nfOutcome("ship-it", nfGotoThen("ship"))
	nfSetOutcomes(t, &belt, "quick-build", leave)
	nfSetOutcomes(t, &belt, "thorough-build", leave)
	nfSetOutcomes(t, &belt, "thorough-spec", nfOutcome("done", nfThenOf(ThenActionNext)))
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	nfFinishLeg(&run, "split", "r:build")
	nfFinishLeg(&run, "ship-it", "r:quick")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "ship-it", "r:thorough")
	departed := 0
	for _, leg := range run.Legs {
		if leg.Depart != nil && *leg.Depart == "ship" {
			departed++
		}
	}
	if departed != 2 {
		t.Errorf("legs departing for ship = %d, want 2", departed)
	}
	nfFinishLeg(&run, "done", "r:skip")
	resumed := nfLegsAt(run, "ship")
	if len(resumed) != 2 {
		t.Fatalf("legs resumed at ship = %d, want 2", len(resumed))
	}
	if resumed[0].ID == resumed[1].ID {
		t.Errorf("resumed legs share id %q", resumed[0].ID)
	}
}

// TestABackwardJumpClearsChoicesOnlyForTheContinuation
func TestABackwardJumpClearsChoicesOnlyForTheContinuation(t *testing.T) {
	t.Parallel()
	_, run := nfJumped(t)
	nfFinishLeg(&run, "back", "r:quick")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:thorough")
	nfFinishLeg(&run, "done", "r:skip")
	returned := slices.ContainsFunc(run.Legs, func(leg Leg) bool { return nfStageOf(&leg) == "triage" })
	if !returned {
		t.Errorf("no leg returned to triage: %+v", run.Legs)
	}
	if wave := run.Routes()["wave"]; wave != nil && !slices.Equal(wave, []string{"skip"}) {
		t.Errorf("wave = %v, want none or [skip]", wave)
	}
	if skip := nfLegOn(run, "skip"); skip != nil && !skip.IsFinished() {
		t.Errorf("skip leg = %+v, want none or finished", skip)
	}
}

// TestASiblingRouteInsideAnActiveForkIsRefused
func TestASiblingRouteInsideAnActiveForkIsRefused(t *testing.T) {
	t.Parallel()
	belt := nfBelt(false, true, true)
	nfSetOutcomes(t, &belt, "quick-build", nfOutcome("switch", nfGotoThen("thorough-build")))
	if !nfContains(nfWarnings(belt), "goes from the Quick route into another route") {
		t.Errorf("warnings = %v, want the sibling route refused", nfWarnings(belt))
	}
	run := engineStartRun(nfBothInner(t, belt))
	nfFinish(&run, "go")
	nfFinish(&run, "split")
	nfFinishLeg(&run, "switch", "r:quick")
	quick := nfLegOn(run, "quick")
	if quick == nil || quick.State != RunStateHeld {
		t.Fatalf("quick leg = %+v, want held", quick)
	}
	if quick.Reason == nil || !nfContains([]string{*quick.Reason}, "Quick") {
		t.Errorf("reason = %v, want it to name Quick", quick.Reason)
	}
	if quick.Reason == nil || !nfContains([]string{*quick.Reason}, "Thorough") {
		t.Errorf("reason = %v, want it to name Thorough", quick.Reason)
	}
	if got := nfStageOf(nfLegOn(run, "thorough")); got != "thorough-spec" {
		t.Errorf("thorough leg at %q, want thorough-spec", got)
	}
}

// TestANestedRunRestoresJoinsDeparturesAndMerges
func TestANestedRunRestoresJoinsDeparturesAndMerges(t *testing.T) {
	t.Parallel()
	belt := nfBelt(true, true, true)
	nfSetOutcomes(t, &belt, "triage", nfOutcome("split", nfRoutesThen("build", "skip")))
	nfSetOutcomes(t, &belt, "assess", nfOutcome("split", nfRoutesThen("quick", "thorough")))
	nfSetOutcomes(t, &belt, "quick-build", nfOutcome("ship-it", nfGotoThen("ship")))
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	nfFinishLeg(&run, "split", "r:build")
	nfFinishLeg(&run, "ship-it", "r:quick")
	run.MergeInto = new("build-branch")
	run.MergeContext = new("the Quick route into build-branch")
	data, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Run
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	joined := slices.ContainsFunc(loaded.Joins, func(join Join) bool { return join.ForkID == "pace" })
	if !joined {
		t.Errorf("joins = %+v, want the pace join", loaded.Joins)
	}
	if d := nfLegByID(loaded, "r:quick").Depart; d == nil || *d != "ship" {
		t.Errorf("quick depart = %v, want ship", d)
	}
	if !nfIsString(loaded.MergeInto, "build-branch") {
		t.Errorf("merge into = %v, want build-branch", loaded.MergeInto)
	}
	if !reflect.DeepEqual(loaded.Forks, run.Forks) {
		t.Errorf("forks changed across a round trip")
	}
	library := belt
	library.Stages[0].Name = "Changed"
	if loaded.Stages[0].Name != "Triage" {
		t.Errorf("stage name = %q, want Triage", loaded.Stages[0].Name)
	}
	if library.Stages[0].Name != "Changed" {
		t.Errorf("library stage name = %q, want Changed", library.Stages[0].Name)
	}
}
