package core

import "slices"
import "sort"

// cloneMetroTracks copies an order of tracks, so an option can change without changing the order it came from.
func cloneMetroTracks(tracks [][]int) [][]int {
	out := make([][]int, len(tracks))
	for i, members := range tracks {
		out[i] = append([]int(nil), members...)
	}
	return out
}

// metroSingleTracks is each lane of order as a track of its own, in order.
func metroSingleTracks(order []int) [][]int {
	out := make([][]int, len(order))
	for i, lane := range order {
		out[i] = []int{lane}
	}
	return out
}

// improve moves one main line at a time to wherever it lowers the cost, until none does.
func (p metroLayoutPlan) improve(start []int) []int {
	order := append([]int(nil), start...)
	best := p.cost(metroSingleTracks(order))
	improved := true
	for improved {
		improved = false
		n := len(order)
		for i := 0; i < n; i++ {
			for j := 0; j < len(order); j++ {
				if j == i {
					continue
				}
				// Remove the line at i, then insert it at j of what is left.
				moved := append([]int(nil), order...)
				item := moved[i]
				moved = append(moved[:i], moved[i+1:]...)
				moved = append(moved[:j], append([]int{item}, moved[j:]...)...)
				if c := p.cost(metroSingleTracks(moved)); c < best-0.001 {
					best = c
					order = moved
					improved = true
				}
			}
		}
	}
	return order
}

// metroInsertTrack is tracks with track inserted at position t, as Array.insert(_:at:).
func metroInsertTrack(tracks [][]int, t int, track []int) [][]int {
	out := make([][]int, 0, len(tracks)+1)
	out = append(out, tracks[:t]...)
	out = append(out, track)
	out = append(out, tracks[t:]...)
	return out
}

// branch places each branch on the track, new or shared, where it costs least.
func (p metroLayoutPlan) branch(mains []int) [][]int {
	tracks := metroSingleTracks(mains)
	var branches []int
	for i, lane := range p.Lanes {
		if !lane.isMain() {
			branches = append(branches, i)
		}
	}
	sort.SliceStable(branches, func(a, b int) bool {
		la, lb := p.Lanes[branches[a]], p.Lanes[branches[b]]
		if la.Path.Depth != lb.Path.Depth {
			return la.Path.Depth < lb.Path.Depth
		}
		return la.Start < lb.Start
	})
	for _, lane := range branches {
		var options [][][]int
		for t := range tracks {
			fits := !slices.ContainsFunc(tracks[t], func(member int) bool {
				return p.Lanes[member].span().overlaps(p.Lanes[lane].span())
			})
			if !fits {
				continue
			}
			option := cloneMetroTracks(tracks)
			option[t] = append(option[t], lane)
			options = append(options, option)
		}
		for t := 0; t <= len(tracks); t++ {
			options = append(options, metroInsertTrack(cloneMetroTracks(tracks), t, []int{lane}))
		}
		// min(by:) keeps the first of equal minima.
		best := options[0]
		bestCost := p.cost(best)
		for _, option := range options[1:] {
			if c := p.cost(option); c < bestCost {
				best = option
				bestCost = c
			}
		}
		tracks = best
	}
	return tracks
}

// best is the best order of the tracks over several starts.
func (p metroLayoutPlan) best() [][]int {
	var mains []int
	for i, lane := range p.Lanes {
		if lane.isMain() {
			mains = append(mains, i)
		}
	}
	first := map[int]float64{}
	for _, i := range mains {
		first[i] = 0
		if stops := p.Lanes[i].Path.Stops; len(stops) > 0 {
			if l, ok := p.Layer[stops[0]]; ok {
				first[i] = l
			}
		}
	}
	byFirst := append([]int(nil), mains...)
	sort.SliceStable(byFirst, func(a, b int) bool {
		fa, fb := first[byFirst[a]], first[byFirst[b]]
		if fa != fb {
			return fa < fb
		}
		return byFirst[a] < byFirst[b]
	})
	reversed := make([]int, len(mains))
	for i, m := range mains {
		reversed[len(mains)-1-i] = m
	}
	var seen [][]int
	for _, start := range [][]int{mains, byFirst, reversed} {
		order := p.improve(start)
		if !metroContainsOrder(seen, order) {
			seen = append(seen, order)
		}
	}
	if len(seen) == 0 {
		return metroSingleTracks(mains)
	}
	// min(by:) keeps the first of equal minima.
	best := p.branch(seen[0])
	bestCost := p.cost(best)
	for _, order := range seen[1:] {
		candidate := p.branch(order)
		if c := p.cost(candidate); c < bestCost {
			best = candidate
			bestCost = c
		}
	}
	return best
}

// metroContainsOrder reports whether orders holds order.
func metroContainsOrder(orders [][]int, order []int) bool {
	for _, existing := range orders {
		if len(existing) != len(order) {
			continue
		}
		same := true
		for i := range existing {
			if existing[i] != order[i] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}
