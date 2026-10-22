// Engine tests of parking, the second part: sending a parked split, nested
// splits, edits and drops while parked, and an
// outcome park without the button.

package core

import (
	"reflect"
	"testing"
	"time"
)

// TestSendToEndsParkingAndRestoresTheOtherRoutes
func TestSendToEndsParkingAndRestoresTheOtherRoutes(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutes())
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("s", "f", new("r:simple")), engineT0)
	nfParked(&run, engineT0)
	EngineAdvance(&run, EventResume(new("verify"), new("Ship the simple route."), new("r:simple")), engineT0.Add(time.Second))
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
	if got := nfStageOf(nfLegOn(run, "simple")); got != "verify" {
		t.Errorf("simple stage = %q, want verify", got)
	}
	if nfStateOf(nfLegOn(run, "complex")) != RunStateQueued {
		t.Errorf("complex state = %q, want queued", nfStateOf(nfLegOn(run, "complex")))
	}
	if got := nfStageOf(nfLegOn(run, "complex")); got != "spec" {
		t.Errorf("complex stage = %q, want spec", got)
	}
}

// TestANestedSplitParksEveryUnfinishedRouteAndDoesNotJoin
func TestANestedSplitParksEveryUnfinishedRouteAndDoesNotJoin(t *testing.T) {
	t.Parallel()
	belt := nfBelt(true, true, true)
	nfSetOutcomes(t, &belt, "triage", nfOutcome("split", nfRoutesThen("build", "skip")))
	nfSetOutcomes(t, &belt, "assess", nfOutcome("split", nfRoutesThen("quick", "thorough")))
	run := engineStartRun(belt)
	nfFinish(&run, "split")
	nfFinishLeg(&run, "split", "r:build")
	EngineAdvance(&run, nfStarted("q", "f", new("r:quick")), engineT0)
	joins := run.Joins
	nfParked(&run, engineT0)
	for _, leg := range run.Legs {
		if leg.State.IsActive() && leg.State != RunStateParked {
			t.Errorf("leg %q is %q, want parked", leg.ID, leg.State)
		}
	}
	if !nfSameJSON(t, run.Joins, joins) {
		t.Errorf("joins changed while parking")
	}
	if run.StageID == "ship" {
		t.Errorf("stage = ship, want the run to wait")
	}
	if run.Parking != nil && len(run.Parking.Stopping) > 0 {
		nfParkStopped(&run, run.Parking.Stopping[0], nil, engineT0)
	}
	nfUnpark(&run, engineT0.Add(time.Second))
	for _, route := range []string{"quick", "thorough", "skip"} {
		if state := nfStateOf(nfLegOn(run, route)); state != RunStateQueued {
			t.Errorf("%s state = %q, want queued", route, state)
		}
	}
	if !nfSameJSON(t, run.Joins, joins) {
		t.Errorf("joins changed after unparking")
	}
}

// TestEditsWhileParkedDoNotRetriageAndClosingStillDrops
func TestEditsWhileParkedDoNotRetriageAndClosingStillDrops(t *testing.T) {
	t.Parallel()
	run := nfWaitingApproval()
	run.Policies.RetriageOnEdit = true
	edited := run.Subject.Clone()
	edited.Body = "A different export."
	nfParked(&run, engineT0)
	stage := run.StageID
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0.Add(time.Second))
	if run.Subject.Body != "A different export." {
		t.Errorf("subject body = %q", run.Subject.Body)
	}
	if run.StageID != stage {
		t.Errorf("stage = %q, want %q", run.StageID, stage)
	}
	if !run.IsButtonParked() {
		t.Errorf("not button parked")
	}
	nfUnpark(&run, engineT0.Add(2*time.Second))
	if run.StageID != stage {
		t.Errorf("stage after unpark = %q, want %q", run.StageID, stage)
	}
	if run.State != RunStateWaiting {
		t.Errorf("state = %q, want waiting", run.State)
	}
	nfParked(&run, engineT0.Add(3*time.Second))
	EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0.Add(4*time.Second))
	if run.State != RunStateDropped {
		t.Errorf("state = %q, want dropped", run.State)
	}
	if !nfReasonIs(run, "the issue was closed") {
		t.Errorf("reason = %v, want the issue was closed", run.Reason)
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
}

// TestDropWhileParkedDropsTheRun
func TestDropWhileParkedDropsTheRun(t *testing.T) {
	t.Parallel()
	run := nfWaitingApproval()
	nfParked(&run, engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindDrop, Reason: "dropped by you"}, engineT0)
	if run.State != RunStateDropped {
		t.Errorf("state = %q, want dropped", run.State)
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
	if !engineHasEffect(effects, EffectKindCleanUp) {
		t.Errorf("effects = %+v, want cleanUp", effects)
	}
}

// TestAnOutcomeParkWithoutTheButtonKeepsTheOldUnpark
func TestAnOutcomeParkWithoutTheButtonKeepsTheOldUnpark(t *testing.T) {
	t.Parallel()
	run := nfDefaultRun()
	nfFinish(&run, "wait to implement")
	if run.State != RunStateParked {
		t.Errorf("state = %q, want parked", run.State)
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
	if run.CanButtonPark() {
		t.Errorf("canButtonPark = true, want false")
	}
	before := run.Clone()
	if effects := nfUnpark(&run, engineT0); len(effects) != 0 {
		t.Errorf("unpark effects = %+v, want none", effects)
	}
	if !nfSameJSON(t, run, before) {
		t.Errorf("unpark changed the run")
	}
	EngineAdvance(&run, EventResume(nil, nil, nil), engineT0.Add(time.Second))
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
	if run.StageID != "triage" {
		t.Errorf("stage = %q, want triage", run.StageID)
	}
}

// nfSameParkingRoutes reports whether two parking records name the same legs in order.
func nfSameParkingRoutes(a, b []ParkingRoute) bool {
	legs := func(routes []ParkingRoute) []string {
		var out []string
		for _, route := range routes {
			out = append(out, route.Leg)
		}
		return out
	}
	return reflect.DeepEqual(legs(a), legs(b))
}
