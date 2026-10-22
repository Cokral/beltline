// Routes inside routes: the
// splits, their joins and the merges that follow them.

package core

import (
	"maps"
	"time"
)

// nestedRoutes is the run's routes as a map that can be written: a fresh copy
// of run.Routes, empty when the run has none. Write it back with SetRoutes.
func nestedRoutes(run *Run) map[string][]string {
	routes := run.Routes()
	if routes == nil {
		routes = map[string][]string{}
	}
	return routes
}

// nestedMergeRoutes merges other into the run's routes: a key in other replaces
// the run's own.
func nestedMergeRoutes(run *Run, other map[string][]string) {
	routes := nestedRoutes(run)
	maps.Copy(routes, other)
	run.SetRoutes(routes)
}

// nestedLegOf is the run's leg with id, or nil.
func nestedLegOf(run Run, id string) *Leg {
	return run.LegByID(new(id))
}

// engineNestedTake starts one leg per named route from a nested fork, as
// engineTake does for a flat one.
func engineNestedTake(run *Run, routes []string, from string, note *string, now time.Time) []Effect {
	layout := run.Layout()
	if len(routes) == 0 {
		return nil
	}
	first := layout.ForkWithRoute(routes[0])
	if first == nil {
		return nil
	}
	fork := layout.Fork(first.ID)
	if fork == nil {
		return nil
	}
	taken := nestedTakenRoutes(fork.Routes, routes)
	if len(taken) == 0 {
		return nil
	}
	forkID := fork.ID
	takenIDs := make([]string, 0, len(taken))
	for _, route := range taken {
		takenIDs = append(takenIDs, route.ID)
	}
	nestedSetRoute(run, forkID, takenIDs)
	if len(taken) == 1 {
		return engineNestedTakeOne(run, layout, from, takenIDs, note, now)
	}
	join := engineNestedSplit(run, layout, *fork, takenIDs, from)
	created := engineNestedLegs(run, layout, forkID, taken, join, note)
	nestedSetJoinLegs(run, join.ID, created)
	run.Legs = append(run.Legs, created...)
	var effects []Effect
	for _, leg := range created {
		if !leg.IsFinished() {
			effects = append(effects, engineStartLeg(run, leg.ID, CopyPointer(leg.StageID), CopyPointer(note), now)...)
		}
	}
	return effects
}

// nestedTakenRoutes is the fork's routes that appear in routes, in the fork's order.
func nestedTakenRoutes(forkRoutes []Route, routes []string) []Route {
	var taken []Route
	for _, route := range forkRoutes {
		for _, id := range routes {
			if route.ID == id {
				taken = append(taken, route)
				break
			}
		}
	}
	return taken
}

// engineNestedTakeOne moves the leg from onto its only taken route, with no join.
func engineNestedTakeOne(run *Run, layout BeltLayout, from string, takenIDs []string, note *string, now time.Time) []Effect {
	route := takenIDs[0]
	engineSet(run, new(from), func(leg *Leg) { leg.Route = new(route) })
	step := layout.TakeRoutes(takenIDs)
	if step == nil {
		return nil
	}
	return engineGoLeg(run, *step, new(from), note, now)
}

// engineNestedSplit records the join that gathers the taken routes of fork, takes
// the leg from's place in its parent join and in the run's legs, and returns the
// new join. The join is appended to run.Joins before the caller adds its legs.
func engineNestedSplit(run *Run, layout BeltLayout, fork Fork, takenIDs []string, from string) Join {
	forkID := fork.ID
	source := nestedLegOf(*run, from)
	resume := layout.Resume(fork)
	var sourceJoin *string
	var sourceBranch *string
	if source != nil {
		sourceJoin = CopyPointer(source.Join)
		sourceBranch = CopyPointer(source.Branch)
	}
	into := sourceBranch
	if into == nil {
		into = CopyPointer(run.Branch)
	}
	join := Join{
		ID: engineFreshJoin(*run, forkID), ForkID: forkID, Routes: takenIDs, Parent: sourceJoin,
		Into: into, Checkout: LegFromID(new(from)), AfterStage: CopyPointer(resume.Stage),
		AfterRoutes: nestedCopyRoutes(resume.Routes),
	}
	if sourceJoin != nil {
		if index := run.JoinIndexOf(*sourceJoin); index >= 0 {
			run.Joins[index].Legs = nestedWithout(run.Joins[index].Legs, from)
		}
	}
	run.Joins = append(run.Joins, join)
	if at := run.LegIndexOf(from); at >= 0 {
		run.Legs = append(run.Legs[:at:at], run.Legs[at+1:]...)
	}
	return join
}

// engineNestedLegs makes one leg per taken route of fork, each queued when its
// stage sits inside the fork and done when it does not, and merges the routes
// each step takes into the run.
func engineNestedLegs(run *Run, layout BeltLayout, forkID string, taken []Route, join Join, note *string) []Leg {
	var created []Leg
	for _, route := range taken {
		step := layout.TakeRoutes([]string{route.ID})
		var stage *string
		if step != nil {
			stage = CopyPointer(step.Stage)
		}
		inside := false
		if stage != nil {
			for _, way := range layout.Ancestry(*stage) {
				if way.Fork.ID == forkID {
					inside = true
					break
				}
			}
		}
		id := engineFreshLeg(*run, route.ID)
		state := RunStateDone
		if inside {
			state = RunStateQueued
		}
		created = append(created, Leg{
			ID: id, Route: new(route.ID), StageID: stage, State: state,
			Note: CopyPointer(note), Join: new(join.ID),
		})
		if step != nil {
			stepRoutes := nestedCopyRoutes(step.Routes)
			stepRoutes[forkID] = join.Routes
			nestedMergeRoutes(run, stepRoutes)
		}
	}
	return created
}

// nestedSetJoinLegs lists the created legs as the legs of join id.
func nestedSetJoinLegs(run *Run, id string, created []Leg) {
	if index := run.JoinIndexOf(id); index >= 0 {
		ids := make([]string, 0, len(created))
		for _, leg := range created {
			ids = append(ids, leg.ID)
		}
		run.Joins[index].Legs = ids
	}
}

// nestedSetRoute records the routes taken through fork.
func nestedSetRoute(run *Run, fork string, routes []string) {
	all := nestedRoutes(run)
	all[fork] = routes
	run.SetRoutes(all)
}

// nestedCopyRoutes is a fresh copy of routes, never nil, so it encodes as an object.
func nestedCopyRoutes(routes map[string][]string) map[string][]string {
	out := make(map[string][]string, len(routes))
	for key, value := range routes {
		out[key] = append([]string(nil), value...)
	}
	return out
}

// nestedWithout is ids with every occurrence of id removed.
func nestedWithout(ids []string, id string) []string {
	var out []string
	for _, item := range ids {
		if item != id {
			out = append(out, item)
		}
	}
	return out
}
