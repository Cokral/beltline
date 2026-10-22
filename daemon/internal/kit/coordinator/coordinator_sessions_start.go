package coordinator

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// start starts one leg's agent stage: it prepares the checkout, the model and
// the session identity, and launches the agent. After you
// answered a held stage, its session continues instead.
// Lock: runs with c.mu held. Suspends: base, workspaces.prepare, runner.prepareSessionId,
// runner.endContinuing, runner.usageMark, runner.usageCheckpoint, workspaces.fingerprint,
// runner.start, runner.stop. Spawns: none.
func (s *Sessions) start(ctx context.Context, runID string, legID *string) {
	run, err := s.admittedRun(runID)
	if err != nil {
		s.book.note(err.Error(), &runID)
		return
	}
	leg, found := startableLeg(run, legID)
	if run.Parking != nil || !found {
		return
	}
	stage := leg.Stage(run)
	if stage == nil || stage.Kind() != core.StageKindAgent || leg.State != core.RunStateQueued || s.busy(run, leg) || s.book.isErasing(runID) {
		return
	}
	// A session that a move displaced may still be stopping; this one starts once it has stopped.
	if stop := s.c.pendingStop(runID); stop != nil {
		stop.awaited = true
		return
	}
	generation, operation := run.Generation, run.Operation
	s.mark(run, leg, true)
	defer s.mark(run, leg, false)
	folderName := core.EngineAttemptFolderLeg(run, *stage, &leg.ID)
	number := attemptNumber(run, *stage, leg)
	// After you answered a held stage, its session continues instead.
	held := run.AttemptByID(leg.Continues)
	if held != nil && held.SessionID == nil {
		held = nil
	}
	replay := run.AttemptByID(leg.Replays)
	settings, backend, blocked := s.startSettings(run, *stage, leg, held)
	if blocked != nil {
		legID := leg.ID
		s.book.apply(runID, core.Event{Kind: core.EventKindAdmissionBlocked, Reason: *blocked, Leg: &legID}, &generation, &operation, nil)
		return
	}
	sessionID := ""
	if held != nil && held.SessionID != nil {
		sessionID = *held.SessionID
	} else {
		sessionID = strings.ToLower(s.c.ids.UUID())
	}
	// Repository names and PR numbers can repeat across owners or review
	// requests. The agent's persisted UUID gives each run its own session.
	l := &legLaunch{
		runID: runID, generation: generation, operation: operation, run: run, leg: leg, stage: *stage,
		number: number, name: sessionName(run, *stage, leg.ID, number, sessionID), sessionID: sessionID,
		folderName: folderName, held: held, replay: replay, settings: settings, backend: backend,
		// The leg as the run's working legs show it, which is what the launch prepares for.
		prepared: workingLeg(run, leg.ID),
	}
	if err := s.launchLeg(ctx, l); err != nil {
		s.abandonStart(*l, err)
	}
}

// startableLeg is the leg a start works on: the one legID names, or else the first
// queued working leg. found is false when neither exists.
// Lock: none (pure).
func startableLeg(run core.Run, legID *string) (leg core.Leg, found bool) {
	if legID != nil {
		if picked := run.LegByID(legID); picked != nil {
			return *picked, true
		}
	}
	for _, working := range run.WorkingLegs() {
		if working.State == core.RunStateQueued {
			return working, true
		}
	}
	return core.Leg{}, false
}

// attemptNumber is the number of the leg's next attempt at stage: one more than
// the attempts it already has there.
// Lock: none (pure).
func attemptNumber(run core.Run, stage core.Stage, leg core.Leg) int {
	key := core.AttemptKey(stage.ID, &leg.ID)
	number := 1
	for _, attempt := range run.Attempts {
		if core.AttemptKey(attempt.StageID, attempt.Leg) == key {
			number++
		}
	}
	return number
}

