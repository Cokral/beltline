//
// Each lane's row at each column: bundles, settled home rows, the lift and stack
// of groups, the dotted returns, and the sweep that places the columns.

package core

import (
	"maps"
	"math"
	"slices"
	"sort"
)

// metroSumF is the sum of values, added from zero in order, as reduce(0, +).
func metroSumF(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}

// metroMinF is the least of values; the caller checks there are some.
func metroMinF(values []float64) float64 {
	best := values[0]
	for _, v := range values[1:] {
		if v < best {
			best = v
		}
	}
	return best
}

// metroMaxF is the greatest of values; the caller checks there are some.
func metroMaxF(values []float64) float64 {
	best := values[0]
	for _, v := range values[1:] {
		if v >= best {
			best = v
		}
	}
	return best
}

// placeRows places each lane's y at each column: on its home row when alone, snug in a bundle with the
// lanes beside it on the same errand. A bundle keeps its lanes where they were as far as it can, and
// never comes closer than apart to the line above it.
func (m *MetroLayoutMap) placeRows(g int) {
	plan := m.Groups[g].Plan
	homes := m.Groups[g].Homes
	track := func(lane int) int {
		if t := plan.trackOf(lane, plan.Tracks); t != nil {
			return *t
		}
		return 0
	}
	through := map[int][]int{}
	for lane := range plan.Lanes {
		for _, c := range m.laneRuns(g, lane) {
			through[c] = append(through[c], lane)
		}
	}
	last := map[int]float64{}
	ys := map[int]map[int]float64{}
	bottom := 0.0
	if len(homes) > 0 {
		bottom = metroMaxF(homes)
	}
	for _, c := range m.Groups[g].Columns {
		here := append([]int(nil), through[c]...)
		sort.SliceStable(here, func(a, b int) bool {
			if track(here[a]) != track(here[b]) {
				return track(here[a]) < track(here[b])
			}
			return here[a] < here[b]
		})
		var bundles [][]int
		for _, lane := range here {
			if n := len(bundles); n > 0 {
				previous := bundles[n-1][len(bundles[n-1])-1]
				if m.errand(g, previous, c) == m.errand(g, lane, c) {
					bundles[n-1] = append(bundles[n-1], lane)
					continue
				}
			}
			bundles = append(bundles, []int{lane})
		}
		floor := math.Inf(-1)
		for _, bundle := range bundles {
			count := len(bundle)
			offsets := make([]float64, count)
			for i := range bundle {
				offsets[i] = float64((float64(i) - float64(count-1)/2) * MetroLayoutLanePitch)
			}
			var centre float64
			if count == 1 {
				centre = homes[track(bundle[0])]
			} else {
				terms := make([]float64, count)
				for i, lane := range bundle {
					v, ok := last[lane]
					if !ok {
						v = homes[track(lane)]
					}
					terms[i] = v - offsets[i]
				}
				centre = metroSumF(terms) / float64(count)
			}
			centre = math.Round(float64(centre*2)) / 2
			if centre+offsets[0] < floor+metroMapApart {
				centre = floor + metroMapApart - offsets[0]
			}
			for i, lane := range bundle {
				y := centre + offsets[i]
				if ys[lane] == nil {
					ys[lane] = map[int]float64{}
				}
				ys[lane][c] = y
				last[lane] = y
			}
			floor = centre + offsets[count-1]
			// max(bottom, floor)
			if floor >= bottom {
				bottom = floor
			}
		}
		// A lane that is not running here goes back to its row when it next runs.
		for lane := range last {
			if !slices.Contains(here, lane) {
				delete(last, lane)
			}
		}
	}
	m.Groups[g].Ys = ys
	m.Groups[g].Bottom = bottom
}

