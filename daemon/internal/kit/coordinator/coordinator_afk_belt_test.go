// Coordinator tests of a belt's AFK mode: the mode of its new runs, a switch or a run's choice
// turned off while a decision is in flight, the gates on the assistant's start and status,
// restarts, the decision log's limit across cancellations, and the factory import.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// coordDurations is the AFK durations: how long simulated sessions run. AFK status checks from
// holdAfter on stay running for an hour; zero never holds.
type coordDurations struct {
	mu        sync.Mutex
	afk       float64
	stage     float64
	holdAfter int
	afkChecks int
}

// duration is the length of spec's session, counting AFK status checks.
func (d *coordDurations) duration(spec runner.SessionSpec) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if spec.AgentID == nil || *spec.AgentID != "afk" {
		return d.stage
	}
	d.afkChecks++
	if d.holdAfter > 0 && d.afkChecks >= d.holdAfter {
		return 60 * 60
	}
	return d.afk
}

// update changes the durations under the lock, as a test does between its steps.
func (d *coordDurations) update(change func(d *coordDurations)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	change(d)
}

// durationRunner is a simulated runner whose session lengths come from d and whose outcomes come from script.
func durationRunner(script *harnessScript, d *coordDurations) runner.AgentRunner {
	return runner.NewSimulatedRunner(d.duration, func() time.Time { return testkit.ContractTime }, script.next)
}

// beltHolding is the holding runner: AFK calls wait at named gates (afk-start,
// afk-status, afk-finish) the test suspends and releases. Stage sessions never finish.
type beltHolding struct {
	*runner.SimulatedRunner
	mu          sync.Mutex
	holding     map[string]bool
	waiters     map[string]int
	gates       map[string]chan struct{}
	started     map[string]bool
	stopped     []string
	afkFinished bool
	afkChoice   string
}

// newH5m4BeltHolding builds the holding runner over a simulated runner (for the calls it does not hold).
func newH5m4BeltHolding(script *harnessScript) *beltHolding {
	return &beltHolding{
		SimulatedRunner: runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 },
			func() time.Time { return testkit.ContractTime }, script.next),
		holding:   map[string]bool{},
		waiters:   map[string]int{},
		gates:     map[string]chan struct{}{},
		started:   map[string]bool{},
		afkChoice: "decide approved",
	}
}

// gateChan returns the channel gate's waiters block on, creating it. Lock: held by the caller.
func (r *beltHolding) gateChan(gate string) chan struct{} {
	if r.gates[gate] == nil {
		r.gates[gate] = make(chan struct{})
	}
	return r.gates[gate]
}

// suspend holds gate: the next call that reaches it waits until release.
func (r *beltHolding) suspend(gate string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.holding[gate] = true
	r.gateChan(gate)
}

// release lets every call waiting at gate go on, and stops holding it.
func (r *beltHolding) release(gate string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.holding, gate)
	if ch := r.gates[gate]; ch != nil {
		close(ch)
		delete(r.gates, gate)
	}
	r.waiters[gate] = 0
}

// isWaiting reports whether a call is waiting at gate.
func (r *beltHolding) isWaiting(gate string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.waiters[gate] > 0
}

// wait blocks a call at gate while the test holds it.
func (r *beltHolding) wait(gate string) {
	r.mu.Lock()
	if !r.holding[gate] {
		r.mu.Unlock()
		return
	}
	ch := r.gateChan(gate)
	r.waiters[gate]++
	r.mu.Unlock()
	<-ch
}

// setFinished makes the AFK sessions report their decision and exit from now on.
func (r *beltHolding) setFinished() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.afkFinished = true
}

// startedEmpty reports whether no session is registered as started.
func (r *beltHolding) startedEmpty() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started) == 0
}

// stoppedSessions returns the sessions stopped so far.
func (r *beltHolding) stoppedSessions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.stopped...)
}

// isAFK reports whether spec is an assistant session.
func isAFK(spec runner.SessionSpec) bool { return spec.AgentID != nil && *spec.AgentID == "afk" }

