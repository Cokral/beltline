// Tests of the floor layout placement: the belt fixture and the
// tests that place a track. The path-data tests are in present_floor_layout_test.go.

package core

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// floorTestBelt is the default belt on acme/<id>, named by id.
func floorTestBelt(id string) OverviewBelt {
	source := DefaultsBelt([]string{"acme/" + id}, "agent", DefaultsBuiltInAgents(), true)
	source.ID = id
	source.Name = floorTestCapitalized(id)
	return OverviewBeltFromBelt(source, false, false)
}

// floorTestCapitalized capitalizes the one-word ids of these tests.
func floorTestCapitalized(id string) string {
	if id == "" {
		return id
	}
	return strings.ToUpper(id[:1]) + strings.ToLower(id[1:])
}

// floorTestFinite reports whether v is a finite number.
func floorTestFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// floorTestPlace places track with every machine cell 140 points high and the minimum columns.
func floorTestPlace(track FactoryTrack) FloorLayout {
	return NewFloorLayout(track, nil, nil, func(FactoryTrackMachine) float64 { return 140 })
}

// floorTestNodeCount is the number of nodes in track's layers.
func floorTestNodeCount(track FactoryTrack) int {
	count := 0
	for _, layer := range track.Layers {
		count += len(layer)
	}
	return count
}

// TestEveryNodeAndStrokeHasFiniteBoundsAtTheMinimumColumn
func TestEveryNodeAndStrokeHasFiniteBoundsAtTheMinimumColumn(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues"), floorTestBelt("docs"), floorTestBelt("reviews")})
	layout := floorTestPlace(track)
	if layout.Column != FloorLayoutMetricsDefault().MinColumn {
		t.Errorf("column = %v, want the minimum column", layout.Column)
	}
	if !floorTestFinite(layout.Width) || !floorTestFinite(layout.Height) {
		t.Errorf("width %v and height %v must be finite", layout.Width, layout.Height)
	}
	if layout.Width < FloorLayoutMinimumWidth(track, nil) {
		t.Errorf("width %v is below the minimum width %v", layout.Width, FloorLayoutMinimumWidth(track, nil))
	}
	if len(layout.Placed) != floorTestNodeCount(track) {
		t.Errorf("placed %d nodes, want %d", len(layout.Placed), floorTestNodeCount(track))
	}
	for _, box := range layout.Placed {
		if !floorTestFinite(box.X) || !floorTestFinite(box.Top) || !floorTestFinite(box.Height) || !floorTestFinite(box.Anchor) {
			t.Errorf("box %+v has a non-finite bound", box)
		}
		if box.Top < 0 || box.Top+box.Height > layout.Height {
			t.Errorf("box %+v lies outside the layout's height %v", box, layout.Height)
		}
	}
	for _, stroke := range layout.Tracks {
		if !strings.HasPrefix(stroke.Path, "M ") {
			t.Errorf("stroke path %q is not path data", stroke.Path)
		}
		if stroke.Width <= 0 {
			t.Errorf("stroke width = %v, want positive", stroke.Width)
		}
		for _, id := range stroke.Belts {
			if track.Belt(id) == nil {
				t.Errorf("stroke names belt %q, which the track lacks", id)
			}
		}
	}
}

// TestEveryTrackIsDrawnAtOneWidthHoweverManyBeltsShareIt
func TestEveryTrackIsDrawnAtOneWidthHoweverManyBeltsShareIt(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues"), floorTestBelt("docs"), floorTestBelt("reviews")})
	layout := floorTestPlace(track)
	shared := slices.ContainsFunc(layout.Tracks, func(stroke FloorLayoutStroke) bool {
		return len(stroke.Belts) > 1
	})
	if !shared {
		t.Fatal("the fixture has stretches several belts share")
	}
	for _, stroke := range layout.Tracks {
		if stroke.Width != FloorLayoutMetricsDefault().Thickness {
			t.Errorf("stroke width = %v, want the one track thickness %v: a shared stretch is not drawn heavier", stroke.Width, FloorLayoutMetricsDefault().Thickness)
		}
	}
}

// TestASharedStageIsOneMachineWithEveryBeltThatRunsIt
func TestASharedStageIsOneMachineWithEveryBeltThatRunsIt(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues"), floorTestBelt("docs")})
	var shared []FactoryTrackMachine
	for _, machine := range track.Machines {
		if len(machine.Belts()) > 1 {
			shared = append(shared, machine)
		}
	}
	if len(shared) == 0 {
		t.Fatal("belts with the same stage names share their machines")
	}
	for _, machine := range shared {
		want := map[string]struct{}{}
		for _, member := range machine.Members {
			want[member.BeltID] = struct{}{}
		}
		got := map[string]struct{}{}
		for _, id := range machine.Belts() {
			got[id] = struct{}{}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("machine %s belts = %v, want the members' belts %v", machine.ID, got, want)
		}
	}
	layout := floorTestPlace(track)
	for _, machine := range shared {
		node := FactoryTrackMachineNode(machine.ID)
		count := 0
		for _, placed := range layout.Placed {
			if placed.Node == node {
				count++
			}
		}
		if count != 1 {
			t.Errorf("shared machine %s is placed %d times, want once", machine.ID, count)
		}
	}
}

