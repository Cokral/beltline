package coordinator

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// autopilotSlots is how many decision sessions run at once.
const autopilotSlots = 2

// autopilotTimeout is how long a decision session may run before it is given up on.
const autopilotTimeout = 20 * time.Minute

// autopilotTurnedOffReason is why a decision still in progress was dropped when effective AFK went off.
const autopilotTurnedOffReason = "AFK was turned off for this run"

// autopilotGlobalOffDetail is the log detail when the global switch took AFK away.
const autopilotGlobalOffDetail = "global AFK mode was turned off (global-off)"

// Autopilot is AFK mode. A run that waits for you gets a short agent session when
// AFK is on for that run: its own choice, or the global switch when it has none.
// The session decides within what Settings allows, and the decision is carried
// out through the same events your own decisions use. Every decision is logged
// and saved, for you to look over when you are back. A part of Coordinator whose
// methods run with c.mu held; each await is a c.Suspend.
type Autopilot struct {
	// c is the coordinator the autopilot belongs to (the back pointer).
	c *Coordinator
	// decisions is the decision log, oldest first.
	// Every write to it bumps decisionsRevision (see coordinator_autopilot_log.go),
	// and the AFK projection is memoised at that revision.
	decisions         []core.AfkDecision
	decisionsRevision uint64
	decisionsMemo     projectionMemo[uint64]
	book              *RunBook
	runner            runner.AgentRunner
	storage           store.Storage
	clock             Clock
	// currentAdmission reports the allowed models now; a decision runs only on one of them.
	currentAdmission func() core.ModelAdmission
	// settings returns the AFK settings now.
	settings func() core.AfkSettings
	// notify delivers a notification.
	notify func(title, body string)
	// output returns a run's latest agent output, or that of one attempt.
	output func(run core.Run, attempt *core.Attempt) *string
	// checking is set while a check pass runs.
	checking bool
}

// newAutopilot builds the autopilot.
// Lock: runs during New, before the coordinator is shared.
func newAutopilot(c *Coordinator, book *RunBook, runner runner.AgentRunner, storage store.Storage, clock Clock) Autopilot {
	return Autopilot{c: c, book: book, runner: runner, storage: storage, clock: clock, decisions: storage.LoadAfkDecisions()}
}

// Deciding returns the decisions still in progress.
// Lock: part of Coordinator; runs with c.mu held.
func (a *Autopilot) Deciding() []core.AfkDecision {
	var out []core.AfkDecision
	for _, decision := range a.decisions {
		if decision.Status == core.AfkDecisionStatusDeciding {
			out = append(out, decision)
		}
	}
	return out
}

// Unreviewed returns the decisions still to be checked.
// Lock: part of Coordinator; runs with c.mu held.
func (a *Autopilot) Unreviewed() []core.AfkDecision {
	var out []core.AfkDecision
	for _, decision := range a.decisions {
		if decision.NeedsReview() {
			out = append(out, decision)
		}
	}
	return out
}

// DecisionsFor returns the decisions of one run, in stored order.
// Lock: part of Coordinator; runs with c.mu held.
func (a *Autopilot) DecisionsFor(runID string) []core.AfkDecision {
	var out []core.AfkDecision
	for _, decision := range a.decisions {
		if decision.RunID == runID {
			out = append(out, decision)
		}
	}
	return out
}

// Decision returns the decision with id, or nil.
// Lock: part of Coordinator; runs with c.mu held.
func (a *Autopilot) Decision(id *string) *core.AfkDecision {
	if id == nil {
		return nil
	}
	for i := range a.decisions {
		if a.decisions[i].ID == *id {
			copied := a.decisions[i]
			return &copied
		}
	}
	return nil
}

// offReason says which switch took a run's AFK away: its own or the global one.
// Lock: runs with c.mu held.
func (a *Autopilot) offReason(run core.Run) string {
	if run.AfkEnabled != nil && !*run.AfkEnabled {
		return autopilotTurnedOffReason
	}
	return autopilotGlobalOffDetail
}

// check is one pass: finish the sessions that decided, then start one for each
// run that waits for you and has AFK on.
// Lock: runs with c.mu held. Suspends: follow, startNew.
func (a *Autopilot) check(ctx context.Context) {
	if a.checking {
		return
	}
	a.checking = true
	defer func() { a.checking = false }()
	a.follow(ctx)
	a.startNew(ctx)
}