// settle moves each track's home row to the mean of where its lanes meet other lines: in bundles, and
// where a branch leaves and rejoins its line. Rows keep their order and at least homeGap between them.
func (m *MetroLayoutMap) settle(g int) {
	plan := m.Groups[g].Plan
	homes := m.Groups[g].Homes
	wanted := append([]float64(nil), homes...)
	for t, lanes := range plan.Tracks {
		var ys []float64
		for _, lane := range lanes {
			row := m.Groups[g].Ys[lane]
			columns := slices.Sorted(maps.Keys(row))
			for _, c := range columns {
				if row[c] != homes[t] {
					ys = append(ys, row[c])
				}
			}
			type attach struct{ column, lane int }
			var attachments []attach
			if c, l, ok := m.origin(g, lane); ok {
				attachments = append(attachments, attach{c, l})
			}
			if c, l, ok := m.target(g, lane); ok {
				attachments = append(attachments, attach{c, l})
			}
			for _, a := range attachments {
				if y := m.y(g, a.lane, a.column); y != nil {
					ys = append(ys, *y, *y)
				}
			}
		}
		if len(ys) > 0 {
			wanted[t] = metroSumF(ys) / float64(len(ys))
		}
	}
	settled := append([]float64(nil), wanted...)
	for t := 1; t < len(settled); t++ {
		settled[t] = metroMax(settled[t], settled[t-1]+metroMapHomeGap)
	}
	for t := len(settled) - 2; t >= 0; t-- {
		settled[t] = metroMin(settled[t], settled[t+1]-metroMapHomeGap)
	}
	rounded := make([]float64, len(settled))
	for t, v := range settled {
		rounded[t] = math.Round(v)
	}
	m.Groups[g].Homes = rounded
}

// lift moves a whole group so its highest line sits at top.
func (m *MetroLayoutMap) lift(g int, top float64) {
	ys := append([]float64(nil), m.Groups[g].Homes...)
	for _, row := range m.Groups[g].Ys {
		for _, v := range row {
			ys = append(ys, v)
		}
	}
	low := top
	if len(ys) > 0 {
		low = metroMinF(ys)
	}
	m.move(g, top-low)
}

// move moves a whole group by shift.
func (m *MetroLayoutMap) move(g int, shift float64) {
	group := &m.Groups[g]
	for t := range group.Homes {
		group.Homes[t] = group.Homes[t] + shift
	}
	for lane, v := range group.Entries {
		group.Entries[lane] = v + shift
	}
	for _, row := range group.Ys {
		for c, v := range row {
			row[c] = v + shift
		}
	}
	group.Bottom += shift
}

// raise lifts the stretch a main line runs alone from its name pill, with no line under it, as close under
// the lines above it as they allow there, so a line that lines further on keep low does not run far from
// the rest all the way. It steps down to its row where they need it to, a row or more at a time.
func (m *MetroLayoutMap) raise(g int) {
	plan := m.Groups[g].Plan
	track := func(lane int) int {
		if t := plan.trackOf(lane, plan.Tracks); t != nil {
			return *t
		}
		return 0
	}
	var mains []int
	for lane, item := range plan.Lanes {
		if item.isMain() {
			mains = append(mains, lane)
		}
	}
	sort.SliceStable(mains, func(a, b int) bool { return track(mains[a]) < track(mains[b]) })
	for _, lane := range mains {
		home := m.Groups[g].Homes[track(lane)]
		// The least y it may take at each column of the stretch, never less than at the ones before.
		type least struct {
			column int
			y      float64
		}
		var leastList []least
		for _, c := range m.laneRuns(g, lane) {
			var others []float64
			for key, row := range m.Groups[g].Ys {
				if key == lane {
					continue
				}
				if v, ok := row[c]; ok {
					others = append(others, v)
				}
			}
			at := m.y(g, lane, c)
			if at == nil || *at != home {
				break
			}
			blocked := slices.ContainsFunc(others, func(o float64) bool { return o > home-metroMapApart })
			if blocked {
				break
			}
			above := math.Inf(-1)
			if len(others) > 0 {
				above = metroMaxF(others) + metroMapRowGap
			}
			prev := math.Inf(-1)
			if n := len(leastList); n > 0 {
				prev = leastList[n-1].y
			}
			leastList = append(leastList, least{column: c, y: metroMax(prev, above)})
		}
		level := home
		for i := len(leastList) - 1; i >= 0; i-- {
			item := leastList[i]
			if !math.IsInf(item.y, 0) && !math.IsNaN(item.y) && item.y <= level-metroMapRowGap {
				level = item.y
			}
			if row, ok := m.Groups[g].Ys[lane]; ok {
				row[item.column] = level
			}
		}
		if level != home {
			m.Groups[g].Entries[lane] = level
		}
	}
}

