package coordinator

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// OverruleHalt is where an in-flight overrule stops, so a test can reload the
// daemon at that boundary.
type OverruleHalt string

const (
	// OverruleHaltIntent stops after the intent is saved.
	OverruleHaltIntent OverruleHalt = "intent"
	// OverruleHaltStopped stops after the work is stopped.
	OverruleHaltStopped OverruleHalt = "stopped"
	// OverruleHaltLogged stops after the overrule is logged.
	OverruleHaltLogged OverruleHalt = "logged"
)

// OverruleAfk restarts the run an applied AFK decision belongs to, at an agent
// stage, with comments.
// Lock: exported entry point; takes c.mu. Suspends: each wait for a restart
// already in progress (Task.yield), and finishOverrule.
func (c *Coordinator) OverruleAfk(ctx context.Context, decisionID, stageID, comments string, acceptedWarning bool) error {
	c.Lock()
	defer c.EndTurn()
	return c.overruleAfk(ctx, decisionID, stageID, comments, acceptedWarning)
}

// overruleAfk is OverruleAfk with c.mu held.
// Lock: runs with c.mu held. Suspends: as OverruleAfk.
func (c *Coordinator) overruleAfk(ctx context.Context, decisionID, stageID, comments string, acceptedWarning bool) error {
	trimmed := strings.TrimSpace(comments)
	if !acceptedWarning {
		return core.NewFailure("Accept the warning before overruling.")
	}
	if trimmed == "" {
		return core.NewFailure("Comments are required.")
	}
	decision := c.autopilot.Decision(&decisionID)
	if decision == nil {
		return core.NewFailure(core.AfkDecisionGone)
	}
	runID := decision.RunID

	if _, restarting := c.book.restarting[runID]; restarting {
		for {
			if _, still := c.book.restarting[runID]; !still {
				break
			}
			c.Suspend(func() {
				runtime.Gosched()
				time.Sleep(time.Millisecond)
			})
		}
		if current := c.book.Run(runID); current != nil && current.Restart != nil &&
			current.Restart.DecisionID == decisionID && current.Restart.Phase == core.RestartIntentPhaseApplied {
			return c.projectOverrule(*current)
		}
	}

	offer, err := c.requireOffer(decisionID)
	if err != nil {
		return err
	}
	chosen := slices.ContainsFunc(offer.Choices, func(choice core.AfkTarget) bool {
		return choice.StageID == stageID
	})
	if !chosen {
		return core.NewFailure(core.AfkOverruleNoAgentStage)
	}
	if current := c.book.Run(runID); current != nil && current.Restart != nil &&
		current.Restart.DecisionID == decisionID && current.Restart.Phase == core.RestartIntentPhaseApplied {
		return core.NewFailure(core.AfkOverruledAlready)
	}

	c.book.restarting[runID] = struct{}{}
	err = c.overruleSteps(ctx, runID, decisionID, stageID, trimmed, offer)
	if err != nil {
		delete(c.book.restarting, runID)
		return err
	}
	delete(c.book.restarting, runID)
	c.kick()
	return nil
}

// overruleSteps saves the restart intent and finishes the overrule (the body of
// the do block of the overrule AFK step).
// Lock: runs with c.mu held. Suspends: finishOverrule.
func (c *Coordinator) overruleSteps(ctx context.Context, runID, decisionID, stageID, comments string, offer core.AfkOffer) error {
	if current := c.book.Run(runID); current == nil || current.Restart == nil || current.Restart.DecisionID != decisionID {
		now := c.clock.Now()
		saved := c.book.update(runID, nil, nil, func(run *core.Run) {
			run.Restart = &core.RestartIntent{
				DecisionID: decisionID, AttemptID: offer.AttemptID, StageID: stageID, Comments: comments,
				At: jsonx.Date{Time: now}, Phase: core.RestartIntentPhaseIntent,
			}
		})
		if !saved {
			return core.NewFailure("Could not save the restart.")
		}
	}
	if c.overruleHalt != nil && *c.overruleHalt == OverruleHaltIntent {
		return core.NewFailure("Halted after saving the restart.")
	}
	return c.finishOverrule(ctx, runID, decisionID, stageID, comments)
}