// TestAMachinesHeightFollowsItsDrawnChips
func TestAMachinesHeightFollowsItsDrawnChips(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues")})
	machine := track.Machines[0]
	runs := make([]Run, 0, 6)
	for range 6 {
		runs = append(runs, NewRun(RunArgs{Belt: DefaultsBelt([]string{"acme/x"}, "agent", DefaultsBuiltInAgents(), true),
			Subject: DefaultSubject("acme/x", "t"), Now: engineT0}))
	}
	mixed := 0.0
	if machine.Mixed() {
		mixed = 26
	}
	full := 140 + mixed + float64(OverviewFloorChipLimit)*26
	if got := FactoryCellHeight(machine, runs); got != full {
		t.Errorf("height with six runs = %v, want %v", got, full)
	}
	if got := FactoryCellHeight(machine, nil); got != 140+mixed {
		t.Errorf("height with no runs = %v, want %v", got, 140+mixed)
	}
	layout := NewFloorLayout(track, nil, nil, func(m FactoryTrackMachine) float64 { return FactoryCellHeight(m, runs) })
	found := slices.ContainsFunc(layout.Placed, func(placed FloorLayoutPlaced) bool {
		return placed.Height == full
	})
	if !found {
		t.Errorf("no placed node has the height %v of a machine with six runs", full)
	}
}

// TestALayoutWithNoBeltsIsEmptyButFinite
func TestALayoutWithNoBeltsIsEmptyButFinite(t *testing.T) {
	t.Parallel()
	layout := floorTestPlace(OverviewPresentationTrack(nil))
	if len(layout.Placed) != 0 || len(layout.Tracks) != 0 || len(layout.Lanes) != 0 {
		t.Errorf("an empty track places %d nodes, %d tracks, %d lanes; want none", len(layout.Placed), len(layout.Tracks), len(layout.Lanes))
	}
	if !floorTestFinite(layout.Height) || layout.Height < 0 {
		t.Errorf("height = %v, want finite and not negative", layout.Height)
	}
}

// TestLanesLieBelowTheirLabelsAndCarryTheirBelts
func TestLanesLieBelowTheirLabelsAndCarryTheirBelts(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues"), floorTestBelt("docs")})
	layout := floorTestPlace(track)
	for _, lane := range layout.Lanes {
		if !floorTestFinite(lane.LabelX) || !floorTestFinite(lane.LabelY) {
			t.Errorf("lane label (%v, %v) is not finite", lane.LabelX, lane.LabelY)
		}
		if !strings.HasPrefix(lane.Path, "M ") {
			t.Errorf("lane path %q is not path data", lane.Path)
		}
		if !strings.HasPrefix(lane.Arrow, "M ") {
			t.Errorf("lane arrow %q is not path data", lane.Arrow)
		}
		if len(lane.Belts) == 0 {
			t.Error("a lane carries no belts")
		}
	}
}

// TestAWiderViewSpreadsTheColumnsAndANarrowerOneKeepsTheMinimum
func TestAWiderViewSpreadsTheColumnsAndANarrowerOneKeepsTheMinimum(t *testing.T) {
	t.Parallel()
	track := OverviewPresentationTrack([]OverviewBelt{floorTestBelt("issues"), floorTestBelt("docs")})
	minimum := FloorLayoutMinimumWidth(track, nil)
	wideWidth := minimum + 400
	wide := NewFloorLayout(track, nil, &wideWidth, func(FactoryTrackMachine) float64 { return 140 })
	if wide.Column <= FloorLayoutMetricsDefault().MinColumn {
		t.Errorf("wide column = %v, want more than the minimum %v", wide.Column, FloorLayoutMetricsDefault().MinColumn)
	}
	if math.Abs(wide.Width-(minimum+400)) >= 0.001 {
		t.Errorf("wide width = %v, want %v", wide.Width, minimum+400)
	}
	narrowWidth := minimum - 400
	narrow := NewFloorLayout(track, nil, &narrowWidth, func(FactoryTrackMachine) float64 { return 140 })
	if narrow.Column != FloorLayoutMetricsDefault().MinColumn {
		t.Errorf("narrow column = %v, want the minimum %v", narrow.Column, FloorLayoutMetricsDefault().MinColumn)
	}
	if !reflect.DeepEqual(narrow, floorTestPlace(track)) {
		t.Error("a narrower view than the minimum places the same layout as no width")
	}
}
