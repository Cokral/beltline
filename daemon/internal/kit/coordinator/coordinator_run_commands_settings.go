package coordinator

import (
	"context"
	"fmt"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// StartNowCommand starts a queued agent stage now, past a full stage and a paused belt.
// Lock: exported entry point; takes c.mu. Suspends: sessions.startNow.
func (c *Coordinator) StartNowCommand(ctx context.Context, id string) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	command, err := c.commandOn(id, core.RunCommandStartNow)
	if err != nil {
		return core.Run{}, err
	}
	if err := command.refuse(core.RunActionTarget{}); err != nil {
		return core.Run{}, err
	}
	c.startNow(ctx, id)
	return command.answer("Could not start the stage.", changed)
}

// ContinueSessionCommand launches or reuses the interactive conversation of a
// saved attempt and returns the writable tmux attach command.
// Lock: exported entry point; takes c.mu. Suspends: continueSession.
func (c *Coordinator) ContinueSessionCommand(ctx context.Context, id, attempt string) (core.Run, []string, error) {
	c.Lock()
	defer c.EndTurn()
	command, err := c.commandOn(id, core.RunCommandContinueSession)
	if err != nil {
		return core.Run{}, nil, err
	}
	var saved *core.Attempt
	if found := command.run().AttemptByID(&attempt); found != nil && found.Session != nil {
		copied := *found
		saved = &copied
	}
	if saved == nil {
		return core.Run{}, nil, core.NewValidationFailure("This run has no saved attempt with that id.", nil)
	}
	if err := command.refuse(core.RunActionTarget{Stage: &saved.StageID, Attempt: &saved.ID}); err != nil {
		return core.Run{}, nil, err
	}
	attach, err := c.sessions.continueSession(ctx, id, *saved)
	if err != nil {
		return core.Run{}, nil, err
	}
	after, err := command.answer("", nil)
	if err != nil {
		return core.Run{}, nil, err
	}
	return after, attach, nil
}

// AdjustStageCommand changes only this run's selected agent stage: backend,
// model, effort and brief. An identical save is a no-op.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AdjustStageCommand(id, stageID, model string, thinking *core.Thinking, brief *string, runner *core.Backend) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	command, err := c.commandOn(id, core.RunCommandAdjustStage)
	if err != nil {
		return core.Run{}, err
	}
	if !command.run().HasAgentStage(stageID) {
		return core.Run{}, core.NewValidationFailure(core.StageAdjustNoAgentStage, nil)
	}
	if err := command.refuse(core.RunActionTarget{Stage: &stageID}); err != nil {
		return core.Run{}, err
	}
	if _, err := c.adjustStage(id, stageID, &model, thinking, brief, runner); err != nil {
		return core.Run{}, err
	}
	return command.answer("", nil)
}

// AdjustSwitchCommand is the Adjust draft as it reads after a runner change: the
// model stays only when this run may use it on that runner, and the effort only
// when that runner takes it. Nothing is saved.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AdjustSwitchCommand(id, stageID string, draft core.StageAdjustDraft) (core.StageAdjustDraft, error) {
	c.Lock()
	defer c.EndTurn()
	command, err := c.commandOn(id, core.RunCommandAdjustStage)
	if err != nil {
		return core.StageAdjustDraft{}, err
	}
	run := command.run()
	if !run.HasAgentStage(stageID) {
		return core.StageAdjustDraft{}, core.NewValidationFailure(core.StageAdjustNoAgentStage, nil)
	}
	if err := command.refuse(core.RunActionTarget{Stage: &stageID}); err != nil {
		return core.StageAdjustDraft{}, err
	}
	index := run.StageIndexOf(stageID)
	runnerChoice := draft.Runner
	var allowed []string
	for _, choice := range c.adjustmentModels(run, run.Stages[index], &runnerChoice) {
		allowed = append(allowed, choice.ID)
	}
	return core.StageAdjustSwitching(draft, draft.Runner, allowed), nil
}

// AfkCommand saves an active run's AFK mode and lets it take effect.
// Lock: exported entry point; takes c.mu. Suspends: autopilot.cancelTurnedOff.
func (c *Coordinator) AfkCommand(ctx context.Context, id string, mode core.AfkMode) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	command, err := c.commandOn(id, core.RunCommandAfk)
	if err != nil {
		return core.Run{}, err
	}
	if err := command.refuse(core.RunActionTarget{}); err != nil {
		return core.Run{}, err
	}
	if command.run().Afk() != mode {
		now := c.clock.Now()
		saved := c.book.update(id, nil, nil, func(r *core.Run) {
			r.SetAfk(mode)
			r.UpdatedAt = jsonx.Date{Time: now}
		})
		if !saved {
			return core.Run{}, core.NewValidationFailure("Could not save the AFK choice.", nil)
		}
		c.autopilot.cancelTurnedOff(ctx)
		c.kick()
	}
	return command.answer("", nil)
}

// EraseCommand erases a finished run, including a repeated erase of an id that is
// still erased. A failed erase keeps what can be retried.
// Lock: exported entry point; takes c.mu. Suspends: erase.
func (c *Coordinator) EraseCommand(ctx context.Context, id string) error {
	c.Lock()
	defer c.EndTurn()
	// A run still here answers to the table; an erased id has no context and goes straight to the erasure.
	if context := c.runActionContextByID(id); context != nil {
		if refusal := core.RunAvailabilityRefusal(core.RunCommandErase, *context, core.RunActionTarget{}); refusal != nil {
			return core.NewValidationFailure(*refusal, nil)
		}
	}
	return c.erase(ctx, id)
}