// recoverOverrules completes a restart that was saved and not finished, or holds the run when it cannot.
// Lock: runs with c.mu held. Suspends: finishOverrule.
func (c *Coordinator) recoverOverrules(ctx context.Context) {
	if c.recoveringOverrules {
		return
	}
	c.recoveringOverrules = true
	defer func() { c.recoveringOverrules = false }()
	restarting := func(run core.Run) bool { return run.Restart != nil }
	for _, run := range runBookCopiesWhere(&c.book, restarting) {
		if _, restarting := c.book.restarting[run.ID]; restarting {
			continue
		}
		intent := *run.Restart
		switch intent.Phase {
		case core.RestartIntentPhaseIntent:
			if !run.State.IsActive() {
				c.book.update(run.ID, nil, nil, func(r *core.Run) { r.Restart = nil })
				continue
			}
			c.book.restarting[run.ID] = struct{}{}
			if err := c.finishOverrule(ctx, run.ID, intent.DecisionID, intent.StageID, intent.Comments); err != nil {
				// A test halt leaves the boundary on disk for a reload. A real failure holds the run.
				if !strings.HasPrefix(err.Error(), "Halted after") {
					c.holdOverrule(run.ID, err.Error())
				}
			}
			delete(c.book.restarting, run.ID)
		case core.RestartIntentPhaseApplied, core.RestartIntentPhaseHeld:
			if record := c.autopilot.overruleRecord(run); record != nil {
				_ = c.autopilot.recordOverrule(*record, intent.DecisionID)
			}
		}
	}
}

// finishOverrule stops the overruled work and queues the restart.
// Lock: runs with c.mu held. Suspends: stopWork.
func (c *Coordinator) finishOverrule(ctx context.Context, runID, decisionID, stageID, comments string) error {
	run := c.book.Run(runID)
	if run == nil || !run.State.IsActive() {
		c.book.update(runID, nil, nil, func(r *core.Run) { r.Restart = nil })
		return core.NewFailure(core.AfkOverruleFinished)
	}
	if !run.HasAgentStage(stageID) {
		c.book.update(runID, nil, nil, func(r *core.Run) {
			if r.Restart != nil && r.Restart.Phase == core.RestartIntentPhaseIntent {
				r.Restart = nil
			}
		})
		return core.NewFailure(core.AfkOverruleNoAgentStage)
	}
	if err := c.stopWork(ctx, *run); err != nil {
		return core.NewFailure("Could not stop the current work: " + err.Error())
	}
	if c.overruleHalt != nil && *c.overruleHalt == OverruleHaltStopped {
		return core.NewFailure("Halted after stopping.")
	}
	current := c.book.Run(runID)
	if current == nil || !current.State.IsActive() {
		c.book.update(runID, nil, nil, func(r *core.Run) { r.Restart = nil })
		return core.NewFailure(core.AfkOverruleFinished)
	}
	if current.Restart != nil && current.Restart.Phase == core.RestartIntentPhaseApplied && current.Restart.DecisionID == decisionID {
		return c.projectOverrule(*current)
	}
	attempt := ""
	if current.Restart != nil {
		attempt = current.Restart.AttemptID
	}
	now := c.clock.Now()
	saved := c.book.update(runID, nil, nil, func(r *core.Run) {
		if r.Restart != nil && r.Restart.Phase == core.RestartIntentPhaseApplied {
			return
		}
		stage := stageID
		note := comments
		core.EngineAdvance(r, core.Event{Kind: core.EventKindOverrule, RestartStage: stage, Comments: note, Decision: decisionID, AttemptID: attempt}, now)
	})
	if !saved {
		return core.NewFailure("Could not save the restart.")
	}
	after := c.book.Run(runID)
	if after == nil || after.Restart == nil || after.Restart.Phase != core.RestartIntentPhaseApplied || after.Restart.DecisionID != decisionID {
		c.book.update(runID, nil, nil, func(r *core.Run) {
			if r.Restart != nil && r.Restart.Phase == core.RestartIntentPhaseIntent {
				r.Restart = nil
			}
		})
		return core.NewFailure("This run can no longer be overruled.")
	}
	// The engine asked to stop a session that was already stopped before the restart was queued.
	if err := c.projectOverrule(*after); err != nil {
		return core.NewFailure("Could not save the AFK log: " + err.Error())
	}
	if c.overruleHalt != nil && *c.overruleHalt == OverruleHaltLogged {
		return core.NewFailure("Halted after the log.")
	}
	name := stageID
	for _, stage := range after.Stages {
		if stage.ID == stageID {
			name = stage.Name
			break
		}
	}
	runIDCopy := runID
	c.book.note("Overruled "+after.Subject.Ref()+": restarting "+name, &runIDCopy)
	return nil
}

