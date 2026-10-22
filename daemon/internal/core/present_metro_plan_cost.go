package core

import "slices"

// cost is how badly an order reads: lines running through interchanges they skip, branches crossing
// other belts' lines, and every track it takes.
func (p metroLayoutPlan) cost(tracks [][]int) float64 {
	c := metroLayoutCost{plan: p, tracks: tracks, placed: placedLanes(tracks), home: mainTracks(p, tracks)}
	total := float64(len(tracks)) * 4
	for _, at := range p.machines() {
		total += c.skippedInterchanges(at)
	}
	for t, members := range tracks {
		for _, lane := range members {
			if !p.Lanes[lane].isMain() {
				total += c.branchCost(lane, t)
			}
		}
	}
	return total
}

// metroLayoutCost is one order of tracks being costed: the plan, the tracks, the lanes already on a
// track and the track each belt's main line is on.
type metroLayoutCost struct {
	plan   metroLayoutPlan
	tracks [][]int
	placed map[int]struct{}
	home   map[string]int
}

// placedLanes is the set of lanes that sit on some track.
func placedLanes(tracks [][]int) map[int]struct{} {
	placed := map[int]struct{}{}
	for _, members := range tracks {
		for _, lane := range members {
			placed[lane] = struct{}{}
		}
	}
	return placed
}

// mainTracks is the track each belt's main line is on.
func mainTracks(p metroLayoutPlan, tracks [][]int) map[string]int {
	home := map[string]int{}
	for t, members := range tracks {
		for _, lane := range members {
			if p.Lanes[lane].isMain() {
				home[p.Lanes[lane].Path.Belt] = t
			}
		}
	}
	return home
}

// active reports whether track t holds a lane that passes test, other than a lane of except (when set).
func (c metroLayoutCost) active(t int, test func(metroLayoutLane) bool, except *string) bool {
	for _, lane := range c.tracks[t] {
		item := c.plan.Lanes[lane]
		if (except == nil || item.Path.Belt != *except) && test(item) {
			return true
		}
	}
	return false
}

// crossing is what a branch costs for crossing track k at x: crossing another belt's line costs most,
// crossing the belt's own other branches a little.
func (c metroLayoutCost) crossing(k int, x float64, belt string) float64 {
	covers := func(l metroLayoutLane) bool { return l.covers(x) }
	if !c.active(k, covers, nil) {
		return 0
	}
	if c.active(k, covers, &belt) {
		return 5
	}
	return 1.5
}

// stopTracks are the tracks a machine's stops sit on. A belt whose branches are not placed yet counts
// as stopping on its main line's track.
func (c metroLayoutCost) stopTracks(id string) map[int]struct{} {
	members := map[int]struct{}{}
	for t := range c.tracks {
		for _, lane := range c.tracks[t] {
			if slices.Contains(c.plan.Lanes[lane].Path.Stops, id) {
				members[t] = struct{}{}
				break
			}
		}
	}
	for lane, item := range c.plan.Lanes {
		if _, ok := c.placed[lane]; ok || !slices.Contains(item.Path.Stops, id) {
			continue
		}
		if t, ok := c.home[item.Path.Belt]; ok {
			members[t] = struct{}{}
		}
	}
	return members
}

// skippedInterchanges is what the lines running past a machine between its outermost tracks cost:
// 10 for a line through the machine's layer, 4 for one that ends in an earlier layer.
func (c metroLayoutCost) skippedInterchanges(at metroLayoutPlanMachine) float64 {
	members := c.stopTracks(at.ID)
	if len(members) == 0 {
		return 0
	}
	lo, hi := 0, 0
	first := true
	for t := range members {
		if first || t < lo {
			lo = t
		}
		if first || t > hi {
			hi = t
		}
		first = false
	}
	if hi-lo <= 1 {
		return 0
	}
	total := 0.0
	for t := lo + 1; t < hi; t++ {
		if _, ok := members[t]; ok {
			continue
		}
		// A line that ends at another station in this layer may still pass this one first.
		if c.active(t, func(l metroLayoutLane) bool { return l.through(at.Layer) }, nil) {
			total += 10
		} else if c.active(t, func(l metroLayoutLane) bool { return l.Start < at.Layer && l.End == at.Layer }, nil) {
			total += 4
		}
	}
	return total
}

// branchCost is what a branch lane on track t costs: the distance to the tracks of its stops, and the
// lines it crosses on the way.
func (c metroLayoutCost) branchCost(lane, t int) float64 {
	item := c.plan.Lanes[lane]
	path := item.Path
	total := 0.0
	if from := c.plan.trackAt(path.Belt, path.From, c.tracks, lane); from != nil {
		total += float64(absInt(*from-t)) * 0.5
		for k := min(*from, t) + 1; k < max(*from, t); k++ {
			total += c.crossing(k, item.Start+0.5, path.Belt)
		}
	}
	if path.To != nil {
		if into := c.plan.trackAt(path.Belt, path.To, c.tracks, lane); into != nil {
			total += float64(absInt(*into-t)) * 0.5
			for k := min(*into, t) + 1; k < max(*into, t); k++ {
				total += c.crossing(k, item.End-0.5, path.Belt)
			}
		}
	}
	return total
}
