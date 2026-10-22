// The box layout's fork routes and their leads.
package core

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

// TestNoTrackRunsAcrossAForkWithoutARouteOnItsRow checks that no track runs across a fork without a route on its row.
func TestNoTrackRunsAcrossAForkWithoutARouteOnItsRow(t *testing.T) {
	t.Parallel()
	belt := boxTestSplitBelt([]boxTestSplitRoute{{ID: "work", Agents: []string{"build"}}, {ID: "skip"}})
	m := BoxLayoutMap(belt.Layout(), BoxMapMetricsRun, true, new(TriggerKindManual), boxTestLook)
	for _, edge := range m.Edges {
		if edge.From == "triage" && edge.To == "ship" && edge.Route == nil {
			t.Error("a track runs across the fork on the line")
		}
	}
	// The line itself only runs from the trigger to the first stage; everything else is on a route.
	if line := boxTestOnLine(m.Edges); len(line) != 1 || line[0] != "trigger>triage" {
		t.Errorf("line = %v, want [trigger>triage]", line)
	}
}

// TestAnEmptyRouteDrawsItsRowFromTheSpineToWhereTheRoutesMeet checks that an empty route draws its row from the spine to where the routes meet.
func TestAnEmptyRouteDrawsItsRowFromTheSpineToWhereTheRoutesMeet(t *testing.T) {
	t.Parallel()
	belt := boxTestSplitBelt([]boxTestSplitRoute{{ID: "work", Agents: []string{"build"}}, {ID: "skip"}})
	m := BoxLayoutMap(belt.Layout(), BoxMapMetricsRun, true, new(TriggerKindManual), boxTestLook)
	lane := boxTestEdgeNamed(t, m, "skip")
	if lane.From != "triage" || lane.To != "ship" {
		t.Errorf("empty route runs %s>%s, want triage>ship", lane.From, lane.To)
	}
	if lane.Straight {
		t.Error("the empty route's track is straight")
	}
	pill := boxTestRoutePill(t, m, "skip")
	ship := boxTestNode(t, m, "ship")
	points := boxTestFirstSubpath(t, lane.Path)
	onRow := slices.ContainsFunc(points, func(point MapPoint) bool { return math.Abs(point.Y-pill.Y) < 0.5 })
	if !onRow {
		t.Error("the track does not run along the empty route's row")
	}
	last := points[len(points)-1]
	if last.X != ship.X {
		t.Errorf("track ends at x %v, want the ship's %v", last.X, ship.X)
	}
	if last.Y != ship.Y+ship.Height/2 {
		t.Errorf("track ends at y %v, want the ship's middle %v", last.Y, ship.Y+ship.Height/2)
	}
	// The work route still runs through its own box on the same fork.
	if got := boxTestOnRoute(m.Edges, "work"); fmt.Sprint(got) != fmt.Sprint([]string{"triage>work-build", "work-build>ship"}) {
		t.Errorf("work route edges = %v", got)
	}
}

// TestAnEmptyRouteOnAnyRowStaysOnTheMap checks that an empty route on any row stays on the map.
func TestAnEmptyRouteOnAnyRowStaysOnTheMap(t *testing.T) {
	t.Parallel()
	// An empty route has no box to measure the map by, so its row must still be drawn inside it, above or below.
	orders := [][]boxTestSplitRoute{
		{{ID: "skip"}, {ID: "work", Agents: []string{"build"}}},
		{{ID: "work", Agents: []string{"build"}}, {ID: "skip"}},
	}
	for _, metrics := range []BoxMapMetrics{BoxMapMetricsRun, BoxMapMetricsEditor} {
		for _, routes := range orders {
			m := BoxLayoutMap(boxTestSplitBelt(routes).Layout(), metrics, true, new(TriggerKindManual), boxTestLook)
			pill := boxTestRoutePill(t, m, "skip")
			lane := boxTestEdgeNamed(t, m, "skip")
			top, bottom := pill.Y-metrics.NodeHeight/2, pill.Y+metrics.NodeHeight/2
			for _, node := range m.Nodes {
				top = min(top, node.Y)
				bottom = max(bottom, node.Y+node.Height)
			}
			if top != metrics.PadTop {
				t.Errorf("highest row at %v, want the top padding %v", top, metrics.PadTop)
			}
			if m.Height != bottom-top+metrics.PadTop+metrics.PadBottom {
				t.Errorf("map height %v, want the lowest row %v", m.Height, bottom-top+metrics.PadTop+metrics.PadBottom)
			}
			for _, point := range boxTestFirstSubpath(t, lane.Path) {
				if point.Y < 0 || point.Y > m.Height {
					t.Errorf("track point at y %v is off the map (height %v)", point.Y, m.Height)
				}
			}
		}
	}
}

// TestARouteRunsALeadOfTrackBeforeItsFirstBoxWithItsNameOnIt checks that a route runs a lead of track before its first box with its name on it.
func TestARouteRunsALeadOfTrackBeforeItsFirstBoxWithItsNameOnIt(t *testing.T) {
	t.Parallel()
	belt := boxTestSplitBelt([]boxTestSplitRoute{{ID: "work", Agents: []string{"build"}}, {ID: "skip"}})
	m := BoxLayoutMap(belt.Layout(), BoxMapMetricsRun, true, new(TriggerKindManual), boxTestLook)
	build := boxTestNode(t, m, "work-build")
	var entry BoxMapEdge
	for _, edge := range m.Edges {
		if edge.Route != nil && *edge.Route == "work" && edge.To == "work-build" {
			entry = edge
		}
	}
	points := boxTestFirstSubpath(t, entry.Path)
	spine := points[1].X
	if build.X-spine < BoxMapMetricsRun.NodeWidth {
		t.Errorf("lead is %v, want at least a box (%v)", build.X-spine, BoxMapMetricsRun.NodeWidth)
	}
	if points[len(points)-1].Y != build.Y+build.Height/2 {
		t.Error("the entry does not end at the box's middle")
	}
	pill := boxTestRoutePill(t, m, "work")
	if !(pill.X > spine && pill.X+40 < build.X) {
		t.Errorf("name at x %v is not on the lead (spine %v, box %v)", pill.X, spine, build.X)
	}
	if math.Abs(pill.Y-(build.Y+build.Height/2)) >= 0.5 {
		t.Errorf("name at y %v is not on the box's row", pill.Y)
	}
}

// TestARouteWithoutALeadPutsItsNameOnTheSpine checks that a route without a lead puts its name on the spine.
func TestARouteWithoutALeadPutsItsNameOnTheSpine(t *testing.T) {
	t.Parallel()
	belt := boxTestSplitBelt([]boxTestSplitRoute{{ID: "work", Agents: []string{"build"}}, {ID: "skip"}})
	m := BoxLayoutMap(belt.Layout(), BoxMapMetricsEditor, true, new(TriggerKindManual), boxTestLook)
	var entry BoxMapEdge
	for _, edge := range m.Edges {
		if edge.Route != nil && *edge.Route == "work" && edge.To == "work-build" {
			entry = edge
		}
	}
	spine := boxTestFirstSubpath(t, entry.Path)[1].X
	pill := boxTestRoutePill(t, m, "work")
	if pill.X != spine {
		t.Errorf("name at x %v, want the spine %v", pill.X, spine)
	}
}
