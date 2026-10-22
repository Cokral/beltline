//

package core

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// mapTestPoint is a MapPoint as the Swift tests write one.
func mapTestPoint(x, y float64) MapPoint {
	return MapPoint{X: x, Y: y}
}

// mapTestLanePoints is a lane's points as MapPoints, for comparison with the Swift literals.
func mapTestLanePoints(lane MapLane) []MapPoint {
	var out []MapPoint
	for _, point := range lane.Points {
		out = append(out, mapTestPoint(point.X, point.Y))
	}
	return out
}

// mapTestFlatten is every subpath's points in order, as Swift's subpaths.flatMap { $0 }.
func mapTestFlatten(subpaths [][]MapPoint) []MapPoint {
	var out []MapPoint
	for _, sub := range subpaths {
		out = append(out, sub...)
	}
	return out
}

// mapTestMetrics is MapMetrics(cell:track:thickness:pad:) with the default rowGap, corner and minColumn.
func mapTestMetrics(cell, track, thickness, pad float64) MapMetrics {
	return NewMapMetrics(MapMetricsArgs{Cell: cell, Track: track, Thickness: &thickness, Pad: &pad})
}

// TestEveryVerticalShiftTurnsAtTheForkEdges checks that every vertical shift turns at the fork edges.
func TestEveryVerticalShiftTurnsAtTheForkEdges(t *testing.T) {
	t.Parallel()
	for _, shift := range []float64{-80, -1, 1, 80} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			t.Parallel()
			lane := NewMapLane(MapLaneArgs{Start: 104, End: 424, MainY: 100, Y: 100 + shift, Thickness: 10})
			want := []MapPoint{mapTestPoint(104, 100), mapTestPoint(104, 100+shift), mapTestPoint(424, 100+shift), mapTestPoint(424, 100)}
			if got := mapTestLanePoints(lane); !reflect.DeepEqual(got, want) {
				t.Errorf("points = %v, want %v", got, want)
			}
		})
	}
}

// TestSameHeightIsStraight checks that same height is straight.
func TestSameHeightIsStraight(t *testing.T) {
	t.Parallel()
	lane := NewMapLane(MapLaneArgs{Start: 104, End: 424, MainY: 100, Y: 100, Thickness: 8})
	want := []MapPoint{mapTestPoint(104, 100), mapTestPoint(424, 100)}
	if got := mapTestLanePoints(lane); !reflect.DeepEqual(got, want) {
		t.Errorf("points = %v, want %v", got, want)
	}
}

// TestLabelClearsTheCorner checks that label clears the corner.
func TestLabelClearsTheCorner(t *testing.T) {
	t.Parallel()
	for _, thickness := range []float64{8, 10} {
		t.Run(fmt.Sprint(thickness), func(t *testing.T) {
			t.Parallel()
			lane := NewMapLane(MapLaneArgs{Start: 104, End: 424, MainY: 100, Y: 20, Thickness: thickness})
			if want := 104 + thickness/2 + 4; lane.LabelX != want {
				t.Errorf("labelX = %v, want %v", lane.LabelX, want)
			}
			// The cell is a variable so that cell * 0.28 rounds at run time, as Swift's Double product does.
			cell, column := 178.0, 104.0
			want := 160 + column/2 - float64(cell*0.28) - lane.LabelX
			if got := lane.Room(160, column, cell); got != want {
				t.Errorf("room = %v, want %v", got, want)
			}
		})
	}
}

// TestSharedGeometryKeepsRoundedLanesAtForkEdges checks that shared geometry keeps rounded lanes at fork edges.
func TestSharedGeometryKeepsRoundedLanesAtForkEdges(t *testing.T) {
	t.Parallel()
	for _, pad := range []float64{40, 56, 64} {
		t.Run(fmt.Sprint(pad), func(t *testing.T) {
			t.Parallel()
			belt := forkedBeltWith([]string{"build"})
			metrics := mapTestMetrics(178, 117, 10, pad)
			plan := NewMapPlan(belt.Layout(), metrics)
			geometry := plan.Geometry(plan.Width().MinimumWidth())
			if len(geometry.Forks) == 0 {
				t.Fatal("no fork")
			}
			fork := geometry.Forks[0]
			line := mapTestFlatten(geometry.Line.Subpaths)
			if !slices.Contains(line, mapTestPoint(fork.Start, geometry.MainY)) {
				t.Error("the line does not reach the fork's start")
			}
			if !slices.Contains(line, mapTestPoint(fork.End, geometry.MainY)) {
				t.Error("the line does not reach the fork's end")
			}
			for _, lane := range geometry.Lanes {
				points := boxTestFirstSubpath(t, lane.Path)
				if points[0] != mapTestPoint(fork.Start-metrics.Corner, geometry.MainY) {
					t.Errorf("lane starts at %v", points[0])
				}
				if points[len(points)-1] != mapTestPoint(fork.End+metrics.Corner, geometry.MainY) {
					t.Errorf("lane ends at %v", points[len(points)-1])
				}
				if len(points) != 6 {
					t.Errorf("lane has %d points, want 6", len(points))
				}
				if lane.Path.CornerRadius != metrics.Corner {
					t.Errorf("corner radius = %v", lane.Path.CornerRadius)
				}
				if lane.LabelX != fork.Start+metrics.Thickness/2+4 {
					t.Errorf("labelX = %v", lane.LabelX)
				}
				if want := fork.Start + pad + geometry.Column/2 - float64(metrics.Cell*0.28) - lane.LabelX; lane.Room != want {
					t.Errorf("room = %v, want %v", lane.Room, want)
				}
				filled := geometry.Path(map[string][]string{fork.ID: {lane.Route.ID}})
				want := []MapPath{}
				for _, sub := range geometry.Line.Subpaths {
					want = append(want, NewMapPathPoints(sub, 0))
				}
				want = append(want, lane.Path)
				if !reflect.DeepEqual(filled, want) {
					t.Errorf("filled path = %v, want %v", filled, want)
				}
			}
			var quick *MapGeometryPlaced
			for index := range geometry.Cells {
				if geometry.Cells[index].Stage.ID == "quick-build" {
					quick = &geometry.Cells[index]
				}
			}
			if quick == nil {
				t.Fatal("no quick-build cell")
			}
			fill := "quick-build"
			crate := geometry.Crate(&fill, false, map[string][]string{fork.ID: {"simple"}})
			if crate != mapTestPoint(quick.X, geometry.Lanes[len(geometry.Lanes)-1].Y) {
				t.Errorf("crate = %v", crate)
			}
		})
	}
}

