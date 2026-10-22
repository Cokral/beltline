// The layout walk: the pieces of a
// route, the belt as drawn with its stage numbers, and the stages a run goes
// through along the routes it took.

package core

import (
	"math"
	"slices"
	"sort"
)

// Pieces returns the stages and nested forks of one route, or of the line when
// parent is nil.
func (l BeltLayout) Pieces(parent *string) []BeltLayoutPiece {
	type ranked struct {
		index int
		tie   int
		piece BeltLayoutPiece
	}
	var rs []ranked
	tie := 0
	for i := range l.Stages {
		if !SamePointee(l.Stages[i].Route, parent) {
			continue
		}
		s := l.Stages[i]
		rs = append(rs, ranked{index: i, tie: tie, piece: BeltLayoutPiece{Kind: BeltLayoutPieceStage, Stage: &s}})
		tie++
	}
	for i := range l.Forks {
		f := l.Forks[i]
		if !SamePointee(f.Parent, parent) {
			continue
		}
		index := math.MaxInt
		if d := l.descendantIndices(f); len(d) > 0 {
			index = d[0]
			for _, x := range d[1:] {
				index = min(index, x)
			}
		} else if f.At != nil {
			index = *f.At
		}
		rs = append(rs, ranked{index: index, tie: tie, piece: BeltLayoutPiece{Kind: BeltLayoutPieceFork, Fork: &f}})
		tie++
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].index != rs[j].index {
			return rs[i].index < rs[j].index
		}
		return rs[i].tie < rs[j].tie
	})
	out := make([]BeltLayoutPiece, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.piece)
	}
	return out
}

// Segments is the belt as it is drawn.
func (l BeltLayout) Segments() []BeltLayoutSegment {
	return l.sequence(nil)
}

// Number is a stage's number as drawn: its column, so each route of a fork
// counts from the same place and the stage where they meet has one number.
func (l BeltLayout) Number(stageID string) int {
	var search func(segments []BeltLayoutSegment, column *int) (int, bool)
	search = func(segments []BeltLayoutSegment, column *int) (int, bool) {
		for _, segment := range segments {
			if segment.Kind == BeltLayoutSegmentStage {
				*column++
				if segment.Stage.ID == stageID {
					return *column, true
				}
				continue
			}
			start := *column
			width := 0
			found, hit := 0, false
			for _, route := range segment.Routes {
				cursor := start
				if n, ok := search(route, &cursor); ok {
					found, hit = n, true
				}
				width = max(width, cursor-start)
			}
			*column = start + max(width, 1)
			if hit {
				return found, true
			}
		}
		return 0, false
	}
	column := 0
	n, _ := search(l.Segments(), &column)
	return n
}

// Path returns the stages a run goes through: at each fork the routes it took,
// in the belt's order; where it has not chosen yet, the first route, or with
// longest the longest one.
func (l BeltLayout) Path(routes map[string][]string, longest bool) []Stage {
	return l.walk(l.Segments(), routes, longest)
}

// sequence is Segments for one route or the line.
func (l BeltLayout) sequence(parent *string) []BeltLayoutSegment {
	return l.sequenceSeen(parent, map[string]struct{}{})
}

// sequenceSeen is sequence with the forks already seen on the way down.
func (l BeltLayout) sequenceSeen(parent *string, seen map[string]struct{}) []BeltLayoutSegment {
	pieces := l.Pieces(parent)
	out := make([]BeltLayoutSegment, 0, len(pieces))
	for _, piece := range pieces {
		if piece.Kind == BeltLayoutPieceStage {
			out = append(out, BeltLayoutSegment{Kind: BeltLayoutSegmentStage, Stage: piece.Stage})
			continue
		}
		f := piece.Fork
		if forkHasKey(seen, f.ID) {
			out = append(out, BeltLayoutSegment{Kind: BeltLayoutSegmentFork, Fork: f})
			continue
		}
		routes := make([][]BeltLayoutSegment, 0, len(f.Routes))
		for _, r := range f.Routes {
			rid := r.ID
			routes = append(routes, l.sequenceSeen(&rid, forkSeen(seen, f.ID)))
		}
		out = append(out, BeltLayoutSegment{Kind: BeltLayoutSegmentFork, Fork: f, Routes: routes})
	}
	return out
}

