//
// Places station names and run labels where they cover no line, glyph, dot
// or other text: as close to their station or dots as there is room.

package core

import "slices"
import "sort"

// metroLayoutObstacles is the lines and boxes a name or label must keep clear of.
type metroLayoutObstacles struct {
	Segments []metroLayoutSegment
	Boxes    []MetroLayoutBox
}

// free reports whether box keeps clear of every obstacle.
func (o metroLayoutObstacles) free(box MetroLayoutBox) bool {
	wide := MetroLayoutBox{
		X:      box.X - MetroLayoutLineWidth/2 - 1.5,
		Y:      box.Y - MetroLayoutLineWidth/2 - 1.5,
		Width:  box.Width + MetroLayoutLineWidth + 3,
		Height: box.Height + MetroLayoutLineWidth + 3,
	}
	for _, b := range o.Boxes {
		if b.Intersects(box) {
			return false
		}
	}
	return !slices.ContainsFunc(o.Segments, func(s metroLayoutSegment) bool {
		return metroLayoutSegmentHits(s.A, s.B, wide)
	})
}

// metroLayoutSegmentHits reports whether the segment from a to b passes through box (Liang–Barsky).
func metroLayoutSegmentHits(a, b MetroLayoutPoint, box MetroLayoutBox) bool {
	t0, t1 := 0.0, 1.0
	dx, dy := b.X-a.X, b.Y-a.Y
	edges := [4][2]float64{
		{-dx, a.X - box.X},
		{dx, box.MaxX() - a.X},
		{-dy, a.Y - box.Y},
		{dy, box.MaxY() - a.Y},
	}
	for _, edge := range edges {
		p, q := edge[0], edge[1]
		if p == 0 {
			if q < 0 {
				return false
			}
			continue
		}
		r := q / p
		if p < 0 {
			if r > t1 {
				return false
			}
			t0 = metroMax(t0, r)
		} else {
			if r < t0 {
				return false
			}
			t1 = metroMin(t1, r)
		}
	}
	return t0 <= t1
}

// metroMaxOrZero is the largest of values, or 0 when there are none.
func metroMaxOrZero(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return metroMaxF(values)
}

// placeText places the station names and run labels, and sizes the map to hold them.
func (m *MetroLayoutMap) placeText() {
	room, points := m.textObstacles()
	m.placeStationNames(&room)
	m.placeRunLabels(&room)
	m.sizeToText(room, points)
}

// textObstacles collects what a name or label must keep clear of: the lines, pills, stations and dots, and the points
// the lines pass through, which size the map.
func (m *MetroLayoutMap) textObstacles() (metroLayoutObstacles, []MetroLayoutPoint) {
	room := metroLayoutObstacles{}
	var points []MetroLayoutPoint
	for _, line := range m.Lines {
		strokes := append(line.Strokes(), line.Returns...)
		for _, path := range strokes {
			for i := 0; i+1 < len(path); i++ {
				room.Segments = append(room.Segments, metroLayoutSegment{A: path[i], B: path[i+1]})
			}
			points = append(points, path...)
		}
		room.Boxes = append(room.Boxes, line.NameBox)
		for _, end := range line.Ends() {
			room.Segments = append(room.Segments, metroLayoutSegment{
				A: MetroLayoutPoint{X: end.X, Y: end.Y - 8},
				B: MetroLayoutPoint{X: end.X, Y: end.Y + 8},
			})
		}
	}
	for _, line := range m.Lines {
		for _, pill := range line.Pills {
			room.Boxes = append(room.Boxes, pill.Box)
		}
	}
	for _, station := range m.Stations {
		room.Boxes = append(room.Boxes, station.Box(), station.Status.Box)
	}
	for _, dot := range m.Dots {
		room.Boxes = append(room.Boxes, MetroLayoutBox{X: dot.At.X - 6, Y: dot.At.Y - 6, Width: 12, Height: 12})
	}
	return room, points
}

// placeStationNames places each station's name, left to right, and adds it to the room the next names keep clear of.
func (m *MetroLayoutMap) placeStationNames(room *metroLayoutObstacles) {
	for _, i := range metroSortedByX(m.Stations) {
		box := m.stationNameBox(m.Stations[i], *room)
		m.Stations[i].NameBox = box
		room.Boxes = append(room.Boxes, box)
	}
}

// stationNameBox picks the first place for a station's name that keeps clear of the room, or the first option when none does.
// Names go over the station, else under it, then a little further out, then beside it.
func (m *MetroLayoutMap) stationNameBox(station MetroLayoutStation, room metroLayoutObstacles) MetroLayoutBox {
	width := 0.0
	for j, line := range station.Lines {
		if w := m.textWidth(line); j == 0 || w > width {
			width = w
		}
	}
	height := float64(float64(len(station.Lines)) * metroMapTextHeight)
	glyph := station.Box()
	var options []MetroLayoutBox
	for step := 0.0; step <= 24; step += 4 {
		options = append(options, MetroLayoutBox{X: station.X - width/2, Y: glyph.Y - 3 - height - step, Width: width, Height: height})
		options = append(options, MetroLayoutBox{X: station.X - width/2, Y: glyph.MaxY() + 3 + step, Width: width, Height: height})
	}
	// Then beside it, at its top or bottom corner, a little further out each time.
	for step := 0.0; step <= 48; step += 6 {
		for _, left := range []float64{station.X + 10, station.X - 10 - width, station.X - width/2 + step, station.X - width/2 - step} {
			options = append(options, MetroLayoutBox{X: left, Y: glyph.Y - 1 - height - step/2, Width: width, Height: height})
			options = append(options, MetroLayoutBox{X: left, Y: glyph.MaxY() + 1 + step/2, Width: width, Height: height})
		}
	}
	for _, option := range options {
		if room.free(option) {
			return option
		}
	}
	return options[0]
}