// allows reports whether the assistant may decide for this run right now.
// Lock: runs with c.mu held.
func (a *Autopilot) allows(run core.Run) bool { return run.AfkEffective(a.settings().Enabled) }

// supersedeParked drops decisions still in progress because the run was parked.
// Lock: runs with c.mu held. Spawns: the decision session stop, through c.Spawn.
func (a *Autopilot) supersedeParked(runID string) {
	for _, decision := range a.Deciding() {
		if decision.RunID != runID {
			continue
		}
		session := decision.Session
		a.change(decision.ID, func(d *core.AfkDecision) {
			d.Status = core.AfkDecisionStatusSuperseded
			d.Detail = new("the run was parked")
			d.DecidedAt = a.nowDate()
		})
		if session != nil {
			name := *session
			a.c.Spawn(func(ctx context.Context) { a.runner.Stop(ctx, name) })
		}
	}
}

// cancelTurnedOff stops decisions still in progress for runs whose AFK is now off.
// Lock: runs with c.mu held. Suspends: give.
func (a *Autopilot) cancelTurnedOff(ctx context.Context) {
	for _, decision := range a.Deciding() {
		run := a.book.Run(decision.RunID)
		if run == nil || a.allows(*run) {
			continue
		}
		a.give(ctx, decision, core.AfkDecisionStatusSuperseded, a.offReason(*run))
	}
}

// follow finishes the decision sessions that have ended, and gives up on the
// ones that are unknown or ran too long.
// Lock: runs with c.mu held. Suspends: stillDeciding, runner.status, give, take.
func (a *Autopilot) follow(ctx context.Context) {
	for _, decision := range a.Deciding() {
		if watchesHas(a.book.busy, decision.RunID) || a.book.isErasing(decision.RunID) || watchesHas(a.book.restarting, decision.RunID) ||
			a.runRestartIntent(decision.RunID) {
			continue
		}
		run := a.book.Run(decision.RunID)
		if run == nil {
			a.give(ctx, decision, core.AfkDecisionStatusSuperseded, "the run is gone")
			continue
		}
		if !a.stillDeciding(ctx, decision.ID, run.ID, decision.Key) {
			continue
		}
		spec := a.spec(decision, *run)
		if spec == nil {
			a.give(ctx, decision, core.AfkDecisionStatusFailed, "its session is unknown")
			continue
		}
		generation := run.Generation
		operation := run.Operation
		var status runner.SessionStatus
		a.c.Suspend(func() { status = a.runner.Status(ctx, *spec) })
		if !a.stillDeciding(ctx, decision.ID, decision.RunID, decision.Key) || !a.book.accepts(run.ID, generation, &operation) {
			continue
		}
		if status.Kind == runner.SessionStatusRunning {
			if a.clock.Now().Sub(decision.StartedAt.Time) > autopilotTimeout {
				a.give(ctx, decision, core.AfkDecisionStatusFailed, fmt.Sprintf("it took longer than %d minutes", int(autopilotTimeout/time.Minute)))
			}
			continue
		}
		a.take(ctx, decision, *spec, generation, operation)
	}
}

// runRestartIntent reports whether the run's restart is at its intent phase.
// Lock: none (pure).
func (a *Autopilot) runRestartIntent(runID string) bool {
	run := a.book.Run(runID)
	return run != nil && run.Restart != nil && run.Restart.Phase == core.RestartIntentPhaseIntent
}

// blocks reports whether a decision for this stop blocks another try.
// A superseded decision does not block; deciding, applied, left-for-you and failed ones do.
// Lock: runs with c.mu held.
func (a *Autopilot) blocks(key string) bool {
	return slices.ContainsFunc(a.decisions, func(decision core.AfkDecision) bool {
		return decision.Key == key && decision.Status != core.AfkDecisionStatusSuperseded
	})
}

// startNew starts a decision for each eligible stop.
// Lock: runs with c.mu held. Suspends: startEligible.
func (a *Autopilot) startNew(ctx context.Context) {
	// A choice that lands while a start is in flight, including off then
	// straight back on, is taken on the next pass of this call.
	for range 4 {
		before := len(a.decisions)
		a.startEligible(ctx)
		if len(a.decisions) == before {
			return
		}
	}
}

