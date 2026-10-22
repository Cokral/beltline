//
// The finished map: each belt's line and branches, the stations, and the run dots.

package core

import (
	"maps"
	"math"
	"slices"
	"sort"
)

// metroLayoutLaneTrace is a lane's polyline from where it starts through the columns it runs through,
// staying level past each station's dots and turning at 45° just before the next column when its row
// changes there. Its routes' name pills, where they come before a column, sit one after another on the
// level stretch just before that column.
func (m MetroLayoutMap) trace(g, lane int) ([]MetroLayoutPoint, []MetroLayoutRoutePill) {
	path := m.Groups[g].Plan.Lanes[lane].Path
	own := m.laneRuns(g, lane)
	var first struct{ x, y, level float64 }
	found := false
	if len(own) > 0 {
		if x, y, level, ok := m.before(g, lane, own[0]); ok {
			first.x, first.y, first.level, found = x, y, level, true
		}
	}
	if !found {
		if t, _, ok := m.target(g, lane); ok {
			if x, y, level, ok := m.before(g, lane, t); ok {
				first.x, first.y, first.level, found = x, y, level, true
			}
		}
	}
	if !found {
		return nil, nil
	}
	points := []MetroLayoutPoint{{X: first.x, Y: first.y}}
	var pills []MetroLayoutRoutePill
	level := first.level
	// The last stop the lane made, and the edge its dots reach to: a name pill after it starts past that edge.
	anchored := false
	var anchorEdge float64
	reached := append([]int(nil), own...)
	if t, _, ok := m.target(g, lane); ok {
		reached = append(reached, t)
	}
	for _, c := range reached {
		y := m.atColumn(g, lane, c)
		if y == nil {
			continue
		}
		last := points[len(points)-1]
		x := m.Xs[c]
		room := m.pillRoom(g, lane, c)
		// lead is how far the lane stays level before this column: past the last stop's dots when a pill comes here.
		lead := level
		if room > 0 {
			if anchored {
				lead = metroMax(lead, anchorEdge-last.X)
			}
			offset := last.X + lead + metroMapPillInset
			for _, pill := range m.pillsAt(g, lane, c) {
				width := m.metroMapPillWidth(pill.Text)
				pills = append(pills, MetroLayoutRoutePill{ID: pill.Fork + ":" + pill.Route, Fork: pill.Fork, Route: pill.Route, Text: pill.Text,
					Box: MetroLayoutBox{X: offset, Y: last.Y - metroMapPillHeight/2, Width: width, Height: metroMapPillHeight}})
				offset += width + metroMapPillGap
			}
		}
		if *y != last.Y {
			end := x - metroMapArrival
			start := metroMin(metroMax(last.X+lead+room, end-math.Abs(*y-last.Y)), end-2)
			points = append(points, MetroLayoutPoint{X: start, Y: last.Y}, MetroLayoutPoint{X: end, Y: *y})
		}
		var stop *string
		for i := range path.Stops {
			if s := m.spot(&path.Stops[i]); s != nil && s.Column == c {
				stop = &path.Stops[i]
				break
			}
		}
		if stop != nil || c == reached[len(reached)-1] || *y != last.Y {
			points = append(points, MetroLayoutPoint{X: x, Y: *y})
		}
		if stop != nil {
			anchored = true
			anchorEdge = x + m.stationReach(*stop) + 4
			level = m.dotReach(*stop, path.Belt) + 4
		} else {
			level = 4
		}
	}
	return points, pills
}

// draw builds the lines, stations and dots of the finished map from the placed rows and columns.
func (m *MetroLayoutMap) draw() {
	for g, group := range m.Groups {
		for _, r := range group.Routes {
			if line, ok := m.routeLine(g, r); ok {
				m.Lines = append(m.Lines, line)
			}
		}
	}
	for si, spot := range m.Spots {
		m.drawStation(si, spot)
	}
}

