package coordinator

import (
	"context"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// PauseRun pauses the whole run. A second call while one is in flight does nothing.
// Lock: exported entry point; takes c.mu. Suspends: autopilot.discardPaused,
// confirmPendingInterrupts.
func (c *Coordinator) PauseRun(ctx context.Context, id string) {
	c.Lock()
	defer c.EndTurn()
	c.pauseRun(ctx, id)
}

// pauseRun is PauseRun with c.mu held.
// Lock: runs with c.mu held. Suspends: as PauseRun.
func (c *Coordinator) pauseRun(ctx context.Context, id string) {
	if _, busy := c.runPauseBusy[id]; busy {
		return
	}
	c.runPauseBusy[id] = struct{}{}
	defer delete(c.runPauseBusy, id)
	run := c.book.Run(id)
	if c.globallyPaused || run == nil || !core.RunCanPause(*run) {
		return
	}
	operation := strings.ToLower(c.ids.UUID())
	if !c.book.apply(id, core.Event{Kind: core.EventKindPause, Operation: operation}, nil, nil, nil) {
		return
	}
	after := c.book.Run(id)
	if after == nil || after.Pause == nil || after.Pause.Operation != operation {
		return
	}
	c.autopilot.discardPaused(ctx, id)
	c.confirmPendingInterrupts(ctx, id, operation)
}

// ResumeRun resumes a run paused by you. Global continue does not call this.
// Lock: exported entry point; takes c.mu. Suspends: reconcileThenAdmit.
func (c *Coordinator) ResumeRun(ctx context.Context, id string) {
	c.Lock()
	defer c.EndTurn()
	c.resumeRun(ctx, id)
}

// resumeRun is ResumeRun with c.mu held.
// Lock: runs with c.mu held. Suspends: reconcileThenAdmit.
func (c *Coordinator) resumeRun(ctx context.Context, id string) {
	if _, busy := c.runPauseBusy[id]; busy {
		return
	}
	c.runPauseBusy[id] = struct{}{}
	defer delete(c.runPauseBusy, id)
	run := c.book.Run(id)
	if c.globallyPaused || run == nil {
		return
	}
	if run.Pause != nil && run.Pause.Phase == core.RunPausePhasePaused {
		if !c.book.apply(id, core.Event{Kind: core.EventKindResumePaused, Operation: strings.ToLower(c.ids.UUID())}, nil, nil, nil) {
			return
		}
	}
	current := c.book.Run(id)
	if current == nil || current.Pause == nil {
		return
	}
	phase := current.Pause.Phase
	if phase != core.RunPausePhaseResuming && phase != core.RunPausePhaseReconcileFailed {
		return
	}
	if phase == core.RunPausePhaseReconcileFailed {
		operation := ""
		if run.Pause != nil {
			operation = run.Pause.Operation
		} else {
			operation = strings.ToLower(c.ids.UUID())
		}
		c.book.apply(id, core.Event{Kind: core.EventKindResumePaused, Operation: operation}, nil, nil, nil)
	}
	c.reconcileThenAdmit(ctx, id)
}

// RetryContinuation sends a failed Continue again. A sibling that already
// launched is not started twice.
// Lock: exported entry point; takes c.mu. Suspends: runner calls in the launch.
func (c *Coordinator) RetryContinuation(ctx context.Context, id string, leg *string) {
	c.Lock()
	defer c.EndTurn()
	c.retryContinuation(ctx, id, leg)
}

// retryContinuation is RetryContinuation with c.mu held.
// Lock: runs with c.mu held. Spawns: kick.
func (c *Coordinator) retryContinuation(ctx context.Context, id string, leg *string) {
	if c.globallyPaused {
		return
	}
	if run := c.book.Run(id); run != nil && run.Pause != nil && run.Pause.BlocksProgress() {
		return
	}
	c.book.apply(id, core.Event{Kind: core.EventKindRetryContinuation, Leg: leg}, nil, nil, nil)
	c.kick()
}

// recoverRunPauses finishes the run pauses and resumes a restart interrupted.
// Lock: runs with c.mu held. Suspends: confirmPendingInterrupts, recoverContinuations, reconcileThenAdmit.
func (c *Coordinator) recoverRunPauses(ctx context.Context) {
	paused := func(run core.Run) bool { return run.Pause != nil }
	for _, run := range runBookCopiesWhere(&c.book, paused) {
		if _, busy := c.runPauseBusy[run.ID]; busy {
			continue
		}
		pause := *run.Pause
		switch pause.Phase {
		case core.RunPausePhasePausing:
			c.confirmPendingInterrupts(ctx, run.ID, pause.Operation)
		case core.RunPausePhaseResuming:
			c.recoverContinuations(ctx, run)
			if !c.globallyPaused {
				c.reconcileThenAdmit(ctx, run.ID)
			}
		case core.RunPausePhasePaused, core.RunPausePhaseIncomplete, core.RunPausePhaseReconcileFailed:
		}
	}
	// recoverContinuations acts only on a launching Continue, which few runs have.
	launching := func(run core.Run) bool { return run.Pause == nil && runHasLaunchingContinuation(run) }
	for _, run := range runBookCopiesWhere(&c.book, launching) {
		if _, busy := c.runPauseBusy[run.ID]; busy {
			continue
		}
		c.recoverContinuations(ctx, run)
	}
}

// confirmPendingInterrupts confirms each interrupt of a pause still in flight.
// Lock: runs with c.mu held. Suspends: confirmInterrupt.
func (c *Coordinator) confirmPendingInterrupts(ctx context.Context, id, operation string) {
	run := c.book.Run(id)
	if run == nil || run.Pause == nil || run.Pause.Operation != operation || run.Pause.Phase != core.RunPausePhasePausing {
		return
	}
	var pending []core.Attempt
	for _, attempt := range run.Attempts {
		if attempt.IsOpen() && attempt.Interruption != nil && attempt.Interruption.Operation == operation &&
			attempt.Interruption.Confirmation == core.AttemptInterruptionConfirmationPending {
			pending = append(pending, attempt)
		}
	}
	for _, attempt := range pending {
		c.confirmInterrupt(ctx, id, attempt, operation)
	}
}

// confirmInterrupt asks the runner to interrupt one attempt and records what it confirmed or failed on.
// Lock: runs with c.mu held. Suspends: runner.conversationAlive, runner.interrupt.
func (c *Coordinator) confirmInterrupt(ctx context.Context, id string, attempt core.Attempt, operation string) {
	run := c.book.Run(id)
	var spec *runner.SessionSpec
	if run != nil {
		spec = c.sessions.spec(*run, attempt, "")
	}
	if run == nil || run.Pause == nil || run.Pause.Operation != operation || run.Pause.Phase != core.RunPausePhasePausing || spec == nil {
		c.book.apply(id, core.Event{Kind: core.EventKindPauseFailed, Operation: operation, Leg: attemptLegOrNil(attempt), Reason: "the session is missing"}, nil, nil, nil)
		return
	}
	leg := "main"
	if legID := attemptLegOrNil(attempt); legID != nil {
		leg = *legID
	}
	if attempt.Session != nil {
		continued := runner.TmuxContinueName(*attempt.Session)
		alive := false
		c.Suspend(func() { alive = c.runner.ConversationAlive(ctx, continued) })
		if alive {
			interactive := *spec
			interactive.Name = continued
			c.Suspend(func() { c.runner.Interrupt(ctx, interactive) })
		}
	}
	var result runner.SessionInterrupt
	c.Suspend(func() { result = c.runner.Interrupt(ctx, *spec) })
	if current := c.book.Run(id); current == nil || current.Pause == nil || current.Pause.Operation != operation {
		return
	}
	legPtr := &leg
	if result.Kind == runner.SessionInterruptConfirmed {
		identity := result.Conversation
		if identity == nil && spec.Backend != core.BackendCodex {
			session := spec.SessionID
			identity = &session
		}
		c.book.apply(id, core.Event{Kind: core.EventKindPauseConfirmed, Operation: operation, Leg: legPtr, Conversation: identity}, nil, nil, nil)
		return
	}
	reason := result.Message
	c.book.apply(id, core.Event{Kind: core.EventKindPauseFailed, Operation: operation, Leg: legPtr, Reason: reason}, nil, nil, nil)
}

// attemptLegOrNil is the leg an attempt's interruption names, else the attempt's own leg.
// Lock: none (pure).
func attemptLegOrNil(attempt core.Attempt) *string {
	if attempt.Interruption != nil && attempt.Interruption.LegID != "" {
		legID := attempt.Interruption.LegID
		return &legID
	}
	return attempt.Leg
}

// recoverContinuations resumes the Continue launches a restart left behind.
// Lock: runs with c.mu held. Suspends: runner.status.
func (c *Coordinator) recoverContinuations(ctx context.Context, run core.Run) {
	for _, attempt := range run.Attempts {
		if !attemptLaunchesContinuation(attempt) {
			continue
		}
		spec := c.sessions.spec(run, attempt, core.RunPauseContinuePrompt)
		if spec == nil {
			continue
		}
		operation := attempt.Interruption.Continuation.Operation
		leg := attemptLegOrNil(attempt)
		var status runner.SessionStatus
		c.Suspend(func() { status = c.runner.Status(ctx, *spec) })
		switch status.Kind {
		case runner.SessionStatusRunning, runner.SessionStatusFinished, runner.SessionStatusFailed:
			c.book.apply(run.ID, core.Event{Kind: core.EventKindContinuationLaunched, Operation: operation, Leg: leg}, nil, nil, nil)
		case runner.SessionStatusGone:
			reason := "Beltline could not tell whether Continue was delivered"
			c.book.apply(run.ID, core.Event{Kind: core.EventKindContinuationAmbiguous, Operation: operation, Leg: leg, Reason: reason}, nil, nil, nil)
		}
	}
}

// runHasLaunchingContinuation reports whether any of run's attempts has a Continue that
// recoverContinuations would look at.
// Lock: none (pure).
func runHasLaunchingContinuation(run core.Run) bool {
	for i := range run.Attempts {
		if attemptLaunchesContinuation(run.Attempts[i]) {
			return true
		}
	}
	return false
}

// attemptLaunchesContinuation reports whether attempt is open with a Continue still launching.
// Lock: none (pure).
func attemptLaunchesContinuation(attempt core.Attempt) bool {
	return attempt.IsOpen() && attempt.Interruption != nil && attempt.Interruption.Continuation != nil &&
		attempt.Interruption.Continuation.Status == core.AttemptInterruptionContinuationStatusLaunching
}

// reconcileThenAdmit reconciles a run with its source, then admits its queued work.
// Lock: runs with c.mu held. Suspends: intake.reconcile, sessions.startContinuations.
func (c *Coordinator) reconcileThenAdmit(ctx context.Context, id string) {
	run := c.book.Run(id)
	if run == nil || run.Pause == nil || run.Pause.Phase != core.RunPausePhaseResuming {
		return
	}
	if err := c.intake.reconcile(ctx, id); err != nil {
		ref := "the run"
		if current := c.book.Run(id); current != nil {
			ref = current.Subject.Ref()
		}
		c.book.apply(id, core.Event{Kind: core.EventKindReconcileFailed, Reason: "Could not read " + ref + ": " + err.Error()}, nil, nil, nil)
		return
	}
	current := c.book.Run(id)
	if current == nil || current.Pause == nil || current.Pause.Phase != core.RunPausePhaseResuming || !current.State.IsActive() {
		return
	}
	c.book.apply(id, core.Event{Kind: core.EventKindContinuationsAdmitted}, nil, nil, nil)
	c.sessions.startContinuations(ctx)
}
