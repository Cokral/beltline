// Starting a decision's session, rebuilding its spec after a restart, and ending it.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// startSession writes the decision's folder and brief, asks for a session id, and starts the session.
// A nil error with the decision not logged means a check after a runner call ended the start.
// Lock: runs with c.mu held. Suspends: runner.prepareSessionId, runner.start, stillDeciding.
func (a *Autopilot) startSession(ctx context.Context, record *core.AfkDecision, run core.Run, situation core.AfkSituation,
	settings core.AfkSettings, folder, runFolder, id string, generation, operation int) error {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	var latest *string
	switch situation.Kind {
	case core.AfkSituationKindQuestion, core.AfkSituationKindFailure:
		latest = a.output(run, run.LastAttempt(run.StageID))
	default:
		latest = a.output(run, nil)
	}
	decisionFile := filepath.Join(folder, core.AfkDecisionFile)
	prompt := core.AfkBriefRender(run, situation, core.AfkBriefContext{
		RunFolder: runFolder, Decision: decisionFile, LatestOutput: latest, Guidance: settings.Guidance,
	})
	var sessionID string
	var err error
	proposed := strings.ToLower(a.c.ids.UUID())
	a.c.Suspend(func() { sessionID, err = a.runner.PrepareSessionID(ctx, settings.Backend, proposed, runFolder) })
	if err != nil {
		return err
	}
	record.SessionID = &sessionID
	if !a.openToStart(run.ID, situation.Key) {
		return nil
	}
	session := runner.TmuxSessionName([]string{"afk", run.BeltID, run.Subject.SessionPart(), run.StageID, id[:min(8, len(id))]})
	record.Session = &session
	if !a.book.accepts(run.ID, generation, &operation) {
		return nil
	}
	a.add(*record)
	current := run
	if latest := a.book.Run(run.ID); latest != nil {
		current = *latest
	}
	spec := a.spec(*record, current)
	if spec == nil {
		return nil
	}
	spec.Prompt = prompt
	if !a.book.accepts(run.ID, generation, &operation) {
		return nil
	}
	a.c.Suspend(func() { err = a.runner.Start(ctx, *spec) })
	if err != nil {
		return err
	}
	if !a.stillDeciding(ctx, id, run.ID, situation.Key) {
		if record.Session != nil {
			name := *record.Session
			a.c.Suspend(func() { a.runner.Stop(ctx, name) })
		}
		return nil
	}
	a.book.note(core.AfkName+" is deciding for "+run.Subject.Ref()+" at "+record.StageName, &run.ID)
	return nil
}

// openToStart reports whether a new decision may still be recorded for this stop.
// It is checked again after a runner call, so an off switch during startup is kept.
// Lock: runs with c.mu held.
func (a *Autopilot) openToStart(runID, key string) bool {
	run := a.book.Run(runID)
	if run == nil || !a.allows(*run) {
		return false
	}
	situation := core.AfkSituationOf(*run, a.settings())
	if situation == nil || situation.Key != key {
		return false
	}
	return !a.blocks(key)
}

// spec returns the session of a decision, rebuilt from what was saved, also after a restart.
// Lock: runs with c.mu held.
func (a *Autopilot) spec(decision core.AfkDecision, run core.Run) *runner.SessionSpec {
	if decision.Session == nil || decision.Backend == nil {
		return nil
	}
	runFolder := a.storage.Paths.RunFolder(run)
	folder := filepath.Join(runFolder, decision.Folder)
	// 	// AfkSettings(drop: true): every other allowance keeps its default.
	allowing := core.DefaultAfkSettings()
	allowing.Drop = true
	situation := core.AfkSituationOf(run, allowing)
	sessionID := core.ValueOr(decision.SessionID, *decision.Session)
	model := core.ValueOf(decision.Model)
	var outcomes []string
	if situation != nil {
		for _, option := range situation.Options {
			outcomes = append(outcomes, option.ID)
		}
	}
	agent := "afk"
	return &runner.SessionSpec{
		Name: *decision.Session, SessionID: sessionID, Backend: *decision.Backend, Model: model, Thinking: decision.Thinking,
		Prompt: "", Directory: runFolder, AttemptFolder: folder, Output: filepath.Join(folder, core.AfkDecisionFile),
		Outcomes: outcomes, AgentID: &agent,
	}
}

// discardPaused drops a decision that was still in flight when the run was paused, without applying it.
// Lock: runs with c.mu held. Suspends: give.
func (a *Autopilot) discardPaused(ctx context.Context, runID string) {
	for _, decision := range a.Deciding() {
		if decision.RunID == runID {
			a.give(ctx, decision, core.AfkDecisionStatusSuperseded, "the run was paused")
		}
	}
}

// give stops a decision's session and records its end with status and why.
// Lock: runs with c.mu held. Suspends: runner.stop.
func (a *Autopilot) give(ctx context.Context, decision core.AfkDecision, status core.AfkDecisionStatus, why string) {
	if decision.Session != nil {
		name := *decision.Session
		a.c.Suspend(func() { a.runner.Stop(ctx, name) })
	}
	a.change(decision.ID, func(d *core.AfkDecision) {
		d.Status = status
		reason := why
		d.Detail = &reason
		d.DecidedAt = a.nowDate()
	})
}
