// The run's belt as machines on a track: the machine map's geometry, in points.
package api

import (
	"slices"
	"strconv"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// RunMapWire is the renderer-independent map geometry, in points. `y` in a
// placement is the top of its cell; route spans identify where each fork leaves
// and rejoins.
type RunMapWire struct {
	AvailableWidth      float64                `json:"availableWidth"`
	ContentWidth        float64                `json:"contentWidth"`
	MinimumContentWidth float64                `json:"minimumContentWidth"`
	Compact             bool                   `json:"compact"`
	Column              float64                `json:"column"`
	Height              float64                `json:"height"`
	MainY               float64                `json:"mainY"`
	Placements          []RunMapWirePlacement  `json:"placements"`
	Tracks              []RunMapWireTrackPath  `json:"tracks"`
	FilledTrackPathIDs  []string               `json:"filledTrackPathIds"`
	FillStageID         *string                `json:"fillStageId,omitzero"`
	FillUntilX          float64                `json:"fillUntilX"`
	Finished            bool                   `json:"finished"`
	Forks               []RunMapWireForkSpan   `json:"forks"`
	RouteLabels         []RunMapWireRouteLabel `json:"routeLabels"`
	Crates              []RunMapWireCrate      `json:"crates"`
	// Caption is the caption over the map: where the run is now, or what it shipped as.
	Caption string `json:"caption"`
	// Moving says whether the filled track's stripes move: the run is working and nothing pauses it.
	Moving bool `json:"moving"`
}

// NewRunMapWire is the machine map of a run view snapshot.
// Track paths are the main line's subpaths then one per lane; the filled track
// ids are the main subpaths plus the lanes on the run's selected path.
func NewRunMapWire(snapshot coordinator.CoordinatorRunViewSnapshot) RunMapWire {
	run := snapshot.Run
	geometry := snapshot.Map
	compact := snapshot.Presentation.Compact
	metrics := core.MapMetricsRun(compact)
	plan := core.NewMapPlan(run.Layout(), metrics)
	routes := run.Routes()
	fill := geometry.RunFill(run, routes, snapshot.Pause.IsPaused || snapshot.BeltPaused)
	routeTracks, routeLabels := runMapWireRoutes(plan, geometry, routes)
	stageID := run.StageID
	return RunMapWire{
		AvailableWidth: snapshot.AvailableWidth,
		ContentWidth:   snapshot.MapContentWidth,
		MinimumContentWidth: core.MapWidth{
			Columns:       plan.Columns(),
			Forks:         plan.Forks(),
			MinimumColumn: metrics.MinColumn,
			Pad:           metrics.Pad,
		}.MinimumWidth(),
		Compact:            compact,
		Column:             geometry.Column,
		Height:             geometry.Height,
		MainY:              geometry.MainY,
		Placements:         runMapWirePlacements(geometry, metrics.Cell, snapshot.Presentation.Stages),
		Tracks:             append(runMapWireMainTracks(geometry), routeTracks...),
		FilledTrackPathIDs: runMapWireFilledIDs(geometry, fill),
		FillStageID:        &stageID,
		FillUntilX:         fill.UntilX,
		Finished:           fill.Finished,
		Forks:              runMapWireForks(geometry),
		// RouteLabels are the names of the fork routes, beside their tracks.
		RouteLabels: routeLabels,
		Crates:      runMapWireCrates(run, geometry, routes, metrics.Thickness),
		Caption:     core.RunPresentationCaption(run, snapshot.AmbiguousRepositories),
		Moving:      fill.Moving,
	}
}

// runMapWirePlacements is one cell per stage, centred on its column. A cell is
// dimmed when its stage panel's step is one the map dims; a stage with no panel is not.
func runMapWirePlacements(geometry core.MapGeometry, height float64, panels []core.RunPresentationStagePanel) []RunMapWirePlacement {
	dimmed := map[string]bool{}
	for _, panel := range panels {
		dimmed[panel.ID] = panel.Step.Dimmed()
	}
	out := []RunMapWirePlacement{}
	for _, cell := range geometry.Cells {
		out = append(out, RunMapWirePlacement{
			StageID: cell.Stage.ID,
			Number:  cell.Number,
			X:       cell.X - geometry.Column/2,
			Y:       cell.Top,
			Width:   geometry.Column,
			Height:  height,
			Dimmed:  dimmed[cell.Stage.ID],
		})
	}
	return out
}

// runMapWireMainTracks is the main line, one track per subpath.
func runMapWireMainTracks(geometry core.MapGeometry) []RunMapWireTrackPath {
	out := []RunMapWireTrackPath{}
	for index, subpath := range geometry.Line.Subpaths {
		out = append(out, RunMapWireTrackPath{
			ID:           "main-" + strconv.Itoa(index),
			Kind:         "main",
			Subpaths:     [][]RunMapWirePoint{mapWirePoints(subpath)},
			CornerRadius: geometry.Line.CornerRadius,
		})
	}
	return out
}

// runMapWireRoutes is one track per fork route, and a route label beside each
// track except the line's own, which a feeding fork's first route carries.
func runMapWireRoutes(plan core.MapPlan, geometry core.MapGeometry, routes map[string][]string) ([]RunMapWireTrackPath, []RunMapWireRouteLabel) {
	tracks := []RunMapWireTrackPath{}
	labels := []RunMapWireRouteLabel{}
	for _, lane := range geometry.Lanes {
		forkID := lane.Fork
		routeID := lane.Route.ID
		var taken *bool
		if routeIDs, ok := routes[lane.Fork]; ok {
			value := slices.Contains(routeIDs, lane.Route.ID)
			taken = &value
		}
		subpaths := make([][]RunMapWirePoint, 0, len(lane.Path.Subpaths))
		for _, subpath := range lane.Path.Subpaths {
			subpaths = append(subpaths, mapWirePoints(subpath))
		}
		tracks = append(tracks, RunMapWireTrackPath{
			ID:           "route:" + lane.Fork + ":" + lane.Route.ID,
			Kind:         "route",
			ForkID:       &forkID,
			RouteID:      &routeID,
			Taken:        taken,
			Subpaths:     subpaths,
			CornerRadius: lane.Path.CornerRadius,
		})
		if plan.IsLine(lane, geometry) {
			continue
		}
		labels = append(labels, RunMapWireRouteLabel{
			ID:      lane.Fork + ":" + lane.Route.ID,
			ForkID:  lane.Fork,
			RouteID: lane.Route.ID,
			Text:    lane.Route.Name,
			X:       lane.LabelX,
			Y:       lane.Y - 10,
			Room:    lane.Room,
			Taken:   taken,
		})
	}
	return tracks, labels
}

// runMapWireFilledIDs is the main subpaths plus the lanes on the run's selected path.
func runMapWireFilledIDs(geometry core.MapGeometry, fill core.MapFill) []string {
	var ids []string
	for index := range geometry.Line.Subpaths {
		ids = append(ids, "main-"+strconv.Itoa(index))
	}
	for _, lane := range fill.Lanes {
		ids = append(ids, "route:"+lane.Fork+":"+lane.Route.ID)
	}
	return ids
}

// runMapWireForks is the horizontal span of each fork.
func runMapWireForks(geometry core.MapGeometry) []RunMapWireForkSpan {
	out := []RunMapWireForkSpan{}
	for _, span := range geometry.Forks {
		out = append(out, RunMapWireForkSpan{ID: span.ID, Start: span.Start, End: span.End})
	}
	return out
}

// runMapWireCrates is the run's crates on the map.
func runMapWireCrates(run core.Run, geometry core.MapGeometry, routes map[string][]string, thickness float64) []RunMapWireCrate {
	out := []RunMapWireCrate{}
	for _, crate := range geometry.RunCrates(run, routes, thickness) {
		out = append(out, RunMapWireCrate{
			ID:        crate.ID,
			Reference: crate.Reference,
			State:     crate.State,
			StageID:   crate.StageID,
			X:         crate.X,
			Y:         crate.Y,
		})
	}
	return out
}

// mapWirePoints maps a subpath to its wire points; an empty subpath is an empty array.
func mapWirePoints(points []core.MapPoint) []RunMapWirePoint {
	out := make([]RunMapWirePoint, 0, len(points))
	for _, point := range points {
		out = append(out, NewRunMapWirePoint(point))
	}
	return out
}

// RunMapWirePlacement is one stage cell of the map. Dimmed is set for a stage
// the run has not reached or a route skipped.
type RunMapWirePlacement struct {
	StageID string  `json:"stageId"`
	Number  int     `json:"number"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Dimmed  bool    `json:"dimmed"`
}

// RunMapWirePoint is a point of the map in points.
type RunMapWirePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NewRunMapWirePoint is a map point on the wire.
func NewRunMapWirePoint(point core.MapPoint) RunMapWirePoint {
	return RunMapWirePoint{X: point.X, Y: point.Y}
}

// RunMapWireTrackPath is one drawn track: the main line or a fork route.
// ForkID, RouteID and Taken are set for route tracks only.
type RunMapWireTrackPath struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	ForkID       *string             `json:"forkId,omitzero"`
	RouteID      *string             `json:"routeId,omitzero"`
	Taken        *bool               `json:"taken,omitzero"`
	Subpaths     [][]RunMapWirePoint `json:"subpaths"`
	CornerRadius float64             `json:"cornerRadius"`
}

// RunMapWireForkSpan is the horizontal span of a fork on the map.
type RunMapWireForkSpan struct {
	ID    string  `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// RunMapWireRouteLabel is the name of a fork route, drawn beside its track.
type RunMapWireRouteLabel struct {
	ID      string  `json:"id"`
	ForkID  string  `json:"forkId"`
	RouteID string  `json:"routeId"`
	Text    string  `json:"text"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Room    float64 `json:"room"`
	Taken   *bool   `json:"taken,omitzero"`
}

// RunMapWireCrate is the run's crate (its reference box) on the map. StageID is omitted when none.
type RunMapWireCrate struct {
	ID        string  `json:"id"`
	Reference string  `json:"reference"`
	State     string  `json:"state"`
	StageID   *string `json:"stageId,omitzero"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
}
