//

package core

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// mapTestStageIDs is the sorted stage IDs of a belt.
func mapTestStageIDs(stages []Stage) []string {
	var ids []string
	for _, stage := range stages {
		ids = append(ids, stage.ID)
	}
	sort.Strings(ids)
	return ids
}

// mapTestCell is the cell of stageID, failing the test when there is none.
func mapTestCell(t *testing.T, geometry MapGeometry, stageID string) MapGeometryPlaced {
	t.Helper()
	for _, cell := range geometry.Cells {
		if cell.Stage.ID == stageID {
			return cell
		}
	}
	t.Fatalf("no cell for %q", stageID)
	return MapGeometryPlaced{}
}

// TestWidthsChooseCompactMetricsAndPreserveMinimumContent checks that widths choose compact metrics and preserve minimum content.
func TestWidthsChooseCompactMetricsAndPreserveMinimumContent(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		width   float64
		compact bool
	}{{420, true}, {620, true}, {621, false}, {1080, false}} {
		t.Run(fmt.Sprint(c.width), func(t *testing.T) {
			t.Parallel()
			if got := RunLayoutCompact(c.width); got != c.compact {
				t.Fatalf("compact(%v) = %v", c.width, got)
			}
			metrics := MapMetricsRun(c.compact)
			belt := forkedBeltWith([]string{"build"})
			plan := NewMapPlan(belt.Layout(), metrics)
			if metrics.MinColumn != MapWidthFloorMinimumColumn {
				t.Errorf("minColumn = %v", metrics.MinColumn)
			}
			contentWidth := plan.Width().ContentWidth(c.width)
			if contentWidth < c.width {
				t.Errorf("contentWidth %v is narrower than the viewport", contentWidth)
			}
			if plan.Width().Column(c.width) < metrics.MinColumn {
				t.Errorf("column is narrower than the minimum")
			}
			geometry := plan.Geometry(contentWidth)
			if geometry.Width != contentWidth {
				t.Errorf("geometry width = %v, want %v", geometry.Width, contentWidth)
			}
			var got []string
			for _, cell := range geometry.Cells {
				got = append(got, cell.Stage.ID)
			}
			sort.Strings(got)
			if want := mapTestStageIDs(belt.Stages); !reflect.DeepEqual(got, want) {
				t.Errorf("cells = %v, want %v", got, want)
			}
			for _, cell := range geometry.Cells {
				if math.IsNaN(cell.X) || math.IsInf(cell.X, 0) || math.IsNaN(cell.Top) || math.IsInf(cell.Top, 0) {
					t.Errorf("cell %s is not finite", cell.Stage.ID)
				}
			}
		})
	}
}

// TestStraightMapPlacesEveryStageOnOneRowAndUsesAvailableWidth checks that straight map places every stage on one row and uses available width.
func TestStraightMapPlacesEveryStageOnOneRowAndUsesAvailableWidth(t *testing.T) {
	t.Parallel()
	belt := boxTestStraightBelt()
	plan := NewMapPlan(belt.Layout(), MapMetricsRun(false))
	if plan.Forks() != 0 {
		t.Errorf("forks = %d, want 0", plan.Forks())
	}
	if plan.Columns() != len(belt.Stages) {
		t.Errorf("columns = %d, want %d", plan.Columns(), len(belt.Stages))
	}
	geometry := plan.Geometry(1080)
	if geometry.Width != 1080 {
		t.Errorf("width = %v, want 1080", geometry.Width)
	}
	tops := map[float64]bool{}
	var numbers []int
	for _, cell := range geometry.Cells {
		tops[cell.Top] = true
		numbers = append(numbers, cell.Number)
	}
	if len(tops) != 1 {
		t.Errorf("stages sit on %d rows, want 1", len(tops))
	}
	var want []int
	for index := range belt.Stages {
		want = append(want, index+1)
	}
	if !reflect.DeepEqual(numbers, want) {
		t.Errorf("numbers = %v, want %v", numbers, want)
	}
	if len(geometry.Line.Subpaths) != 1 {
		t.Fatalf("line subpaths = %d, want 1", len(geometry.Line.Subpaths))
	}
	if first := geometry.Line.Subpaths[0][0]; first != mapTestPoint(0, geometry.MainY) {
		t.Errorf("line starts at %v", first)
	}
	line := geometry.Line.Subpaths[0]
	if last := line[len(line)-1]; last != mapTestPoint(1080, geometry.MainY) {
		t.Errorf("line ends at %v", last)
	}
}

