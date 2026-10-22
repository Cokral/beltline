// Coordinator tests of overruling an AFK decision: a restart with your comments, a replay of an
// earlier stage, the refusals, a paused belt and a full slot, the races with a late completion, a
// start, a watch and an AFK reply, a stop that fails, and the recovery of a restart after a reload or
// a failed save.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// overruleWorld is the AFK world for overrules: the default belt opted in to AFK, sessions that run for
// duration seconds, and a fake GitHub that opens pull requests in the open state.
func overruleWorld(t *testing.T, duration float64) *harness {
	t.Helper()
	open := core.PullRequestStateOpen
	return newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, StageDuration: duration, OpensPulls: &open})
}

// overruleWorldOpening is overruleWorld with the pull requests the fake opens in state.
func overruleWorldOpening(t *testing.T, state core.PullRequestState) *harness {
	t.Helper()
	return newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, OpensPulls: &state})
}

// overruleWaitingWorld is the world that holds a run at its approval, meant as a run that waits at its
// approval: the triage and spec sessions finish at once and every later session runs on, so the run
// stands at approve. A stalled wait fails here: the sessions do not run for 10_000 seconds of real time, so the
// `until { waiting }` helper returns and the tests go on from a running spec.
func overruleWaitingWorld(t *testing.T) *harness {
	t.Helper()
	open := core.PullRequestStateOpen
	return newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, OpensPulls: &open,
		Runner: func(script *harnessScript) runner.AgentRunner {
			return runner.NewSimulatedRunner(waitingDuration, func() time.Time { return testkit.ContractTime }, script.next)
		}})
}

// waitingDuration is how long each simulated session runs in overruleWaitingWorld.
func waitingDuration(spec runner.SessionSpec) float64 {
	if spec.AgentID != nil && (*spec.AgentID == "triage" || *spec.AgentID == "spec") {
		return 0
	}
	return 10_000
}

// coordAppliedDecision is the applied AFK decision written
// onto the run's last attempt at its stage, and added to the log.
func coordAppliedDecision(t *testing.T, h *harness) core.AfkDecision {
	t.Helper()
	var made core.AfkDecision
	h.Turn(func() {
		run := h.coord.book.Run(h.nowRunID())
		if run == nil {
			t.Fatal("no run for the decision")
		}
		h.coord.book.update(run.ID, nil, nil, func(r *core.Run) {
			index := -1
			for i := range r.Attempts {
				if r.Attempts[i].StageID == r.StageID && r.Attempts[i].IsOpen() {
					index = i
				}
			}
			if index < 0 && len(r.Attempts) > 0 {
				index = len(r.Attempts) - 1
			}
			if index < 0 {
				return
			}
			choice := "decide approved"
			action := "Chose “approved” at " + r.Stage().Name
			message := "Ship it."
			why := "The spec is complete."
			made = core.AfkDecision{
				ID: "overrule-1", RunID: r.ID, BeltName: r.BeltName, SubjectRef: r.Subject.Ref(), SubjectTitle: r.Subject.Title,
				StageID: r.StageID, StageName: r.Stage().Name, Situation: core.AfkSituationKindApproval, Key: "overrule-1",
				Status: core.AfkDecisionStatusApplied, StartedAt: jsonx.Date{Time: time.Now()}, Choice: &choice, Action: &action,
				Message: &message, Why: &why, Folder: "",
			}
			r.Attempts[index].AfkDecision = &made.ID
		})
		h.coord.autopilot.add(made)
	})
	return made
}

// nowRunID is the id of the first run, read without the lock (for turns that already hold it).
func (h *harness) nowRunID() string {
	if len(h.coord.book.runs) == 0 {
		return ""
	}
	return h.coord.book.runs[0].ID
}

// coordOverrule calls OverruleAfk for decision, returning its error.
func coordOverrule(h *harness, decision, stage, comments string, accepted bool) error {
	return h.coord.OverruleAfk(context.Background(), decision, stage, comments, accepted)
}

// overruleOf is the log's entry for decision id, or nil.
func overruleOf(h *harness, id string) *core.AfkDecision {
	return decisionOf(coordDecisions(h), id)
}

// waitingRun runs the harness until its first run waits at approve.
func waitingIt(h *harness) {
	h.Until("run := h.nowRun(); return run != nil && run.State == core.RunStateWaiting", func() bool { run := h.nowRun(); return run != nil && run.State == core.RunStateWaiting })
}