// ysAt is where a group's lines run at a column, or at their name pills before its first.
func (m MetroLayoutMap) ysAt(g, k int) []float64 {
	group := m.Groups[g]
	if len(group.Columns) == 0 {
		return nil
	}
	first, last := slices.Min(group.Columns), slices.Max(group.Columns)
	if k > last+1 {
		return nil
	}
	if k < first {
		var out []float64
		for lane, item := range group.Plan.Lanes {
			if item.isMain() {
				if e := m.entry(g, lane); e != nil {
					out = append(out, *e)
				}
			}
		}
		return out
	}
	// The nearest column at or before k, and the nearest at or after it.
	var near []int
	hasBefore, hasAfter := false, false
	before, after := 0, 0
	for _, c := range group.Columns {
		if c <= k && (!hasBefore || c > before) {
			before, hasBefore = c, true
		}
		if c >= k && (!hasAfter || c < after) {
			after, hasAfter = c, true
		}
	}
	if hasBefore {
		near = append(near, before)
	}
	if hasAfter {
		near = append(near, after)
	}
	var out []float64
	for _, c := range near {
		for _, row := range group.Ys {
			if v, ok := row[c]; ok {
				out = append(out, v)
			}
		}
	}
	return out
}

// backs is a belt's dotted lanes back to an earlier machine, one per return (nil when it has none there).
func (m MetroLayoutMap) backs(g, r int) []*metroLayoutBack {
	out := make([]*metroLayoutBack, len(m.Routes[r].Backs))
	for k, back := range m.Routes[r].Backs {
		to := m.spot(&back.To)
		if to == nil {
			continue
		}
		from := -1
		if s := m.spot(back.From); s != nil {
			from = s.Column
		}
		lo, hi := from, to.Column
		if hi < lo {
			lo, hi = hi, lo
		}
		var ys []float64
		for c := lo; c <= hi; c++ {
			ys = append(ys, m.ysAt(g, c)...)
		}
		lowest := m.Groups[g].Bottom
		if len(ys) > 0 {
			lowest = metroMaxF(ys)
		}
		out[k] = &metroLayoutBack{From: from, To: to.Column, Y: lowest + 26 + float64(float64(k)*5)}
	}
	return out
}

// extent is the highest and lowest a group's lines and dotted lanes run at a column.
func (m MetroLayoutMap) extent(g, k int) *MetroRange {
	ys := m.ysAt(g, k)
	if len(ys) == 0 {
		return nil
	}
	for _, r := range m.Groups[g].Routes {
		for _, back := range m.backs(g, r) {
			if back == nil {
				continue
			}
			lo, hi := back.From, back.To
			if hi < lo {
				lo, hi = hi, lo
			}
			if lo-1 <= k && k <= hi+1 {
				ys = append(ys, back.Y)
			}
		}
	}
	return &MetroRange{Lower: metroMinF(ys), Upper: metroMaxF(ys)}
}

// stack moves a group below the ones placed before it, groupGap under their lines wherever it runs beside them.
func (m *MetroLayoutMap) stack(g int) {
	shift := math.Inf(-1)
	for p := 0; p < g; p++ {
		for k := -1; k <= len(m.Columns); k++ {
			above := m.extent(p, k)
			here := m.extent(g, k)
			if above == nil || here == nil {
				continue
			}
			shift = metroMax(shift, above.Upper+metroMapGroupGap-here.Lower)
		}
	}
	if !math.IsInf(shift, 0) && !math.IsNaN(shift) {
		m.move(g, metroMax(shift, 0))
	}
}