// routeLine builds one route's line: its main lane's trace from the name pill, the other lanes of its belt
// and the arrows back to earlier stops. It reports false when the route has no main lane on the map.
func (m *MetroLayoutMap) routeLine(g, r int) (MetroLayoutLine, bool) {
	plan := m.Groups[g].Plan
	belt := m.Routes[r].Belt
	main := -1
	for lane, item := range plan.Lanes {
		if item.Path.Belt == belt.ID && item.isMain() {
			main = lane
			break
		}
	}
	if main < 0 {
		return MetroLayoutLine{}, false
	}
	homePtr := m.entry(g, main)
	if homePtr == nil {
		return MetroLayoutLine{}, false
	}
	home := *homePtr
	pillWidth := m.textWidth(belt.Name) + 22
	pill := MetroLayoutBox{X: m.PillRight - pillWidth, Y: home - MetroLayoutPillHeight/2, Width: pillWidth, Height: MetroLayoutPillHeight}
	points, pills := m.trace(g, main)
	if len(points) == 0 {
		points = []MetroLayoutPoint{{X: m.PillRight, Y: home}}
	}
	reach := 0.0
	if stops := plan.Lanes[main].Path.Stops; len(stops) > 0 {
		reach = m.dotReach(stops[len(stops)-1], belt.ID)
	}
	tail := points[len(points)-1]
	extent := 26.0
	if len(points) != 1 {
		extent = metroMax(30, reach+14)
	}
	terminus := MetroLayoutPoint{X: tail.X + extent, Y: tail.Y}
	points = append(points, terminus)
	branches, branchPills := m.routeBranches(g, belt.ID)
	pills = append(pills, branchPills...)
	return MetroLayoutLine{BeltID: belt.ID, Name: belt.Name, Colour: m.Colours[belt.ID], NameBox: pill, Points: points,
		Branches: branches, Returns: m.routeReturns(g, r, home), Terminus: terminus, Pills: pills}, true
}

// routeBranches builds the lanes of a belt in group g other than its main lane: each one's line, which ends
// in an arrow when it does not rejoin the main line, and the name pills its trace places.
func (m *MetroLayoutMap) routeBranches(g int, beltID string) ([]MetroLayoutBranch, []MetroLayoutRoutePill) {
	var branches []MetroLayoutBranch
	var pills []MetroLayoutRoutePill
	for lane, item := range m.Groups[g].Plan.Lanes {
		if item.isMain() || item.Path.Belt != beltID {
			continue
		}
		path := item.Path
		line, branchPills := m.trace(g, lane)
		pills = append(pills, branchPills...)
		if len(line) == 0 {
			continue
		}
		_, _, rejoins := m.target(g, lane)
		var end *MetroLayoutPoint
		if !rejoins {
			branchReach := 0.0
			if n := len(path.Stops); n > 0 {
				branchReach = m.dotReach(path.Stops[n-1], beltID)
			}
			back := 30.0
			if path.Back {
				back = 22
			}
			lineTail := line[len(line)-1]
			e := MetroLayoutPoint{X: lineTail.X + metroMax(back, branchReach+14), Y: lineTail.Y}
			end = &e
			line = append(line, e)
		}
		var to *string
		if rejoins {
			to = path.To
		}
		branches = append(branches, MetroLayoutBranch{Points: line, From: path.From, To: to, Stops: path.Stops, End: end, Back: path.Back})
	}
	return branches, pills
}