// attemptsWithNote counts the attempts of run whose note is note.
func attemptsWithNote(run core.Run, note string) int {
	n := 0
	for _, attempt := range run.Attempts {
		if attempt.Note != nil && *attempt.Note == note {
			n++
		}
	}
	return n
}

// coordSignal is a one-shot signal, as the OverruleGate's entered and released flags are.
type coordSignal struct {
	once sync.Once
	ch   chan struct{}
}

func newH5m4Signal() *coordSignal { return &coordSignal{ch: make(chan struct{})} }

// fire opens the signal once.
func (s *coordSignal) fire() { s.once.Do(func() { close(s.ch) }) }

// wait blocks until the signal fires.
func (s *coordSignal) wait() { <-s.ch }

// coordClaim is OverruleGate.claim(): true for the first caller only.
type coordClaim struct {
	mu      sync.Mutex
	claimed bool
}

func (c *coordClaim) claim() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claimed {
		return false
	}
	c.claimed = true
	return true
}

// TestRestartingCarriesCommentsAndDoesNotContinueTheOldSession
func TestRestartingCarriesCommentsAndDoesNotContinueTheOldSession(t *testing.T) {
	t.Parallel()
	h := overruleWorld(t, 0)
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	h.Script.add("build", "done")
	waitingIt(h)
	decision := coordAppliedDecision(t, h)
	before := afkSettingsNow(h)
	if err := coordOverrule(h, decision.ID, "build", "  Cover the empty export.\n", true); err != nil {
		t.Fatalf("overrule: %v", err)
	}
	h.Until("r := h.nowRun() ...", func() bool {
		r := h.nowRun()
		return r != nil && hasAttemptAt(r.Attempts, "build")
	})
	run := h.Run()
	attempt := lastAttemptAt(run.Attempts, "build")
	if attempt == nil {
		t.Fatal("no build attempt")
	}
	if attempt.Note == nil || *attempt.Note != "Cover the empty export." {
		t.Fatalf("note = %v, want the trimmed comments", attempt.Note)
	}
	if attempt.Continues != nil || attempt.Replays != nil {
		t.Fatalf("continues %v, replays %v; want neither", attempt.Continues, attempt.Replays)
	}
	brief := readText(t, filepath.Join(h.coord.Paths().RunFolder(*run), attempt.Folder, "brief.md"))
	if !strings.Contains(brief, "Cover the empty export.") || !strings.Contains(brief, "A note for this attempt") {
		t.Fatal("the brief does not carry the note")
	}
	if strings.Contains(brief, "What this stage already had") || strings.Contains(brief, "continued") {
		t.Fatal("the brief carries an earlier session")
	}
	if !reflect.DeepEqual(afkSettingsNow(h), before) {
		t.Fatal("the overrule changed the AFK settings")
	}
	logged := overruleOf(h, decision.ID)
	if logged == nil || logged.Status != core.AfkDecisionStatusApplied || logged.Choice == nil || *logged.Choice != "decide approved" {
		t.Fatalf("logged = %+v, want applied with decide approved", logged)
	}
	if logged.Overrule == nil || logged.Overrule.Comments != "Cover the empty export." || logged.Overrule.StageID != "build" {
		t.Fatalf("overrule = %+v, want the comments at build", logged.Overrule)
	}
	if core.AfkLogLabel(*logged) != "OVERRULED" {
		t.Fatalf("label = %s, want OVERRULED", core.AfkLogLabel(*logged))
	}
	if logged.NeedsReview() {
		t.Fatal("an overruled decision still needs review")
	}
	if rows := core.AfkLogRows(coordDecisions(h), core.AfkLogFilterReview); len(rows) != 0 {
		t.Fatalf("review rows = %d, want 0", len(rows))
	}
	if rows := core.AfkLogRows(coordDecisions(h), core.AfkLogFilterAll); len(rows) != 1 || rows[0].ID != decision.ID {
		t.Fatalf("all rows = %+v, want the one decision", rows)
	}
	h.coord.ReviewAfk(nil)
	h.coord.ReviewAfk(map[string]struct{}{decision.ID: {}})
	reviewed := overruleOf(h, decision.ID)
	if reviewed == nil || reviewed.Overrule == nil || reviewed.Overrule.Comments != "Cover the empty export." || reviewed.Why == nil || *reviewed.Why != "The spec is complete." {
		t.Fatalf("reviewed = %+v", reviewed)
	}
}

