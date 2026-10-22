package core

import "strconv"

// assemble turns the lines into segments with a travel state, and the stations and labels into the wire
// shape.
func (b runMetroLayoutBuilder) assemble(pill RunMetroPill, terminus, lineY float64) RunMetro {
	segments, ends := b.lineSegments(terminus, lineY, b.currentStationX())
	var stations []RunMetroStation
	for _, placed := range b.Placed {
		stations = append(stations, b.station(placed))
	}
	return RunMetro{
		Width:    b.mapWidth(pill, terminus, stations) + 12,
		Height:   b.mapBottom(lineY) + 52,
		Colour:   MetroLayoutColourIndex(b.Run.BeltID),
		Trigger:  pill,
		Segments: segments,
		Stations: stations,
		Routes:   b.Tags,
		Ends:     ends,
	}
}

// currentStationX is the x of the stage the run is at, or nil when no stage is current.
func (b runMetroLayoutBuilder) currentStationX() *float64 {
	id := b.currentID()
	if id == nil {
		return nil
	}
	for _, placed := range b.Placed {
		if placed.Stage.ID == *id {
			x := placed.At.X
			return &x
		}
	}
	return nil
}

// runMetroLayoutPiece is a stretch of one line that shares a travel state.
type runMetroLayoutPiece struct {
	state  RunMetroTravel
	points []MapPoint
}

// lineSegments turns every line into its travel-state segments, and collects the open ends of the lines, the
// terminus first.
func (b runMetroLayoutBuilder) lineSegments(terminus, lineY float64, currentX *float64) ([]RunMetroSegment, []MapPoint) {
	var segments []RunMetroSegment
	ends := []MapPoint{{X: terminus, Y: lineY}}
	for index, line := range b.Lines {
		if len(line.Nodes) < 2 {
			continue
		}
		for number, p := range b.linePieces(line, currentX) {
			segments = append(segments, RunMetroSegment{ID: "line-" + strconv.Itoa(index) + "-" + strconv.Itoa(number), State: p.state,
				Path: NewMapPathPoints(p.points, runMetroLayoutCorner)})
		}
		if line.OpenStart {
			ends = append(ends, line.Nodes[0].At)
		}
		if line.OpenEnd {
			ends = append(ends, line.Nodes[len(line.Nodes)-1].At)
		}
	}
	return segments, ends
}

// linePieces cuts one line into pieces of a single travel state. A jump breaks the piece, so the next stretch
// starts a new one.
func (b runMetroLayoutBuilder) linePieces(line runMetroLayoutLine, currentX *float64) []runMetroLayoutPiece {
	var pieces []runMetroLayoutPiece
	broken := false
	for i := 0; i+1 < len(line.Nodes); i++ {
		from, to := line.Nodes[i], line.Nodes[i+1]
		if to.Jump {
			broken = true
			continue
		}
		state := b.travel(to.Owner, to.At.X, currentX)
		if broken {
			broken = false
			pieces = append(pieces, runMetroLayoutPiece{state, []MapPoint{from.At, to.At}})
		} else if n := len(pieces); n > 0 && pieces[n-1].state == state {
			pieces[n-1].points = append(pieces[n-1].points, to.At)
		} else {
			pieces = append(pieces, runMetroLayoutPiece{state, []MapPoint{from.At, to.At}})
		}
	}
	return pieces
}

// mapWidth is the right edge of everything the map draws, before its margin: the terminus, the trigger pill,
// the station names and status boxes, and the route tags.
func (b runMetroLayoutBuilder) mapWidth(pill RunMetroPill, terminus float64, stations []RunMetroStation) float64 {
	// Nested maxima: max(max(max(a, b), c), d).
	right := metroMax(terminus+14, pill.Box.X+pill.Box.Width)
	stationRight := 0.0
	for i, station := range stations {
		w := metroMax(station.Label.X+station.Label.Width, station.StatusBox.X+station.StatusBox.Width)
		if i == 0 || w > stationRight {
			stationRight = w
		}
	}
	right = metroMax(right, stationRight)
	tagRight := 0.0
	for i, tag := range b.Tags {
		w := tag.X + tag.Width
		if i == 0 || w > tagRight {
			tagRight = w
		}
	}
	return metroMax(right, tagRight)
}