// stopWork stops the sessions of the overruled attempt on run.
// Lock: runs with c.mu held. Suspends: runner.ensureStopped.
func (c *Coordinator) stopWork(ctx context.Context, run core.Run) error {
	// The restart happens on the leg the overruled attempt ran on, which on
	// a run that works several routes need not be the one in front.
	var leg *string
	if run.Restart != nil {
		attemptID := run.Restart.AttemptID
		if attempt := run.AttemptByID(&attemptID); attempt != nil {
			leg = attempt.Leg
		}
	}
	for _, attempt := range run.Attempts {
		if !attempt.IsOpen() || !core.SamePointee(attempt.Leg, leg) {
			continue
		}
		if attempt.Session == nil || !run.HasAgentStage(attempt.StageID) {
			continue
		}
		session := *attempt.Session
		var err error
		c.Suspend(func() { err = c.runner.EnsureStopped(ctx, session) })
		if err != nil {
			return err
		}
	}
	var deciding []core.AfkDecision
	for _, decision := range c.autopilot.decisions {
		if decision.RunID == run.ID && decision.Status == core.AfkDecisionStatusDeciding {
			deciding = append(deciding, decision)
		}
	}
	for _, decision := range deciding {
		if decision.Session != nil {
			session := *decision.Session
			var err error
			c.Suspend(func() { err = c.runner.EnsureStopped(ctx, session) })
			if err != nil {
				return err
			}
		}
		now := c.clock.Now()
		c.autopilot.change(decision.ID, func(d *core.AfkDecision) {
			d.Status = core.AfkDecisionStatusSuperseded
			detail := "the run was overruled"
			d.Detail = &detail
			at := jsonx.Date{Time: now}
			d.DecidedAt = &at
		})
	}
	return nil
}

// holdOverrule holds the run with reason when the overrule cannot finish.
// Lock: runs with c.mu held.
func (c *Coordinator) holdOverrule(runID, reason string) {
	why := "Could not finish overruling: " + reason
	now := c.clock.Now()
	saved := c.book.update(runID, nil, nil, func(r *core.Run) {
		if r.Restart == nil || r.Restart.Phase != core.RestartIntentPhaseIntent {
			return
		}
		core.EngineAdvance(r, core.Event{Kind: core.EventKindRestartHeld, Reason: why}, now)
	})
	if !saved {
		return
	}
	run := c.book.Run(runID)
	if run == nil {
		return
	}
	if record := c.autopilot.overruleRecord(*run); record != nil {
		decisionID := ""
		if run.Restart != nil {
			decisionID = run.Restart.DecisionID
		}
		_ = c.autopilot.recordOverrule(*record, decisionID)
	}
	c.book.note(why, &runID)
}

// projectOverrule checks that the overrule may apply and projects the run's new state.
// Lock: runs with c.mu held.
func (c *Coordinator) projectOverrule(run core.Run) error {
	record := c.autopilot.overruleRecord(run)
	if record == nil || run.Restart == nil {
		return nil
	}
	return c.autopilot.recordOverrule(*record, run.Restart.DecisionID)
}

// requireOffer returns the offer the decision allows, or the refusal as an error.
// Lock: runs with c.mu held.
func (c *Coordinator) requireOffer(id string) (core.AfkOffer, error) {
	decision := c.autopilot.Decision(&id)
	if decision == nil {
		return core.AfkOffer{}, core.NewFailure(core.AfkDecisionGone)
	}
	var run *core.Run
	if current := c.book.Run(decision.RunID); current != nil {
		run = current
	}
	offer, err := core.AfkOfferFor(*decision, run)
	if err == nil {
		return offer, nil
	}
	if refusal, ok := err.(core.AfkRefusal); ok && refusal.Kind == core.AfkRefusalKindIneligible {
		return core.AfkOffer{}, core.NewFailure("This decision cannot be overruled.")
	}
	return core.AfkOffer{}, core.NewFailure(err.Error())
}