// afkSettingsNow is the AFK settings, through the lock.
func afkSettingsNow(h *harness) core.AfkSettings {
	h.coord.Lock()
	defer h.coord.EndTurn()
	return h.coord.settings.Afk
}

// hasAttemptAt reports whether an attempt is at stage.
func hasAttemptAt(attempts []core.Attempt, stage string) bool {
	return lastAttemptAt(attempts, stage) != nil
}

// lastAttemptAt is the last attempt at stage, or nil.
func lastAttemptAt(attempts []core.Attempt, stage string) *core.Attempt {
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].StageID == stage {
			attempt := attempts[i]
			return &attempt
		}
	}
	return nil
}

// TestAFinishedRunRejectsTheSubmission
func TestAFinishedRunRejectsTheSubmission(t *testing.T) {
	t.Parallel()
	h := overruleWorld(t, 0)
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	waitingIt(h)
	decision := coordAppliedDecision(t, h)
	h.coord.Drop(h.Run().ID, nil)
	if err := coordOverrule(h, decision.ID, "spec", "Too late.", true); err == nil {
		t.Fatal("a finished run accepted the overrule")
	}
	if logged := overruleOf(h, decision.ID); logged == nil || logged.Overrule != nil {
		t.Fatalf("overrule = %+v, want none", logged)
	}
	if h.Run().Restart != nil {
		t.Fatal("a restart was recorded")
	}
	if h.Run().State != core.RunStateDropped {
		t.Fatalf("state = %s, want dropped", h.Run().State)
	}
}

// TestAStageWithoutAFreeSlotStaysQueued
func TestAStageWithoutAFreeSlotStaysQueued(t *testing.T) {
	t.Parallel()
	h := overruleWorld(t, 0)
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	waitingIt(h)
	decision := coordAppliedDecision(t, h)
	waiting := *h.Run()
	h.Turn(func() {
		h.coord.book.update(waiting.ID, nil, nil, func(r *core.Run) {
			for i := range r.Stages {
				if r.Stages[i].ID == "build" {
					r.Stages[i].Slots = 1
				}
			}
		})
	})
	occupant := waiting.Clone()
	occupant.ID = "issues:acme/billing#143"
	occupant.Subject = core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 143, Title: "Other", Body: "Busy."}
	occupant.State = core.RunStateRunning
	occupant.StageID = "build"
	for i := range occupant.Stages {
		if occupant.Stages[i].ID == "build" {
			occupant.Stages[i].Slots = 1
		}
	}
	open := sessionSampleAttempt("build", 1, "occupant")
	session := "bl-occupant"
	open.Session = &session
	occupant.Attempts = append(occupant.Attempts, open)
	if err := h.storage.SaveRun(occupant); err != nil {
		t.Fatalf("save occupant: %v", err)
	}
	sim := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
	sim.PretendRunning(session, testkit.ContractTime)
	resumed := h.RestartWith(sim)
	var target core.Run
	for _, run := range resumed.Runs() {
		if run.Subject.Number == 142 {
			target = run
		}
	}
	if got := decisionOf(resumed.ReadAfkDecisions(), decision.ID); got == nil || got.Status != core.AfkDecisionStatusApplied {
		t.Fatalf("decision on restart = %+v, want applied", got)
	}
	if err := resumed.OverruleAfk(bgCtx, decision.ID, "build", "After the other build.", true); err != nil {
		t.Fatalf("overrule: %v", err)
	}
	resumed.Tick(bgCtx)
	resumed.Idle()
	run := resumed.Run(target.ID)
	if run == nil || run.State != core.RunStateQueued || run.StageID != "build" {
		t.Fatalf("target = %+v, want queued at build", run)
	}
	for _, attempt := range run.Attempts {
		if attempt.Session != nil && *attempt.Session == session {
			t.Fatal("the target took the occupant's session")
		}
	}
	if got := resumed.Run(occupant.ID); got == nil || got.State != core.RunStateRunning {
		t.Fatalf("occupant = %+v, want running", got)
	}
}

