// The spots of the Map: each
// machine's tracks, span, top and its interchange and the columns each machine is placed in.

package core

import (
	"maps"
	"slices"
	"sort"
)

// tracks are the tracks a spot's members run on, in the group's plan.
func (m MetroLayoutMap) tracks(spot MetroLayoutSpot) []int {
	plan := m.Groups[spot.Group].Plan
	var out []int
	for _, lane := range spot.Members {
		if t := plan.trackOf(lane, plan.Tracks); t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// span is how many tracks apart a spot's members are.
func (m MetroLayoutMap) span(spot MetroLayoutSpot) int {
	tracks := m.tracks(spot)
	return metroMaxIntOf(tracks) - metroMinIntOf(tracks)
}

// top is the highest track a spot's members run on.
func (m MetroLayoutMap) top(spot MetroLayoutSpot) int {
	return metroMinIntOf(m.tracks(spot))
}

// metroMaxIntOf is the largest of values, or 0 when there are none.
func metroMaxIntOf(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return slices.Max(values)
}

// metroMinIntOf is the smallest of values, or 0 when there are none.
func metroMinIntOf(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return slices.Min(values)
}

// spot is the spot with machineID, or nil.
func (m MetroLayoutMap) spot(machineID *string) *MetroLayoutSpot {
	if machineID == nil {
		return nil
	}
	index, ok := m.SpotIndex[*machineID]
	if !ok {
		return nil
	}
	s := m.Spots[index]
	return &s
}

// interchange reports whether a machine is shared by more than one belt.
func (m MetroLayoutMap) interchange(machineID string) bool {
	s := m.spot(&machineID)
	return s != nil && len(s.Members) > 1
}

// placeSpots places every machine in a column of its own, by layer, then the smaller ones first.
func (m *MetroLayoutMap) placeSpots() {
	m.collectSpots()
	m.assignColumns()
}

// collectSpots lists each group's machines, with the lane each belt's line uses there, in
// id order. A route's back-edges also reach the machines they go back to, on its main lane.
func (m *MetroLayoutMap) collectSpots() {
	for g, group := range m.Groups {
		plan := group.Plan
		members := map[string]map[string]int{}
		for lane, item := range plan.Lanes {
			for _, stop := range item.Path.Stops {
				setSpotMember(members, stop, item.Path.Belt, lane)
			}
		}
		for _, r := range group.Routes {
			belt := m.Routes[r].Belt.ID
			main := mainLane(plan, belt)
			if main < 0 {
				continue
			}
			for _, back := range m.Routes[r].Backs {
				setSpotMember(members, back.To, belt, main)
			}
		}
		ids := slices.Sorted(maps.Keys(members))
		for _, id := range ids {
			m.SpotIndex[id] = len(m.Spots)
			m.Spots = append(m.Spots, MetroLayoutSpot{ID: id, Group: g, Layer: plan.Layer[id], Members: members[id]})
		}
	}
}

// setSpotMember records the lane a belt's line uses at a machine, unless one was already recorded.
func setSpotMember(members map[string]map[string]int, id, belt string, lane int) {
	if _, ok := members[id][belt]; ok {
		return
	}
	if members[id] == nil {
		members[id] = map[string]int{}
	}
	members[id][belt] = lane
}

// mainLane is the lane of a belt's main line in a plan, or -1 when the plan has none.
func mainLane(plan metroLayoutPlan, belt string) int {
	return slices.IndexFunc(plan.Lanes, func(item metroLayoutLane) bool {
		return item.Path.Belt == belt && item.isMain()
	})
}

// assignColumns gives every machine a column of its own, by layer, then the smaller
// ones first, so a line ending at one has stopped before a wider one.
// Groups lie apart, so each group's machines take the columns from the first.
func (m *MetroLayoutMap) assignColumns() {
	for _, layer := range m.layers() {
		m.placeLayer(layer)
	}
}

// layers is the distinct layers the spots sit in, lowest first.
func (m *MetroLayoutMap) layers() []float64 {
	var layers []float64
	seenLayer := map[float64]struct{}{}
	for _, spot := range m.Spots {
		if _, ok := seenLayer[spot.Layer]; !ok {
			seenLayer[spot.Layer] = struct{}{}
			layers = append(layers, spot.Layer)
		}
	}
	sort.Float64s(layers)
	return layers
}

// placeLayer gives the spots on one layer their columns: the first of each group in the first
// column, the second of each group in the next, and so on.
func (m *MetroLayoutMap) placeLayer(layer float64) {
	var here []int
	for s := range m.Spots {
		if m.Spots[s].Layer == layer {
			here = append(here, s)
		}
	}
	byGroup := map[int][]int{}
	for _, s := range here {
		byGroup[m.Spots[s].Group] = append(byGroup[m.Spots[s].Group], s)
	}
	var groupKeys []int
	for g, ids := range byGroup {
		groupKeys = append(groupKeys, g)
		m.sortByExtent(ids)
	}
	sort.Ints(groupKeys)
	count := 0
	for _, ids := range byGroup {
		count = max(count, len(ids))
	}
	for k := 0; k < count; k++ {
		var ids []int
		for _, g := range groupKeys {
			if len(byGroup[g]) > k {
				ids = append(ids, byGroup[g][k])
			}
		}
		m.addColumn(ids)
	}
}

// sortByExtent orders spots by how many tracks their members span, then by their top track.
func (m *MetroLayoutMap) sortByExtent(ids []int) {
	sort.SliceStable(ids, func(a, b int) bool {
		sa, sb := m.Spots[ids[a]], m.Spots[ids[b]]
		spanA, spanB := m.span(sa), m.span(sb)
		if spanA != spanB {
			return spanA < spanB
		}
		return m.top(sa) < m.top(sb)
	})
}

// addColumn appends a column holding the given spots, and records it in each spot's group.
func (m *MetroLayoutMap) addColumn(ids []int) {
	for _, s := range ids {
		m.Spots[s].Column = len(m.Columns)
		m.Groups[m.Spots[s].Group].Columns = append(m.Groups[m.Spots[s].Group].Columns, len(m.Columns))
	}
	m.Columns = append(m.Columns, ids)
}
