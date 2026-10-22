package coordinator

import (
	"context"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// take reads a finished session and carries it out, dropping the result when AFK
// went off, the run moved or it was erased while the runner call was in flight
// .
// Lock: runs with c.mu held. Suspends: runner.reply, stillDeciding, runner.finish.
func (a *Autopilot) take(ctx context.Context, decision core.AfkDecision, spec runner.SessionSpec, generation, operation int) {
	key := decision.Key
	a.book.busy[decision.RunID] = struct{}{}
	defer delete(a.book.busy, decision.RunID)
	if !a.book.accepts(decision.RunID, generation, &operation) {
		return
	}
	text := autopilotReadOutput(spec.Output)
	if run := a.book.Run(decision.RunID); run != nil {
		if situation := core.AfkSituationOf(*run, a.settings()); situation != nil && situation.Key == key &&
			core.AfkParse(text, situation.Options) == nil {
			var reply *string
			a.c.Suspend(func() { reply = a.runner.Reply(ctx, spec) })
			if reply != nil {
				if !a.book.accepts(decision.RunID, generation, &operation) || !a.stillDeciding(ctx, decision.ID, decision.RunID, key) {
					return
				}
				if current := a.book.Run(decision.RunID); current != nil {
					if situation := core.AfkSituationOf(*current, a.settings()); situation != nil && core.AfkParse(*reply, situation.Options) != nil {
						text = *reply
					}
				}
			}
		}
	}
	if !a.book.accepts(decision.RunID, generation, &operation) || !a.stillDeciding(ctx, decision.ID, decision.RunID, key) {
		return
	}
	a.c.Suspend(func() { a.runner.Finish(ctx, spec) })
	if !a.book.accepts(decision.RunID, generation, &operation) || !a.stillDeciding(ctx, decision.ID, decision.RunID, key) {
		return
	}
	run := a.book.Run(decision.RunID)
	if run == nil {
		return
	}
	situation := core.AfkSituationOf(*run, a.settings())
	if situation == nil || situation.Key != key {
		return
	}
	parsed := core.AfkParse(text, situation.Options)
	if parsed == nil {
		why := fmt.Sprintf("%s names no option it had", core.AfkDecisionFile)
		if text == "" {
			why = fmt.Sprintf("its session ended without writing %s", core.AfkDecisionFile)
		}
		a.change(decision.ID, func(d *core.AfkDecision) {
			d.Status = core.AfkDecisionStatusFailed
			d.Detail = &why
			d.DecidedAt = a.nowDate()
		})
		runID := run.ID
		a.book.note(core.AfkName+" could not decide for "+run.Subject.Ref()+": "+why, &runID)
		a.notify(run.Subject.Ref()+" still needs you", "The "+core.AfkName+" could not decide: "+why)
		return
	}
	current := decision
	if saved := a.decision(decision.ID); saved != nil {
		current = *saved
	}
	a.carryOut(*parsed, current, *run, generation, operation)
}

// autopilotReadOutput reads a decision file as UTF-8 text, or "" when it cannot.
// Lock: none (pure apart from the file read).
func autopilotReadOutput(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return ""
	}
	return string(data)
}

// stillDeciding reports whether this decision may still move the run. A decision
// already superseded is left as it is; a run that is gone, paused, parked or
// moved on supersedes it.
// Lock: runs with c.mu held. Suspends: give.
func (a *Autopilot) stillDeciding(ctx context.Context, id, runID, key string) bool {
	current := a.decision(id)
	if current == nil || current.Status != core.AfkDecisionStatusDeciding {
		return false
	}
	run := a.book.Run(runID)
	if run == nil {
		a.give(ctx, *current, core.AfkDecisionStatusSuperseded, "the run is gone")
		return false
	}
	if run.Pause != nil && run.Pause.BlocksProgress() {
		a.give(ctx, *current, core.AfkDecisionStatusSuperseded, "the run was paused")
		return false
	}
	if !a.allows(*run) {
		a.give(ctx, *current, core.AfkDecisionStatusSuperseded, a.offReason(*run))
		return false
	}
	if run.Parking != nil {
		a.give(ctx, *current, core.AfkDecisionStatusSuperseded, "the run was parked")
		return false
	}
	situation := core.AfkSituationOf(*run, a.settings())
	if situation == nil || situation.Key != key {
		a.give(ctx, *current, core.AfkDecisionStatusSuperseded, "the run moved on before it decided")
		return false
	}
	return true
}

