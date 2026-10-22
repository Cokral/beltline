// Entering a stage: the leg's
// state, the routes the run took around the stage, and the attempt a gate or
// watch opens.

package core

import (
	"slices"
	"time"
)

// engineEnter puts a leg at a stage: the only place a run's own state is set
// from.
func engineEnter(run *Run, stage string, leg *string, note *string, now time.Time) []Effect {
	index := run.StageIndexOf(stage)
	if index < 0 {
		return nil
	}
	st := run.Stages[index].Clone()
	id := engineEnterLegID(run, leg)
	if !run.HasLeg(id) {
		// A run that starts over as one leg is in no split anymore.
		run.Legs = []Leg{{ID: id, StageID: new(stage), State: RunStateQueued}}
		run.Joins = []Join{}
	}
	layout := run.Layout()
	left := engineEnterLeaves(*run, layout, stage, id)
	engineEnterClearForks(run, layout, id, index, left)
	engineEnterTakeRoutes(run, layout, stage)
	engineSet(run, &id, func(l *Leg) {
		l.StageID = new(stage)
		l.Note = CopyPointer(note)
		l.Reason = nil
		l.Continues = nil
		l.Replays = nil
		switch {
		case left:
			l.State = RunStateDone
		case st.Kind() == StageKindAgent:
			l.State = RunStateQueued
		default:
			l.State = RunStateWaiting
		}
	})
	if left {
		return nil
	}
	return engineEnterStage(run, st, stage, id, now)
}

// engineEnterLegID is the leg a stage is entered on: the one given, else the
// run's focus leg, else "main".
func engineEnterLegID(run *Run, leg *string) string {
	if leg != nil {
		return *leg
	}
	if focus := run.Focus(); focus != nil {
		return focus.ID
	}
	return "main"
}

// engineEnterLeaves says whether the leg leaves the stage to the others rather
// than taking it. A leg that reaches the stage where the routes meet, or jumps
// past it, runs on alone; the join happens once the others are finished. A
// nested fork waits only for the split it is still inside.
func engineEnterLeaves(run Run, layout BeltLayout, stage, id string) bool {
	if layout.Nested() {
		leggedDeparts := false
		if l := run.LegByID(&id); l != nil && l.Depart != nil {
			leggedDeparts = true
		}
		return engineNestedWaits(run, stage, id) || leggedDeparts
	}
	region, hasRegion := engineForkOfFirstRoute(run, layout)
	var regionID *string
	if hasRegion {
		regionID = &region
	}
	isMeeting := false
	if meeting := engineMeeting(run); meeting != nil && meeting.Stage != nil && *meeting.Stage == stage {
		isMeeting = true
	}
	var placeFork *string
	if place := layout.Place(stage); place != nil {
		fork := place.Fork.ID
		placeFork = &fork
	}
	return run.IsSplit() && (isMeeting || !SamePointee(placeFork, regionID))
}

// engineEnterClearForks forgets the choices of the forks ahead of the stage, which
// are yet to be chosen. A nested leg clears only the forks it will meet again.
func engineEnterClearForks(run *Run, layout BeltLayout, id string, index int, left bool) {
	if layout.Nested() {
		if !left {
			engineNestedClear(run, id, index)
		}
		return
	}
	for _, fork := range run.Forks {
		if span := layout.Span(fork); span != nil && span.Lower > index {
			delete(engineTaken(run), fork.ID)
		}
	}
}

// engineEnterTakeRoutes records the routes the run took to reach the stage,
// including the forks around it. Send to a nested stage replaces an ancestor that
// recorded the other exclusive route. A list that already includes this route
// stays, so routes taken together are not collapsed to the one stage entered.
func engineEnterTakeRoutes(run *Run, layout BeltLayout, stage string) {
	taken := engineTaken(run)
	for _, way := range layout.Ancestry(stage) {
		list := taken[way.Fork.ID]
		if !slices.Contains(list, way.Route.ID) {
			list = []string{way.Route.ID}
		}
		taken[way.Fork.ID] = list
	}
}

// engineEnterStage does what a leg entering a stage starts: a gate opens an
// attempt and asks you, a watch opens one and runs, and an agent waits for its
// session to be started by the coordinator.
func engineEnterStage(run *Run, st Stage, stage, id string, now time.Time) []Effect {
	switch st.Kind() {
	case StageKindGate:
		engineStartAttempt(run, st, stage, id, now)
		engineSet(run, &id, func(l *Leg) { l.Note = nil })
		engineRefresh(run)
		return []Effect{{
			Kind:  EffectKindNotify,
			Title: st.Name + " is waiting for you",
			Body:  run.Subject.Ref() + " " + run.Subject.Title,
		}}
	case StageKindWatch:
		engineStartAttempt(run, st, stage, id, now)
		engineSet(run, &id, func(l *Leg) {
			l.State = RunStateRunning
			l.Note = nil
		})
		engineRefresh(run)
	}
	return nil
}

// engineStartAttempt appends the attempt that a gate or watch stage opens for the
// leg.
func engineStartAttempt(run *Run, st Stage, stage, id string, now time.Time) {
	attempt := engineNewAttempt(*run, &id, &stage, now, EngineAttemptFolderLeg(*run, st, &id))
	run.Attempts = append(run.Attempts, attempt)
}