// metroLabelOption is one place a run label can go, and whether it sits above its station's dots.
type metroLabelOption struct {
	box  MetroLayoutBox
	over bool
}

// runLabelOptions lists the places for a run label of the given width by a station's dots: under them, then over them,
// a little further out each time. A label for runs that need you also moves along the station, so it may go beside it.
func runLabelOptions(dotAt MetroLayoutPoint, below, above, width float64, urgent bool) []metroLabelOption {
	reach := 8.0
	shifts := []float64{0}
	if urgent {
		reach = 72
		// A label for runs that need you moves along too, until it finds room.
		shifts = []float64{0, 16, 32, 48, 64, -width + 12, 96, 128, 160}
	}
	x := dotAt.X - 6
	var options []metroLabelOption
	for _, shift := range shifts {
		for step := 0.0; step <= reach; step += 4 {
			options = append(options, metroLabelOption{MetroLayoutBox{X: x + shift, Y: metroMax(below, dotAt.Y+8) + step, Width: width, Height: 15}, false})
			options = append(options, metroLabelOption{MetroLayoutBox{X: x + shift, Y: metroMin(above, dotAt.Y-8) - 15 - step, Width: width, Height: 15}, true})
		}
	}
	return options
}

// placeRunLabels places each belt's run label at each station where its runs stop, under the station's dots, else over
// them, and adds it to the room the next labels keep clear of.
func (m *MetroLayoutMap) placeRunLabels(room *metroLayoutObstacles) {
	for _, i := range metroSortedByX(m.Stations) {
		station := m.Stations[i]
		below := station.Box().MaxY() + 3
		above := station.Box().Y - 3
		for _, belt := range metroSortedStopBelts(station.Stops) {
			here := m.Active[metroMapKey(station.ID, belt)]
			if len(here) == 0 {
				continue
			}
			dotAt, found := m.firstDotAt(belt, station.ID)
			if !found {
				continue
			}
			text, urgent := metroMapLabelText(here)
			width := m.textWidth(text)
			if urgent != nil {
				width += 12
			}
			options := runLabelOptions(dotAt, below, above, width, urgent != nil)
			var chosen *metroLabelOption
			for k := range options {
				if room.free(options[k].box) {
					chosen = &options[k]
					break
				}
			}
			if chosen == nil {
				if urgent == nil {
					continue
				}
				chosen = &options[0]
			}
			room.Boxes = append(room.Boxes, chosen.box)
			if chosen.over {
				above = chosen.box.Y - 2
			} else {
				below = chosen.box.MaxY() + 2
			}
			runIDs, openRunID := labelRuns(here)
			m.Labels = append(m.Labels, MetroLayoutLabel{Text: text, Box: chosen.box, NeedsYou: urgent != nil, Urgent: urgent, RunIDs: runIDs,
				BeltID: belt, Above: chosen.over, Dot: dotAt, OpenRunID: openRunID})
		}
	}
}

// labelRuns lists the IDs of the runs a label stands for, and the run it opens: the first that needs you, else the first.
func labelRuns(here []Run) ([]string, *string) {
	runIDs := make([]string, 0, len(here))
	for _, run := range here {
		runIDs = append(runIDs, run.ID)
	}
	var openRunID *string
	for k := range here {
		if here[k].State.NeedsYou() {
			id := here[k].ID
			openRunID = &id
			break
		}
	}
	if openRunID == nil {
		id := here[0].ID
		openRunID = &id
	}
	return runIDs, openRunID
}

// sizeToText sizes the map to hold every name, label, box and line point, with a margin on the right and bottom.
func (m *MetroLayoutMap) sizeToText(room metroLayoutObstacles, points []MetroLayoutPoint) {
	var boxes []MetroLayoutBox
	boxes = append(boxes, room.Boxes...)
	for _, label := range m.Labels {
		boxes = append(boxes, label.Box)
	}
	var maxX, maxY []float64
	for _, box := range boxes {
		maxX = append(maxX, box.MaxX())
		maxY = append(maxY, box.MaxY())
	}
	var pointXs, pointYs []float64
	for _, p := range points {
		pointXs = append(pointXs, p.X)
		pointYs = append(pointYs, p.Y)
	}
	m.Width = metroMax(metroMaxOrZero(maxX), metroMaxOrZero(pointXs)) + 18
	m.Height = metroMax(metroMaxOrZero(maxY), metroMaxOrZero(pointYs)) + 16
}

// firstDotAt is the position of the first dot a belt has at a station, if it has one.
func (m MetroLayoutMap) firstDotAt(belt, station string) (MetroLayoutPoint, bool) {
	for _, dot := range m.Dots {
		if dot.BeltID == belt && dot.StationID == station {
			return dot.At, true
		}
	}
	return MetroLayoutPoint{}, false
}

// metroSortedByX is the indices of stations ordered by their x.
func metroSortedByX(stations []MetroLayoutStation) []int {
	order := make([]int, len(stations))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return stations[order[a]].X < stations[order[b]].X })
	return order
}