// Start registers the session and writes its brief; an AFK session waits at afk-start.
func (r *beltHolding) Start(_ context.Context, spec runner.SessionSpec) error {
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(spec.AttemptFolder, "brief.md"), []byte(spec.Prompt), 0o644); err != nil {
		return err
	}
	r.mu.Lock()
	r.started[spec.Name] = true
	r.mu.Unlock()
	if isAFK(spec) {
		r.wait("afk-start")
	}
	return nil
}

// Status waits at afk-status for an AFK session; once finished it writes the decision the
// test chose, and a stage session always reports running.
func (r *beltHolding) Status(_ context.Context, spec runner.SessionSpec) runner.SessionStatus {
	r.mu.Lock()
	known := r.started[spec.Name]
	r.mu.Unlock()
	if !known {
		return runner.SessionStatus{Kind: runner.SessionStatusGone}
	}
	if !isAFK(spec) {
		return runner.SessionStatus{Kind: runner.SessionStatusRunning}
	}
	r.wait("afk-status")
	r.mu.Lock()
	finished, choice := r.afkFinished, r.afkChoice
	r.mu.Unlock()
	if finished {
		if _, err := os.Stat(spec.Output); err != nil {
			_ = os.WriteFile(spec.Output, []byte(runner.SimulatedAFKDecision(choice)), 0o644)
		}
		return coordFinished()
	}
	return runner.SessionStatus{Kind: runner.SessionStatusRunning}
}

// Finish waits at afk-finish and forgets the session.
func (r *beltHolding) Finish(_ context.Context, spec runner.SessionSpec) {
	r.wait("afk-finish")
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.started, spec.Name)
}

// Stop records the session as stopped and forgets it.
func (r *beltHolding) Stop(_ context.Context, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = append(r.stopped, session)
	delete(r.started, session)
}

// untils reports whether done holds, with the lock taken.
func untils(c *Coordinator, done func(c *Coordinator) bool) bool {
	c.Lock()
	defer c.EndTurn()
	return done(c)
}

// coordCancelAndReenable is cancelAndReenable: a decision is started, its ready output
// is written, the turn-off happens, and the decision is superseded with the reason.
func coordCancelAndReenable(t *testing.T, h *harness, before core.Run, reason string, turnOff func()) {
	t.Helper()
	h.coord.SetAfk(bgCtx, true)
	h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
	decision := decidingFor(coordDecisions(h), before.ID)
	if decision == nil {
		t.Fatal("no decision in progress")
	}
	writeAfkDecision(t, "decide sent back", before, *decision, h.coord.Paths())
	turnOff()
	h.Until("decisions := decisionsNow(h) ...", func() bool {
		decisions := decisionsNow(h)
		return len(decisions) > 0 && decisions[0].Status == core.AfkDecisionStatusSuperseded
	})
	first := coordDecisions(h)[0]
	if first.Detail == nil || !strings.Contains(*first.Detail, reason) {
		t.Fatalf("detail = %v, want %q", first.Detail, reason)
	}
	if run := h.coord.Run(before.ID); run == nil || run.State != core.RunStateWaiting || run.StageID != before.StageID {
		t.Fatalf("run = %+v, want waiting at %s", run, before.StageID)
	}
}

// TestABeltsModeIsForItsNewRunsAndLeavesRunsGoingAlone
func TestABeltsModeIsForItsNewRunsAndLeavesRunsGoingAlone(t *testing.T) {
	t.Parallel()
	d := &coordDurations{}
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", false)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	before := runOn(h, "issues")
	if before == nil || before.Afk() != core.AfkModeNever {
		t.Fatalf("run = %+v, want a never run", before)
	}
	updated := h.coord.Belt("issues").Belt
	updated.Afk = core.AfkModeAlways
	saved, err := h.coord.SaveBelt(updated)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Version != before.BeltVersion+1 {
		t.Fatalf("saved version = %d, want %d", saved.Version, before.BeltVersion+1)
	}
	existing := h.coord.Run(before.ID)
	if existing == nil || existing.Afk() != core.AfkModeNever {
		t.Fatalf("existing run = %+v, want never", existing)
	}
	if !sameStages(existing.Stages, before.Stages) || !sameRunAgents(existing.Agents, before.Agents) {
		t.Fatal("saving the belt changed the run's stages or agents")
	}
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	h.Script.add("afk", "decide approved")
	created, err := h.coord.StartTask("issues", "acme/billing", "New", "Do it", nil)
	if err != nil {
		t.Fatalf("start task: %v", err)
	}
	if created.Afk() != core.AfkModeAlways {
		t.Fatalf("new run mode = %s, want always", created.Afk())
	}
	h.Until("appliedCount(decisionsNow(h)) > 0", func() bool { return appliedCount(decisionsNow(h)) > 0 })
	if globalAfk(h.coord) {
		t.Fatal("the belt's mode turned the global switch on")
	}
	decisions := coordDecisions(h)
	if len(decisions) != 1 || decisions[0].RunID != created.ID {
		t.Fatalf("decisions = %+v, want one for %s", decisions, created.ID)
	}
	if run := h.coord.Run(before.ID); run == nil || run.State != core.RunStateWaiting {
		t.Fatalf("old run = %+v, want waiting", run)
	}
}