// TestALateCompletionCannotFinishTheReplacement
func TestALateCompletionCannotFinishTheReplacement(t *testing.T) {
	t.Parallel()
	var sim *runner.SimulatedRunner
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, StageDuration: 10_000, OpensPulls: new(core.PullRequestStateOpen),
		Runner: func(script *harnessScript) runner.AgentRunner {
			sim = durationRunner2(script, 10_000)
			return sim
		}})
	entered := newH5m4Signal()
	release := newH5m4Signal()
	sim.BeforeStatus = func(spec runner.SessionSpec) {
		if spec.AgentID != nil && *spec.AgentID == "afk" {
			return
		}
		entered.fire()
		release.wait()
	}
	runningIt(h)
	decision := coordAppliedDecision(t, h)
	oldSession := runSession(h.Run())
	checking := make(chan struct{})
	go func() { defer close(checking); h.coord.Tick(bgCtx) }()
	entered.wait()
	if err := coordOverrule(h, decision.ID, h.Run().StageID, "Start this stage over.", true); err != nil {
		t.Fatalf("overrule: %v", err)
	}
	release.fire()
	<-checking
	h.Until("r := h.nowRun() ...", func() bool {
		r := h.nowRun()
		return r != nil && r.State == core.RunStateRunning && r.CurrentAttempt() != nil && r.CurrentAttempt().Note != nil && *r.CurrentAttempt().Note == "Start this stage over."
	})
	run := h.Run()
	current := run.CurrentAttempt()
	if current == nil || current.Continues != nil || !current.IsOpen() {
		t.Fatalf("current attempt = %+v, want an open attempt with no continue", current)
	}
	stopped := 0
	for _, attempt := range run.Attempts {
		if attempt.Route != nil && *attempt.Route == "Stopped: overruled" {
			stopped++
		}
	}
	if stopped != 1 {
		t.Fatalf("stopped-by-overrule attempts = %d, want 1", stopped)
	}
	trail := sim.Trail()
	stop, start := -1, -1
	for i, entry := range trail {
		if entry == "stop "+oldSession {
			stop = i
		}
		if strings.HasPrefix(entry, "start ") && entry != "start "+oldSession {
			start = i
		}
	}
	if stop < 0 || start < 0 || stop >= start {
		t.Fatalf("trail = %v, want the old session stopped before the replacement starts", trail)
	}
	if sim.IsRunning(oldSession) {
		t.Fatal("the old session still runs")
	}
}

// runningIt runs the harness until its first run is running.
func runningIt(h *harness) {
	h.Until("r := h.nowRun(); return r != nil && r.State == core.RunStateRunning", func() bool { r := h.nowRun(); return r != nil && r.State == core.RunStateRunning })
}

// durationRunner2 is a simulated runner whose sessions run for duration seconds, picking outcomes from script.
func durationRunner2(script *harnessScript, duration float64) *runner.SimulatedRunner {
	return runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return duration },
		func() time.Time { return testkit.ContractTime }, script.next)
}

// TestAnInFlightStartDoesNotKeepACompetingSession
func TestAnInFlightStartDoesNotKeepACompetingSession(t *testing.T) {
	t.Parallel()
	var sim *runner.SimulatedRunner
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, StageDuration: 10_000, OpensPulls: new(core.PullRequestStateOpen),
		Runner: func(script *harnessScript) runner.AgentRunner {
			sim = durationRunner2(script, 10_000)
			return sim
		}})
	entered := newH5m4Signal()
	release := newH5m4Signal()
	claim := &coordClaim{}
	sim.BeforeStart = func(runner.SessionSpec) {
		if claim.claim() {
			entered.fire()
			release.wait()
		}
	}
	starting := make(chan struct{})
	go func() { defer close(starting); h.coord.Tick(bgCtx) }()
	entered.wait()
	decision := coordAppliedDecision(t, h)
	if err := coordOverrule(h, decision.ID, "spec", "Not triage.", true); err != nil {
		t.Fatalf("overrule: %v", err)
	}
	release.fire()
	<-starting
	h.Until("r := h.nowRun() ...", func() bool {
		r := h.nowRun()
		return r != nil && r.CurrentAttempt() != nil && r.CurrentAttempt().StageID == "spec" && r.CurrentAttempt().Note != nil && *r.CurrentAttempt().Note == "Not triage."
	})
	run := h.Run()
	open := 0
	for _, attempt := range run.Attempts {
		if attempt.IsOpen() {
			open++
		}
	}
	if open != 1 || run.CurrentAttempt().StageID != "spec" {
		t.Fatalf("open attempts = %d at %s, want one at spec", open, run.CurrentAttempt().StageID)
	}
	if n := sim.RunningCount(); n != 1 {
		t.Fatalf("running sessions = %d, want 1", n)
	}
}