// carryOut carries out what the session chose, and logs it.
// Lock: runs with c.mu held. Spawns: the session stop, through c.Spawn.
func (a *Autopilot) carryOut(parsed core.AfkParsed, decision core.AfkDecision, run core.Run, generation, operation int) {
	if !a.book.accepts(run.ID, generation, &operation) {
		return
	}
	if current := a.decision(decision.ID); current == nil || current.Status != core.AfkDecisionStatusDeciding {
		return
	}
	now := a.book.Run(run.ID)
	if now == nil || (now.Pause != nil && now.Pause.BlocksProgress()) || !a.allows(*now) {
		if current := a.decision(decision.ID); current != nil && current.Status == core.AfkDecisionStatusDeciding {
			why := "the run is gone"
			if now != nil {
				why = a.offReason(*now)
				if now.Pause != nil && now.Pause.BlocksProgress() {
					why = "the run was paused"
				}
			}
			a.change(decision.ID, func(d *core.AfkDecision) {
				d.Status = core.AfkDecisionStatusSuperseded
				d.Detail = &why
				d.DecidedAt = a.nowDate()
			})
			if decision.Session != nil {
				name := *decision.Session
				a.c.Spawn(func(ctx context.Context) { a.runner.Stop(ctx, name) })
			}
		}
		return
	}
	run = *now
	label := core.AfkActionLabel(parsed.Option, run)
	a.change(decision.ID, func(d *core.AfkDecision) {
		choice := parsed.Option.ID
		d.Choice = &choice
		d.Action = &label
		d.Message = parsed.Message
		d.Why = parsed.Why
		d.DecidedAt = a.nowDate()
	})
	event := core.AfkEventFor(parsed.Option, parsed.Message)
	if event == nil {
		a.change(decision.ID, func(d *core.AfkDecision) { d.Status = core.AfkDecisionStatusLeftForYou })
		runID := run.ID
		a.book.note(core.AfkName+" left "+run.Subject.Ref()+" for you at "+decision.StageName, &runID)
		reason := core.ValueOr(parsed.Why, decision.StageName)
		a.notify(run.Subject.Ref()+" needs you", "The "+core.AfkName+" left it for you: "+reason)
		return
	}
	stageID := run.StageID
	var focusLeg *string
	if focus := run.Focus(); focus != nil {
		id := focus.ID
		focusLeg = &id
	}
	decisionID := decision.ID
	markAttempt := func(r *core.Run) {
		key := core.AttemptKey(stageID, focusLeg)
		for i := len(r.Attempts) - 1; i >= 0; i-- {
			if core.AttemptKey(r.Attempts[i].StageID, r.Attempts[i].Leg) == key {
				id := decisionID
				r.Attempts[i].AfkDecision = &id
				return
			}
		}
	}
	a.book.apply(run.ID, *event, &generation, &operation, markAttempt)
	after := a.book.Run(run.ID)
	if after != nil && (after.State != run.State || after.StageID != run.StageID || !core.SamePointee(after.Continues, run.Continues)) {
		a.change(decision.ID, func(d *core.AfkDecision) {
			d.Status = core.AfkDecisionStatusApplied
		})
		runID := run.ID
		a.book.note(core.AfkName+": "+label+" for "+run.Subject.Ref(), &runID)
		reason := core.ValueOf(parsed.Why)
		a.notify(core.AfkName+": "+run.Subject.Ref(), label+". "+reason)
		return
	}
	// The engine took nothing from the event, so take back the mark too.
	a.book.update(run.ID, &generation, &operation, func(r *core.Run) {
		for i := len(r.Attempts) - 1; i >= 0; i-- {
			if r.Attempts[i].AfkDecision != nil && *r.Attempts[i].AfkDecision == decisionID {
				r.Attempts[i].AfkDecision = nil
				break
			}
		}
	})
	detail := "Beltline could not carry out “" + label + "”"
	a.change(decision.ID, func(d *core.AfkDecision) {
		d.Status = core.AfkDecisionStatusFailed
		d.Detail = &detail
	})
}
