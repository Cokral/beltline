// Holds, shipping, finishing,
// aborting, stopping and dropping a run or a leg, and the helpers they share.

package core

import "time"

// engineHoldLeg holds the given leg (nil: the focus) with reason, optionally
// commenting, and notifies you unless notify is false.
func engineHoldLeg(run *Run, leg *string, reason string, comment *string, now time.Time, notify bool) []Effect {
	id := leg
	if id == nil {
		id = run.focusLegID()
	}
	engineSet(run, id, func(l *Leg) {
		l.State = RunStateHeld
		l.Reason = new(reason)
	})
	engineRefresh(run)
	var effects []Effect
	if comment != nil {
		effects = append(effects, Effect{Kind: EffectKindComment, Text: *comment})
	}
	if notify {
		stageName := "A stage"
		if stage := run.Stage(); stage != nil {
			stageName = stage.Name
		}
		effects = append(effects, Effect{
			Kind:  EffectKindNotify,
			Title: run.Subject.Ref() + " needs you",
			Body:  stageName + " held it: " + reason,
		})
	}
	return effects
}

// engineComplete finishes the run successfully, as Shipped or Done: its state and
// reason are set, pause is cleared, and the worktree is cleaned up.
func engineComplete(run *Run) []Effect {
	run.State = engineCompletedState(*run)
	run.Reason = nil
	engineClearPauseIfTerminal(run)
	return []Effect{{Kind: EffectKindCleanUp}}
}

// engineCompletedState is the state a successful completion ends in: Shipped when
// this trip shipped the work, otherwise Done. A run that is already Shipped stays
// Shipped, as runs saved before Done existed did.
func engineCompletedState(run Run) RunState {
	if run.Shipping || run.State == RunStateShipped {
		return RunStateShipped
	}
	return RunStateDone
}

// engineFinish is a leg reaching Done, or Ship when ship is set: it leaves the
// run's route work, and the join takes over when the others are done too. Ship
// records the trip's shipping intent on the run; reaching a join never does.
func engineFinish(run *Run, leg *string, ship bool, now time.Time) []Effect {
	if ship {
		run.Shipping = true
	}
	engineSet(run, leg, func(l *Leg) {
		l.State = RunStateDone
		l.Reason = nil
	})
	if engineAllFinished(run.Legs) && !run.IsSplit() {
		return engineComplete(run)
	}
	engineRefresh(run)
	return nil
}

// engineAbortLeg stops a leg's working agent and holds the leg at its current
// stage. Queued work has no session and no attempt, so it is only held.
func engineAbortLeg(run *Run, leg *string, now time.Time) []Effect {
	at := engineLeg(*run, leg)
	engineInvalidateContinuation(run, &at.ID)
	// A watch has no session, and an approval is only waiting for a decision.
	var effects []Effect
	if stage := at.Stage(*run); at.State == RunStateRunning && stage != nil && stage.Kind() == StageKindAgent {
		effects = engineStopping(*run, &at.ID)
	}
	if engineOpenAttemptIndex(*run, &at.ID) != nil {
		engineCloseAttempt(run, &at.ID, now, func(a *Attempt) { a.Route = new("Stopped: by you") })
	}
	engineSet(run, &at.ID, func(l *Leg) {
		l.State = RunStateHeld
		l.Reason = new("stopped by you")
	})
	engineRefresh(run)
	return effects
}

// engineStopping stops a leg's agent session: the run's own, or, on a split run,
// the one that leg's open attempt runs, by name, so another leg's goes on.
func engineStopping(run Run, leg *string) []Effect {
	if !run.IsSplit() {
		return []Effect{{Kind: EffectKindStopSession}}
	}
	stageID := run.StageID
	if stage := engineLegStage(run, leg); stage != nil {
		stageID = *stage
	}
	key := AttemptKey(stageID, leg)
	var session *string
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if AttemptKey(attempt.StageID, attempt.Leg) == key && attempt.IsOpen() {
			session = attempt.Session
			break
		}
	}
	if session == nil {
		return nil
	}
	return []Effect{{Kind: EffectKindStop, Session: *session}}
}

// engineDropRun drops the run for good, for reason.
func engineDropRun(run *Run, reason string, now time.Time) []Effect {
	var running []Leg
	for _, leg := range run.Legs {
		if leg.State == RunStateRunning {
			if stage := leg.Stage(*run); stage != nil && stage.Kind() == StageKindAgent {
				running = append(running, leg)
			}
		}
	}
	var stops []Effect
	if len(running) > 0 {
		if run.IsSplit() {
			for _, leg := range running {
				id := leg.ID
				stops = append(stops, engineStopping(*run, &id)...)
			}
		} else {
			stops = []Effect{{Kind: EffectKindStopSession}}
		}
	}
	// Iterate over the legs as they were before the drop.
	legs := append([]Leg(nil), run.Legs...)
	for _, leg := range legs {
		id := leg.ID
		engineCloseAttempt(run, &id, now, func(a *Attempt) {
			if a.Route == nil {
				a.Route = new("Stopped: " + reason)
			}
		})
		if leg.State.IsActive() {
			engineSet(run, &id, func(l *Leg) {
				l.State = RunStateDropped
				l.Reason = new(reason)
			})
		}
	}
	run.Legs = []Leg{}
	run.Joins = []Join{}
	run.MergesWaiting = []Merge{}
	run.Parking = nil
	run.State = RunStateDropped
	run.Reason = new(reason)
	engineClearPauseIfTerminal(run)
	return append(stops, Effect{Kind: EffectKindCleanUp})
}

// engineDropLeg drops one leg of a fork, which drops that route's work.
func engineDropLeg(run *Run, leg *string, reason string, now time.Time) []Effect {
	if leg == nil {
		return engineDropRun(run, reason, now)
	}
	current := run.LegByID(leg)
	if current == nil {
		return engineDropRun(run, reason, now)
	}
	if !run.IsSplit() {
		return engineDropRun(run, reason, now)
	}
	engineInvalidateContinuation(run, leg)
	var stops []Effect
	if current.State == RunStateRunning {
		if stage := current.Stage(*run); stage != nil && stage.Kind() == StageKindAgent {
			stops = engineStopping(*run, leg)
		}
	}
	engineCloseAttempt(run, leg, now, func(a *Attempt) {
		if a.Route == nil {
			a.Route = new("Stopped: " + reason)
		}
	})
	engineSet(run, leg, func(l *Leg) {
		l.State = RunStateDropped
		l.Reason = new(reason)
	})
	if run.Parking != nil {
		parking := run.Parking
		for _, route := range parking.Routes {
			if route.Leg == *leg {
				if route.Session != nil {
					parking.Stopping = engineWithout(parking.Stopping, *route.Session)
				}
				break
			}
		}
		kept := []ParkingRoute{}
		for _, route := range parking.Routes {
			if route.Leg != *leg {
				kept = append(kept, route)
			}
		}
		parking.Routes = kept
		parking.UnparkPending = false
		if len(kept) == 0 {
			run.Parking = nil
		} else {
			run.Parking = parking
		}
	}
	engineRefresh(run)
	return stops
}

// arraySafe is the element at index, and whether the index is in range.
func arraySafe[T any](s []T, index int) (T, bool) {
	var zero T
	if index < 0 || index >= len(s) {
		return zero, false
	}
	return s[index], true
}

// engineWithout is list with every s removed, never nil.
func engineWithout(list []string, s string) []string {
	out := []string{}
	for _, item := range list {
		if item != s {
			out = append(out, item)
		}
	}
	return out
}
