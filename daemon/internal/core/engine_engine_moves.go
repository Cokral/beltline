// Moving between stages, the
// watch, holds, shipping, finishing, aborting, stopping and dropping.

package core

import "time"

// engineGo takes the run where step leads: into its stage, or off the end of the
// belt, noting the routes it takes on the way. The run's focus leg is used.
func engineGo(run *Run, step BeltLayoutStep, note *string, now time.Time) []Effect {
	return engineGoLeg(run, step, run.focusLegID(), note, now)
}

// engineGoLeg is engineGo for the given leg. A nil leg at the end of the belt
// completes the run; a leg finishes as Done, which is what the end of the belt
// without an explicit Ship records.
func engineGoLeg(run *Run, step BeltLayoutStep, leg *string, note *string, now time.Time) []Effect {
	if step.Stage == nil {
		engineMergeRoutes(run, step.Routes)
		if leg == nil {
			return engineComplete(run)
		}
		return engineFinish(run, leg, false, now)
	}
	effects := engineEnter(run, *step.Stage, leg, note, now)
	engineMergeRoutes(run, step.Routes)
	return effects
}

// engineWatched applies what the leg's watch stage saw (when, labelled label): its
// outcome for it, or a hold.
func engineWatched(run *Run, leg *string, when, label string, detail *string, now time.Time) []Effect {
	at := engineLeg(*run, leg)
	stage := at.Stage(*run)
	if at.State != RunStateRunning || stage == nil || stage.Kind() != StageKindWatch {
		return nil
	}
	if detail != nil {
		engineCloseAttempt(run, &at.ID, now, func(a *Attempt) { a.Detail = CopyPointer(detail) })
	}
	for i := range stage.Outcomes {
		if stage.Outcomes[i].When == when {
			outcome := stage.Outcomes[i].Clone()
			return engineApply(run, at.ID, *stage, outcome, detail, now)
		}
	}
	// An unhandled event needs a person's decision.
	engineCloseAttempt(run, &at.ID, now, func(a *Attempt) {
		a.Tone = new(ToneProblem)
		a.Route = new("Held for you")
	})
	return engineHoldLeg(run, &at.ID, label, nil, now, true)
}