// routeReturns builds a route's return arrows: from each back-link's stop down to a low row, then across to
// the stop it returns to. A link whose stops are not on the map is skipped. home is the route's home row.
func (m *MetroLayoutMap) routeReturns(g, r int, home float64) [][]MetroLayoutPoint {
	belt := m.Routes[r].Belt
	lows := m.backs(g, r)
	var returns [][]MetroLayoutPoint
	for k, back := range m.Routes[r].Backs {
		to := m.spot(&back.To)
		if to == nil {
			continue
		}
		toLane, ok := to.Members[belt.ID]
		if !ok {
			continue
		}
		toY := m.y(g, toLane, to.Column)
		if toY == nil || k >= len(lows) || lows[k] == nil {
			continue
		}
		low := lows[k].Y
		from := MetroLayoutPoint{X: m.PillRight, Y: home}
		if s := m.spot(back.From); s != nil {
			if fromLane, ok := s.Members[belt.ID]; ok {
				if fy := m.y(g, fromLane, s.Column); fy != nil {
					from = MetroLayoutPoint{X: m.Xs[s.Column], Y: *fy}
				}
			}
		}
		returns = append(returns, []MetroLayoutPoint{from, {X: from.X, Y: low}, {X: m.Xs[to.Column], Y: low}, {X: m.Xs[to.Column], Y: *toY}})
	}
	return returns
}

// drawStation adds one station: its glyph, the pieces its line is drawn in, its status and the dots of the
// runs stopped at it. si is the spot's index in m.Spots.
func (m *MetroLayoutMap) drawStation(si int, spot MetroLayoutSpot) {
	x := m.Xs[spot.Column]
	machine := m.Track.Machine(spot.ID)
	stops := map[string]float64{}
	for belt, lane := range spot.Members {
		if y := m.y(spot.Group, lane, spot.Column); y != nil {
			stops[belt] = *y
		}
	}
	interchange := len(spot.Members) > 1
	stageKind := StageKindAgent
	if machine != nil {
		for _, member := range machine.Members {
			if _, ok := spot.Members[member.BeltID]; ok {
				stageKind = member.Stage.Kind()
				break
			}
		}
	}
	kind := metroMapGlyph(stageKind)
	glyph := kind
	if interchange {
		glyph = MetroLayoutGlyphInterchange
	}
	ys := metroSortedUniqueF(stops)
	name := spot.ID
	if machine != nil {
		name = machine.Name
	}
	nameY := 0.0
	if len(ys) > 0 {
		nameY = ys[0]
	}
	station := MetroLayoutStation{ID: spot.ID, Name: name, Lines: m.spotLines(si), Glyph: glyph, Kind: kind, X: x,
		Stops: stops, NameBox: MetroLayoutBox{X: x, Y: nameY}, Pieces: m.stationPieces(spot, ys)}
	statusText, statusLook := m.spotStatus(spot)
	station.Status = MetroLayoutStatus{Text: statusText, Look: statusLook, Box: m.statusBox(station, statusText)}
	m.Stations = append(m.Stations, station)
	m.addStationDots(spot, stops, interchange)
}

// stationPieces splits a station's column into the pieces its line is drawn in. An interchange breaks where
// a line it does not serve runs between two of its stops. ys are the station's stop heights, in order.
func (m *MetroLayoutMap) stationPieces(spot MetroLayoutSpot, ys []float64) []MetroRange {
	var others []float64
	for lane, row := range m.Groups[spot.Group].Ys {
		if metroLayoutSpotHasLane(spot, lane) {
			continue
		}
		if v, ok := row[spot.Column]; ok {
			others = append(others, v)
		}
	}
	var pieces []MetroRange
	first := 0.0
	if len(ys) > 0 {
		first = ys[0]
	}
	for i := 0; i+1 < len(ys); i++ {
		a, b := ys[i], ys[i+1]
		crossed := slices.ContainsFunc(others, func(o float64) bool { return o > a && o < b })
		if crossed {
			pieces = append(pieces, MetroRange{Lower: first, Upper: a})
			first = b
		}
	}
	last := first
	if len(ys) > 0 {
		last = ys[len(ys)-1]
	}
	pieces = append(pieces, MetroRange{Lower: first, Upper: last})
	return pieces
}

