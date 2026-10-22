// The event handlers of Engine.step:
// subject changes, resumes, answers, merges, merge conflicts, restarts and overrules.

package core

import (
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// engineIsWatchTarget reports whether stage is a watch that waits on target. A
// watch stage without a target waits on the pull request (Stage.WatchTarget).
func engineIsWatchTarget(stage *Stage, target WatchTarget) bool {
	if stage == nil {
		return false
	}
	watched := stage.WatchTarget()
	return watched != nil && *watched == target
}

// engineSubjectChanged is the subjectChanged event: an edit refreshes the subject,
// and before any stage that changes code it starts the belt again.
func engineSubjectChanged(run *Run, event Event, now time.Time) []Effect {
	// An edit while the button has the run parked refreshes the subject
	// and does not start the belt again. Unpark does not replay it.
	if run.Parking != nil {
		run.Subject = event.Subject.Clone()
		return nil
	}
	edited := event.Subject.Content() != run.Subject.Content()
	run.Subject = event.Subject.Clone()
	// Only before any stage on the run's way changes code.
	path := run.Layout().Path(run.Routes(), false)
	if !edited || !run.State.IsActive() || !run.Policies.RetriageOnEdit {
		return nil
	}
	index := -1
	for i := range path {
		if path[i].ID == run.StageID {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	firstCode := len(path)
	for i := range path {
		if path[i].ChangesCode() {
			firstCode = i
			break
		}
	}
	if index >= firstCode && (index != firstCode || run.State == RunStateRunning) {
		return nil
	}
	if index == 0 && run.State == RunStateQueued {
		return nil
	}
	noun := run.Subject.Kind.Noun()
	// Every leg's session stops and the run starts again as one.
	var effects []Effect
	if run.IsSplit() {
		for _, leg := range run.Legs {
			if leg.State == RunStateRunning {
				id := leg.ID
				effects = append(effects, engineStopping(*run, &id)...)
			}
		}
	} else if run.State == RunStateRunning {
		effects = []Effect{{Kind: EffectKindStopSession}}
	}
	engineCloseAttempt(run, run.focusLegID(), now, func(a *Attempt) {
		a.Route = new("Stopped: the " + noun + " was edited")
	})
	engineInvalidateContinuations(run)
	run.Pause = nil
	run.Legs = []Leg{}
	run.Joins = []Join{}
	run.MergesWaiting = []Merge{}
	note := "The " + noun + " was edited after this stage last ran. Triage it again."
	return append(effects, engineStartAtBeginning(run, &note, now)...)
}

// engineResume is the resume event, Send to…: the route moves to a stage, with a
// note, whatever it was doing. A whole-run park is ended first, and a pause stays
// in place.
func engineResume(run *Run, event Event, now time.Time) []Effect {
	// Send to… ends whole-run parking, puts the other routes back, and
	// then uses the usual destination for the route it names.
	if run.Parking != nil {
		selected := engineLeg(*run, event.Leg).ID
		engineRestoreParked(run, &selected, now)
		run.Parking = nil
	}
	at := engineLeg(*run, event.Leg)
	if !at.State.IsActive() {
		return nil
	}
	var target string
	switch {
	case event.Stage != nil:
		target = *event.Stage
	case at.StageID != nil:
		target = *at.StageID
	default:
		target = run.StageID
	}
	if run.StageIndexOf(target) < 0 {
		return nil
	}
	var text *string
	if event.Note != nil {
		trimmed := strings.TrimSpace(*event.Note)
		if trimmed != "" {
			text = &trimmed
		}
	}
	// What the route was doing stops first: its agent session, or a watch or an
	// approval that is only waiting. Queued work has no attempt to close.
	effects := engineStopForMove(run, &at, now, "Stopped: sent to "+engineStageName(*run, target))
	var replay *string
	if index := run.StageIndexOf(target); event.Earlier && index >= 0 && run.Stages[index].Kind() == StageKindAgent {
		closed := run.ClosedAttempts(target, &at.ID)
		if len(closed) > 0 {
			id := closed[len(closed)-1].ID
			replay = &id
		}
	}
	engineSet(run, &at.ID, func(l *Leg) { l.Reason = nil })
	// A stage outside the split the leg is in waits for that split, as a goto there does.
	if run.Layout().Nested() {
		engineDepart(run, at.ID, target)
	}
	effects = append(effects, engineEnter(run, target, &at.ID, text, now)...)
	engineSet(run, &at.ID, func(l *Leg) { l.Replays = replay })
	// A pause that was waiting on the stopped session can settle now.
	if run.Pause != nil && run.Pause.Phase == RunPausePhasePausing {
		enginePauseSettle(run, now)
	}
	return effects
}

// engineStopForMove stops the work a route has at its stage before Send to moves
// it: an agent's session is stopped, its open attempt closes as route, and its
// Continue is dropped. A watch or an approval closes its open attempt without a
// session. Nothing happens to a route with no open attempt, so queued work keeps
// no attempt.
func engineStopForMove(run *Run, at *Leg, now time.Time, route string) []Effect {
	if engineOpenAttemptIndex(*run, &at.ID) == nil {
		return nil
	}
	var effects []Effect
	if stage := at.Stage(*run); stage != nil && stage.Kind() == StageKindAgent {
		effects = engineStopping(*run, &at.ID)
	}
	engineInvalidateContinuation(run, &at.ID)
	engineCloseAttempt(run, &at.ID, now, func(a *Attempt) { a.Route = new(route) })
	return effects
}

// engineStageName is the name of stage id on run, or the id when the run has no such stage.
func engineStageName(run Run, id string) string {
	if index := run.StageIndexOf(id); index >= 0 {
		return run.Stages[index].Name
	}
	return id
}

// engineAnswered is the answered event: you answered a held agent stage, and its
// session continues with your answer.
func engineAnswered(run *Run, event Event, now time.Time) []Effect {
	at := engineLeg(*run, event.Leg)
	held := run.Answerable()
	if held == nil || at.StageID == nil || *at.StageID != held.StageID {
		return nil
	}
	stage := ValueOr(at.StageID, run.StageID)
	effects := engineEnter(run, stage, &at.ID, event.Answer, now)
	heldID := held.ID
	engineSet(run, &at.ID, func(l *Leg) { l.Continues = new(heldID) })
	return effects
}

// engineMergedLegs is the mergedLegs event: the branches of the routes were merged
// into the run's branch, and the note says so on the join's attempt.
func engineMergedLegs(run *Run, branches []string, now time.Time) []Effect {
	if len(run.MergeBranches) == 0 {
		return nil
	}
	run.MergeBranches = []string{}
	destination := "the run's branch"
	if run.MergeInto != nil {
		destination = *run.MergeInto
	} else if run.Branch != nil {
		destination = *run.Branch
	}
	var note string
	if len(branches) == 0 {
		note = "Nothing to merge: no route changed code."
	} else {
		note = "Merged " + engineNames(branches) + " into " + destination + "."
	}
	run.MergeNote = &note
	// The note goes on the attempt of the leg the merge was for, not whichever leg is in front.
	merged := run.LegByID(run.MergeLeg)
	if merged == nil {
		merged = run.Focus()
	}
	run.MergeNoteLeg = CopyPointer(run.MergeLeg)
	var mergedID *string
	if merged != nil {
		mergedID = new(merged.ID)
	}
	var stage *Stage
	if merged != nil {
		stage = merged.Stage(*run)
	}
	if stage == nil {
		stage = run.Stage()
	}
	if stage != nil {
		for i := len(run.Attempts) - 1; i >= 0; i-- {
			if run.Attempts[i].StageID == stage.ID && SamePointee(run.Attempts[i].Leg, mergedID) {
				run.Attempts[i].Detail = CopyPointer(run.MergeNote)
				break
			}
		}
	}
	return engineNextMerge(run)
}

// engineMergeConflict is the mergeConflict event: the merge conflicted, so the
// person decides what happens next.
func engineMergeConflict(run *Run, files []string, now time.Time) []Effect {
	// The merge is no longer pending: the person decides what happens next.
	run.MergeBranches = []string{}
	leg := run.MergeLeg
	if leg == nil {
		leg = run.focusLegID()
	}
	whereName := "the stage where the routes meet"
	found := false
	if l := run.LegByID(leg); l != nil {
		if stage := l.Stage(*run); stage != nil {
			whereName = stage.Name
			found = true
		}
	}
	if !found {
		if stage := run.Stage(); stage != nil {
			whereName = stage.Name
		}
	}
	context := ""
	if run.MergeContext != nil {
		context = " (" + *run.MergeContext + ")"
	}
	reason := "Merging the routes conflicted at " + whereName + context + ": " + strings.Join(files, ", ") + ". Resolve it and continue."
	effects := engineHoldLeg(run, leg, reason, nil, now, true)
	return append(effects, engineNextMerge(run)...)
}

// engineRestartHeld is the restartHeld event: an overrule restart could not be
// finished, so the leg is held with its comments kept.
func engineRestartHeld(run *Run, reason string, now time.Time) []Effect {
	if !run.State.IsActive() || run.Restart == nil || run.Restart.Phase != RestartIntentPhaseIntent {
		return nil
	}
	attemptID := run.Restart.AttemptID
	var legID *string
	if attempt := run.AttemptByID(&attemptID); attempt != nil {
		legID = attempt.Leg
	}
	at := engineLeg(*run, legID)
	var effects []Effect
	if stage := at.Stage(*run); at.State == RunStateRunning && stage != nil && stage.Kind() == StageKindAgent {
		effects = append(effects, engineStopping(*run, &at.ID)...)
	}
	if engineOpenAttempt(*run, &at.ID) {
		engineCloseAttempt(run, &at.ID, now, func(a *Attempt) {
			a.Route = new("Stopped: overrule failed")
			a.Detail = new(reason)
		})
	}
	run.Restart.Phase = RestartIntentPhaseHeld
	run.Restart.Detail = new(reason)
	return append(effects, engineHoldLeg(run, &at.ID, reason, nil, now, true)...)
}

// engineOverrule queues a fresh attempt at an agent stage, on the leg the
// overruled attempt ran on. A finished or dropped run, a stage that is not an
// agent, or a decision already applied is left untouched.
func engineOverrule(run *Run, stage string, note string, decision string, attempt string, now time.Time) []Effect {
	if !run.State.IsActive() {
		return nil
	}
	index := run.StageIndexOf(stage)
	if index < 0 || run.Stages[index].Kind() != StageKindAgent {
		return nil
	}
	if run.Restart != nil && run.Restart.DecisionID == decision && run.Restart.Phase == RestartIntentPhaseApplied {
		return nil
	}
	text := strings.TrimSpace(note)
	if text == "" || attempt == "" || decision == "" {
		return nil
	}
	attemptID := attempt
	var legID *string
	if found := run.AttemptByID(&attemptID); found != nil {
		legID = found.Leg
	}
	at := engineLeg(*run, legID)

	var effects []Effect
	if stageOfAt := at.Stage(*run); at.State == RunStateRunning && stageOfAt != nil && stageOfAt.Kind() == StageKindAgent {
		effects = append(effects, engineStopping(*run, &at.ID)...)
	}
	if engineOpenAttempt(*run, &at.ID) {
		engineCloseAttempt(run, &at.ID, now, func(a *Attempt) {
			a.Route = new("Stopped: overruled")
			a.Detail = new("Overruled")
		})
	} else if at.State == RunStateHeld || at.State == RunStateParked {
		stageID := ValueOr(at.StageID, run.StageID)
		key := AttemptKey(stageID, &at.ID)
		found := -1
		for i := len(run.Attempts) - 1; i >= 0; i-- {
			if AttemptKey(run.Attempts[i].StageID, run.Attempts[i].Leg) == key {
				found = i
				break
			}
		}
		if found >= 0 {
			detail := run.Attempts[found].Detail
			if detail == nil || !strings.Contains(*detail, "Overruled") {
				if detail != nil {
					run.Attempts[found].Detail = new(*detail + "\nOverruled")
				} else {
					run.Attempts[found].Detail = new("Overruled")
				}
			}
		}
	}
	var replay *string
	if closed := run.ClosedAttempts(stage, nil); len(closed) > 0 {
		id := closed[len(closed)-1].ID
		replay = &id
	}
	started := now
	if run.Restart != nil && run.Restart.DecisionID == decision {
		started = run.Restart.At.Time
	}
	effects = append(effects, engineEnter(run, stage, &at.ID, new(text), now)...)
	engineSet(run, &at.ID, func(l *Leg) {
		l.Replays = replay
		l.Continues = nil
	})
	engineRefresh(run)
	run.Operation++
	run.Restart = &RestartIntent{
		DecisionID: decision, AttemptID: attempt, StageID: stage, Comments: text,
		At: jsonx.Date{Time: started}, Phase: RestartIntentPhaseApplied,
	}
	return effects
}

// engineOpenAttempt reports whether the leg's stage has an attempt still open.
func engineOpenAttempt(run Run, leg *string) bool {
	stageID := run.StageID
	if stage := engineLegStage(run, leg); stage != nil {
		stageID = *stage
	}
	key := AttemptKey(stageID, leg)
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		if AttemptKey(run.Attempts[i].StageID, run.Attempts[i].Leg) == key {
			return run.Attempts[i].IsOpen()
		}
	}
	return false
}
