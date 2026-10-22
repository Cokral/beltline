// It reads the plan's unexported lanes and tracks, so it lives beside them.

package core

import (
	"reflect"
	"strconv"
	"testing"
)

// metroPlanTestBelt is a belt of stages named names, each taken from the issues defaults in turn
func metroPlanTestBelt(id string, names []string) Belt {
	defaults := DefaultsStages(TriggerKindIssues, DefaultsBuiltInAgents(), true)
	stages := make([]Stage, 0, len(names))
	for index, name := range names {
		stage := defaults[index%len(defaults)]
		stage.ID = id + "-" + strconv.Itoa(index)
		stage.Name = name
		stage.Route = nil
		stages = append(stages, stage)
	}
	return runMetroBelt(id, id, TriggerKindManual, nil, stages, nil)
}

// metroPlanTestRoutes is each belt's route on track.
func metroPlanTestRoutes(track FactoryTrack) []MetroLayoutRoute {
	routes := make([]MetroLayoutRoute, 0, len(track.Belts))
	for _, belt := range track.Belts {
		routes = append(routes, metroLayoutRoute(belt, track))
	}
	return routes
}

// TestMetroLayoutACachedPlanIsTheOneItWouldFindAndAChangedBeltFindsItsOwn
func TestMetroLayoutACachedPlanIsTheOneItWouldFindAndAChangedBeltFindsItsOwn(t *testing.T) {
	t.Parallel()
	first := factoryTrackTestFloor(
		metroPlanTestBelt("a", []string{"Build", "Test", "Verify"}),
		metroPlanTestBelt("b", []string{"Build", "Verify", "Ship"}),
		metroPlanTestBelt("c", []string{"Spec", "Test"}),
	)
	routes := metroPlanTestRoutes(first)
	fresh := newMetroLayoutPlan(routes, first)
	for range 2 {
		if got := metroLayoutPlanCached(routes, first); !reflect.DeepEqual(got.Tracks, fresh.Tracks) {
			t.Fatalf("cached tracks %v, want %v", got.Tracks, fresh.Tracks)
		}
	}
	changed := factoryTrackTestFloor(
		metroPlanTestBelt("a", []string{"Build", "Verify", "Test"}),
		metroPlanTestBelt("b", []string{"Build", "Verify", "Ship"}),
		metroPlanTestBelt("c", []string{"Spec", "Test"}),
	)
	moved := metroPlanTestRoutes(changed)
	wantPlan := newMetroLayoutPlan(moved, changed)
	gotPlan := metroLayoutPlanCached(moved, changed)
	if !reflect.DeepEqual(metroPlanStops(gotPlan), metroPlanStops(wantPlan)) {
		t.Fatalf("cached lane stops %v, want %v", metroPlanStops(gotPlan), metroPlanStops(wantPlan))
	}
	if !reflect.DeepEqual(gotPlan.Tracks, wantPlan.Tracks) {
		t.Fatalf("cached tracks %v, want %v", gotPlan.Tracks, wantPlan.Tracks)
	}
}

// metroPlanStops is each lane's stops, in lane order.
func metroPlanStops(plan metroLayoutPlan) [][]string {
	out := make([][]string, 0, len(plan.Lanes))
	for _, lane := range plan.Lanes {
		out = append(out, lane.Path.Stops)
	}
	return out
}