// TestALateWatchCannotMoveTheRestartedRun
func TestALateWatchCannotMoveTheRestartedRun(t *testing.T) {
	t.Parallel()
	h := overruleWorldOpening(t, core.PullRequestStateOpen)
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	h.Script.add("build", "done")
	h.Script.add("verify", "pass")
	h.Script.add("ship", "opened")
	h.Until("r := h.nowRun() ...", func() bool {
		r := h.nowRun()
		return r != nil && r.State == core.RunStateWaiting && r.StageID == "approve"
	})
	h.coord.Decide(h.Run().ID, "approved", nil)
	h.Until("r := h.nowRun() ...", func() bool {
		r := h.nowRun()
		return r != nil && r.Stage() != nil && r.Stage().Kind() == core.StageKindWatch && r.State == core.RunStateRunning
	})
	if h.Run().Branch == nil {
		t.Fatal("the watch run has no branch")
	}
	h.github.HoldPulls()
	checking := make(chan struct{})
	go func() {
		defer close(checking)
		h.coord.Lock()
		h.coord.watches.check(bgCtx, true)
		h.coord.EndTurn()
	}()
	coordWaits(t, "a pull request read waits", func() bool { return h.github.WaitingPulls() > 0 })
	decision := coordAppliedDecision(t, h)
	if err := coordOverrule(h, decision.ID, "build", "Leave the watch.", true); err != nil {
		t.Fatalf("overrule: %v", err)
	}
	h.github.ReleasePulls()
	<-checking
	h.coord.Tick(bgCtx)
	h.coord.Idle()
	run := h.Run()
	watch := lastAttemptAt(run.Attempts, "watch")
	if watch == nil || watch.Route == nil || *watch.Route != "Stopped: overruled" || watch.Outcome != nil {
		t.Fatalf("watch attempt = %+v, want stopped by the overrule with no outcome", watch)
	}
	if run.StageID == "watch" {
		t.Fatal("the late watch moved the restarted run")
	}
	if run.Restart == nil || run.Restart.DecisionID != decision.ID {
		t.Fatalf("restart = %+v, want decision %s", run.Restart, decision.ID)
	}
}

// TestAStopFailureLeavesNoCompetingSession
func TestAStopFailureLeavesNoCompetingSession(t *testing.T) {
	t.Parallel()
	var sim *runner.SimulatedRunner
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, StageDuration: 10_000, OpensPulls: new(core.PullRequestStateOpen),
		Runner: func(script *harnessScript) runner.AgentRunner {
			sim = durationRunner2(script, 10_000)
			return sim
		}})
	runningIt(h)
	decision := coordAppliedDecision(t, h)
	session := runSession(h.Run())
	failure := "the session would not stop"
	sim.StopError = &failure
	if err := coordOverrule(h, decision.ID, "triage", "Try again.", true); err == nil {
		t.Fatal("a stop that failed was reported as an overrule")
	}
	if h.Run().Restart == nil || h.Run().Restart.Phase != core.RestartIntentPhaseIntent || h.Run().Restart.Comments != "Try again." {
		t.Fatalf("restart = %+v, want the intent with the comments", h.Run().Restart)
	}
	if logged := overruleOf(h, decision.ID); logged == nil || logged.Overrule != nil {
		t.Fatalf("overrule = %+v, want none", logged)
	}
	if !sim.IsRunning(session) || sim.RunningCount() != 1 {
		t.Fatal("the session stopped or a second one started")
	}
	h.Turn(func() { h.coord.recoverOverrules(bgCtx) })
	run := h.Run()
	if run.State != core.RunStateHeld || run.Restart == nil || run.Restart.Phase != core.RestartIntentPhaseHeld || run.Restart.Comments != "Try again." {
		t.Fatalf("run after recovery = %s, restart %+v; want held with the comments", run.State, run.Restart)
	}
	logged := overruleOf(h, decision.ID)
	if logged == nil || logged.Overrule == nil || logged.Overrule.Phase != core.AfkOverrulePhaseHeld || logged.Overrule.Comments != "Try again." {
		t.Fatalf("logged overrule = %+v, want held with the comments", logged)
	}
	if sim.RunningCount() != 1 {
		t.Fatalf("running sessions = %d, want 1", sim.RunningCount())
	}
	for _, attempt := range run.Attempts {
		if attempt.Note != nil && *attempt.Note == "Try again." {
			t.Fatal("the stopped attempt took the comments")
		}
	}
}

