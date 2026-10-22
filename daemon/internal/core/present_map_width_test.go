// The run map's width arithmetic.
package core

import (
	"fmt"
	"math"
	"testing"
)

// TestSpreadKeepsOrderAndCentresOverlaps checks that spread keeps order and centres overlaps.
func TestSpreadKeepsOrderAndCentresOverlaps(t *testing.T) {
	t.Parallel()
	metrics := FloorLayoutMetricsDefault()
	metrics.Gap = 10
	plan := newFloorLayoutPlan(FactoryTrack{}, metrics, func(FactoryTrackMachine) float64 { return 0 })
	if got := plan.spread([]float64{0, 100}, []float64{20, 20}); fmt.Sprint(got) != "[0 100]" {
		t.Errorf("spread = %v, want [0 100]", got)
	}
	if got := plan.spread([]float64{50, 50}, []float64{20, 20}); fmt.Sprint(got) != "[35 65]" {
		t.Errorf("spread = %v, want [35 65]", got)
	}
}

// TestFittingBeltStretchesToViewport checks that fitting belt stretches to viewport.
func TestFittingBeltStretchesToViewport(t *testing.T) {
	t.Parallel()
	width := MapWidth{Columns: 3, Forks: 0, MinimumColumn: 104, Pad: 64}
	if got := width.ContentWidth(900); got != 900 {
		t.Errorf("contentWidth = %v, want 900", got)
	}
	if got := width.Column(900); got != 300 {
		t.Errorf("column = %v, want 300", got)
	}
	if width.MinimumWidth() > 900 {
		t.Error("overflows at 900")
	}
}

// TestCrowdedBeltKeepsMachinesAndBendsAtTheirMinimum checks that crowded belt keeps machines and bends at their minimum.
func TestCrowdedBeltKeepsMachinesAndBendsAtTheirMinimum(t *testing.T) {
	t.Parallel()
	width := MapWidth{Columns: 12, Forks: 2, MinimumColumn: 104, Pad: 64}
	if got := width.MinimumWidth(); got != 1504 {
		t.Errorf("minimumWidth = %v, want 1504", got)
	}
	if got, want := width.ContentWidth(600), float64(12*104+2*2*64); got != want {
		t.Errorf("contentWidth = %v, want %v", got, want)
	}
	if got := width.Column(600); got != 104 {
		t.Errorf("column = %v, want 104", got)
	}
	if width.MinimumWidth() <= 600 {
		t.Error("does not overflow at 600")
	}
}

// TestFittingForkReservesBendsBeforeSharingColumns checks that fitting fork reserves bends before sharing columns.
func TestFittingForkReservesBendsBeforeSharingColumns(t *testing.T) {
	t.Parallel()
	width := MapWidth{Columns: 4, Forks: 1, MinimumColumn: 104, Pad: 64}
	if got := width.Column(928); got != 200 {
		t.Errorf("column = %v, want 200", got)
	}
	if got := width.ContentWidth(928); got != 928 {
		t.Errorf("contentWidth = %v, want 928", got)
	}
	if width.MinimumWidth() > 928 {
		t.Error("overflows at 928")
	}
}

// TestExactFitDoesNotScroll checks that exact fit does not scroll.
func TestExactFitDoesNotScroll(t *testing.T) {
	t.Parallel()
	width := MapWidth{Columns: 4, Forks: 1, MinimumColumn: 104, Pad: 64}
	if got := width.ContentWidth(544); got != 544 {
		t.Errorf("contentWidth(544) = %v", got)
	}
	if got := width.Column(544); got != 104 {
		t.Errorf("column(544) = %v", got)
	}
	if width.MinimumWidth() > 544 {
		t.Error("overflows at 544")
	}
	if width.MinimumWidth() <= 543.5 {
		t.Error("does not overflow at 543.5")
	}
	if got := width.ContentWidth(543.5); got != 544 {
		t.Errorf("contentWidth(543.5) = %v, want 544", got)
	}
}

// TestEachMapUsesItsOwnMinimum checks that each map uses its own minimum.
func TestEachMapUsesItsOwnMinimum(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ minimum, pad float64 }{{104, 64}, {78, 40}, {96, 56}} {
		t.Run(fmt.Sprint(c.minimum), func(t *testing.T) {
			t.Parallel()
			width := MapWidth{Columns: 8, Forks: 1, MinimumColumn: c.minimum, Pad: c.pad}
			if got := width.Column(400); got != c.minimum {
				t.Errorf("column = %v, want %v", got, c.minimum)
			}
			if got, want := width.ContentWidth(400), float64(8)*c.minimum+2*c.pad; got != want {
				t.Errorf("contentWidth = %v, want %v", got, want)
			}
			if width.MinimumWidth() <= 400 {
				t.Error("does not overflow at 400")
			}
			if width.MinimumWidth() > 2000 {
				t.Error("overflows at 2000")
			}
		})
	}
}

