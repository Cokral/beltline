// The launch records of `start(_:leg:)`.

package coordinator

import (
	"context"
	"path/filepath"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// legLaunch is one launch of a leg's agent session: the identity and settings start
// works out before it launches, the run state it was prepared against, and what the
// launch fills in on the way (the session's name once its identity is known, and
// whether the attempt was saved before the runner was asked to start).
type legLaunch struct {
	runID      string
	generation int
	operation  int
	run        core.Run
	leg        core.Leg
	stage      core.Stage
	number     int
	name       string
	sessionID  string
	folderName string
	// held is the attempt an answer continues, or nil for a fresh session.
	held *core.Attempt
	// replay is the attempt this leg replays, or nil.
	replay   *core.Attempt
	settings core.SessionSettings
	backend  core.Backend
	logMark  int
	// prepared is the leg as the run's working legs showed it when the launch prepared.
	prepared *core.Leg
	recorded bool
}

// abandonStart records a launch that failed. A
// launch a move displaced fails for the route it was made for; a session that
// never ran costs nothing, and an answer then starts a fresh one. l.recorded says
// whether the attempt was already saved before the runner was asked to start.
// Lock: runs with c.mu held. Suspends: none (book updates only).
func (s *Sessions) abandonStart(l legLaunch, err error) {
	if !s.book.accepts(l.runID, l.generation, &l.operation) {
		return
	}
	// A preparation that a move displaced fails for the launch it was made for. The
	// route is queued at its new stage now, so nothing is recorded or finished there.
	if !l.recorded {
		current := s.book.Run(l.runID)
		if current == nil || !legInState(*current, l.leg.ID, core.RunStateQueued) || !preparedLegStill(*current, l.leg.ID, l.prepared) {
			return
		}
		s.book.update(l.runID, &l.generation, &l.operation, func(r *core.Run) {
			legID := l.leg.ID
			core.EngineAdvance(r, core.Event{Kind: core.EventKindSessionStarted, Session: l.name, SessionID: l.sessionID, Folder: l.folderName, Leg: &legID}, s.clock.Now())
		})
	}
	// A new session never ran, so an answer starts a fresh one rather
	// than resuming a session or chat that holds nothing.
	if l.held == nil {
		s.book.update(l.runID, &l.generation, &l.operation, func(r *core.Run) {
			if index := len(r.Attempts) - 1; index >= 0 {
				r.Attempts[index].SessionID = nil
			}
		})
	}
	// A session that never started cost nothing; without a usage the
	// run's and today's spend would read as partial.
	attemptID := core.AttemptID(l.stage.ID, l.number, &l.leg.ID)
	legID := l.leg.ID
	s.book.apply(l.runID, core.Event{Kind: core.EventKindSessionFinished, Detail: new("could not start: " + err.Error()), Leg: &legID}, &l.generation, &l.operation, func(r *core.Run) {
		for index := range r.Attempts {
			if r.Attempts[index].ID == attemptID {
				usage := core.NewUsage(core.UsageArgs{CostNanos: new(0)})
				r.Attempts[index].Usage = &usage
				return
			}
		}
	})
}

// prepareLegWorkspaces prepares every repository of the run for a leg's session
// and returns each repository's checkout directory and base branch
// `start(_:leg:)`). branch and folder are the leg's own branch and work folder.
// Lock: runs with c.mu held. Suspends: base, workspaces.prepare.
func (s *Sessions) prepareLegWorkspaces(ctx context.Context, run core.Run, leg core.Leg, branch, folder string, checkout runner.WorkspaceCheckout) (checkouts, bases map[string]string, err error) {
	checkouts = map[string]string{}
	bases = map[string]string{}
	for _, repo := range run.Repos {
		repoBase, err := s.base(ctx, run, repo)
		if err != nil {
			return nil, nil, err
		}
		bases[repo] = repoBase
		workspace := runner.NewWorkspaceSpec(runner.WorkspaceSpecArgs{
			Repo: repo, Base: repoBase, Branch: branch, Checkout: checkout, Folder: folder, Repos: run.Repos,
			From: sessionsBranchBase(run, &leg),
		})
		var prepareErr error
		s.c.Suspend(func() { _, prepareErr = s.workspaces.Prepare(ctx, workspace) })
		if prepareErr != nil {
			return nil, nil, prepareErr
		}
		checkouts[repo] = workspace.CheckoutDirectory()
	}
	return checkouts, bases, nil
}

// legPrompt renders the brief a leg's session starts with: a replay runs its
// attempt's brief again, an answer follows up on the held attempt, and a fresh
// session gets the stage's brief.
// Lock: none (pure).
func legPrompt(run core.Run, stage core.Stage, replay, held *core.Attempt, briefContext core.BriefContext) string {
	switch {
	case replay != nil:
		return core.BriefRerun(run, stage, *replay, briefContext)
	case held != nil:
		return core.BriefFollowUp(run, stage, *held, briefContext)
	default:
		return core.BriefRender(run, stage, briefContext)
	}
}

// claimLegBranch gives a worktree leg its branch. The run's own branch belongs to
// the line, so it is set only when the run has none and the leg is the line's own;
// each route's work always goes on the leg's own branch.
// Lock: none (pure).
func claimLegBranch(r *core.Run, leg core.Leg, branch string) {
	at := legIndexOf(*r, leg.ID)
	if at < 0 {
		return
	}
	if core.LegFromID(&leg.ID) == nil && r.Branch == nil {
		owned := branch
		r.Branch = &owned
	}
	owned := branch
	r.Legs[at].Branch = &owned
}

// stampLaunch writes what a launch learned onto the run's newest attempt: its
// settings, log and usage marks, the interactive launch with its completion token,
// and a read-only checkout's baseline.
// Lock: none (pure).
func stampLaunch(r *core.Run, settings core.SessionSettings, logMark int, spec *runner.SessionSpec, token string, baseline *string) {
	index := len(r.Attempts) - 1
	if index < 0 {
		return
	}
	attempt := &r.Attempts[index]
	stored := settings
	attempt.Settings = &stored
	if logMark > 0 {
		mark := logMark
		attempt.LogMark = &mark
	}
	if spec.UsageMark > 0 {
		usageMark := spec.UsageMark
		attempt.UsageMark = &usageMark
	}
	if spec.UsageCheckpoint != nil {
		checkpoint := *spec.UsageCheckpoint
		attempt.UsageCheckpoint = &checkpoint
	}
	mode, launched := core.AttemptLaunchInteractive, token
	attempt.LaunchMode, attempt.CompletionToken = &mode, &launched
	if baseline != nil {
		stored := *baseline
		attempt.ReadOnlyBaseline = &stored
	}
}

// launchLeg prepares a leg's checkout, session and spec, saves the attempt, and starts
// the runner (the launch of start(_:leg:)). It returns the error a failed
// preparation or start reports; abandonStart then records it.
// Lock: runs with c.mu held. Suspends: base, workspaces.prepare, runner.prepareSessionId,
// runner.endContinuing, runner.usageMark, runner.usageCheckpoint, workspaces.fingerprint,
// runner.start, runner.stop. Spawns: none.
func (s *Sessions) launchLeg(ctx context.Context, l *legLaunch) error {
	base, err := s.base(ctx, l.run, l.run.Subject.Repo)
	if err != nil {
		return err
	}
	// Each leg works on its own branch, off the run's base.
	branch := sessionsBranch(l.run, &l.leg)
	folder := s.paths.WorkFolderLeg(l.run, &l.leg.ID)
	checkout := sessionsCheckout(l.run, l.stage)
	directory := runner.WorkspaceCheckoutPath(s.paths, l.run, checkout, &l.leg.ID)
	checkouts, bases, err := s.prepareLegWorkspaces(ctx, l.run, l.leg, branch, folder, checkout)
	if err != nil {
		return err
	}
	attemptFolder := filepath.Join(s.paths.RunFolder(l.run), l.folderName)
	briefContext := core.BriefContext{
		RunFolder: s.paths.RunFolder(l.run), AttemptFolder: attemptFolder, Base: base, Branch: branch,
		Agents: s.library.AgentsOf(l.run), Checkouts: checkouts, Bases: bases,
	}
	if l.held == nil {
		if err := s.prepareSession(ctx, l, directory); err != nil {
			return err
		}
	} else {
		s.endContinuing(ctx, l)
		l.logMark = runner.AdapterFor(l.backend).Mark(directory, l.sessionID)
	}
	spec, token := s.sessionSpec(ctx, l, briefContext, attemptFolder, directory)
	var baseline *string
	if l.stage.Workspace.IsReadOnly() {
		var fingerprint string
		s.c.Suspend(func() { fingerprint = s.workspaces.Fingerprint(ctx, directory) })
		baseline = &fingerprint
		s.fingerprints[core.AttemptID(l.stage.ID, l.number, &l.leg.ID)+l.run.ID] = fingerprint
	}
	current := s.book.Run(l.runID)
	if current == nil || current.Parking != nil || (current.Pause != nil && current.Pause.BlocksProgress()) ||
		current.Generation != l.generation || current.Operation != l.operation || !legInState(*current, l.leg.ID, core.RunStateQueued) {
		return nil
	}
	// Send to can move a queued route while it prepares, and the operation does not
	// change then: the launch goes on only with the leg it was prepared for. The next tick starts it again.
	if !preparedLegStill(*current, l.leg.ID, l.prepared) {
		return nil
	}
	if !s.recordLaunch(l, branch, &spec, token, baseline) {
		return core.NewFailure("Could not persist the attempt before launch")
	}
	l.recorded = true
	s.c.Suspend(func() { err = s.runner.Start(ctx, spec) })
	if err != nil {
		return err
	}
	current = s.book.Run(l.runID)
	if current == nil || current.Parking != nil || current.Generation != l.generation || current.Operation != l.operation ||
		!legInState(*current, l.leg.ID, core.RunStateRunning) {
		s.c.Suspend(func() { s.runner.Stop(ctx, l.name) })
	}
	return nil
}

// prepareSession takes the runner's session identity for a fresh session, and the
// name it is known by once that identity is set.
// Lock: runs with c.mu held. Suspends: runner.prepareSessionId.
func (s *Sessions) prepareSession(ctx context.Context, l *legLaunch, directory string) error {
	var prepared string
	var err error
	s.c.Suspend(func() { prepared, err = s.runner.PrepareSessionID(ctx, l.backend, l.sessionID, directory) })
	if err != nil {
		return err
	}
	l.sessionID = prepared
	l.name = sessionName(l.run, l.stage, l.leg.ID, l.number, l.sessionID)
	return nil
}

// endContinuing ends the held attempt's session before an answer continues it in
// this launch: two agents must never write one session log.
// Lock: runs with c.mu held. Suspends: runner.endContinuing.
func (s *Sessions) endContinuing(ctx context.Context, l *legLaunch) {
	if previous := s.spec(l.run, *l.held, ""); previous != nil {
		previous := *previous
		s.c.Suspend(func() { s.runner.EndContinuing(ctx, previous) })
	}
}

// sessionSpec is the runner's spec for the launch: the leg's prompt, ending on the
// handoff that names the completion token on runners that finish on it, with its
// model, agent and usage marks. It also returns that token.
// Lock: runs with c.mu held. Suspends: runner.usageMark, runner.usageCheckpoint.
func (s *Sessions) sessionSpec(ctx context.Context, l *legLaunch, briefContext core.BriefContext, attemptFolder, directory string) (runner.SessionSpec, string) {
	prompt := legPrompt(l.run, l.stage, l.replay, l.held, briefContext)
	// Every new stage session runs in its runner's own interface and ends on
	// this launch's handoff, or on Copilot's own task completion after its checkpoint.
	token := core.AttemptCompletionToken(l.sessionID, nil)
	if l.backend.FinishesOnHandoff() {
		prompt += core.BriefHandoff(attemptFolder, token)
	}
	model := l.settings.Model
	if l.run.ModelAdmission != nil {
		model = l.run.ModelAdmission.RunnerModel(l.settings.Model, l.settings.Thinking, l.backend)
	}
	var agentID *string
	if l.stage.Agent != nil {
		id := l.stage.Agent.ID
		agentID = &id
	}
	spec := runner.NewSessionSpec(runner.SessionSpecArgs{
		Name: l.name, SessionID: l.sessionID, Backend: l.backend, Model: model, Thinking: l.settings.Thinking, Prompt: prompt,
		Directory: directory, AttemptFolder: attemptFolder, Output: core.BriefOutputPath(l.stage, briefContext),
		Outcomes: stageOutcomeNames(l.stage), AgentID: agentID, Resume: l.held != nil, AgentName: heldAgentName(l.held),
		LogMark: l.logMark, UsageCheckpoint: heldUsageCheckpoint(l.held), Interactive: true, CompletionToken: token,
	})
	s.c.Suspend(func() { spec.UsageMark = s.runner.UsageMark(ctx, spec) })
	s.c.Suspend(func() { spec.UsageCheckpoint = s.runner.UsageCheckpoint(ctx, spec) })
	return spec, token
}

// recordLaunch saves the attempt before the runner starts it, so a restart still finds
// it. It reports false when the run could not be saved.
// Lock: runs with c.mu held. Suspends: none (book updates only).
func (s *Sessions) recordLaunch(l *legLaunch, branch string, spec *runner.SessionSpec, token string, baseline *string) bool {
	return s.book.update(l.runID, &l.generation, &l.operation, func(r *core.Run) {
		if at := legIndexOf(*r, l.leg.ID); at >= 0 {
			r.Legs[at].State = core.RunStateQueued
		}
		if l.stage.Workspace == core.WorkspaceWorktree {
			claimLegBranch(r, l.leg, branch)
		}
		legID := l.leg.ID
		core.EngineAdvance(r, core.Event{Kind: core.EventKindSessionStarted, Session: l.name, SessionID: l.sessionID, Folder: l.folderName, Leg: &legID}, s.clock.Now())
		stampLaunch(r, l.settings, l.logMark, spec, token, baseline)
	})
}
