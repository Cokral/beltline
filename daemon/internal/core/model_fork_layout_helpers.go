// The private lookups the layout
// methods share: stages, pieces, routes and fork id sets.

package core

import "slices"

// stageNamed is the first stage with id, or nil.
func (l BeltLayout) stageNamed(id string) *Stage {
	for i := range l.Stages {
		if l.Stages[i].ID == id {
			return &l.Stages[i]
		}
	}
	return nil
}

// indexOfStage is the position of the first stage with id in the list, or -1.
func (l BeltLayout) indexOfStage(id string) int {
	for i := range l.Stages {
		if l.Stages[i].ID == id {
			return i
		}
	}
	return -1
}

// indexWithin is the position of stageID in StagesWithin(route), or -1.
func (l BeltLayout) indexWithin(route string, stageID string) int {
	return slices.IndexFunc(l.StagesWithin(route), func(s Stage) bool { return s.ID == stageID })
}

// piecesIn is Pieces for one route, by its id.
func (l BeltLayout) piecesIn(route string) []BeltLayoutPiece {
	return l.Pieces(&route)
}

// indexOfForkPiece is the position of the fork piece with id in items, or -1.
// It replaces the loops that found a fork among the pieces of its parent route.
func indexOfForkPiece(items []BeltLayoutPiece, id string) int {
	return slices.IndexFunc(items, func(p BeltLayoutPiece) bool {
		return p.Kind == BeltLayoutPieceFork && p.Fork.ID == id
	})
}

// indexOfStagePiece is the position of the stage piece with id in items, or -1.
func (l BeltLayout) indexOfStagePiece(items []BeltLayoutPiece, id string) int {
	return slices.IndexFunc(items, func(p BeltLayoutPiece) bool {
		return p.Kind == BeltLayoutPieceStage && p.Stage.ID == id
	})
}

// forkStep is a step at stage (nil at the end of the belt) with no routes taken yet.
func forkStep(stage *string) BeltLayoutStep {
	return BeltLayoutStep{Stage: stage, Routes: map[string][]string{}}
}

// forkHasRoute reports whether one of f's routes has id.
func forkHasRoute(f Fork, id string) bool {
	return forkRoute(f, id) != nil
}

// forkRoute is the route of f with id, a copy, or nil.
func forkRoute(f Fork, id string) *Route {
	for i := range f.Routes {
		if f.Routes[i].ID == id {
			r := f.Routes[i]
			return &r
		}
	}
	return nil
}

// forkRouteIndex is the position of the route with id in f, or -1.
func forkRouteIndex(f Fork, id string) int {
	for i := range f.Routes {
		if f.Routes[i].ID == id {
			return i
		}
	}
	return -1
}

// forkHasKey reports whether the set holds id.
func forkHasKey(set map[string]struct{}, id string) bool {
	_, ok := set[id]
	return ok
}

// forkSeen is a copy of seen with id added.
func forkSeen(seen map[string]struct{}, id string) map[string]struct{} {
	out := make(map[string]struct{}, len(seen)+1)
	for k := range seen {
		out[k] = struct{}{}
	}
	out[id] = struct{}{}
	return out
}