// sameRunAgents reports whether two agent snapshot lists are equal.
func sameRunAgents(a, b []core.AgentSnapshot) bool {
	x, errA := jsonx.Marshal(a)
	y, errB := jsonx.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

// TestABeltsModeAndARunsOwnSurviveARestart
func TestABeltsModeAndARunsOwnSurviveARestart(t *testing.T) {
	t.Parallel()
	d := &coordDurations{}
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", false)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	before := runOn(h, "issues")
	updated := h.coord.Belt("issues").Belt
	updated.Afk = core.AfkModeAlways
	if _, err := h.coord.SaveBelt(updated); err != nil {
		t.Fatalf("save: %v", err)
	}
	restarted := h.RestartWith(durationRunner(h.Script, d))
	if record := restarted.Belt("issues"); record == nil || record.Belt.Afk != core.AfkModeAlways {
		t.Fatalf("restarted belt = %+v, want always", record)
	}
	if run := restarted.Run(before.ID); run == nil || run.Afk() != core.AfkModeNever {
		t.Fatalf("restarted run = %+v, want never", run)
	}
	restarted.SetAfk(bgCtx, true)
	for range 3 {
		restarted.Tick(bgCtx)
		restarted.Idle()
	}
	if n := len(restarted.ReadAfkDecisions()); n != 0 {
		t.Fatalf("decisions after the restart = %d, want 0", n)
	}
}

// TestAFailedBeltSaveKeepsItsMode
func TestAFailedBeltSaveKeepsItsMode(t *testing.T) {
	t.Parallel()
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", false)}, harnessOptions{})
	folder := filepath.Join(h.coord.Paths().Belts(), "issues")
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(folder, 0o755) }()
	if _, err := h.coord.SetBeltAfk("issues", core.AfkModeAlways); err == nil {
		t.Fatal("a save into a read-only belt folder succeeded")
	}
	if got := h.coord.Belt("issues"); got == nil || got.Belt.Afk != core.AfkModeNever {
		t.Fatalf("belt after the failed save = %+v, want never", got)
	}
	if err := os.Chmod(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	saved, err := h.coord.SetBeltAfk("issues", core.AfkModeAlways)
	if err != nil || saved == nil || saved.Afk != core.AfkModeAlways {
		t.Fatalf("saved = %+v, %v; want always", saved, err)
	}
	again, err := h.coord.SetBeltAfk("issues", core.AfkModeAlways)
	if err != nil || again != nil {
		t.Fatalf("unchanged save = %+v, %v; want nothing", again, err)
	}
}

// TestTurningASwitchOffDropsARunningSessionAndReadyOutput
func TestTurningASwitchOffDropsARunningSessionAndReadyOutput(t *testing.T) {
	t.Parallel()
	d := &coordDurations{holdAfter: 1}
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	before := runOn(h, "issues")
	h.coord.SetAfk(bgCtx, true)
	h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
	decision := coordDeciding(coordDecisions(h))[0]
	if decision.Session == nil {
		t.Fatal("the decision has no session")
	}
	session := *decision.Session
	sim := h.coord.runner.(*runner.SimulatedRunner)
	if !sim.IsRunning(session) {
		t.Fatal("the assistant's session is not running")
	}
	writeAfkDecision(t, "decide approved", *before, decision, h.coord.Paths())
	h.coord.SetAfk(bgCtx, false)
	h.Until("decisions := decisionsNow(h) ...", func() bool {
		decisions := decisionsNow(h)
		return len(decisions) > 0 && decisions[0].Status == core.AfkDecisionStatusSuperseded
	})
	first := coordDecisions(h)[0]
	if first.Detail == nil || !strings.Contains(*first.Detail, "global-off") {
		t.Fatalf("detail = %v, want the global-off detail", first.Detail)
	}
	if sim.IsRunning(session) {
		t.Fatal("the assistant's session still runs")
	}
	run := h.coord.Run(before.ID)
	if run == nil || run.State != core.RunStateWaiting || run.StageID != before.StageID || len(run.Attempts) != len(before.Attempts) {
		t.Fatalf("run = %+v, want waiting, unchanged", run)
	}
	saved := h.storage.LoadAfkDecisions()
	if len(saved) == 0 || saved[0].Status != core.AfkDecisionStatusSuperseded || saved[0].Detail == nil || !strings.Contains(*saved[0].Detail, "global-off") {
		t.Fatalf("saved = %+v, want the superseded global-off decision", saved)
	}
}

// TestASwitchFlipDuringStatusIsRecheckedBeforeTheOutputIsApplied
func TestASwitchFlipDuringStatusIsRecheckedBeforeTheOutputIsApplied(t *testing.T) {
	t.Parallel()
	var holding *beltHolding
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner {
			holding = newH5m4BeltHolding(s)
			return holding
		},
	})
	before := runOn(h, "issues")
	holding.suspend("afk-status")
	holding.setFinished()
	h.coord.SetAfk(bgCtx, true)
	// The decision is recorded before its start returns; the first pass does not reach the status call.
	h.coord.Idle()
	coordWaits(t, "one decision in progress", func() bool {
		return untils(h.coord, func(c *Coordinator) bool { return len(c.autopilot.Deciding()) == 1 && !c.ticking })
	})
	tick := make(chan struct{})
	go func() { defer close(tick); h.coord.Tick(bgCtx) }()
	coordWaits(t, "the status call waits", func() bool { return holding.isWaiting("afk-status") })
	session := coordDecisions(h)[0].Session
	if session == nil {
		t.Fatal("the decision has no session")
	}
	h.coord.SetAfk(bgCtx, false)
	holding.release("afk-status")
	<-tick
	h.coord.Idle()
	decisions := coordDecisions(h)
	if decisions[0].Detail == nil || !strings.Contains(*decisions[0].Detail, "global-off") {
		t.Fatalf("detail = %v, want the global-off detail", decisions[0].Detail)
	}
	if run := h.coord.Run(before.ID); run == nil || run.State != core.RunStateWaiting || run.StageID != before.StageID {
		t.Fatalf("run = %+v, want waiting at %s", run, before.StageID)
	}
	if !slices.Contains(holding.stoppedSessions(), *session) {
		t.Fatalf("stopped = %v, want %s", holding.stoppedSessions(), *session)
	}
	if saved := h.storage.LoadAfkDecisions(); len(saved) == 0 || saved[0].Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("saved = %+v, want superseded", saved)
	}
}

