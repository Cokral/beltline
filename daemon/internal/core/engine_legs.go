package core

import (
	"slices"
	"strings"
	"time"
)

// engineLeg is the leg an event is about: the one it names, or the run's focus.
// A caller that sets the run's own state speaks for the leg in front.
func engineLeg(run Run, id *string) Leg {
	var focus *Leg
	if id != nil {
		focus = run.LegByID(id)
	}
	if focus == nil {
		focus = run.Focus()
	}
	if focus == nil {
		focus = &Leg{ID: "main", StageID: new(run.StageID), State: run.State}
	}
	if run.IsSplit() {
		return *focus
	}
	return Leg{
		ID: focus.ID, Route: focus.Route, StageID: new(run.StageID), State: run.State, Reason: run.Reason,
		Note: run.Note, Continues: run.Continues, Replays: run.Replays, Branch: focus.Branch,
	}
}

// engineSet applies change to the leg with id, if the run has it.
func engineSet(run *Run, id *string, change func(leg *Leg)) {
	if id == nil {
		return
	}
	for i := range run.Legs {
		if run.Legs[i].ID == *id {
			change(&run.Legs[i])
			return
		}
	}
}

// engineRefresh spreads the legs over the run's own stage, state and reason,
// which is what the rest of Beltline reads.
func engineRefresh(run *Run) {
	if len(run.Legs) == 0 {
		return
	}
	if focus := run.Focus(); focus != nil {
		if focus.StageID != nil {
			run.StageID = *focus.StageID
		}
		run.Note = CopyPointer(focus.Note)
		run.Continues = CopyPointer(focus.Continues)
		run.Replays = CopyPointer(focus.Replays)
	}
	layout := run.Layout()
	// A run working several routes reads as waiting for the ones still
	// going, naming them.
	going := run.WaitingForRoutes(layout)
	var waiting *string
	if run.IsSplit() && !engineAllFinished(run.Legs) {
		text := "WAITING FOR ROUTES"
		if len(going) > 0 {
			text += ": " + engineNames(going)
		}
		waiting = &text
	}
	reason := func(state RunState) *string {
		var leg *Leg
		for i := range run.Legs {
			if run.Legs[i].State == state {
				leg = &run.Legs[i]
				break
			}
		}
		if leg == nil || !run.IsSplit() {
			return nil
		}
		name := leg.RouteName(layout)
		if name == nil {
			return nil
		}
		stageName := "a stage"
		if stage := leg.Stage(*run); stage != nil {
			stageName = stage.Name
		}
		detail := ValueOr(leg.Reason, string(state))
		text := *name + " · " + stageName + " · " + detail
		return &text
	}
	if held := engineFirstLegIn(run.Legs, RunStateHeld); held != nil {
		run.State = RunStateHeld
		run.Reason = reason(RunStateHeld)
		if run.Reason == nil {
			run.Reason = CopyPointer(held.Reason)
		}
	} else if engineHasLegIn(run.Legs, RunStateWaiting) {
		run.State = RunStateWaiting
		run.Reason = engineFirstSome(waiting, reason(RunStateWaiting))
	} else if engineHasLegIn(run.Legs, RunStateRunning) {
		run.State = RunStateRunning
		run.Reason = engineFirstSome(waiting, reason(RunStateRunning))
	} else if engineHasLegIn(run.Legs, RunStateQueued) {
		run.State = RunStateQueued
		run.Reason = engineFirstSome(waiting, reason(RunStateQueued))
	} else if parked := engineFirstLegIn(run.Legs, RunStateParked); parked != nil {
		run.State = RunStateParked
		run.Reason = reason(RunStateParked)
		if run.Reason == nil {
			run.Reason = CopyPointer(parked.Reason)
		}
	} else if engineAllInState(run.Legs, RunStateDropped) {
		run.State = RunStateDropped
		run.Reason = CopyPointer(run.Legs[0].Reason)
	} else {
		run.State = engineCompletedState(*run)
		run.Reason = nil
	}
}

// engineSettle is what the engine does once an event is through: when every leg
// of a fork has finished, the run leaves the fork and merges on into the stage
// where the routes meet.
func engineSettle(run *Run, now time.Time) []Effect {
	if run.Layout().Nested() {
		return engineNestedSettle(run, now)
	}
	if !run.IsSplit() || !engineAllFinished(run.Legs) {
		return nil
	}
	if departed := engineDeparted(*run); len(departed) > 0 {
		run.Legs = departed
		engineRefresh(run)
		return nil
	}
	return engineJoin(run, now)
}

