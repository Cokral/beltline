// The lookups and id helpers
// the editing methods share, and the flattening of a belt back to its stages.

package core

import (
	"strconv"
)

// forget moves outcomes that took a route in routes to the next stage, once
// none of their routes is left.
func (b *Belt) forget(routes map[string]struct{}) {
	for s := range b.Stages {
		for o := range b.Stages[s].Outcomes {
			then := &b.Stages[s].Outcomes[o].Then
			if then.Action != ThenActionRoute {
				continue
			}
			var kept []string
			for _, r := range then.Routes {
				if !forkHasKey(routes, r) {
					kept = append(kept, r)
				}
			}
			then.Routes = kept
			if len(then.Routes) == 0 {
				then.Action = ThenActionNext
			}
		}
	}
}

// freshRouteID is an unused route id, also avoiding the ids in also.
func (b Belt) freshRouteID(also map[string]struct{}) string {
	taken := forkRouteIDSet(b.Forks)
	for k := range also {
		taken[k] = struct{}{}
	}
	return freshID("route", taken)
}

// freshID is the first "<base>-<n>" id not in taken.
func freshID(base string, taken map[string]struct{}) string {
	for n := 1; ; n++ {
		id := base + "-" + strconv.Itoa(n)
		if !forkHasKey(taken, id) {
			return id
		}
	}
}

// subtree is the route and every route of a fork nested inside it, however
// deep.
func (b Belt) subtree(routeID string) map[string]struct{} {
	gone := map[string]struct{}{routeID: {}}
	changed := true
	for changed {
		changed = false
		for _, f := range b.Forks {
			if f.Parent == nil || !forkHasKey(gone, *f.Parent) {
				continue
			}
			for _, r := range f.Routes {
				if !forkHasKey(gone, r.ID) {
					gone[r.ID] = struct{}{}
					changed = true
				}
			}
		}
	}
	return gone
}

// pieceOverride replaces the pieces of one route when flattening.
// A nil Route is the line.
type pieceOverride struct {
	Route *string
	Items []BeltLayoutPiece
}

// flatten is the belt's stages in order, with the pieces of one route replaced
// by override when given.
func (b Belt) flatten(parent *string, override *pieceOverride) []Stage {
	return b.flattenSeen(parent, override, map[string]struct{}{})
}

// flattenSeen is flatten with the forks already seen on the way down.
func (b Belt) flattenSeen(parent *string, override *pieceOverride, seen map[string]struct{}) []Stage {
	var items []BeltLayoutPiece
	if override != nil && SamePointee(override.Route, parent) {
		items = override.Items
	} else {
		items = b.Layout().Pieces(parent)
	}
	var result []Stage
	for _, item := range items {
		if item.Kind == BeltLayoutPieceStage {
			result = append(result, *item.Stage)
			continue
		}
		if forkHasKey(seen, item.Fork.ID) {
			continue
		}
		for _, r := range item.Fork.Routes {
			rid := r.ID
			result = append(result, b.flattenSeen(&rid, override, forkSeen(seen, item.Fork.ID))...)
		}
	}
	return result
}

// stageIndex is the position of the first stage with id, or -1.
func (b Belt) stageIndex(id string) int {
	return b.Layout().indexOfStage(id)
}

// forkNamed is a copy of the fork with id, or nil.
func (b Belt) forkNamed(id string) *Fork {
	return b.Layout().Fork(id)
}

// forkIDSet is the set of the ids of forks.
func forkIDSet(forks []Fork) map[string]struct{} {
	out := make(map[string]struct{}, len(forks))
	for _, f := range forks {
		out[f.ID] = struct{}{}
	}
	return out
}

// forkRouteIDSet is the set of every route id in forks.
func forkRouteIDSet(forks []Fork) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range forks {
		for _, r := range f.Routes {
			out[r.ID] = struct{}{}
		}
	}
	return out
}

// forkInsertStage is a new slice with stage inserted at index; the input is not changed.
func forkInsertStage(stages []Stage, index int, stage Stage) []Stage {
	out := make([]Stage, 0, len(stages)+1)
	out = append(out, stages[:index]...)
	out = append(out, stage)
	return append(out, stages[index:]...)
}