// startSettings works out the settings and runner a leg's session starts with
// . Earlier work changes only the prompt. A fresh session,
// replaying it or not, uses today's stage settings, picked from its pool. Answering
// keeps the held attempt's backend. A legacy blank model is the one case that takes
// the repaired stage. blocked is the reason the leg is held instead of started.
// Lock: runs with c.mu held. Suspends: none (pure).
func (s *Sessions) startSettings(run core.Run, stage core.Stage, leg core.Leg, held *core.Attempt) (settings core.SessionSettings, backend core.Backend, blocked *string) {
	stageSettings := s.pooledSettings(run, stage, leg.ID)
	var fresh core.SessionSettings
	if stageSettings != nil {
		fresh = *stageSettings
	} else {
		stageBackend := core.StageAdjustBackend(stage)
		fresh = core.SessionSettings{Model: stage.Model, Thinking: stage.Thinking, Runner: &stageBackend}
	}
	legacyRaw := func(attempt core.Attempt) *core.SessionSettings {
		if attempt.Settings != nil {
			return attempt.Settings
		}
		if run.ModelAdmission != nil {
			if settings, ok := run.ModelAdmission.LegacyAttemptSettings[attempt.ID]; ok {
				return &settings
			}
		}
		return nil
	}
	var continuing *core.SessionSettings
	if held != nil {
		raw := legacyRaw(*held)
		if raw == nil || strings.TrimSpace(raw.Model) != "" {
			continuing = attemptSettingsHistorical(run, *held, stage)
		}
	}
	if held != nil && continuing == nil {
		raw := legacyRaw(*held)
		blank := raw != nil && strings.TrimSpace(raw.Model) == ""
		if !blank && (raw != nil || attemptSettingsBackendAdjusted(run, *held, stage.ID)) {
			reason := attemptSettingsUnrecoverableBackend
			return settings, backend, &reason
		}
	}
	settings = fresh
	if continuing != nil {
		settings = *continuing
	}
	backend = core.StageAdjustBackend(stage)
	if settings.Runner != nil {
		backend = *settings.Runner
	}
	// The run's own frozen admission, never the live settings, decides. A leg
	// that cannot be admitted is held on its own; the other legs go on.
	admission := core.ValueOr(run.ModelAdmission, core.ModelAdmission{Models: map[string][]core.ModelChoice{}})
	return settings, backend, core.ModelPolicyWarning(settings.Model, backend, stage.Name, admission, nil)
}

// sessionName is the tmux session name of a leg's attempt.
// Lock: none (pure).
func sessionName(run core.Run, stage core.Stage, legID string, number int, sessionID string) string {
	return runner.TmuxSessionName([]string{
		run.BeltID, repoLastComponent(run.Subject.Repo), run.Subject.SessionPart(), stage.ID, legID, strconv.Itoa(number), sessionID,
	})
}

// repoLastComponent is the part of owner/repo after its slash, or "repo" when there is none.
// Lock: none (pure).
func repoLastComponent(repo string) string {
	last := ""
	for _, part := range strings.Split(repo, "/") {
		if part != "" {
			last = part
		}
	}
	if last == "" {
		return "repo"
	}
	return last
}

// legInState reports whether the run has a working leg with id in state.
// Lock: none (pure).
func legInState(run core.Run, id string, state core.RunState) bool {
	return slices.ContainsFunc(run.WorkingLegs(), func(leg core.Leg) bool {
		return leg.ID == id && leg.State == state
	})
}

// workingLeg is the leg with id as run.WorkingLegs shows it, or nil. For a run that is
// not split, that is the run's own state, stage and note, not the stored leg.
// Lock: none (pure).
func workingLeg(run core.Run, id string) *core.Leg {
	for _, leg := range run.WorkingLegs() {
		if leg.ID == id {
			return &leg
		}
	}
	return nil
}

// preparedLegStill reports whether the leg with id, as run.WorkingLegs shows it now, is
// still the one a launch prepared for. A move can change a queued route's stage meanwhile.
// Lock: none (pure).
func preparedLegStill(run core.Run, id string, prepared *core.Leg) bool {
	now := workingLeg(run, id)
	return prepared != nil && now != nil && reflect.DeepEqual(*now, *prepared)
}

// legIndexOf is the index of the leg with id in run.Legs, or -1.
// Lock: none (pure).
func legIndexOf(run core.Run, id string) int {
	for index := range run.Legs {
		if run.Legs[index].ID == id {
			return index
		}
	}
	return -1
}

// heldAgentName is the name a held attempt's agent knows its session by, or nil.
// Lock: none (pure).
func heldAgentName(held *core.Attempt) *string {
	if held == nil {
		return nil
	}
	return held.AgentSessionName()
}

// heldUsageCheckpoint is a held attempt's usage checkpoint, or nil.
// Lock: none (pure).
func heldUsageCheckpoint(held *core.Attempt) *core.UsageCheckpoint {
	if held == nil {
		return nil
	}
	return held.UsageCheckpoint
}

// sessionsCheckout is the checkout stage works in on run. A run about a pull
// request reads its head, whatever the stage's workspace says.
// Lock: none (pure).
func sessionsCheckout(run core.Run, stage core.Stage) runner.WorkspaceCheckout {
	if about := run.Subject.About(); about != nil && about.Kind == core.ResourceRefKindPullRequest {
		return runner.WorkspaceCheckout{Kind: runner.WorkspaceCheckoutKindPullRequest, Number: about.Number}
	}
	if stage.Workspace == core.WorkspaceReadOnly {
		return runner.WorkspaceCheckout{Kind: runner.WorkspaceCheckoutKindBase}
	}
	return runner.WorkspaceCheckout{Kind: runner.WorkspaceCheckoutKindWorktree}
}

// base returns the branch work starts from in repo: a pull request's own base,
// the belt's, or the repository's default.
// Lock: runs with c.mu held. Suspends: bases.base.
func (s *Sessions) base(ctx context.Context, run core.Run, repo string) (string, error) {
	return s.bases.base(ctx, run, repo)
}
