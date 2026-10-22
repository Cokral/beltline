//
// How a fork's routes connect to the line. A route opens off the line unless it
// feeds in (Route.Feeds), and joins the line again when a run that finishes it
// carries on to where the routes meet.

package core

import "slices"
import "math"

// Opens reports whether a run can move onto routeID where its fork splits:
// every route but one that feeds in, which starts on its own.
func (l BeltLayout) Opens(routeID string) bool {
	r := l.Route(routeID)
	return r == nil || !r.Feeds
}

// Joins reports whether a run that finishes routeID carries on to where its
// fork's routes meet: the route is empty, or its last stage has an outcome that
// moves on past the fork. A route whose last stage only goes back, holds or
// drops ends there.
func (l BeltLayout) Joins(routeID string) bool {
	return l.joins(routeID, map[string]struct{}{})
}

// joins is Joins with the forks already seen on the way down.
func (l BeltLayout) joins(routeID string, seen map[string]struct{}) bool {
	fork := l.ForkWithRoute(routeID)
	if fork == nil || forkHasKey(seen, fork.ID) {
		return true
	}
	items := l.piecesIn(routeID)
	if len(items) == 0 {
		return true
	}
	last := items[len(items)-1]
	if last.Kind == BeltLayoutPieceFork {
		return slices.ContainsFunc(last.Fork.Routes, func(r Route) bool {
			return l.joins(r.ID, forkSeen(seen, fork.ID))
		})
	}
	return l.movesPast(*fork, *last.Stage)
}

// movesPast reports whether an outcome of stage, the last on its route, takes a
// run past fork.
func (l BeltLayout) movesPast(fork Fork, stage Stage) bool {
	if len(stage.Outcomes) == 0 {
		return true
	}
	after := math.MaxInt
	if span := l.Span(fork); span != nil {
		after = span.Upper
	}
	for _, outcome := range stage.Outcomes {
		moves := false
		switch outcome.Then.Action {
		case ThenActionNext, ThenActionRoute:
			moves = true
		case ThenActionDone, ThenActionShip:
			// Past a fork at the end of the belt, finishing the run is moving on.
			n := l.Next(stage.ID)
			moves = n == nil || n.Stage == nil
		case ThenActionGoto:
			for _, id := range outcome.Then.To {
				if l.indexOfStage(id) > after {
					moves = true
					break
				}
			}
		}
		if moves {
			return true
		}
	}
	return false
}