// TestASwitchFlipDuringStartStopsTheSessionBeforeItCanDecide
func TestASwitchFlipDuringStartStopsTheSessionBeforeItCanDecide(t *testing.T) {
	t.Parallel()
	var holding *beltHolding
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner {
			holding = newH5m4BeltHolding(s)
			return holding
		},
	})
	before := runOn(h, "issues")
	holding.suspend("afk-start")
	h.coord.SetAfk(bgCtx, true)
	coordWaits(t, "the start call waits", func() bool { return holding.isWaiting("afk-start") })
	h.coord.SetRunAfk(bgCtx, before.ID, core.AfkModeNever)
	holding.release("afk-start")
	h.coord.Idle()
	h.Until("decisions := decisionsNow(h) ...", func() bool {
		decisions := decisionsNow(h)
		return len(decisions) > 0 && decisions[0].Status == core.AfkDecisionStatusSuperseded
	})
	decision := coordDecisions(h)[0]
	if decision.Detail == nil || *decision.Detail != autopilotTurnedOffReason {
		t.Fatalf("detail = %v, want the turned-off reason", decision.Detail)
	}
	if decision.Session == nil || !slices.Contains(holding.stoppedSessions(), *decision.Session) {
		t.Fatalf("stopped = %v, want the decision's session", holding.stoppedSessions())
	}
	if !holding.startedEmpty() {
		t.Fatal("a session stayed started")
	}
	if run := h.coord.Run(before.ID); run == nil || run.State != core.RunStateWaiting || run.StageID != before.StageID {
		t.Fatalf("run = %+v, want waiting at %s", run, before.StageID)
	}
}

