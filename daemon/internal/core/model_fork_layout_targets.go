// The goto counterparts on re-entered
// forks, and the target on the route a run took.

package core

import (
	"slices"
)

// Counterparts are, for a goto from `from` to stageID, the matching stage on
// each other nonempty route of every fork the jump comes back into. The match
// is the last stage with the same agent, or else the one in the same place.
func (l BeltLayout) Counterparts(stageID string, from string) []string {
	stage := l.stageNamed(stageID)
	if stage == nil {
		return nil
	}
	source := map[string]struct{}{}
	for _, w := range l.Ancestry(from) {
		source[w.Fork.ID] = struct{}{}
	}
	var found []string
	for _, way := range l.Ancestry(stageID) {
		if forkHasKey(source, way.Fork.ID) || !l.isAfter(way.Fork, from) || !l.Opens(way.Route.ID) {
			continue
		}
		// A route only a jump reaches has nothing to match: runs on another route never come back to it.
		at := max(l.indexWithin(way.Route.ID, stageID), 0)
		for _, route := range way.Fork.Routes {
			if route.ID == way.Route.ID || !l.Opens(route.ID) {
				continue
			}
			others := l.StagesWithin(route.ID)
			if len(others) == 0 {
				continue
			}
			var pick *Stage
			for i := len(others) - 1; i >= 0; i-- {
				if forkSameAgentOrType(*stage, others[i]) {
					pick = &others[i]
					break
				}
			}
			if pick == nil && at < len(others) {
				pick = &others[at]
			}
			if pick != nil {
				found = append(found, pick.ID)
			}
		}
	}
	return found
}

// Target is, of a goto's targets, the one on the route the run took. along is
// the route path of the work making the jump: a fork it is still inside keeps
// that route, and a fork it is entering uses the recorded choice, or the
// first route when nothing has chosen. A route only jumps reach is always
// a fit. Nil when none fits.
func (l BeltLayout) Target(ids []string, routes map[string][]string, along []BeltLayoutWay) *string {
	for i := range ids {
		if l.targetFits(ids[i], routes, along) {
			id := ids[i]
			return &id
		}
	}
	return nil
}

// targetFits reports whether a goto target is on the route the run took.
func (l BeltLayout) targetFits(id string, routes map[string][]string, along []BeltLayoutWay) bool {
	if l.stageNamed(id) == nil {
		return false
	}
	chain := l.Ancestry(id)
	if len(chain) == 0 {
		return true
	}
	for _, way := range chain {
		// A route that doesn't open off the line is only ever reached by a jump, whatever route the run took.
		if !l.Opens(way.Route.ID) {
			continue
		}
		if on := forkWayFor(along, way.Fork.ID); on != nil {
			if on.Route.ID != way.Route.ID {
				return false
			}
			continue
		}
		if taken := routes[way.Fork.ID]; len(taken) > 0 {
			if !slices.Contains(taken, way.Route.ID) {
				return false
			}
		} else if way.Fork.Parent != nil || len(chain) > 1 {
			if len(way.Fork.Routes) == 0 || way.Fork.Routes[0].ID != way.Route.ID {
				return false
			}
		}
	}
	return true
}

// forkSameAgentOrType is the Counterparts match: the same agent when stage has
// one, else the same stage type.
func forkSameAgentOrType(stage, other Stage) bool {
	if stage.Agent != nil {
		return other.Agent != nil && other.Agent.ID == stage.Agent.ID
	}
	return other.Type == stage.Type
}

// forkWayFor is the way in along whose fork has id, or nil.
func forkWayFor(along []BeltLayoutWay, id string) *BeltLayoutWay {
	for i := range along {
		if along[i].Fork.ID == id {
			return &along[i]
		}
	}
	return nil
}
