package coordinator

import (
	"context"
	"os"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// continueSession starts an interactive session that continues a finished agent
// attempt. The launch stays busy until tmux has created the session, and a launch
// that loses to erasure stops that session before returning it.
// Lock: runs with c.mu held. Suspends: tmux.continueSession, runner.ensureStopped.
func (s *Sessions) continueSession(ctx context.Context, runID string, attempt core.Attempt) ([]string, error) {
	if run := s.book.Run(runID); run != nil && run.Pause != nil && run.Pause.BlocksProgress() {
		return nil, core.NewFailure(core.DesktopActionsPausedReason)
	}
	if s.book.isErasing(runID) {
		return nil, core.NewFailure("This run is being erased.")
	}
	if run := s.book.Run(runID); run != nil && run.Parking != nil {
		return nil, core.NewFailure("This run is parked.")
	}
	// Claimed before the first suspension point: a second request for the same run is refused while this one launches.
	if _, claimed := s.continuing[runID]; claimed {
		return nil, core.NewFailure("This session is already being continued.")
	}
	s.continuing[runID] = struct{}{}
	_, alreadyBusy := s.book.busy[runID]
	ownedBusy := !alreadyBusy
	if ownedBusy {
		s.book.busy[runID] = struct{}{}
	}
	defer func() {
		delete(s.continuing, runID)
		if ownedBusy {
			delete(s.book.busy, runID)
		}
	}()
	run, err := s.admittedRun(runID)
	if err != nil {
		return nil, err
	}
	if s.book.isErasing(runID) {
		return nil, core.NewFailure("This run is being erased.")
	}
	if _, _, err := s.continueEligibility(run, attempt); err != nil {
		return nil, err
	}
	var saved *core.Attempt
	for index := range run.Attempts {
		if run.Attempts[index].ID == attempt.ID {
			saved = &run.Attempts[index]
			break
		}
	}
	var spec *runner.SessionSpec
	if saved != nil {
		spec = s.spec(run, *saved, "")
	}
	if spec == nil {
		return nil, core.NewFailure(core.RunAvailabilitySessionGone)
	}
	tmux, ok := tmuxRunnerOf(s.runner)
	if !ok {
		return nil, core.NewFailure(core.RunAvailabilitySessionGone)
	}
	var name string
	var launchErr error
	s.c.Suspend(func() { name, launchErr = tmux.ContinueSession(ctx, *spec) })
	if launchErr != nil {
		return nil, launchErr
	}
	generation := run.Generation
	if index := s.book.liveIndex(runID, workContinue, &generation, nil); index < 0 {
		current := s.book.Run(runID)
		parked := current != nil && current.Parking != nil
		var stopErr error
		s.c.Suspend(func() { stopErr = s.runner.EnsureStopped(ctx, name) })
		if stopErr != nil {
			return nil, stopErr
		}
		if parked {
			return nil, core.NewFailure("This run is parked.")
		}
		return nil, core.NewFailure("This run is being erased.")
	}
	return []string{"tmux", "-L", runner.TmuxSocket(), "attach-session", "-t", name}, nil
}

// continueEligibility says why a saved session cannot be continued, or returns
// its frozen settings and session spec. The availability table and the launch
// share it.
// Lock: runs with c.mu held.
func (s *Sessions) continueEligibility(run core.Run, attempt core.Attempt) (core.SessionSettings, runner.SessionSpec, error) {
	saved := run.AttemptByID(&attempt.ID)
	var stage *core.Stage
	if saved != nil {
		stage = findStage(run, saved.StageID)
	}
	if saved == nil || stage == nil {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure(core.RunAvailabilitySessionGone)
	}
	_, hasFallback := fallbackSettings(run, saved.ID)
	if saved.Settings == nil && !hasFallback {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure("This historical attempt has no saved explicit model settings and cannot safely be continued.")
	}
	settings := attemptSettingsHistorical(run, *saved, *stage)
	if settings == nil {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure(attemptSettingsUnrecoverableBackend)
	}
	spec := s.spec(run, *saved, "")
	if spec == nil {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure(core.RunAvailabilitySessionGone)
	}
	// The admitted model, not the variant identifier the spec passes. Never the stage's adjusted backend.
	admission := core.ValueOr(run.ModelAdmission, core.ModelAdmission{Models: map[string][]core.ModelChoice{}})
	if reason := core.ModelPolicyWarning(settings.Model, spec.Backend, "Historical attempt", admission, nil); reason != nil {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure(*reason)
	}
	exists := isDirectory(spec.Directory)
	if reason := core.AttemptPanelCanContinue(run, *saved, exists); reason != nil {
		return core.SessionSettings{}, runner.SessionSpec{}, core.NewFailure(*reason)
	}
	return *settings, *spec, nil
}

// fallbackSettings is the legacy fallback frozen for an attempt, if any.
// Lock: none (pure).
func fallbackSettings(run core.Run, attemptID string) (core.SessionSettings, bool) {
	if run.ModelAdmission == nil {
		return core.SessionSettings{}, false
	}
	settings, ok := run.ModelAdmission.LegacyAttemptSettings[attemptID]
	return settings, ok
}

// isDirectory reports whether path is an existing folder.
// Lock: none (reads the filesystem).
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// startContinuations sends Continue once to each interrupted stage agent, when a slot admits it.
// Lock: runs with c.mu held. Suspends: continueInterrupted.
func (s *Sessions) startContinuations(ctx context.Context) {
	if s.globallyPaused {
		return
	}
	for _, run := range runBookSnapshotWhere(s.book, runHasQueuedContinuation) {
		if run.Pause != nil || !run.State.IsActive() || s.skipsRun(run) {
			continue
		}
		if live := s.library.Belt(run.BeltID); live != nil && live.EffectivelyPaused() {
			continue
		}
		for _, attempt := range run.Attempts {
			if !attempt.IsOpen() || attempt.Interruption == nil || attempt.Interruption.Continuation == nil ||
				attempt.Interruption.Continuation.Status != core.AttemptInterruptionContinuationStatusQueued {
				continue
			}
			stage := findStage(run, attempt.StageID)
			if stage == nil {
				continue
			}
			if s.legsAtStage(run.BeltID, stage.ID) >= max(stage.Slots, 1) {
				continue
			}
			s.continueInterrupted(ctx, run.ID, attempt)
		}
	}
}

// continueInterrupted re-enters an attempt with Continue: the same attempt,
// folder, checkout, conversation and settings.
// Lock: runs with c.mu held. Suspends: runner.usageCheckpoint, runner.start, runner.stop.
func (s *Sessions) continueInterrupted(ctx context.Context, runID string, attempt core.Attempt) {
	run := s.book.Run(runID)
	if run == nil || run.Pause != nil {
		return
	}
	interruptedLeg := core.Leg{ID: "main", StageID: &attempt.StageID, State: core.RunStateRunning}
	if attempt.Leg != nil {
		interruptedLeg.ID = *attempt.Leg
	}
	if s.busy(*run, interruptedLeg) {
		return
	}
	pending, queued := s.queuedContinuation(runID, *run, attempt)
	if !queued {
		fallbackOperation := ""
		if attempt.Interruption != nil && attempt.Interruption.Continuation != nil {
			fallbackOperation = attempt.Interruption.Continuation.Operation
		}
		s.failContinuation(runID, fallbackOperation, attempt.Leg, "the conversation cannot be continued")
		return
	}
	spec, reason := s.continuationSpec(ctx, pending)
	if reason != nil {
		s.failContinuation(runID, pending.operation, pending.leg, *reason)
		return
	}
	if !s.markContinuation(pending, spec) {
		return
	}
	if !s.book.apply(runID, core.Event{Kind: core.EventKindContinuationLaunching, Operation: pending.operation, Leg: pending.leg}, nil, nil, nil) {
		return
	}
	s.launchContinuation(ctx, pending, spec)
}

// pendingContinue is an interrupted attempt's queued Continue, with the stage
// and the session spec it resumes from.
type pendingContinue struct {
	runID     string
	operation string
	leg       *string
	admission core.ModelAdmission
	saved     *core.Attempt
	stage     *core.Stage
	base      *runner.SessionSpec
}

// queuedContinuation finds the attempt's queued Continue, with the stage and spec
// it resumes from. It reports false when any part is missing.
// Lock: runs with c.mu held.
func (s *Sessions) queuedContinuation(runID string, run core.Run, attempt core.Attempt) (pendingContinue, bool) {
	// The guard of continueInterrupted: when any part fails, the
	// continuation fails with the operation the caller's attempt names.
	saved := run.AttemptByID(&attempt.ID)
	if saved == nil || !saved.IsOpen() || saved.Interruption == nil || saved.Interruption.Continuation == nil ||
		saved.Interruption.Continuation.Status != core.AttemptInterruptionContinuationStatusQueued {
		return pendingContinue{}, false
	}
	stage := findStage(run, saved.StageID)
	if stage == nil {
		return pendingContinue{}, false
	}
	base := s.spec(run, *saved, core.RunPauseContinuePrompt)
	if base == nil {
		return pendingContinue{}, false
	}
	leg := "main"
	if saved.Interruption != nil {
		leg = saved.Interruption.LegID
	} else if saved.Leg != nil {
		leg = *saved.Leg
	}
	return pendingContinue{
		runID:     runID,
		operation: saved.Interruption.Continuation.Operation,
		leg:       &leg,
		admission: admissionOf(run),
		saved:     saved,
		stage:     stage,
		base:      base,
	}, true
}

// continuationSpec builds the Continue session from the interrupted turn's
// conversation and checkout. It returns the reason the conversation cannot be
// continued instead, or nil when it can.
// Lock: runs with c.mu held. Suspends: runner.usageCheckpoint (interactive Copilot only).
func (s *Sessions) continuationSpec(ctx context.Context, pending pendingContinue) (runner.SessionSpec, *string) {
	saved, base := pending.saved, pending.base
	backend := base.Backend
	if saved.Settings != nil {
		if reason := core.ModelPolicyWarning(saved.Settings.Model, backend, pending.stage.Name, pending.admission, nil); reason != nil {
			return runner.SessionSpec{}, reason
		}
	}
	if !isDirectory(base.Directory) {
		return runner.SessionSpec{}, new("the run's checkout is gone")
	}
	var token *string
	if saved.Interruption != nil && saved.Interruption.Conversation != nil {
		token = saved.Interruption.Conversation
	} else if backend == core.BackendCodex {
		token = runner.AdapterFor(core.BackendCodex).SessionToken(base.Directory)
	} else {
		token = saved.SessionID
	}
	if backend == core.BackendCodex && token == nil {
		return runner.SessionSpec{}, new("Codex has no rollout to resume")
	}
	if saved.SessionID == nil {
		return runner.SessionSpec{}, new("the conversation identity is missing")
	}
	spec := *base
	spec.Prompt = core.RunPauseContinuePrompt
	spec.Resume = true
	spec.ResumeToken = token
	// The interrupted turn can already contain a failure, such as Claude's
	// synthetic "No response requested." Continue only counts what comes after it.
	spec.LogMark = runner.AdapterFor(backend).Mark(base.Directory, *saved.SessionID)
	spec.UsageCheckpoint = saved.UsageCheckpoint
	// An interactive turn resumes with a token of its own, so the interrupted
	// turn's handoff can never finish it. Older attempts resume as they ran.
	if spec.CompletionToken != "" {
		spec.CompletionToken = core.AttemptCompletionToken(*saved.SessionID, saved.CompletionToken)
		if backend.FinishesOnHandoff() {
			spec.Prompt += core.BriefHandoff(spec.AttemptFolder, spec.CompletionToken)
		}
	}
	if spec.Interactive && backend == core.BackendCopilot {
		s.c.Suspend(func() { spec.UsageCheckpoint = s.runner.UsageCheckpoint(ctx, spec) })
	}
	return spec, nil
}

// markContinuation records the Continue's log mark, completion token and usage
// checkpoint on its attempt before the session launches. It reports false when
// the run is gone.
// Lock: runs with c.mu held.
func (s *Sessions) markContinuation(pending pendingContinue, spec runner.SessionSpec) bool {
	backend := pending.base.Backend
	return s.book.update(pending.runID, nil, nil, func(r *core.Run) {
		if index := attemptIndexOf(*r, pending.saved.ID); index >= 0 {
			r.Attempts[index].LogMark = new(spec.LogMark)
			if spec.CompletionToken != "" {
				token := spec.CompletionToken
				r.Attempts[index].CompletionToken = &token
			}
			if spec.Interactive && backend == core.BackendCopilot && spec.UsageCheckpoint != nil {
				checkpoint := *spec.UsageCheckpoint
				r.Attempts[index].UsageCheckpoint = &checkpoint
			}
		}
	})
}

// launchContinuation starts the Continue session and records it as launched.
// A start that fails, or a launch the run no longer wants, fails or stops the session.
// Lock: runs with c.mu held. Suspends: runner.start, runner.stop.
func (s *Sessions) launchContinuation(ctx context.Context, pending pendingContinue, spec runner.SessionSpec) {
	var startErr error
	s.c.Suspend(func() { startErr = s.runner.Start(ctx, spec) })
	if startErr != nil {
		s.c.Suspend(func() { s.runner.Stop(ctx, spec.Name) })
		s.failContinuation(pending.runID, pending.operation, pending.leg, "could not start: "+startErr.Error())
		return
	}
	if !s.continuationLaunching(pending.runID, pending.saved.ID) {
		s.c.Suspend(func() { s.runner.Stop(ctx, spec.Name) })
		return
	}
	s.book.apply(pending.runID, core.Event{Kind: core.EventKindContinuationLaunched, Operation: pending.operation, Leg: pending.leg}, nil, nil, nil)
}

// continuationLaunching reports whether the run still has the attempt's Continue
// in the launching state.
// Lock: runs with c.mu held.
func (s *Sessions) continuationLaunching(runID, attemptID string) bool {
	current := s.book.Run(runID)
	if current == nil {
		return false
	}
	return slices.ContainsFunc(current.Attempts, func(stored core.Attempt) bool {
		return stored.ID == attemptID && stored.Interruption != nil && stored.Interruption.Continuation != nil &&
			stored.Interruption.Continuation.Status == core.AttemptInterruptionContinuationStatusLaunching
	})
}

// failContinuation records that an interrupted attempt's Continue could not run.
// Lock: runs with c.mu held (the book saves the run).
func (s *Sessions) failContinuation(runID, operation string, leg *string, reason string) {
	s.book.apply(runID, core.Event{Kind: core.EventKindContinuationFailed, Operation: operation, Leg: leg, Reason: reason}, nil, nil, nil)
}

// admissionOf is the run's frozen admission, or an empty one when it has none.
// Lock: none (pure).
func admissionOf(run core.Run) core.ModelAdmission {
	if run.ModelAdmission == nil {
		return core.ModelAdmission{Models: map[string][]core.ModelChoice{}}
	}
	return *run.ModelAdmission
}

// attemptIndexOf is the index of the attempt with id in run.Attempts, or -1.
// Lock: none (pure).
func attemptIndexOf(run core.Run, id string) int {
	for index := range run.Attempts {
		if run.Attempts[index].ID == id {
			return index
		}
	}
	return -1
}