// overruleRecovery builds the waiting world the recovery tests start from, with an applied decision.
func overruleRecovery(t *testing.T) *harness {
	t.Helper()
	h := overruleWaitingWorld(t)
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	waitingIt(h)
	coordAppliedDecision(t, h)
	return h
}

// TestReloadCompletesOneRestart
func TestReloadCompletesOneRestart(t *testing.T) {
	t.Parallel()
	for _, halt := range []OverruleHalt{"intent", "stopped", "logged"} {
		t.Run(string(halt), func(t *testing.T) {
			t.Parallel()
			h := overruleRecovery(t)
			h.coord.overruleHalt = new(halt)
			if err := coordOverrule(h, "overrule-1", "build", "Keep this.", true); err == nil {
				t.Fatal("the overrule ran past its halt")
			}
			again := h.RestartWith(durationRunner2(h.Script, 10_000))
			if run := again.Run("issues:acme/billing#142"); run == nil || run.Restart == nil || run.Restart.Comments != "Keep this." {
				t.Fatalf("restart after reload = %+v, want the comments", run)
			}
			if halt == "logged" {
				if got := decisionOf(again.ReadAfkDecisions(), "overrule-1"); got == nil || got.Overrule == nil || got.Overrule.Comments != "Keep this." {
					t.Fatalf("logged decision after reload = %+v", got)
				}
			}
			again.Tick(bgCtx)
			again.Idle()
			var run core.Run
			for _, candidate := range again.Runs() {
				if candidate.Restart != nil && candidate.Restart.DecisionID == "overrule-1" {
					run = candidate
				}
			}
			if run.Restart == nil || run.Restart.Phase != core.RestartIntentPhaseApplied {
				t.Fatalf("restart = %+v, want applied", run.Restart)
			}
			if got := decisionOf(again.ReadAfkDecisions(), "overrule-1"); got == nil || got.Overrule == nil || got.Overrule.Comments != "Keep this." {
				t.Fatalf("decision = %+v, want the overrule", got)
			}
			again.Tick(bgCtx)
			again.Idle()
			restarted := again.Run(run.ID)
			if restarted == nil || attemptsWithNote(*restarted, "Keep this.") != 1 {
				t.Fatalf("attempts with the comments = %d, want 1", attemptsWithNote(*restarted, "Keep this."))
			}
		})
	}
}

// stopGate is a runner whose first EnsureStopped holds until release is closed (the stop gate of
// the simulated runner): the first overrule is suspended in its stop, and a second submission can
// arrive while it is in flight.
type stopGate struct {
	runner.AgentRunner
	entered chan string
	release chan struct{}
	calls   atomic.Int32
}

// EnsureStopped holds the first call until release, and passes the rest through.
func (g *stopGate) EnsureStopped(ctx context.Context, session string) error {
	if g.calls.Add(1) == 1 {
		g.entered <- session
		<-g.release
	}
	return g.AgentRunner.EnsureStopped(ctx, session)
}

// runningDuration is how long each simulated session runs when the spec session must stay open:
// triage finishes at once and every other session runs on.
func runningDuration(spec runner.SessionSpec) float64 {
	if spec.AgentID != nil && *spec.AgentID == "triage" {
		return 0
	}
	return 10_000
}