// successor is the stage after stageID, or the step where its route ends.
func (l BeltLayout) successor(stageID string) *BeltLayoutStep {
	var parent *string
	if s := l.stageNamed(stageID); s != nil {
		parent = s.Route
	}
	items := l.Pieces(parent)
	at := l.indexOfStagePiece(items, stageID)
	if at < 0 {
		return nil
	}
	if at+1 < len(items) {
		step := l.opening(items[at+1])
		return &step
	}
	if parent == nil {
		step := forkStep(nil)
		return &step
	}
	f := l.ForkWithRoute(*parent)
	if f == nil {
		step := forkStep(nil)
		return &step
	}
	step := l.resumeFork(*f, true)
	return &step
}

// opening is where a run goes when it starts at piece: the piece's stage, or
// the first route of its fork.
func (l BeltLayout) opening(piece BeltLayoutPiece) BeltLayoutStep {
	if piece.Kind == BeltLayoutPieceStage {
		id := piece.Stage.ID
		return forkStep(&id)
	}
	f := piece.Fork
	if len(f.Routes) == 0 {
		return forkStep(nil)
	}
	first := f.Routes[0].ID
	items := l.piecesIn(first)
	var step BeltLayoutStep
	if len(items) > 0 {
		step = l.opening(items[0])
	} else {
		step = l.resumeFork(*f, true)
	}
	if step.Routes == nil {
		step.Routes = map[string][]string{}
	}
	step.Routes[f.ID] = []string{first}
	return step
}

// walk is the stages a run goes through in segments, given the routes taken.
func (l BeltLayout) walk(segments []BeltLayoutSegment, routes map[string][]string, longest bool) []Stage {
	var out []Stage
	for _, segment := range segments {
		if segment.Kind == BeltLayoutSegmentStage {
			out = append(out, *segment.Stage)
			continue
		}
		taken := routes[segment.Fork.ID]
		var chosen []BeltLayoutSegment
		switch {
		case len(taken) > 0:
			for i, r := range segment.Fork.Routes {
				if i < len(segment.Routes) && slices.Contains(taken, r.ID) {
					chosen = append(chosen, segment.Routes[i]...)
				}
			}
		case longest:
			// The first route with the most stages wins a tie.
			if len(segment.Routes) > 0 {
				best := 0
				bestCount := len(l.walk(segment.Routes[0], routes, true))
				for i := 1; i < len(segment.Routes); i++ {
					if n := len(l.walk(segment.Routes[i], routes, true)); bestCount < n {
						best, bestCount = i, n
					}
				}
				chosen = segment.Routes[best]
			}
		default:
			if len(segment.Routes) > 0 {
				chosen = segment.Routes[0]
			}
		}
		out = append(out, l.walk(chosen, routes, longest)...)
	}
	return out
}

// descendantIndices are the positions of the stages in fork's routes and in
// the forks nested in them.
func (l BeltLayout) descendantIndices(fork Fork) []int {
	routeIDs := map[string]struct{}{}
	for _, r := range fork.Routes {
		routeIDs[r.ID] = struct{}{}
	}
	seen := map[string]struct{}{fork.ID: {}}
	changed := true
	for changed {
		changed = false
		for _, child := range l.Forks {
			if child.Parent == nil {
				continue
			}
			if _, ok := routeIDs[*child.Parent]; !ok {
				continue
			}
			if _, dup := seen[child.ID]; dup {
				continue
			}
			seen[child.ID] = struct{}{}
			for _, r := range child.Routes {
				routeIDs[r.ID] = struct{}{}
			}
			changed = true
		}
	}
	var out []int
	for i, s := range l.Stages {
		if s.Route == nil {
			continue
		}
		if _, ok := routeIDs[*s.Route]; ok {
			out = append(out, i)
		}
	}
	return out
}

// isAfter reports whether fork's stages end before stageID in the list.
func (l BeltLayout) isAfter(fork Fork, stageID string) bool {
	index := l.indexOfStage(stageID)
	span := l.Span(fork)
	if index < 0 || span == nil {
		return false
	}
	return index > span.Upper
}
