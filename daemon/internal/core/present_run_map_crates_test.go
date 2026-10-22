package core

import "testing"

// TestRunCratesSitOnTheirTrackOrRideAboveIt checks the run's own crate: on a stage it sits on the
// track, while a finished run or one with no stage rides just above it.
func TestRunCratesSitOnTheirTrackOrRideAboveIt(t *testing.T) {
	t.Parallel()
	metrics := MapMetricsRun(false)
	plan := NewMapPlan(forkedBeltWith([]string{"build"}).Layout(), metrics)
	geometry := plan.Geometry(plan.Width().MinimumWidth())
	lifted := geometry.MainY - metrics.Thickness/2 - 11

	run := engineStartRun(forkedBeltWith([]string{"build"}))
	cell := mapTestCell(t, geometry, run.StageID)
	crates := geometry.RunCrates(run, run.Routes(), metrics.Thickness)
	if len(crates) != 1 || crates[0].ID != "main" {
		t.Fatalf("crates = %+v, want one main crate", crates)
	}
	if crates[0].X != cell.X || crates[0].Y != geometry.Crate(&run.StageID, false, run.Routes()).Y {
		t.Errorf("crate on stage %q = (%v, %v), want on the track at x %v", run.StageID, crates[0].X, crates[0].Y, cell.X)
	}
	if crates[0].StageID == nil || *crates[0].StageID != run.StageID {
		t.Errorf("crate stage = %v, want %q", crates[0].StageID, run.StageID)
	}

	run.StageID = ""
	crates = geometry.RunCrates(run, run.Routes(), metrics.Thickness)
	if crates[0].X != 21 || crates[0].Y != lifted || crates[0].StageID != nil {
		t.Errorf("crate with no stage = %+v, want x 21, y %v and no stage", crates[0], lifted)
	}

	for _, state := range []RunState{RunStateShipped, RunStateDone} {
		run = engineStartRun(forkedBeltWith([]string{"build"}))
		run.State = state
		crates = geometry.RunCrates(run, run.Routes(), metrics.Thickness)
		if crates[0].X != geometry.Width-21 || crates[0].Y != lifted {
			t.Errorf("%s crate = %+v, want x %v and y %v", state, crates[0], geometry.Width-21, lifted)
		}
	}
}

// TestRunCratesOfASplitRunRideAboveEachUnfinishedLeg checks a split run draws one crate per
// unfinished leg, each lifted above its lane.
func TestRunCratesOfASplitRunRideAboveEachUnfinishedLeg(t *testing.T) {
	t.Parallel()
	run := engineStartRun(engineBothRoutes())
	engineFinishOutcome(&run, "ready to implement")
	if !run.IsSplit() {
		t.Fatalf("run has %d legs, want a split run", len(run.Legs))
	}
	metrics := NewMapMetrics(MapMetricsArgs{Cell: 178, Track: 117})
	geometry := NewMapPlan(run.Layout(), metrics).Geometry(1000)
	routes := run.Routes()
	crates := geometry.RunCrates(run, routes, metrics.Thickness)
	var unfinished int
	for _, leg := range run.Legs {
		if !leg.IsFinished() && leg.StageID != nil {
			unfinished++
		}
	}
	if len(crates) != unfinished || unfinished == 0 {
		t.Fatalf("crates = %d, want one per unfinished leg (%d)", len(crates), unfinished)
	}
	for _, crate := range crates {
		point := geometry.Crate(crate.StageID, false, routes)
		if crate.X != point.X || crate.Y != point.Y-metrics.Thickness/2-11 {
			t.Errorf("leg crate %s = (%v, %v), want (%v, %v) lifted above its lane", crate.ID, crate.X, crate.Y, point.X, point.Y-metrics.Thickness/2-11)
		}
	}
}