// startEligible starts a decision for each run that waits for you and has AFK on.
// Lock: runs with c.mu held. Suspends: start.
func (a *Autopilot) startEligible(ctx context.Context) {
	needsYou := func(run core.Run) bool { return run.State.NeedsYou() }
	for _, run := range runBookCopiesWhere(a.book, needsYou) {
		if !a.book.may(run, workDecide) {
			continue
		}
		settings := a.settings()
		if !a.allows(run) {
			continue
		}
		if len(a.Deciding()) >= autopilotSlots {
			return
		}
		situation := core.AfkSituationOf(run, settings)
		if situation == nil || a.blocks(situation.Key) {
			continue
		}
		used := 0
		for _, decision := range a.decisions {
			if decision.RunID == run.ID && decision.StageID == run.StageID && decision.Status == core.AfkDecisionStatusApplied {
				used++
			}
		}
		limit := max(settings.Limit, 1)
		if used >= limit {
			record := core.NewAfkDecision(core.AfkDecisionArgs{ID: a.newID(), Run: run, Situation: *situation, StartedAt: jsonx.Date{Time: a.clock.Now()}, Folder: ""})
			record.Status = core.AfkDecisionStatusLeftForYou
			decidedAt := record.StartedAt
			record.DecidedAt = &decidedAt
			times := "times"
			if used == 1 {
				times = "time"
			}
			detail := fmt.Sprintf("it already decided %d %s at %s on this run", used, times, record.StageName)
			record.Detail = &detail
			a.add(record)
			runID := run.ID
			a.book.note(core.AfkName+" left "+run.Subject.Ref()+" for you: "+detail, &runID)
			continue
		}
		a.start(ctx, run, *situation, settings)
	}
}

// start records and launches one decision session for a run's stop.
// Lock: runs with c.mu held. Suspends: runner.prepareSessionId, runner.start,
// stillDeciding, runner.stop.
func (a *Autopilot) start(ctx context.Context, run core.Run, situation core.AfkSituation, settings core.AfkSettings) {
	generation := run.Generation
	operation := run.Operation
	if !a.book.accepts(run.ID, generation, &operation) {
		return
	}
	a.book.busy[run.ID] = struct{}{}
	defer delete(a.book.busy, run.ID)
	id := a.newID()
	number := 1
	for _, decision := range a.decisions {
		if decision.RunID == run.ID {
			number++
		}
	}
	folderName := fmt.Sprintf("afk/%02d-%s", number, run.StageID)
	runFolder := a.storage.Paths.RunFolder(run)
	folder := filepath.Join(runFolder, folderName)
	record := core.NewAfkDecision(core.AfkDecisionArgs{ID: id, Run: run, Situation: situation, StartedAt: jsonx.Date{Time: a.clock.Now()}, Folder: folderName})
	backend := settings.Backend
	record.Backend = &backend
	// The exact identifier the runner takes, such as Cursor's variant for the level.
	admission := a.currentAdmission()
	model := admission.RunnerModel(settings.Model, settings.Thinking, settings.Backend)
	record.Model = &model
	record.Thinking = settings.Thinking
	if warning := core.ModelPolicyWarning(settings.Model, settings.Backend, core.AfkName, admission, nil); warning != nil {
		record.Status = core.AfkDecisionStatusFailed
		record.DecidedAt = &record.StartedAt
		detail := "could not start: " + *warning
		record.Detail = &detail
		a.add(record)
		return
	}
	err := a.startSession(ctx, &record, run, situation, settings, folder, runFolder, id, generation, operation)
	if err == nil {
		return
	}
	if !a.book.accepts(run.ID, generation, &operation) {
		return
	}
	if current := a.decision(id); current != nil && current.Status == core.AfkDecisionStatusSuperseded {
		return
	}
	if a.decision(id) == nil {
		a.add(record)
	}
	if current := a.decision(id); current == nil || current.Status != core.AfkDecisionStatusDeciding {
		return
	}
	detail := "could not start: " + err.Error()
	a.change(id, func(d *core.AfkDecision) {
		d.Status = core.AfkDecisionStatusFailed
		d.DecidedAt = a.nowDate()
		d.Detail = &detail
	})
}