// engineDeparted are the legs that jumped past the fork's routes: the run goes
// on with those alone. A leg that reached the stage where the routes meet has
// joined, not departed.
func engineDeparted(run Run) []Leg {
	layout := run.Layout()
	forkID, ok := engineForkOfFirstRoute(run, layout)
	if !ok {
		return nil
	}
	var meeting *string
	if step := engineMeeting(run); step != nil {
		meeting = step.Stage
	}
	var out []Leg
	for _, leg := range run.Legs {
		if leg.StageID == nil {
			continue
		}
		if meeting != nil && *leg.StageID == *meeting {
			continue
		}
		place := layout.Place(*leg.StageID)
		if place == nil || place.Fork.ID != forkID {
			out = append(out, leg)
		}
	}
	return out
}

// engineJoin is the routes meeting: one leg carries on at the stage after the
// fork, and the branches the legs worked on are merged into the run's branch.
func engineJoin(run *Run, now time.Time) []Effect {
	layout := run.Layout()
	var branches []string
	for _, leg := range run.Legs {
		if leg.Branch == nil {
			continue
		}
		if run.Branch != nil && *leg.Branch == *run.Branch {
			continue
		}
		branches = append(branches, *leg.Branch)
	}
	step := engineMeeting(*run)
	if step == nil {
		step = layout.Next(run.StageID)
	}
	run.Legs = []Leg{}
	if step == nil || step.Stage == nil {
		if step != nil {
			engineMergeRoutes(run, step.Routes)
		}
		return engineComplete(run)
	}
	engineMergeRoutes(run, step.Routes)
	if branches == nil {
		branches = []string{}
	}
	run.MergeBranches = branches
	run.MergeNote = nil
	run.MergeNoteLeg = nil
	effects := engineEnter(run, *step.Stage, new("main"), nil, now)
	if len(branches) > 0 {
		effects = append(effects, Effect{Kind: EffectKindMergeLegs, Legs: branches})
	}
	return effects
}

// engineMeeting is the stage the routes of a split run meet at, or nil.
func engineMeeting(run Run) *BeltLayoutStep {
	layout := run.Layout()
	forkID, ok := engineForkOfFirstRoute(run, layout)
	if !ok {
		return nil
	}
	fork := layout.Fork(forkID)
	if fork == nil {
		return nil
	}
	span := layout.Span(*fork)
	if span == nil {
		return nil
	}
	return layout.Next(layout.Stages[span.Upper].ID)
}

// engineNames is "the Simple route", or "the Quick and Simple routes".
func engineNames(routes []string) string {
	if len(routes) == 1 {
		return routes[0] + " route"
	}
	var dropped []string
	last := ""
	if len(routes) > 0 {
		dropped = routes[:len(routes)-1]
		last = routes[len(routes)-1]
	}
	return strings.Join(dropped, ", ") + " and " + last + " routes"
}

// engineForkOfFirstRoute is the fork of the first leg that took a route, and
// whether there is one. A first route the layout does not know gives none.
func engineForkOfFirstRoute(run Run, layout BeltLayout) (string, bool) {
	for _, leg := range run.Legs {
		if leg.Route == nil {
			continue
		}
		fork := layout.ForkWithRoute(*leg.Route)
		if fork == nil {
			return "", false
		}
		return fork.ID, true
	}
	return "", false
}

// engineMergeRoutes merges routes into the routes the run took, the newer list
// for a fork replacing the older one.
func engineMergeRoutes(run *Run, routes map[string][]string) {
	taken := engineTaken(run)
	for fork, list := range routes {
		taken[fork] = cloneStrings(list)
	}
}

// engineTaken is the run's route map, made when the run has none yet, so a
// write never reaches a nil map. It is the storage of the run's routes.
func engineTaken(run *Run) map[string][]string {
	if run.Taken.Routes == nil {
		run.Taken.Routes = map[string][]string{}
	}
	return run.Taken.Routes
}

// engineUses is the run's outcome use counts, made when the run has none yet.
func engineUses(run *Run) map[string]int {
	if run.Uses == nil {
		run.Uses = map[string]int{}
	}
	return run.Uses
}

// engineAllFinished reports whether every leg has finished (true for none).
func engineAllFinished(legs []Leg) bool {
	return !slices.ContainsFunc(legs, func(leg Leg) bool { return !leg.IsFinished() })
}

// engineAllInState reports whether every leg is in state (true for none).
func engineAllInState(legs []Leg, state RunState) bool {
	return !slices.ContainsFunc(legs, func(leg Leg) bool { return leg.State != state })
}

// engineHasLegIn reports whether any leg is in state.
func engineHasLegIn(legs []Leg, state RunState) bool {
	return engineFirstLegIn(legs, state) != nil
}

// engineFirstLegIn is a copy of the first leg in state, or nil.
func engineFirstLegIn(legs []Leg, state RunState) *Leg {
	for i := range legs {
		if legs[i].State == state {
			leg := legs[i]
			return &leg
		}
	}
	return nil
}

// engineFirstSome is a copy of a when it is set, else a copy of b.
func engineFirstSome(a, b *string) *string {
	if a != nil {
		return CopyPointer(a)
	}
	return CopyPointer(b)
}
