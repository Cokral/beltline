// The lane helpers of the Map: where a
// branch leaves and rejoins, the columns a lane runs through, its errand, entry and y.

package core

import (
	"strconv"
)

// origin is the column a branch leaves from and the lane it leaves, if it leaves a station.
func (m MetroLayoutMap) origin(g, lane int) (column int, laneID int, ok bool) {
	path := m.Groups[g].Plan.Lanes[lane].Path
	if path.Parent == nil {
		return 0, 0, false
	}
	from := m.spot(path.From)
	if from == nil {
		return 0, 0, false
	}
	parent, found := from.Members[path.Belt]
	if !found {
		return 0, 0, false
	}
	return from.Column, parent, true
}

// target is the column a branch rejoins at and the lane it joins.
func (m MetroLayoutMap) target(g, lane int) (column int, laneID int, ok bool) {
	path := m.Groups[g].Plan.Lanes[lane].Path
	if path.Parent == nil {
		return 0, 0, false
	}
	to := m.spot(path.To)
	if to == nil {
		return 0, 0, false
	}
	into, found := to.Members[path.Belt]
	if !found || into == lane {
		return 0, 0, false
	}
	return to.Column, into, true
}

// laneRuns are the columns a lane runs through on its own, between where it starts and where it stops
// or rejoins.
func (m MetroLayoutMap) laneRuns(g, lane int) []int {
	path := m.Groups[g].Plan.Lanes[lane].Path
	start := -1
	if c, _, ok := m.origin(g, lane); ok {
		start = c
	}
	if end, _, ok := m.target(g, lane); ok {
		var out []int
		for _, c := range m.Groups[g].Columns {
			if c > start && c < end {
				out = append(out, c)
			}
		}
		return out
	}
	var columns []int
	for _, stop := range path.Stops {
		if s := m.spot(&stop); s != nil {
			columns = append(columns, s.Column)
		}
	}
	if len(columns) == 0 {
		return nil
	}
	last := metroMaxIntOf(columns)
	var out []int
	for _, c := range m.Groups[g].Columns {
		if c > start && c <= last {
			out = append(out, c)
		}
	}
	return out
}

// metroStopColumn is a machine a lane stops at, with its column.
type metroStopColumn struct {
	ID     string
	Column int
}

// errand is what a lane is doing at a column: stopping at a machine, or on its way between two. Lanes
// side by side on the same errand make one bundle.
func (m MetroLayoutMap) errand(g, lane, column int) string {
	path := m.Groups[g].Plan.Lanes[lane].Path
	var stops []metroStopColumn
	for _, stop := range path.Stops {
		if s := m.spot(&stop); s != nil {
			stops = append(stops, metroStopColumn{ID: stop, Column: s.Column})
		}
	}
	for _, s := range stops {
		if s.Column == column {
			return "at " + s.ID
		}
	}
	var before string
	found := false
	for i := len(stops) - 1; i >= 0; i-- {
		if stops[i].Column < column {
			before = stops[i].ID
			found = true
			break
		}
	}
	if !found {
		before = "start " + strconv.Itoa(lane)
		if _, _, ok := m.origin(g, lane); ok && path.From != nil {
			before = *path.From
		}
	}
	var after string
	found = false
	for _, s := range stops {
		if s.Column > column {
			after = s.ID
			found = true
			break
		}
	}
	if !found {
		after = "end " + strconv.Itoa(lane)
		if _, _, ok := m.target(g, lane); ok && path.To != nil {
			after = *path.To
		}
	}
	return before + " > " + after
}

// entry is where a main line leaves its name pill.
func (m MetroLayoutMap) entry(g, lane int) *float64 {
	group := m.Groups[g]
	if v, ok := group.Entries[lane]; ok {
		return &v
	}
	if t := group.Plan.trackOf(lane, group.Plan.Tracks); t != nil {
		v := group.Homes[*t]
		return &v
	}
	return nil
}

// y is a lane's y at a column, or nil when it does not run there.
func (m MetroLayoutMap) y(g, lane, column int) *float64 {
	row, ok := m.Groups[g].Ys[lane]
	if !ok {
		return nil
	}
	v, ok := row[column]
	if !ok {
		return nil
	}
	return &v
}

// dotReach is how far past a station its last dot reaches on a belt.
func (m MetroLayoutMap) dotReach(machine, belt string) float64 {
	count := min(len(m.Active[metroMapKey(machine, belt)]), 3)
	if count == 0 {
		return 0
	}
	base := 15.0
	if m.interchange(machine) {
		base = MetroLayoutInterchangeWidth/2 + 9
	}
	return float64(base+float64(float64(count-1)*12)) + 6
}
