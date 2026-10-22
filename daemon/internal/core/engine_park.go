// Parking every route from the Run
// page, its unpark and the stops it waits for.

package core

import (
	"math"
	"slices"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// engineBlocksWhileParked reports whether event may still apply while the Run
// page button has the run parked. Events that may still apply are the ones
// listed there; everything else, including a result that was already in flight,
// is ignored.
func engineBlocksWhileParked(event Event) bool {
	switch event.Kind {
	case EventKindParkRun, EventKindUnparkRun, EventKindParkStopped, EventKindDrop,
		EventKindSubjectGone, EventKindSubjectChanged, EventKindResume,
		EventKindMergedLegs, EventKindMergeConflict:
		return false
	}
	return true
}

// engineParkRun parks every unfinished route that is not already parked by an
// outcome. A finished, dropped, outcome-parked or already button-parked run is
// left as it is.
func engineParkRun(run *Run, now time.Time) []Effect {
	if run.Parking != nil || !run.State.IsActive() {
		return nil
	}
	var targets []Leg
	for _, id := range nestedLegIDs(run.Legs) {
		leg := engineLeg(*run, new(id))
		if leg.State.IsActive() && leg.State != RunStateParked {
			targets = append(targets, leg)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	routes := []ParkingRoute{}
	stopping := []string{}
	var effects []Effect
	for _, at := range targets {
		route := ParkingRoute{
			Leg: at.ID, StageID: CopyPointer(at.StageID), State: at.State, Reason: CopyPointer(at.Reason),
			Note: CopyPointer(at.Note), Continues: CopyPointer(at.Continues), Replays: CopyPointer(at.Replays),
		}
		if at.State == RunStateRunning {
			if stage := at.Stage(*run); stage != nil && stage.Kind() == StageKindAgent {
				stageID := ValueOr(at.StageID, run.StageID)
				key := AttemptKey(stageID, new(at.ID))
				index := -1
				for i := range run.Attempts {
					if AttemptKey(run.Attempts[i].StageID, run.Attempts[i].Leg) == key && run.Attempts[i].IsOpen() {
						index = i
					}
				}
				if index >= 0 {
					route.Interrupted = new(run.Attempts[index].ID)
					route.Session = CopyPointer(run.Attempts[index].Session)
					if run.Attempts[index].EndedAt == nil {
						run.Attempts[index].EndedAt = &jsonx.Date{Time: now}
					}
					run.Attempts[index].Route = new("Parked by you")
					if route.Session != nil {
						stopping = append(stopping, *route.Session)
						effects = append(effects, Effect{Kind: EffectKindStopForPark, Session: *route.Session})
					}
				} else {
					// A running agent with no session cannot be restored as running.
					route.State = RunStateQueued
				}
			}
		}
		routes = append(routes, route)
		leg := at.ID
		engineSet(run, new(leg), func(l *Leg) {
			l.State = RunStateParked
			l.Reason = nil
		})
	}
	run.Parking = &Parking{Routes: routes, Stopping: stopping, At: jsonx.Date{Time: now}}
	// Work that started before this park cannot land, including after a quick Unpark.
	run.Operation++
	return effects
}

// nestedLegIDs is the id of each leg, in order.
func nestedLegIDs(legs []Leg) []string {
	ids := make([]string, 0, len(legs))
	for _, leg := range legs {
		ids = append(ids, leg.ID)
	}
	return ids
}

// engineUnparkRun restores button-parked routes. While a stop is unfinished, it
// records the request and starts nothing. A stop that failed stays parked; asking
// again retries that stop and still starts no replacement until it succeeds.
func engineUnparkRun(run *Run, now time.Time) []Effect {
	if run.Parking == nil {
		return nil
	}
	parking := cloneValue(*run.Parking)
	if parking.StopError != nil {
		sessions := parking.Stopping
		if len(sessions) == 0 {
			for _, route := range parking.Routes {
				if route.Session != nil {
					sessions = append(sessions, *route.Session)
				}
			}
		}
		if len(sessions) > 0 {
			parking.Stopping = sessions
			parking.UnparkPending = true
			parking.StopError = nil
			run.Parking = &parking
			effects := make([]Effect, 0, len(sessions))
			for _, session := range sessions {
				effects = append(effects, Effect{Kind: EffectKindStopForPark, Session: session})
			}
			return effects
		}
		parking.StopError = nil
		run.Parking = &parking
	}
	if len(parking.Stopping) > 0 {
		if parking.UnparkPending {
			return nil
		}
		parking.UnparkPending = true
		run.Parking = &parking
		return nil
	}
	engineRestoreParked(run, nil, now)
	run.Parking = nil
	return nil
}

// engineParkStopped is one parked session finished stopping. The last success can
// complete an Unpark that was waiting. A failure keeps the session in stopping, so
// a later Unpark retries it instead of starting a replacement beside a session
// that is still running. err is nil when the stop worked.
func engineParkStopped(run *Run, session string, err *string, now time.Time) []Effect {
	if run.Parking == nil || !slices.Contains(run.Parking.Stopping, session) {
		return nil
	}
	parking := cloneValue(*run.Parking)
	if err != nil {
		parking.StopError = CopyPointer(err)
		parking.UnparkPending = false
		run.Parking = &parking
		return nil
	}
	var remaining []string
	for _, item := range parking.Stopping {
		if item != session {
			remaining = append(remaining, item)
		}
	}
	parking.Stopping = remaining
	resume := len(parking.Stopping) == 0 && parking.UnparkPending && parking.StopError == nil
	run.Parking = &parking
	if !resume {
		return nil
	}
	engineRestoreParked(run, nil, now)
	run.Parking = nil
	return nil
}

// engineRestoreParked puts button-parked routes back, except `except`, which Send
// to moves itself. Parked time is added onto open watch clocks so grace and the
// quiet period do not include it.
func engineRestoreParked(run *Run, except *string, now time.Time) {
	if run.Parking == nil {
		return
	}
	parking := cloneValue(*run.Parking)
	interval := now.Sub(parking.At.Time).Seconds()
	shiftedAnchor := false
	for _, route := range parking.Routes {
		if except != nil && route.Leg == *except {
			continue
		}
		current := nestedLegOf(*run, route.Leg)
		if current == nil || !current.State.IsActive() {
			continue
		}
		engineSet(run, new(route.Leg), func(leg *Leg) {
			leg.StageID = CopyPointer(route.StageID)
			leg.Reason = CopyPointer(route.Reason)
			leg.Note = CopyPointer(route.Note)
			if route.Interrupted != nil {
				leg.State = RunStateQueued
				leg.Continues = nil
				leg.Replays = CopyPointer(route.Interrupted)
			} else {
				leg.State = route.State
				leg.Continues = CopyPointer(route.Continues)
				leg.Replays = CopyPointer(route.Replays)
			}
		})
		if route.Interrupted == nil && route.State == RunStateRunning && route.StageID != nil {
			if stage := nestedStageOf(*run, *route.StageID); stage != nil && stage.Kind() == StageKindWatch {
				engineShiftWatch(run, route, interval, &shiftedAnchor)
			}
		}
	}
}

// engineFreezeParked, after an event that is allowed while parked, parks again
// any route the button owns. A merge conflict is remembered as the restored
// wait.
func engineFreezeParked(run *Run) {
	if run.Parking == nil {
		return
	}
	parking := cloneValue(*run.Parking)
	changed := false
	for index := range parking.Routes {
		route := parking.Routes[index]
		current := nestedLegOf(*run, route.Leg)
		if current == nil || current.State == RunStateParked || !current.State.IsActive() {
			continue
		}
		parking.Routes[index].State = current.State
		parking.Routes[index].Reason = CopyPointer(current.Reason)
		parking.Routes[index].StageID = CopyPointer(current.StageID)
		parking.Routes[index].Note = CopyPointer(current.Note)
		parking.Routes[index].Continues = CopyPointer(current.Continues)
		parking.Routes[index].Replays = CopyPointer(current.Replays)
		parking.Routes[index].Interrupted = nil
		parking.Routes[index].Session = nil
		engineSet(run, new(route.Leg), func(leg *Leg) {
			leg.State = RunStateParked
			leg.Reason = nil
		})
		changed = true
	}
	if changed {
		run.Parking = &parking
	}
}

// engineShiftWatch shifts the watch clocks of route by interval, recording in
// shiftedAnchor whether the anchor moved.
func engineShiftWatch(run *Run, route ParkingRoute, interval float64, shiftedAnchor *bool) {
	if !(interval > 0) || route.StageID == nil {
		return
	}
	stageID := *route.StageID
	key := AttemptKey(stageID, new(route.Leg))
	shift := time.Duration(math.Round(interval * float64(time.Second)))
	index := -1
	for i := range run.Attempts {
		if AttemptKey(run.Attempts[i].StageID, run.Attempts[i].Leg) == key && run.Attempts[i].IsOpen() {
			index = i
		}
	}
	if index >= 0 {
		started := run.Attempts[index].StartedAt.Time
		run.Attempts[index].StartedAt = jsonx.Date{Time: started.Add(shift)}
		if first := run.FirstPullRequestAt; !*shiftedAnchor && first != nil && !first.Before(started) {
			run.FirstPullRequestAt = &jsonx.Date{Time: first.Add(shift)}
			*shiftedAnchor = true
		}
	}
	stage := nestedStageOf(*run, stageID)
	if stage == nil {
		return
	}
	if !stage.WatchesTicket() {
		return
	}
	seen, ok := run.TicketObservations[stageID]
	if !ok || seen.PendingSince == nil {
		return
	}
	seen.PendingSince = &jsonx.Date{Time: seen.PendingSince.Add(shift)}
	run.TicketObservations[stageID] = seen
}
