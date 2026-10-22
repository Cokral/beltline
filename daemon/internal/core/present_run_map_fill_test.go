package core

import "testing"

// TestRunFillStopsAtTheStageAndMovesWhileWorking checks where a run's fill stops and when its
// stripes move: at the stage's left edge while working, at the end when finished, nowhere with no
// stage, and not moving while paused.
func TestRunFillStopsAtTheStageAndMovesWhileWorking(t *testing.T) {
	t.Parallel()
	metrics := MapMetricsRun(false)
	plan := NewMapPlan(forkedBeltWith([]string{"build"}).Layout(), metrics)
	geometry := plan.Geometry(plan.Width().MinimumWidth())

	run := engineStartRun(forkedBeltWith([]string{"build"}))
	run.State = RunStateRunning
	fill := geometry.RunFill(run, run.Routes(), false)
	if fill.UntilX != geometry.Left(run.StageID) || fill.Finished || !fill.Moving {
		t.Errorf("working fill = %+v, want to stage %q's left edge, not finished, moving", fill, run.StageID)
	}
	if paused := geometry.RunFill(run, run.Routes(), true); paused.Moving {
		t.Errorf("paused fill moves: %+v", paused)
	}

	run.StageID = ""
	if fill := geometry.RunFill(run, run.Routes(), false); fill.UntilX != 0 {
		t.Errorf("fill with no stage stops at %v, want 0", fill.UntilX)
	}

	for _, state := range []RunState{RunStateShipped, RunStateDone} {
		run = engineStartRun(forkedBeltWith([]string{"build"}))
		run.State = state
		fill = geometry.RunFill(run, run.Routes(), false)
		if fill.UntilX != geometry.Width || !fill.Finished || fill.Moving {
			t.Errorf("%s fill = %+v, want the full width, finished and not moving", state, fill)
		}
	}
}

// TestFilledLanesFollowTheTakenRoutes checks that only the lanes on the track the run follows are
// filled, and that an undecided fork takes its first route.
func TestFilledLanesFollowTheTakenRoutes(t *testing.T) {
	t.Parallel()
	metrics := MapMetricsRun(false)
	plan := NewMapPlan(forkedBeltWith([]string{"build"}).Layout(), metrics)
	geometry := plan.Geometry(plan.Width().MinimumWidth())

	lanesOf := func(routes map[string][]string) []string {
		var ids []string
		for _, lane := range geometry.FilledLanes(routes) {
			ids = append(ids, lane.Fork+":"+lane.Route.ID)
		}
		return ids
	}
	if got := lanesOf(map[string][]string{"size": {"simple"}}); len(got) != 1 || got[0] != "size:simple" {
		t.Errorf("filled lanes with simple taken = %v, want [size:simple]", got)
	}
	if got := lanesOf(map[string][]string{}); len(got) != 1 || got[0] != "size:complex" {
		t.Errorf("filled lanes with an undecided fork = %v, want its first route [size:complex]", got)
	}
}
