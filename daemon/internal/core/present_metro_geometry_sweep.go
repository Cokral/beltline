package core

import (
	"math"
	"slices"
	"sort"
)

// before is where a lane is just before it reaches a column: its x and y, and how far past that x it must
// stay level for its dots or for the station it leaves.
func (m MetroLayoutMap) before(g, lane, c int) (x, y, level float64, ok bool) {
	group := m.Groups[g]
	path := group.Plan.Lanes[lane].Path
	earlier, hasEarlier := 0, false
	for _, r := range m.laneRuns(g, lane) {
		if r < c && (!hasEarlier || r > earlier) {
			earlier, hasEarlier = r, true
		}
	}
	// A name pill that comes after a station keeps clear of every belt's dots there, not only this belt's.
	pill := len(m.pillsAt(g, lane, c)) > 0
	if hasEarlier {
		if at := m.y(g, lane, earlier); at != nil {
			level := 4.0
			for _, stop := range path.Stops {
				if s := m.spot(&stop); s != nil && s.Column == earlier {
					level = m.dotReach(stop, path.Belt) + 4
					if pill {
						level = metroMax(level, m.stationReach(stop)+4)
					}
					break
				}
			}
			return m.Xs[earlier], *at, level, true
		}
	}
	if oc, ol, found := m.origin(g, lane); found && oc < c {
		if at := m.y(g, ol, oc); at != nil {
			reach := 0.0
			if path.From != nil {
				reach = m.dotReach(*path.From, path.Belt) + 4
				if pill {
					reach = metroMax(reach, m.stationReach(*path.From)+4)
				}
			}
			return m.Xs[oc], *at, metroMax(reach, 14), true
		}
	}
	if group.Plan.Lanes[lane].isMain() {
		if e := m.entry(g, lane); e != nil {
			return m.PillRight, *e, 10, true
		}
	}
	return 0, 0, 0, false
}

// atColumn is where a lane is at a column it reaches: its own place there, or the lane it rejoins.
func (m MetroLayoutMap) atColumn(g, lane, c int) *float64 {
	if slices.Contains(m.laneRuns(g, lane), c) {
		return m.y(g, lane, c)
	}
	if tc, tl, ok := m.target(g, lane); ok && tc == c {
		return m.y(g, tl, c)
	}
	return nil
}

// marks is the text and marks a column is likely to show, relative to its x, so the next columns keep clear
// of them.
func (m MetroLayoutMap) marks(c int) []MetroLayoutBox {
	var boxes []MetroLayoutBox
	for _, s := range m.Columns[c] {
		spot := m.Spots[s]
		var ys []float64
		for _, lane := range spot.Members {
			if v := m.y(spot.Group, lane, c); v != nil {
				ys = append(ys, *v)
			}
		}
		if len(ys) == 0 {
			continue
		}
		top, bottom := metroMinF(ys), metroMaxF(ys)
		half := 9.0
		if len(spot.Members) > 1 {
			half = MetroLayoutInterchangeWidth/2 + 1
		}
		boxes = append(boxes, MetroLayoutBox{X: -half, Y: top - 9, Width: half * 2, Height: bottom - top + 18})
		// The station's status chip sits above its glyph.
		chipText, _ := m.spotStatus(spot)
		chipWidth := m.metroMapChipWidth(chipText)
		boxes = append(boxes, MetroLayoutBox{X: -chipWidth / 2, Y: top - 27, Width: chipWidth, Height: metroMapChipHeight})
		name := m.spotLines(s)
		width := 0.0
		for i, line := range name {
			if w := m.textWidth(line); i == 0 || w > width {
				width = w
			}
		}
		h := float64(float64(len(name)) * metroMapTextHeight)
		boxes = append(boxes, MetroLayoutBox{X: -width / 2, Y: top - 11 - h, Width: width, Height: h})
		below := bottom + 11
		type belted struct {
			belt string
			lane int
		}
		var belts []belted
		for belt, lane := range spot.Members {
			belts = append(belts, belted{belt, lane})
		}
		sort.Slice(belts, func(a, b int) bool { return belts[a].lane < belts[b].lane })
		for _, bl := range belts {
			here := m.Active[metroMapKey(spot.ID, bl.belt)]
			if len(here) == 0 {
				continue
			}
			y := m.y(spot.Group, bl.lane, c)
			if y == nil {
				continue
			}
			boxes = append(boxes, MetroLayoutBox{X: 9, Y: *y - 6, Width: m.dotReach(spot.ID, bl.belt) - 3, Height: 12})
			text, urgent := metroMapLabelText(here)
			w := m.textWidth(text)
			if urgent != nil {
				w += 12
			}
			boxes = append(boxes, MetroLayoutBox{X: 9, Y: below, Width: w, Height: 15})
			below += 17
		}
	}
	return boxes
}

// sweep places the columns left to right, each as close to the one before as the bends into it and the
// marks beside the ones before allow.
func (m *MetroLayoutMap) sweep() {
	type placedColumn struct {
		column int
		boxes  []MetroLayoutBox
	}
	var placed []placedColumn
	for c := range m.Columns {
		x := m.PillRight + 40
		if c != 0 {
			x = m.Xs[c-1] + metroMapStep
		}
		for g, group := range m.Groups {
			if !slices.Contains(group.Columns, c) {
				continue
			}
			for lane := range group.Plan.Lanes {
				to := m.atColumn(g, lane, c)
				fromX, fromY, fromLevel, ok := m.before(g, lane, c)
				if to == nil || !ok {
					continue
				}
				// A name pill before the column takes room on the lane, past where it must stay level.
				room := m.pillRoom(g, lane, c)
				x = metroMax(x, fromX+fromLevel+room+math.Abs(*to-fromY)+metroMapArrival+2)
				// And past the last stop's dots, which a pill after that stop must clear.
				if room > 0 {
					if column, machine, ok := m.lastStop(g, lane, c); ok {
						if at := m.y(g, lane, column); at != nil {
							x = metroMax(x, m.Xs[column]+m.stationReach(machine)+4+room+math.Abs(*to-*at)+metroMapArrival+2)
						}
					}
				}
			}
		}
		mine := m.marks(c)
		start := 0
		if len(placed) > 14 {
			start = len(placed) - 14
		}
		for _, p := range placed[start:] {
			for _, a := range p.boxes {
				for _, b := range mine {
					if a.Y < b.MaxY()+2 && b.Y < a.MaxY()+2 {
						x = metroMax(x, m.Xs[p.column]+a.MaxX()+8-b.X)
					}
				}
			}
		}
		m.Xs = append(m.Xs, x)
		placed = append(placed, placedColumn{column: c, boxes: mine})
	}
}