// TestNestedForkBandsStayRelativeToTheirParentRoute checks that nested fork bands stay relative to their parent route.
func TestNestedForkBandsStayRelativeToTheirParentRoute(t *testing.T) {
	t.Parallel()
	metrics := MapMetricsRun(false)
	plan := NewMapPlan(nfBelt(false, true, true).Layout(), metrics)
	if plan.Forks() != 2 {
		t.Fatalf("forks = %d, want 2", plan.Forks())
	}
	if got, want := plan.Width().MinimumWidth(), float64(plan.Columns())*metrics.MinColumn+4*metrics.Pad; got != want {
		t.Errorf("minimumWidth = %v, want %v", got, want)
	}
	geometry := plan.Geometry(plan.Width().MinimumWidth())
	assess := mapTestCell(t, geometry, "assess")
	quick := mapTestCell(t, geometry, "quick-build")
	thoroughSpec := mapTestCell(t, geometry, "thorough-spec")
	verify := mapTestCell(t, geometry, "verify")
	centre := func(placed MapGeometryPlaced) float64 { return placed.Top + metrics.Cell/2 }
	nestedCentre := (centre(quick) + centre(thoroughSpec)) / 2
	if math.Abs(centre(assess)-nestedCentre) >= 0.01 {
		t.Errorf("assess centre %v is not the nested centre %v", centre(assess), nestedCentre)
	}
	if math.Abs(centre(verify)-nestedCentre) >= 0.01 {
		t.Errorf("verify centre %v is not the nested centre %v", centre(verify), nestedCentre)
	}
	if quick.Top+metrics.Cell+metrics.RowGap > thoroughSpec.Top {
		t.Errorf("quick-build overlaps thorough-spec")
	}
}

// TestCrateLocationFollowsTheStageRouteAndTheFinishedRun checks that crate location follows the stage route and the finished run.
func TestCrateLocationFollowsTheStageRouteAndTheFinishedRun(t *testing.T) {
	t.Parallel()
	belt := forkedBeltWith([]string{"build"})
	plan := NewMapPlan(belt.Layout(), MapMetricsRun(false))
	geometry := plan.Geometry(plan.Width().MinimumWidth())
	triage := "triage"
	main := mapTestCell(t, geometry, triage)
	if got := geometry.Crate(&triage, false, map[string][]string{}); got != mapTestPoint(main.X, geometry.MainY) {
		t.Errorf("crate on triage = %v", got)
	}
	build := "quick-build"
	build2 := mapTestCell(t, geometry, build)
	simpleLane := MapGeometryLane{}
	for _, lane := range geometry.Lanes {
		if lane.Route.ID == "simple" {
			simpleLane = lane
		}
	}
	if got := geometry.Crate(&build, false, map[string][]string{"size": {"simple"}}); got != mapTestPoint(build2.X, simpleLane.Y) {
		t.Errorf("crate on the simple route = %v, want y %v", got, simpleLane.Y)
	}
	if got := geometry.Crate(nil, false, map[string][]string{}); got != mapTestPoint(21, geometry.MainY) {
		t.Errorf("crate with no stage = %v", got)
	}
	if got := geometry.Crate(&triage, true, map[string][]string{}); got != mapTestPoint(geometry.Width-21, geometry.MainY) {
		t.Errorf("crate when finished = %v", got)
	}
}

// TestTheMapDrawsEveryTakenLaneAndACrateForEachLeg checks that the map draws every taken lane and a crate for each leg.
func TestTheMapDrawsEveryTakenLaneAndACrateForEachLeg(t *testing.T) {
	t.Parallel()
	run := engineStartRun(engineBothRoutes())
	engineFinishOutcome(&run, "ready to implement")
	layout := run.Layout()
	plan := NewMapPlan(run.Layout(), NewMapMetrics(MapMetricsArgs{Cell: 178, Track: 117})).Geometry(1000)
	routes := run.Routes()
	path := plan.Path(routes)
	var taken []MapGeometryLane
	for _, lane := range plan.Lanes {
		if slices.Contains(routes["size"], lane.Route.ID) {
			taken = append(taken, lane)
		}
	}
	if len(taken) != 2 {
		t.Fatalf("taken lanes = %d, want 2", len(taken))
	}
	if len(path) != len(plan.Line.Subpaths)+len(taken) {
		t.Errorf("path has %d tracks, want %d", len(path), len(plan.Line.Subpaths)+len(taken))
	}
	found := slices.ContainsFunc(layout.Path(routes, false), func(stage Stage) bool {
		return stage.ID == "quick-build"
	})
	if !found {
		t.Error("the run's path does not include quick-build")
	}
	fill := "quick-build"
	crate := plan.Crate(&fill, false, routes)
	var simpleY float64
	for _, lane := range plan.Lanes {
		if lane.Route.ID == "simple" {
			simpleY = lane.Y
			break
		}
	}
	if crate.Y != simpleY {
		t.Errorf("crate y = %v, want the simple lane's %v", crate.Y, simpleY)
	}
}