// TestASecondSubmissionDoesNotStartAnotherRestart checks that a second submission does not start another restart:
// both submissions succeed and the restart happens once. The restart must stop the running spec session
// first, so the first submission is held in that stop until the second one has started; the second
// joins the restart in flight.
func TestASecondSubmissionDoesNotStartAnotherRestart(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, OpensPulls: new(core.PullRequestStateOpen),
		Runner: func(script *harnessScript) runner.AgentRunner {
			return runner.NewSimulatedRunner(runningDuration, func() time.Time { return testkit.ContractTime }, script.next)
		}})
	h.Script.add("triage", "ready to spec")
	h.Until("run := h.nowRun(); return run != nil && run.StageID == \"spec\" && run.State == core.RunStateRunning && run.CurrentAttempt() != nil && run.CurrentAttempt().Session != nil", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "spec" && run.State == core.RunStateRunning && run.CurrentAttempt() != nil && run.CurrentAttempt().Session != nil
	})
	coordAppliedDecision(t, h)
	gate := &stopGate{entered: make(chan string, 1), release: make(chan struct{})}
	h.Turn(func() {
		gate.AgentRunner = h.coord.runner
		h.coord.runner = gate
	})

	first := make(chan error, 1)
	go func() { first <- coordOverrule(h, "overrule-1", "build", "Once.", true) }()
	select {
	case <-gate.entered:
	case err := <-first:
		t.Fatalf("the first submission finished without stopping the running spec session: %v", err)
	}
	second := make(chan error, 1)
	go func() { second <- coordOverrule(h, "overrule-1", "build", "Once.", true) }()
	// Nothing observes the second submission arriving, so give it the scheduler before the stop
	// returns; a submission that arrives after the restart is saved would be refused instead.
	for range 1000 {
		runtime.Gosched()
	}
	close(gate.release)
	if err := <-first; err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second submission joined the restart and should succeed: %v", err)
	}
	h.Until("attempt with the comments", func() bool {
		r := h.nowRun()
		return r != nil && attemptsWithNote(*r, "Once.") > 0
	})
	if n := attemptsWithNote(*h.Run(), "Once."); n != 1 {
		t.Fatalf("attempts with the comments = %d, want 1", n)
	}
	if err := coordOverrule(h, "overrule-1", "spec", "Again.", true); err == nil {
		t.Fatal("a second restart was accepted")
	}
	if n := attemptsWithNote(*h.Run(), "Again."); n != 0 {
		t.Fatalf("attempts with the refused comments = %d, want 0", n)
	}
}

// TestAFailedSaveKeepsTheCommentsAndDoesNotMarkTheDecision
func TestAFailedSaveKeepsTheCommentsAndDoesNotMarkTheDecision(t *testing.T) {
	t.Parallel()
	h := overruleRecovery(t)
	folder := h.coord.Paths().RunFolder(*h.Run())
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(folder, 0o755) }()
	if err := coordOverrule(h, "overrule-1", "build", "Still here.", true); err == nil {
		t.Fatal("a failed save was reported as success")
	}
	if h.Run().Restart != nil {
		t.Fatalf("restart = %+v, want none", h.Run().Restart)
	}
	if got := overruleOf(h, "overrule-1"); got == nil || got.Overrule != nil {
		t.Fatalf("overrule = %+v, want none", got)
	}
}

// TestAFailedLogSaveIsRecoveredOnce
func TestAFailedLogSaveIsRecoveredOnce(t *testing.T) {
	t.Parallel()
	h := overruleRecovery(t)
	directory := filepath.Dir(h.coord.Paths().AfkDecisions())
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := coordOverrule(h, "overrule-1", "build", "Saved on the run.", true); err == nil {
		t.Fatal("a failed log save was reported as success")
	}
	if h.Run().Restart == nil || h.Run().Restart.Phase != core.RestartIntentPhaseApplied || h.Run().Restart.Comments != "Saved on the run." {
		t.Fatalf("restart = %+v, want applied with the comments", h.Run().Restart)
	}
	if got := overruleOf(h, "overrule-1"); got == nil || got.Overrule != nil {
		t.Fatalf("overrule = %+v, want none", got)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	again := h.RestartWith(durationRunner2(h.Script, 10_000))
	again.Tick(bgCtx)
	again.Idle()
	if got := decisionOf(again.ReadAfkDecisions(), "overrule-1"); got == nil || got.Overrule == nil || got.Overrule.Comments != "Saved on the run." {
		t.Fatalf("decision after the reload = %+v, want the overrule", got)
	}
	again.Tick(bgCtx)
	again.Idle()
	runs := again.Runs()
	if len(runs) == 0 || attemptsWithNote(runs[0], "Saved on the run.") != 1 {
		t.Fatal("the comments were not carried exactly once")
	}
}