// TestAnAppliedDecisionAndAStageSessionSurviveTurningAfkOff
func TestAnAppliedDecisionAndAStageSessionSurviveTurningAfkOff(t *testing.T) {
	t.Parallel()
	d := &coordDurations{stage: 60 * 60, holdAfter: 2}
	h := twoBeltWorld(t, []core.Belt{afkBelt("one", true), afkBelt("two", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	h.Script.add("afk", "decide approved")
	h.coord.SetAfk(bgCtx, true)
	h.Until("hasSession := false ...", func() bool {
		hasSession := slices.ContainsFunc(h.nowRuns(), func(run core.Run) bool {
			return run.CurrentAttempt() != nil && run.CurrentAttempt().Session != nil
		})
		return appliedCount(decisionsNow(h)) > 0 && hasSession
	})
	var applied core.AfkDecision
	for _, decision := range coordDecisions(h) {
		if decision.Status == core.AfkDecisionStatusApplied {
			applied = decision
			break
		}
	}
	var stageSession string
	for _, run := range h.coord.Runs() {
		if attempt := run.CurrentAttempt(); attempt != nil && attempt.Session != nil && stageSession == "" {
			stageSession = *attempt.Session
		}
	}
	sim := h.coord.runner.(*runner.SimulatedRunner)
	if !sim.IsRunning(stageSession) {
		t.Fatal("the stage session is not running")
	}
	h.coord.SetAfk(bgCtx, false)
	h.Until("for _, decision := range decisionsNow(h) { ...", func() bool {
		return slices.ContainsFunc(decisionsNow(h), func(decision core.AfkDecision) bool {
			return decision.Status == core.AfkDecisionStatusSuperseded
		})
	})
	if got := decisionOf(coordDecisions(h), applied.ID); got == nil || got.Status != core.AfkDecisionStatusApplied {
		t.Fatalf("applied decision = %+v, want still applied", got)
	}
	savedApplied := slices.ContainsFunc(h.storage.LoadAfkDecisions(), func(decision core.AfkDecision) bool {
		return decision.ID == applied.ID && decision.Status == core.AfkDecisionStatusApplied
	})
	if !savedApplied {
		t.Fatal("the applied decision is not saved as applied")
	}
	if !sim.IsRunning(stageSession) {
		t.Fatal("the stage session stopped with the switch")
	}
	var cancelled *core.AfkDecision
	for _, decision := range coordDecisions(h) {
		if decision.Status == core.AfkDecisionStatusSuperseded {
			cancelled = &decision
			break
		}
	}
	if cancelled == nil || cancelled.Session == nil || sim.IsRunning(*cancelled.Session) {
		t.Fatalf("cancelled = %+v, want its session stopped", cancelled)
	}
	if run := h.coord.Run(cancelled.RunID); run == nil || run.State != core.RunStateWaiting {
		t.Fatalf("cancelled run = %+v, want waiting", run)
	}
}

// TestTurningTheSwitchBackOnStartsAFreshDecisionAndNeverReplaysTheCancelledOne
func TestTurningTheSwitchBackOnStartsAFreshDecisionAndNeverReplaysTheCancelledOne(t *testing.T) {
	t.Parallel()
	d := &coordDurations{holdAfter: 1}
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	before := *runOn(h, "issues")
	coordCancelAndReenable(t, h, before, "global-off", func() { h.coord.SetAfk(bgCtx, false) })
	h.coord.SetAfk(bgCtx, true)
	h.Until("decisions := decisionsNow(h) ...", func() bool {
		decisions := decisionsNow(h)
		return len(decisions) == 2 && len(coordDeciding(decisions)) == 1
	})
	decisions := coordDecisions(h)
	if decisions[0].Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("first decision = %s, want superseded", decisions[0].Status)
	}
	if run := h.coord.Run(before.ID); run == nil || run.State != core.RunStateWaiting {
		t.Fatalf("run = %+v, want waiting", run)
	}
	d.update(func(d *coordDurations) { d.holdAfter = 0; d.afk = 0 })
	h.Script.add("afk", "decide approved")
	h.Until("appliedCount(decisionsNow(h)) > 0", func() bool { return appliedCount(decisionsNow(h)) > 0 })
	decisions = coordDecisions(h)
	if len(decisions) < 2 || decisions[0].Status != core.AfkDecisionStatusSuperseded || decisions[1].Status != core.AfkDecisionStatusApplied {
		t.Fatalf("decisions = %+v, want superseded then applied", decisions)
	}
	if decisions[1].Choice == nil || *decisions[1].Choice != "decide approved" {
		t.Fatalf("applied choice = %v, want decide approved", decisions[1].Choice)
	}
	run := h.coord.Run(before.ID)
	if attempt := run.LastAttempt(before.StageID); run == nil || attempt == nil || attempt.Outcome == nil || *attempt.Outcome != "approved" {
		t.Fatalf("run = %+v, want its approval outcome approved", run)
	}
}

// TestLeaveAndFailureStillBlockTheSameStopWhenAfkComesBackOn
func TestLeaveAndFailureStillBlockTheSameStopWhenAfkComesBackOn(t *testing.T) {
	t.Parallel()
	leave := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{})
	leave.Script.add("afk", "leave")
	leave.coord.SetAfk(bgCtx, true)
	leave.Until("decisions := decisionsNow(leave) ...", func() bool {
		decisions := decisionsNow(leave)
		return len(decisions) > 0 && decisions[0].Status == core.AfkDecisionStatusLeftForYou
	})
	leave.coord.SetAfk(bgCtx, false)
	leave.coord.SetAfk(bgCtx, true)
	for range 3 {
		leave.Tick()
	}
	if n := len(coordDecisions(leave)); n != 1 {
		t.Fatalf("decisions after switching back on = %d, want 1", n)
	}
	if run := runOn(leave, "issues"); run == nil || run.State != core.RunStateWaiting {
		t.Fatalf("run = %+v, want waiting", run)
	}

	failed := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{})
	setAfk(failed, func(afk *core.AfkSettings) { afk.Model = "outside" })
	failed.coord.SetAfk(bgCtx, true)
	failed.Until("decisions := decisionsNow(failed) ...", func() bool {
		decisions := decisionsNow(failed)
		return len(decisions) > 0 && decisions[0].Status == core.AfkDecisionStatusFailed
	})
	failed.coord.SetAfk(bgCtx, false)
	failed.coord.SetAfk(bgCtx, true)
	for range 3 {
		failed.Tick()
	}
	decisions := coordDecisions(failed)
	if len(decisions) != 1 || decisions[0].Status != core.AfkDecisionStatusFailed {
		t.Fatalf("decisions = %+v, want the one failed decision", decisions)
	}
}

// TestOneStopDoesNotGetTwoSessionsAtOnce
func TestOneStopDoesNotGetTwoSessionsAtOnce(t *testing.T) {
	t.Parallel()
	d := &coordDurations{holdAfter: 1}
	h := twoBeltWorld(t, []core.Belt{afkBelt("issues", true)}, harnessOptions{
		Runner: func(s *harnessScript) runner.AgentRunner { return durationRunner(s, d) },
	})
	h.coord.SetAfk(bgCtx, true)
	h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
	for range 3 {
		h.Tick()
	}
	if n := len(coordDecisions(h)); n != 1 {
		t.Fatalf("decisions = %d, want 1", n)
	}
	if n := len(coordDeciding(coordDecisions(h))); n != 1 {
		t.Fatalf("deciding = %d, want 1", n)
	}
}