// TestEmptyBeltAndZeroViewportStayFinite checks that empty belt and zero viewport stay finite.
func TestEmptyBeltAndZeroViewportStayFinite(t *testing.T) {
	t.Parallel()
	empty := MapWidth{Columns: 0, Forks: 0, MinimumColumn: 104, Pad: 64}
	if got := empty.ContentWidth(600); got != 600 {
		t.Errorf("contentWidth = %v, want 600", got)
	}
	if got := empty.Column(0); got != 104 {
		t.Errorf("column(0) = %v, want 104", got)
	}
	if empty.MinimumWidth() > 0 {
		t.Error("empty belt overflows at 0")
	}
	width := MapWidth{Columns: 2, Forks: 0, MinimumColumn: 104, Pad: 64}
	if got := width.ContentWidth(0); got != 208 {
		t.Errorf("contentWidth(0) = %v, want 208", got)
	}
	if got := width.Column(0); got != 104 {
		t.Errorf("column(0) = %v, want 104", got)
	}
}

// TestAReturnRunsUnderTheMachinesItPassesAndIntoTheEarlierOne checks that a return runs under the machines it passes and into the earlier one.
func TestAReturnRunsUnderTheMachinesItPassesAndIntoTheEarlierOne(t *testing.T) {
	t.Parallel()
	specs := []struct {
		id    string
		names []string
	}{
		{"a", []string{"Build", "Test", "Verify"}},
		{"b", []string{"Verify", "Build"}},
		{"c", []string{"Verify", "Test"}},
	}
	var tracked []FactoryTrackBelt
	for _, spec := range specs {
		base := DefaultsStages(TriggerKindIssues, DefaultsBuiltInAgents(), true)
		stages := make([]Stage, 0, len(spec.names))
		for index, name := range spec.names {
			stage := base[index]
			stage.ID = fmt.Sprintf("%s-%d", spec.id, index)
			stage.Name = name
			stage.Route = nil
			stages = append(stages, stage)
		}
		belt := NewBeltFromTrigger(BeltFromTriggerArgs{ID: spec.id, Name: spec.id, Trigger: Trigger{Kind: TriggerKindManual}, Stages: stages})
		tracked = append(tracked, FactoryTrackBelt{ID: belt.ID, Name: belt.Name, Layout: belt.Layout()})
	}
	track := NewFactoryTrack(tracked)
	if len(track.Returns) != 2 {
		t.Fatalf("returns = %d, want 2", len(track.Returns))
	}
	layout := NewFloorLayout(track, nil, nil, func(FactoryTrackMachine) float64 { return 140 })
	if len(layout.Lanes) != 2 {
		t.Fatalf("lanes = %d, want 2", len(layout.Lanes))
	}
	metrics := FloorLayoutMetricsDefault()
	for _, lane := range layout.Lanes {
		from, to := layout.Box(lane.From), layout.Box(lane.To)
		if from == nil || to == nil {
			t.Fatal("a lane's ends are not placed")
		}
		if lane.Right != from.X+layout.Column/2 {
			t.Errorf("lane right = %v, want %v", lane.Right, from.X+layout.Column/2)
		}
		if lane.Left != to.X-layout.Column/2 {
			t.Errorf("lane left = %v, want %v", lane.Left, to.X-layout.Column/2)
		}
		for _, cell := range layout.Placed {
			if !isMachine(cell.Node) || cell.X < to.X || cell.X > from.X {
				continue
			}
			if !(lane.LabelY >= cell.Top+cell.Height || lane.LabelY <= cell.Top) {
				t.Errorf("lane at %v runs through machine %v (top %v, height %v)", lane.LabelY, cell.Node, cell.Top, cell.Height)
			}
		}
		if lane.LabelY >= layout.Height {
			t.Errorf("lane label %v lies below the layout's height %v", lane.LabelY, layout.Height)
		}
	}
	// Both go back to Build from Verify's column or before, so they overlap and stack.
	if math.Abs(layout.Lanes[0].LabelY-layout.Lanes[1].LabelY) < metrics.Lane {
		t.Errorf("lane rows %v and %v are closer than the lane spacing %v", layout.Lanes[0].LabelY, layout.Lanes[1].LabelY, metrics.Lane)
	}
}