// mapBottom is the lowest line node or placed stage, starting from lineY, before the map's margin.
func (b runMetroLayoutBuilder) mapBottom(lineY float64) float64 {
	bottom := lineY
	first := true
	for _, line := range b.Lines {
		for _, node := range line.Nodes {
			if first || node.At.Y >= bottom {
				bottom = node.At.Y
				first = false
			}
		}
	}
	placedBottom := lineY
	first = true
	for _, placed := range b.Placed {
		if first || placed.At.Y >= placedBottom {
			placedBottom = placed.At.Y
			first = false
		}
	}
	return metroMax(bottom, placedBottom)
}

// travel is how far the run has got along a stretch that ends at x, from the routes it owns.
func (b runMetroLayoutBuilder) travel(owner []runMetroLayoutWay, x float64, currentX *float64) RunMetroTravel {
	for _, way := range owner {
		if t := b.taken(way.Fork, way.Route); t != nil && !*t {
			return RunMetroTravelSkip
		}
	}
	if b.Run.State.Completed() {
		return RunMetroTravelDone
	}
	if currentX == nil {
		return RunMetroTravelTodo
	}
	if x <= *currentX+0.5 {
		return RunMetroTravelDone
	}
	return RunMetroTravelTodo
}

// station is the wire form of one placed stage.
func (b runMetroLayoutBuilder) station(placed runMetroLayoutPlaced) RunMetroStation {
	names := metroLayoutWrap(placed.Stage.Name, 15)
	width := 0.0
	for i, name := range names {
		if w := runMetroLayoutTextWidth(name); i == 0 || w > width {
			width = w
		}
	}
	panel, hasPanel := b.Panels[placed.Stage.ID]
	var state RunMetroStationState
	currentID := b.currentID()
	switch {
	case hasPanel && panel.Step == StatusStepSkipped:
		state = RunMetroStationStateSkip
	case currentID != nil && placed.Stage.ID == *currentID:
		state = RunMetroStationStateCurrent
	case hasPanel && panel.Step == StatusStepDone:
		state = RunMetroStationStateDone
	default:
		state = RunMetroStationStateTodo
	}
	x, y := placed.At.X, placed.At.Y
	labelHeight := float64(float64(len(names)) * runMetroLayoutLabelLine)
	status := ""
	look := "plain"
	if hasPanel {
		status = panel.Status.Text
		look = string(panel.Status.Look)
	}
	statusWidth := runMetroLayoutTextWidth(status) + 16
	statusTop := y - runMetroLayoutStatusGap - runMetroLayoutStatusHeight
	// The click area and the selection outline: past the halo above, and the name's sides and foot below.
	left := metroMin(x-runMetroLayoutHitReach, x-width/2-runMetroLayoutHitSide)
	right := metroMax(x+runMetroLayoutHitReach, x+width/2+runMetroLayoutHitSide)
	return RunMetroStation{
		ID:     placed.Stage.ID,
		Number: b.Layout.Number(placed.Stage.ID),
		Name:   placed.Stage.Name,
		Lines:  names,
		Glyph:  runMetroLayoutGlyph(placed.Stage.Kind()),
		X:      x,
		Y:      y,
		Label:  RunMetroBox{X: x - width/2, Y: y + 10, Width: width, Height: labelHeight},
		Hit: RunMetroBox{X: left, Y: y - runMetroLayoutHitReach, Width: right - left,
			Height: runMetroLayoutHitReach + 10 + labelHeight + runMetroLayoutHitFoot},
		State:     state,
		Status:    status,
		Look:      look,
		Tagged:    (state == RunMetroStationStateDone || state == RunMetroStationStateCurrent) && status != "",
		StatusBox: RunMetroBox{X: x - statusWidth/2, Y: statusTop, Width: statusWidth, Height: runMetroLayoutStatusHeight},
	}
}
