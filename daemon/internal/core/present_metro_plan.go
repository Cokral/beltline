//
// The order of a group's tracks: the lanes placed on the map, the cost of an order and the search for the
// cheapest one, and the cache of plans. The routes themselves are in present_metro_routes.go.

package core

import (
	"slices"
	"sort"
)

// metroLayoutLane is a path placed on the map, with the stretch of layers it takes up.
type metroLayoutLane struct {
	Path  MetroLayoutPath
	Start float64
	End   float64
}

// isMain reports whether the lane is a main line.
func (l metroLayoutLane) isMain() bool {
	return l.Path.Parent == nil
}

// isOwnEnd reports whether the lane ends on its own.
func (l metroLayoutLane) isOwnEnd() bool {
	return l.Path.To == nil
}

// through reports whether the lane runs past a station at layer without ending there.
func (l metroLayoutLane) through(layer float64) bool {
	return l.Start < layer && layer < l.End
}

// covers reports whether the lane is drawn at x, in layers.
func (l metroLayoutLane) covers(x float64) bool {
	end := l.End
	if l.isOwnEnd() {
		end = l.End + 0.5
	}
	return l.Start < x && x < end
}

// span is the rows the lane takes up.
func (l metroLayoutLane) span() MetroRange {
	lower := l.Start + 0.1
	if l.isMain() {
		lower = -1.5
	}
	upper := l.End - 0.1
	if l.isOwnEnd() {
		upper = l.End + 0.6
	}
	return MetroRange{Lower: lower, Upper: upper}
}

// metroLayoutPlan is the order of a group's tracks, top to bottom, and the lanes on each.
type metroLayoutPlan struct {
	Lanes  []metroLayoutLane
	Tracks [][]int
	// Layer is the layer each machine sits in.
	Layer map[string]float64
}

// metroLayoutPlanMachine is a machine on lanes with the layer it sits in.
type metroLayoutPlanMachine struct {
	ID    string
	Layer float64
}

// newMetroLayoutPlan plans the lanes of routes on track, then the order of their tracks.
func newMetroLayoutPlan(routes []MetroLayoutRoute, track FactoryTrack) metroLayoutPlan {
	layer := map[string]float64{}
	for _, machine := range track.Machines {
		layer[machine.ID] = float64(metroLayerOf(track, machine.ID))
	}
	var lanes []metroLayoutLane
	for _, route := range routes {
		for _, path := range route.Paths {
			last := 0.0
			if len(path.Stops) > 0 {
				if l, ok := layer[path.Stops[len(path.Stops)-1]]; ok {
					last = l
				}
			}
			start := -1.0
			if path.Parent != nil && path.From != nil {
				if l, ok := layer[*path.From]; ok {
					start = l
				}
			}
			end := metroMax(last, start+0.5)
			if path.To != nil {
				if l, ok := layer[*path.To]; ok {
					end = l
				}
			}
			lanes = append(lanes, metroLayoutLane{Path: path, Start: start, End: end})
		}
	}
	p := metroLayoutPlan{Lanes: lanes, Layer: layer}
	p.Tracks = p.best()
	return p
}

// machines are the machines on lanes, with the layer each sits in.
func (p metroLayoutPlan) machines() []metroLayoutPlanMachine {
	seen := map[string]struct{}{}
	var ids []string
	for _, lane := range p.Lanes {
		for _, stop := range lane.Path.Stops {
			if _, ok := seen[stop]; !ok {
				seen[stop] = struct{}{}
				ids = append(ids, stop)
			}
		}
	}
	sort.Strings(ids)
	out := make([]metroLayoutPlanMachine, 0, len(ids))
	for _, id := range ids {
		out = append(out, metroLayoutPlanMachine{ID: id, Layer: p.Layer[id]})
	}
	return out
}

// trackOf is the track a lane is on, among tracks.
func (p metroLayoutPlan) trackOf(lane int, tracks [][]int) *int {
	for t, members := range tracks {
		for _, member := range members {
			if member == lane {
				found := t
				return &found
			}
		}
	}
	return nil
}

// trackAt is the track a belt stops at a machine on (or its main line when machine is nil), among the
// lanes placed so far, other than besides.
func (p metroLayoutPlan) trackAt(belt string, machine *string, tracks [][]int, besides int) *int {
	if machine == nil {
		for i, lane := range p.Lanes {
			if lane.Path.Belt == belt && lane.isMain() {
				return p.trackOf(i, tracks)
			}
		}
		return nil
	}
	for t, members := range tracks {
		for _, member := range members {
			if member != besides && p.Lanes[member].Path.Belt == belt && slices.Contains(p.Lanes[member].Path.Stops, *machine) {
				found := t
				return &found
			}
		}
	}
	return nil
}