// addStationDots adds the dots of the runs stopped at a station, belt by belt in the order of their stops.
// An interchange starts its dots further along than a plain station.
func (m *MetroLayoutMap) addStationDots(spot MetroLayoutSpot, stops map[string]float64, interchange bool) {
	x := m.Xs[spot.Column]
	for _, belt := range metroSortedStopBelts(stops) {
		here := m.Active[metroMapKey(spot.ID, belt)]
		if len(here) == 0 {
			continue
		}
		fromX := x + 15
		if interchange {
			fromX = x + (MetroLayoutInterchangeWidth/2 + 9)
		}
		place := 0
		m.addDots(here, belt, spot.ID, fromX, stops[belt], &place)
	}
}

// metroLayoutSpotHasLane reports whether a lane is one of a spot's members.
func metroLayoutSpotHasLane(spot MetroLayoutSpot, lane int) bool {
	for _, l := range spot.Members {
		if l == lane {
			return true
		}
	}
	return false
}

// metroSortedUniqueF is the distinct values of a map's values, in order.
func metroSortedUniqueF(values map[string]float64) []float64 {
	seen := map[float64]struct{}{}
	var out []float64
	for _, v := range values {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

// metroSortedStopBelts is the belts of a station's stops, by their y and then their id
// so the order is fixed where equal ys would leave it unspecified.
func metroSortedStopBelts(stops map[string]float64) []string {
	belts := slices.Collect(maps.Keys(stops))
	sort.Slice(belts, func(a, b int) bool {
		if stops[belts[a]] != stops[belts[b]] {
			return stops[belts[a]] < stops[belts[b]]
		}
		return belts[a] < belts[b]
	})
	return belts
}

// addDots adds the dots of the runs at one station on a belt, starting at x. Past three runs the rest
// gather into one "+n" dot. place counts the dots already placed at the station.
func (m *MetroLayoutMap) addDots(here []Run, belt, station string, x, y float64, place *int) {
	// prefix(count > 3 ? 2 : 3): every run when there are at most three.
	shown := here
	if len(here) > 3 {
		shown = here[:2]
	}
	for _, run := range shown {
		look := MetroLayoutLookFor(run.State)
		if look == nil {
			continue
		}
		_, paused := m.Paused[belt]
		m.Dots = append(m.Dots, MetroLayoutDot{ID: run.ID, RunIDs: []string{run.ID}, BeltID: belt, StationID: station, Look: *look, Halo: look.Halo(),
			Place: *place, Overflow: 0, At: MetroLayoutPoint{X: x + float64(float64(*place)*12), Y: y},
			Card: MetroLayoutCardRun(run, paused, m.Pause, m.Ambiguous)})
		*place++
	}
	if len(here) > 3 {
		rest := here[2:]
		var ids []string
		halo := false
		restLook := MetroLayoutLookQueued
		foundLook := false
		for _, run := range rest {
			ids = append(ids, run.ID)
			if run.State.NeedsYou() {
				halo = true
			}
			if !foundLook {
				if look := MetroLayoutLookFor(run.State); look != nil {
					restLook = *look
					foundLook = true
				}
			}
		}
		m.Dots = append(m.Dots, MetroLayoutDot{ID: "more-" + belt + "-" + station, RunIDs: ids, BeltID: belt, StationID: station, Look: restLook,
			Halo: halo, Place: *place, Overflow: len(rest), At: MetroLayoutPoint{X: x + float64(float64(*place)*12), Y: y},
			Card: MetroLayoutCardMore(len(rest))})
		*place++
	}
}

// metroMapGlyph is the station glyph for a stage kind: agent a circle, approval a diamond, watch a square.
func metroMapGlyph(kind StageKind) MetroLayoutGlyph {
	switch kind {
	case StageKindGate:
		return MetroLayoutGlyphDiamond
	case StageKindWatch:
		return MetroLayoutGlyphSquare
	default:
		return MetroLayoutGlyphCircle
	}
}
