// The outcome application: its
// limit, and the action each outcome's Then takes.

package core

import (
	"strconv"
	"time"
)

// engineApply applies an outcome an agent session or an approval produced at
// stage, on leg: its counter, the attempt's closing, and the outcome's action
// (next, goto, route, retry, hold, park, done, ship, drop).
func engineApply(run *Run, leg string, stage Stage, outcome Outcome, output *string, now time.Time) []Effect {
	then := outcome.Then
	if then.Max != nil {
		if effects, held := engineApplyLimit(run, leg, stage, outcome, *then.Max, now); held {
			return effects
		}
	}
	var note *string
	if then.WithOutput {
		note = output
	}
	line := EngineDescribe(then, run.Layout(), stage.ID, run.Routes())
	engineCloseAttempt(run, &leg, now, func(a *Attempt) {
		a.Outcome = new(outcome.Name)
		a.Tone = new(outcome.Tone)
		a.Route = new(line)
	})
	switch then.Action {
	case ThenActionNext:
		return engineApplyNext(run, leg, stage, note, now)
	case ThenActionGoto:
		return engineApplyGoto(run, leg, stage, outcome, note, now)
	case ThenActionRoute:
		return engineApplyRoute(run, leg, stage, outcome, note, now)
	case ThenActionRetry:
		return engineEnter(run, stage.ID, &leg, note, now)
	case ThenActionHold:
		return engineApplyHold(run, leg, stage, outcome, output, now)
	case ThenActionPark:
		engineSet(run, &leg, func(l *Leg) {
			l.State = RunStateParked
			l.Reason = new(outcome.Name)
		})
		return nil
	case ThenActionDone:
		return engineFinish(run, &leg, false, now)
	case ThenActionShip:
		return engineFinish(run, &leg, true, now)
	case ThenActionDrop:
		return engineDropLeg(run, &leg, outcome.Name, now)
	}
	return nil
}

// engineApplyLimit counts one use of outcome at stage against its limit. Past
// the limit it closes the attempt as held and holds the leg, reporting held;
// otherwise it counts the use and reports false.
func engineApplyLimit(run *Run, leg string, stage Stage, outcome Outcome, limit int, now time.Time) ([]Effect, bool) {
	key := stage.ID + ":" + outcomeNormalize(outcome.Name)
	used := engineUses(run)[key]
	if used < limit {
		engineUses(run)[key] = used + 1
		return nil, false
	}
	engineCloseAttempt(run, &leg, now, func(a *Attempt) {
		a.Outcome = new(outcome.Name)
		a.Tone = new(outcome.Tone)
		a.Route = new("Held: “" + outcome.Name + "” reached its limit of " + strconv.Itoa(limit))
	})
	times := "times"
	if limit == 1 {
		times = "time"
	}
	return engineHoldLeg(run, &leg, outcome.Name+", "+strconv.Itoa(limit)+" "+times+" already", nil, now, true), true
}

// engineApplyNext moves the leg on to the stage after stage, or ends it when
// stage is the last one.
func engineApplyNext(run *Run, leg string, stage Stage, note *string, now time.Time) []Effect {
	next := run.Layout().Next(stage.ID)
	if next == nil {
		return nil
	}
	effects := engineGoLeg(run, *next, &leg, note, now)
	engineReplayClosedAttempt(run, &leg)
	return effects
}

// engineApplyGoto sends the leg to the outcome's stage, on the route the run
// is on, or holds it when the stage is missing or off that route.
func engineApplyGoto(run *Run, leg string, stage Stage, outcome Outcome, note *string, now time.Time) []Effect {
	then := outcome.Then
	if !engineAnyStage(*run, then.To) {
		return engineHoldLeg(run, &leg, outcome.Name+": its stage is missing", nil, now, true)
	}
	if run.Layout().Nested() {
		// Replays the earlier session itself, once it knows whether the leg lands now or later.
		return engineNestedGoto(run, leg, outcome, stage, then.To, note, now)
	}
	to := run.Layout().Target(then.To, engineGotoRoutes(run, leg), nil)
	if to == nil {
		return engineHoldLeg(run, &leg, outcome.Name+": its stage isn't on this run's route", nil, now, true)
	}
	effects := engineEnter(run, *to, &leg, note, now)
	engineReplayClosedAttempt(run, &leg)
	return effects
}

// engineGotoRoutes is the set of routes a goto searches for its stage: the
// fork route the leg is on when it is on one, otherwise every route the run takes.
func engineGotoRoutes(run *Run, leg string) map[string][]string {
	var taken map[string][]string
	if l := run.LegByID(&leg); l != nil && l.Route != nil {
		if fork := run.Layout().ForkWithRoute(*l.Route); fork != nil {
			taken = map[string][]string{fork.ID: {*l.Route}}
		}
	}
	if taken == nil {
		return run.Routes()
	}
	return taken
}

// engineApplyRoute starts a leg on each route the outcome names that the belt
// has, and replays only the legs this route started. Holds the leg when none
// of the named routes exists.
func engineApplyRoute(run *Run, leg string, stage Stage, outcome Outcome, note *string, now time.Time) []Effect {
	var routes []string
	for _, route := range outcome.Then.Routes {
		if run.Layout().ForkWithRoute(route) != nil {
			routes = append(routes, route)
		}
	}
	if len(routes) == 0 {
		return engineHoldLeg(run, &leg, outcome.Name+": its route is missing", nil, now, true)
	}
	// Replay only the legs this route started. The destination is known
	// only after `take`, and another leg of the fork keeps its own history.
	existing := map[string]bool{}
	for _, l := range run.Legs {
		existing[l.ID] = true
	}
	effects := engineTake(run, routes, leg, note, now)
	var started []string
	for _, l := range run.Legs {
		if !existing[l.ID] || (l.ID == leg && (l.StageID == nil || *l.StageID != stage.ID)) {
			started = append(started, l.ID)
		}
	}
	for _, id := range started {
		id := id
		engineReplayClosedAttempt(run, &id)
	}
	return effects
}

// engineApplyHold holds the leg for you, with the hold comment on the
// subject's issue or ticket when the outcome and the policies ask for one.
func engineApplyHold(run *Run, leg string, stage Stage, outcome Outcome, output *string, now time.Time) []Effect {
	var comment *string
	if outcome.Then.Comment && run.Policies.CommentOnHold && run.Subject.About() != nil {
		text := engineHoldComment(run.Subject, stage, outcome, output)
		comment = &text
	}
	return engineHoldLeg(run, &leg, outcome.Name, comment, now, true)
}