// OverruleAfkCommand restarts the run an applied AFK decision belongs to, at an agent stage.
// Lock: exported entry point; takes c.mu. Suspends: overruleAfk.
func (c *Coordinator) OverruleAfkCommand(ctx context.Context, decision, stage, comments string, acceptedWarning bool) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	record := c.autopilot.Decision(&decision)
	if record == nil {
		return core.Run{}, core.NewValidationFailure(core.AfkDecisionGone, nil)
	}
	command, err := c.commandOn(record.RunID, core.RunCommandOverruleAfk)
	if err != nil {
		return core.Run{}, err
	}
	if err := command.refuse(core.RunActionTarget{Stage: &stage, Decision: &decision}); err != nil {
		return core.Run{}, err
	}
	if err := c.overruleAfk(ctx, decision, stage, comments, acceptedWarning); err != nil {
		return core.Run{}, err
	}
	return command.answer("", nil)
}

// StartTaskCommand starts a task you wrote on a manual belt. Repeated accepted
// submissions create another task, as repeated Start clicks do. It does not kick
// the loop: the run starts on the next tick, as the start-task command does (it
// only calls Intake.startTask; Coordinator.startTask is the one that kicks).
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) StartTaskCommand(belt, repo, title, body string, entry *string) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	return c.intake.startTask(belt, repo, title, body, entry)
}

// linkedAttempt is the attempt a decision ended, when the run still records it,
// or the empty string.
// Lock: none (pure).
func linkedAttempt(decision core.AfkDecision, run core.Run) string {
	for _, attempt := range run.Attempts {
		if attempt.AfkDecision != nil && *attempt.AfkDecision == decision.ID {
			return attempt.ID
		}
	}
	return ""
}

// startNow is StartNow with c.mu held: the same body, for the commands that already hold the lock.
// Lock: runs with c.mu held. Suspends: sessions.startNow.
func (c *Coordinator) startNow(ctx context.Context, id string) {
	if c.globallyPaused {
		return
	}
	run := c.book.Run(id)
	if run == nil || run.Parking != nil || (run.Pause != nil && run.Pause.BlocksProgress()) || run.State != core.RunStateQueued {
		return
	}
	stage := run.Stage()
	if stage == nil || stage.Kind() != core.StageKindAgent {
		return
	}
	runID := id
	c.book.note("Started "+stage.Name+" ahead of the queue", &runID)
	c.sessions.startNow(ctx, id)
}

// adjustStage is AdjustStage with c.mu held: the same body, for AdjustStageCommand, which already holds the lock.
// Lock: runs with c.mu held.
func (c *Coordinator) adjustStage(id, stageID string, model *string, thinking *core.Thinking, brief *string, runner *core.Backend) (*core.Adjustment, error) {
	run, err := c.sessions.admittedRun(id)
	if err != nil {
		return nil, err
	}
	stage := findStage(run, stageID)
	if stage == nil || !core.StageAdjustCanAdjust(*stage, run) {
		return nil, core.NewFailure("This stage cannot be adjusted.")
	}
	// Changes count against the settings this run uses here, which on a
	// stage that spreads its sessions can be one of its alternates.
	current := core.ModelPoolCurrent(run, *stage)
	backend := core.StageAdjustTarget(runner, current, *stage)
	modelID := core.ValueOf(model)
	choices := c.adjustmentModels(run, *stage, &backend)
	if len(choices) == 0 {
		return nil, core.NewFailure(core.StageAdjustNoModelsAllowed)
	}
	choice := core.StageAdjustChoice(choices, modelID)
	if choice == nil {
		return nil, core.NewFailure(fmt.Sprintf("%s: choose a model allowed when this run started for %s.", stage.Name, backend.Label()))
	}
	if thinking != nil {
		if _, ok := backend.SupportedThinking()[*thinking]; !ok {
			return nil, core.NewFailure(fmt.Sprintf("%s: %s doesn't take the thinking level %s.", stage.Name, backend.Label(), thinking.Label()))
		}
	}
	ownBrief := core.StageAdjustOwnBrief(brief, c.library.AgentFor(stage.Agent, run))
	fields := core.StageAdjustChangedFields(current, *stage, backend, modelID, thinking, ownBrief)
	if len(fields) == 0 {
		return nil, nil
	}
	adjustment := core.Adjustment{StageID: stageID, Date: jsonx.Date{Time: c.clock.Now()}, Fields: fields}
	changesRunner := core.StageAdjustChangesRunner(fields)
	saved := c.book.update(id, nil, nil, func(r *core.Run) {
		index := r.StageIndexOf(stageID)
		if index < 0 {
			return
		}
		runPreserveLegacyRunners(r)
		if legacyRepairPending(r.ModelAdmission, stageID) {
			r.ModelAdmission.Repair(stageID, backend, *choice)
		}
		// The model you chose is the one this run uses here from now on.
		if changesRunner {
			if core.StageAdjustBackend(r.Stages[index]) != backend {
				r.Stages[index].Runner = &backend
			}
			r.Stages[index].Model = modelID
			r.Stages[index].Thinking = thinking
			r.Stages[index].Alternates = nil
		}
		r.Stages[index].Brief = ownBrief
		r.Adjustments = append(r.Adjustments, adjustment)
		r.UpdatedAt = adjustment.Date
	})
	if !saved {
		return nil, core.NewFailure("Could not save the stage adjustment.")
	}
	runID := id
	c.book.note(fmt.Sprintf("Adjusted %s: %s", stage.Name, core.AdjustmentLabels(fields)), &runID)
	return &adjustment, nil
}
