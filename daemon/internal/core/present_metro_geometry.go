// The Group and Spot
// records, the Map builder and its placement of machines, columns and rows.
//
// Where everything on the metro map sits. Machines follow the Floor's layers
// left to right, each in a column of its own so no two stations line up.
// Every path has a row of its own, with room around it; where lines travel
// between the same two stations, or stop at the same interchange, they pull
// together into a snug bundle, and go back to their rows when they part.
// The columns are then swept left to right, as close as the bends, dots and
// names between them allow. The rows and the sweep are in
// present_metro_geometry_rows.go and present_metro_geometry_sweep.go; the spots, lanes and labels are in
// present_metro_geometry_spots.go, present_metro_geometry_lanes.go and present_metro_geometry_labels.go.

package core

import (
	"sort"
	"unicode/utf8"
)

// Spacing of the metro map, in points.
const (
	// metroMapRowGap is between two paths' home rows.
	metroMapRowGap = 36.0
	// metroMapHomeGap is the least room between two home rows: a name pill and a little air.
	metroMapHomeGap = 30.0
	// metroMapApart is the least room between a bundle and a line beside it.
	metroMapApart = 30.0
	// metroMapGroupGap is between two groups of belts that share no machine, where they run side by side.
	metroMapGroupGap   = 96.0
	metroMapStep       = 34.0
	metroMapArrival    = 12.0
	metroMapTextHeight = 13.0
)

// metroMapLabelOverflow is the prefix of the "+n" mark for runs past a station's dots. Copy from MetroLayout.Map.labelText.
const metroMapLabelOverflow = " +"

// MetroLayoutGroup is one group of belts that share machines, with its plan and its rows.
type MetroLayoutGroup struct {
	Routes []int
	Plan   metroLayoutPlan
	// Homes is each track's home row.
	Homes []float64
	// Entries is where a main line leaves its name pill, when not on its home row.
	Entries map[int]float64
	// Columns are the columns this group has a station in, left to right.
	Columns []int
	// Ys is each lane's y at each column it runs through.
	Ys     map[int]map[int]float64
	Bottom float64
}

// MetroLayoutSpot is a machine on the map: where it is in the layers and which lane each belt stops on.
type MetroLayoutSpot struct {
	ID    string
	Group int
	Layer float64
	// Members maps each belt that stops here to its lane.
	Members map[string]int
	Column  int
}

// metroLayoutBack is a belt's dotted lane back to an earlier machine: the columns it leaves and reaches
// (-1 for the name pill), and the y it runs at.
type metroLayoutBack struct {
	From int
	To   int
	Y    float64
}

// metroLayoutSegment is a straight segment between two points.
type metroLayoutSegment struct {
	A MetroLayoutPoint
	B MetroLayoutPoint
}

// MetroLayoutMap is the builder of a metro map.
type MetroLayoutMap struct {
	Routes    []MetroLayoutRoute
	Track     FactoryTrack
	Runs      []Run
	CharWidth float64
	Paused    map[string]struct{}
	Pause     Pause
	Ambiguous map[string]struct{}
	Groups    []MetroLayoutGroup
	Spots     []MetroLayoutSpot
	SpotIndex map[string]int
	Columns   [][]int
	Xs        []float64
	PillRight float64
	Colours   map[string]int
	// Active are the runs at a machine on a belt, in the order their dots are drawn.
	Active map[string][]Run

	Lines    []MetroLayoutLine
	Stations []MetroLayoutStation
	Dots     []MetroLayoutDot
	Labels   []MetroLayoutLabel
	Width    float64
	Height   float64
}

// NewMetroLayoutMap is an empty builder over routes, track and runs.
func NewMetroLayoutMap(routes []MetroLayoutRoute, track FactoryTrack, runs []Run, charWidth float64, paused map[string]struct{}, pause Pause) MetroLayoutMap {
	return MetroLayoutMap{
		Routes:    routes,
		Track:     track,
		Runs:      runs,
		CharWidth: charWidth,
		Paused:    paused,
		Pause:     pause,
		Ambiguous: RepoLabelAmbiguousNames(runs),
		SpotIndex: map[string]int{},
		Active:    map[string][]Run{},
	}
}

// textWidth is the width of text at the map's character width.
// The product is rounded before any sum takes it. Characters are counted as runes.
func (m MetroLayoutMap) textWidth(text string) float64 {
	return float64(float64(utf8.RuneCountInString(text)) * m.CharWidth)
}

// metroMapKey is the key of the runs at a machine on a belt.
func metroMapKey(machine, belt string) string {
	return machine + "\u0000" + belt
}

// build draws the whole map: plans each group, places its rows, sweeps the columns and draws the
// lines, stations, dots and labels.
func (m *MetroLayoutMap) build() {
	var routeIDs []string
	pillWidth := 0.0
	for i, route := range m.Routes {
		routeIDs = append(routeIDs, route.Belt.ID)
		if w := m.textWidth(route.Belt.Name) + 22; i == 0 || w > pillWidth {
			pillWidth = w
		}
	}
	m.Colours = MetroLayoutColours(routeIDs)
	m.PillRight = 16 + pillWidth
	order := map[RunState]int{RunStateRunning: 0, RunStateWaiting: 1, RunStateHeld: 2, RunStateQueued: 3, RunStateParked: 4}
	rank := func(state RunState) int {
		if r, ok := order[state]; ok {
			return r
		}
		return 9
	}
	for _, machine := range m.Track.Machines {
		grouped := map[string][]Run{}
		var belts []string
		for _, run := range m.Track.RunsAt(machine, m.Runs) {
			if MetroLayoutLookFor(run.State) == nil {
				continue
			}
			if _, ok := grouped[run.BeltID]; !ok {
				belts = append(belts, run.BeltID)
			}
			grouped[run.BeltID] = append(grouped[run.BeltID], run)
		}
		for _, belt := range belts {
			here := grouped[belt]
			sort.SliceStable(here, func(a, b int) bool { return rank(here[a].State) < rank(here[b].State) })
			m.Active[metroMapKey(machine.ID, belt)] = here
		}
	}
	for _, members := range metroLayoutComponents(m.Routes, m.Track) {
		routes := make([]MetroLayoutRoute, 0, len(members))
		for _, r := range members {
			routes = append(routes, m.Routes[r])
		}
		m.Groups = append(m.Groups, MetroLayoutGroup{Routes: members, Plan: metroLayoutPlanCached(routes, m.Track), Entries: map[int]float64{}, Ys: map[int]map[int]float64{}})
	}
	m.placeSpots()
	for g := range m.Groups {
		m.Groups[g].Homes = make([]float64, len(m.Groups[g].Plan.Tracks))
		for t := range m.Groups[g].Plan.Tracks {
			m.Groups[g].Homes[t] = 44 + float64(float64(t)*metroMapRowGap)
		}
		m.placeRows(g)
		// Each row moves to where its line mostly runs, as far as the rows
		// around it allow, so lines reach their bundles with short bends.
		for i := 0; i < 6; i++ {
			m.settle(g)
			m.placeRows(g)
		}
		m.raise(g)
		m.lift(g, 44)
		m.stack(g)
	}
	m.sweep()
	m.draw()
	m.placeText()
}