// TestEmptyRouteKeepsThePopulatedRouteTrackShape checks that empty route keeps the populated route track shape.
func TestEmptyRouteKeepsThePopulatedRouteTrackShape(t *testing.T) {
	t.Parallel()
	geometry := func(empty bool) MapGeometry {
		simple := []string{"build"}
		if empty {
			simple = nil
		}
		belt := forkedBeltWith(simple)
		plan := NewMapPlan(belt.Layout(), NewMapMetrics(MapMetricsArgs{Cell: 178, Track: 117}))
		return plan.Geometry(plan.Width().MinimumWidth())
	}
	populated := geometry(false).Lanes
	empty := geometry(true).Lanes
	if len(populated) == 0 || len(empty) == 0 {
		t.Fatal("no lanes")
	}
	if !reflect.DeepEqual(empty[len(empty)-1].Path, populated[len(populated)-1].Path) {
		t.Errorf("empty lane path %v, want %v", empty[len(empty)-1].Path, populated[len(populated)-1].Path)
	}
}

// TestSingleRouteForkIsStraight checks that single route fork is straight.
func TestSingleRouteForkIsStraight(t *testing.T) {
	t.Parallel()
	belt := forkedBeltWith(nil)
	belt.Forks[0].Routes = belt.Forks[0].Routes[:len(belt.Forks[0].Routes)-1]
	plan := NewMapPlan(belt.Layout(), NewMapMetrics(MapMetricsArgs{Cell: 178, Track: 117}))
	geometry := plan.Geometry(plan.Width().MinimumWidth())
	if len(geometry.Forks) == 0 || len(geometry.Lanes) == 0 {
		t.Fatal("no fork or lane")
	}
	fork := geometry.Forks[0]
	points := boxTestFirstSubpath(t, geometry.Lanes[0].Path)
	want := []MapPoint{mapTestPoint(fork.Start, geometry.MainY), mapTestPoint(fork.End, geometry.MainY)}
	if !reflect.DeepEqual(points, want) {
		t.Errorf("points = %v, want %v", points, want)
	}
}

// TestALaneThatDoesNotOpenStartsOnItsRow checks that a lane that does not open starts on its row.
func TestALaneThatDoesNotOpenStartsOnItsRow(t *testing.T) {
	t.Parallel()
	opens := false
	lane := NewMapLane(MapLaneArgs{Start: 100, End: 400, MainY: 100, Y: 180, Thickness: 8, Opens: &opens})
	want := []MapPoint{mapTestPoint(104, 180), mapTestPoint(400, 180), mapTestPoint(400, 100)}
	if got := mapTestLanePoints(lane); !reflect.DeepEqual(got, want) {
		t.Errorf("points = %v, want %v", got, want)
	}
	if lane.Opens || !lane.Joins {
		t.Errorf("opens = %v, joins = %v; want false and true", lane.Opens, lane.Joins)
	}
}

// TestALaneThatDoesNotJoinStopsOnItsRow checks that a lane that does not join stops on its row.
func TestALaneThatDoesNotJoinStopsOnItsRow(t *testing.T) {
	t.Parallel()
	stop := 300.0
	lane := NewMapLane(MapLaneArgs{Start: 100, End: 400, MainY: 100, Y: 180, Thickness: 8, Stop: &stop})
	want := []MapPoint{mapTestPoint(100, 100), mapTestPoint(100, 180), mapTestPoint(300, 180)}
	if got := mapTestLanePoints(lane); !reflect.DeepEqual(got, want) {
		t.Errorf("points = %v, want %v", got, want)
	}
	flat := NewMapLane(MapLaneArgs{Start: 100, End: 400, MainY: 100, Y: 100, Thickness: 8, Stop: &stop})
	wantFlat := []MapPoint{mapTestPoint(100, 100), mapTestPoint(300, 100)}
	if got := mapTestLanePoints(flat); !reflect.DeepEqual(got, wantFlat) {
		t.Errorf("flat points = %v, want %v", got, wantFlat)
	}
}
