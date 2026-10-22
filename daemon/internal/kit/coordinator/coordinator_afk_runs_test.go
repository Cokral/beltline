// Coordinator tests of AFK runs: a run's own choice saved across stage changes, restarts and
// failed saves, the shared assistant at an approval and a question, the limit and the leave
// rules, and the runs that are not decided.

package coordinator

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestARunChoiceIsSavedAcrossStageChangesRestartAndAFailedSave
func TestARunChoiceIsSavedAcrossStageChangesRestartAndAFailedSave(t *testing.T) {
	t.Parallel()
	h := coordGate(t, harnessOptions{})
	id := h.Run().ID
	setAfk(h, func(afk *core.AfkSettings) { afk.Approve = false; afk.Enabled = true })
	h.coord.SetRunAfk(bgCtx, id, core.AfkModeAlways)
	if run := h.coord.Run(id); run == nil || run.AfkEnabled == nil || !*run.AfkEnabled {
		t.Fatalf("run choice = %+v, want on", run)
	}
	if saved := h.storage.LoadRuns(); !savedChoiceIs(saved, id, true) {
		t.Fatal("the saved run does not carry the choice")
	}
	stage := h.Run().StageID
	h.coord.Decide(id, "approved", nil)
	if run := h.coord.Run(id); run == nil || run.StageID == stage || run.AfkEnabled == nil || !*run.AfkEnabled {
		t.Fatalf("run after the decision = %+v, want moved on with the choice kept", run)
	}
	if saved := h.storage.LoadRuns(); !savedChoiceIs(saved, id, true) {
		t.Fatal("the saved run lost the choice after a stage change")
	}

	setAfk(h, func(afk *core.AfkSettings) { afk.Enabled = false })
	next := h.RestartWith(pickingRunner(h))
	restored := next.Run(id)
	if restored == nil {
		t.Fatal("run gone after the restart")
	}
	if globalAfk(next) {
		t.Fatal("global AFK on after the restart with it off")
	}
	if restored.AfkEnabled == nil || !*restored.AfkEnabled || !restored.AfkEffective(false) {
		t.Fatalf("restored run = %+v, want its choice and AFK effective", restored)
	}

	blocked := coordGate(t, harnessOptions{})
	blockedID := blocked.Run().ID
	folder := blocked.coord.Paths().RunFolder(*blocked.Run())
	if err := os.Chmod(folder, 0o555); err != nil {
		t.Fatal(err)
	}
	blocked.coord.SetRunAfk(bgCtx, blockedID, core.AfkModeAlways)
	if err := os.Chmod(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	after := blocked.coord.Run(blockedID)
	if after == nil || after.AfkEnabled != nil || after.AfkEffective(false) {
		t.Fatalf("run after a failed save = %+v, want no choice and AFK not effective", after)
	}
	ref := blocked.Run().Subject.Ref()
	if !hasNoticeContaining(blocked.coord.Notices(), "Could not save", ref) {
		t.Fatalf("notices = %+v, want a save failure naming %s", blocked.coord.Notices(), ref)
	}
	if saved := blocked.storage.LoadRuns(); !savedChoiceIs(saved, blockedID, false) {
		t.Fatal("the failed save left a choice on disk")
	}

	blocked.coord.Drop(blockedID, nil)
	blocked.coord.SetRunAfk(bgCtx, blockedID, core.AfkModeAlways)
	if run := blocked.coord.Run(blockedID); run == nil || run.State != core.RunStateDropped || run.AfkEnabled != nil {
		t.Fatalf("dropped run = %+v, want dropped with no choice", run)
	}
	blocked.Turn(func() {
		blocked.coord.book.update(blockedID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	})
	blocked.coord.SetRunAfk(bgCtx, blockedID, core.AfkModeAlways)
	if run := blocked.coord.Run(blockedID); run == nil || run.AfkEnabled != nil {
		t.Fatalf("shipped run = %+v, want no choice", run)
	}
}

// savedChoiceIs reports whether the saved run id has the AFK choice want (nil when want is false).
func savedChoiceIs(runs []core.Run, id string, want bool) bool {
	for _, run := range runs {
		if run.ID != id {
			continue
		}
		if !want {
			return run.AfkEnabled == nil
		}
		return run.AfkEnabled != nil && *run.AfkEnabled == want
	}
	return false
}

// hasNoticeContaining reports whether a notice carries every fragment.
func hasNoticeContaining(notices []Notice, fragments ...string) bool {
	for _, notice := range notices {
		all := !slices.ContainsFunc(fragments, func(fragment string) bool {
			return !strings.Contains(notice.Text, fragment)
		})
		if all {
			return true
		}
	}
	return false
}

// pickingRunner is a simulated runner that runs sessions for no time and picks outcomes from
// the harness's script, as a simulated runner with zero duration and that pick does.
func pickingRunner(h *harness) runner.AgentRunner {
	return runner.NewSimulatedRunner(
		func(runner.SessionSpec) float64 { return 0 },
		func() time.Time { return testkit.ContractTime },
		h.Script.next,
	)
}

// TestRunAfkUsesTheSharedAssistantAtAnApprovalAndAQuestion
func TestRunAfkUsesTheSharedAssistantAtAnApprovalAndAQuestion(t *testing.T) {
	t.Parallel()
	approval := coordGate(t, harnessOptions{})
	gate := approval.Run().StageID
	approval.Script.add("afk", "decide approved")
	setAfk(approval, func(afk *core.AfkSettings) {
		afk.Thinking = new(core.ThinkingHigh)
		afk.Guidance = "Never approve a change to billing"
		afk.Backend = core.BackendPi
	})
	approval.coord.SetRunAfk(bgCtx, approval.Run().ID, core.AfkModeAlways)
	approval.Until("run := approval.nowRun(); return run != nil && run.StageID != gate", func() bool { run := approval.nowRun(); return run != nil && run.StageID != gate })
	run := approval.Run()
	decisions := coordDecisions(approval)
	if len(decisions) == 0 {
		t.Fatal("no decision")
	}
	decision := decisions[0]
	if globalAfk(approval.coord) {
		t.Fatal("the run's choice turned the global switch on")
	}
	if decision.Status != core.AfkDecisionStatusApplied {
		t.Fatalf("status = %s, want applied", decision.Status)
	}
	if decision.Backend == nil || *decision.Backend != core.BackendPi {
		t.Fatalf("backend = %v, want pi", decision.Backend)
	}
	if decision.Thinking == nil || *decision.Thinking != core.ThinkingHigh {
		t.Fatalf("thinking = %v, want high", decision.Thinking)
	}
	if decision.Model == nil || *decision.Model != "fixture-model" {
		t.Fatalf("model = %v, want fixture-model", decision.Model)
	}
	if attempt := run.LastAttempt(gate); attempt == nil || attempt.AfkDecision == nil || *attempt.AfkDecision != decision.ID {
		t.Fatalf("approval attempt = %+v, want the decision", attempt)
	}
	brief := readText(t, filepath.Join(approval.coord.Paths().RunFolder(*run), decision.Folder, "brief.md"))
	if !strings.Contains(brief, "Never approve a change to billing") {
		t.Fatal("the brief does not carry the guidance")
	}
	saved := approval.storage.LoadAfkDecisions()
	if len(saved) == 0 || saved[0].ID != decision.ID || saved[0].Status != core.AfkDecisionStatusApplied {
		t.Fatalf("saved = %+v, want the applied decision", saved)
	}
	// 	// The tick the choice kicks does not run before the assertions after it, so hold it.
	approval.coord.Idle()
	approval.coord.Hold()
	approval.coord.SetRunAfk(bgCtx, run.ID, core.AfkModeNever)
	if got := coordDecisions(approval); len(got) == 0 || got[0].Status != core.AfkDecisionStatusApplied {
		t.Fatalf("decision after the choice changed = %+v, want still applied", got)
	}
	if now := approval.Run(); now == nil || now.StageID != run.StageID {
		t.Fatal("the run moved after its choice changed")
	}
	approval.coord.Release()

	questions := newHarness(t, harnessOptions{})
	questions.Script.add("triage", "needs info", "ready to spec")
	questions.Until("run := questions.nowRun(); return run != nil && run.State == core.RunStateHeld", func() bool { run := questions.nowRun(); return run != nil && run.State == core.RunStateHeld })
	questions.Script.add("afk", "answer")
	questions.coord.SetRunAfk(bgCtx, questions.Run().ID, core.AfkModeAlways)
	questions.Until("run := questions.nowRun(); return run != nil && len(run.Attempts) == 2", func() bool { run := questions.nowRun(); return run != nil && len(run.Attempts) == 2 })
	if globalAfk(questions.coord) {
		t.Fatal("the question's choice turned the global switch on")
	}
	decisions = coordDecisions(questions)
	if len(decisions) == 0 || decisions[0].Situation != core.AfkSituationKindQuestion {
		t.Fatalf("decisions = %+v, want a question", decisions)
	}
	if !attemptsReferToDecision(questions.Run().Attempts, decisions[0].ID) {
		t.Fatal("no attempt carries the question's decision")
	}

	denied := coordGate(t, harnessOptions{})
	setAfk(denied, func(afk *core.AfkSettings) { afk.Approve = false })
	denied.coord.SetRunAfk(bgCtx, denied.Run().ID, core.AfkModeAlways)
	for range 3 {
		denied.Tick()
	}
	if n := len(coordDecisions(denied)); n != 0 {
		t.Fatalf("decisions with approvals off = %d, want 0", n)
	}
	if run := denied.Run(); run.State != core.RunStateWaiting {
		t.Fatalf("state = %s, want waiting", run.State)
	}

	refused := coordGate(t, harnessOptions{})
	setAfk(refused, func(afk *core.AfkSettings) { afk.Model = "outside" })
	refused.coord.SetRunAfk(bgCtx, refused.Run().ID, core.AfkModeAlways)
	refused.Tick()
	if decisions := coordDecisions(refused); len(decisions) == 0 || decisions[0].Status != core.AfkDecisionStatusFailed {
		t.Fatalf("decisions = %+v, want failed", decisions)
	}
	if run := refused.Run(); run.State != core.RunStateWaiting {
		t.Fatalf("state = %s, want waiting", run.State)
	}
	setAfk(refused, func(afk *core.AfkSettings) { afk.Model = "fixture-model" })
	refused.coord.SetRunAfk(bgCtx, refused.Run().ID, core.AfkModeNever)
	refused.coord.SetRunAfk(bgCtx, refused.Run().ID, core.AfkModeAlways)
	for range 3 {
		refused.Tick()
	}
	decisions = coordDecisions(refused)
	if len(decisions) != 1 || decisions[0].Status != core.AfkDecisionStatusFailed {
		t.Fatalf("decisions after retrying = %+v, want the one failed decision", decisions)
	}
}

// attemptsReferToDecision reports whether an attempt carries the AFK decision id.
func attemptsReferToDecision(attempts []core.Attempt, id string) bool {
	return slices.ContainsFunc(attempts, func(attempt core.Attempt) bool {
		return attempt.AfkDecision != nil && *attempt.AfkDecision == id
	})
}

// TestQueuedRunningParkedAndStoppedRunsAreNotDecided
func TestQueuedRunningParkedAndStoppedRunsAreNotDecided(t *testing.T) {
	t.Parallel()
	queued := newHarness(t, harnessOptions{})
	belt := queued.coord.Belts()[0].Belt
	if err := queued.coord.SetPaused(belt.ID, true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	var queuedRun core.Run
	queued.Turn(func() {
		queuedRun = queued.coord.book.add(core.NewRun(core.RunArgs{Belt: belt, Subject: coordSubject(142, "Export invoices as CSV"), Now: testkitNow()}))
	})
	if queuedRun.State != core.RunStateQueued {
		t.Fatalf("state = %s, want queued", queuedRun.State)
	}
	queued.coord.SetAfk(bgCtx, true)
	queued.coord.SetRunAfk(bgCtx, queuedRun.ID, core.AfkModeAlways)
	for range 3 {
		queued.Tick()
	}
	if n := len(coordDecisions(queued)); n != 0 {
		t.Fatalf("decisions for a queued run = %d, want 0", n)
	}
	if run := queued.coord.Run(queuedRun.ID); run == nil || run.State != core.RunStateQueued {
		t.Fatalf("queued run = %+v, want queued", run)
	}
	if err := queued.coord.SetPaused(queuedRun.BeltID, false); err != nil {
		t.Fatalf("resume belt: %v", err)
	}
	queued.Until("run := queued.nowRun(); return run != nil && run.State == core.RunStateRunning", func() bool { run := queued.nowRun(); return run != nil && run.State == core.RunStateRunning })
	if n := len(coordDecisions(queued)); n != 0 {
		t.Fatalf("decisions for a running stage = %d, want 0", n)
	}
	if run := queued.Run(); run == nil || run.CurrentAttempt() == nil || run.CurrentAttempt().Session == nil {
		t.Fatalf("run = %+v, want a session", run)
	}

	running := newHarness(t, harnessOptions{StageDuration: 100_000})
	running.Until("run := running.nowRun(); return run != nil && run.State == core.RunStateRunning", func() bool { run := running.nowRun(); return run != nil && run.State == core.RunStateRunning })
	session := *running.Run().CurrentAttempt().Session
	running.coord.SetAfk(bgCtx, true)
	running.coord.SetRunAfk(bgCtx, running.Run().ID, core.AfkModeNever)
	for range 3 {
		running.Tick()
	}
	sim := running.coord.runner.(*runner.SimulatedRunner)
	if !sim.IsRunning(session) {
		t.Fatal("the stage session stopped")
	}
	if n := len(coordDecisions(running)); n != 0 {
		t.Fatalf("decisions for a never run = %d, want 0", n)
	}
	if run := running.Run(); run.State != core.RunStateRunning {
		t.Fatalf("state = %s, want running", run.State)
	}

	parked := coordGate(t, harnessOptions{})
	parked.coord.Decide(parked.Run().ID, "parked", nil)
	if run := parked.Run(); run.State != core.RunStateParked {
		t.Fatalf("state = %s, want parked", run.State)
	}
	parked.coord.SetAfk(bgCtx, true)
	parked.coord.SetRunAfk(bgCtx, parked.Run().ID, core.AfkModeAlways)
	for range 3 {
		parked.Tick()
	}
	if n := len(coordDecisions(parked)); n != 0 {
		t.Fatalf("decisions for a parked run = %d, want 0", n)
	}
	if run := parked.Run(); run.State != core.RunStateParked {
		t.Fatalf("state = %s, want parked", run.State)
	}

	stopped := newHarness(t, harnessOptions{})
	stopped.Until("run := stopped.nowRun(); return run != nil && run.State == core.RunStateRunning", func() bool { run := stopped.nowRun(); return run != nil && run.State == core.RunStateRunning })
	stopped.coord.Abort(stopped.Run().ID, nil)
	if run := stopped.Run(); run.Reason == nil || *run.Reason != "stopped by you" {
		t.Fatalf("reason = %v, want stopped by you", run.Reason)
	}
	stopped.coord.SetAfk(bgCtx, true)
	stopped.coord.SetRunAfk(bgCtx, stopped.Run().ID, core.AfkModeAlways)
	for range 3 {
		stopped.Tick()
	}
	if n := len(coordDecisions(stopped)); n != 0 {
		t.Fatalf("decisions for a stopped run = %d, want 0", n)
	}
	if run := stopped.Run(); run.State != core.RunStateHeld {
		t.Fatalf("state = %s, want held", run.State)
	}
}

// TestChangingGlobalAfkCancelsOnlyTheDecisionsOfRunsThatFollowIt
func TestChangingGlobalAfkCancelsOnlyTheDecisionsOfRunsThatFollowIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Issues: []core.Subject{coordSubject(142, "Export invoices as CSV"), coordSubject(143, "Export credits"), coordSubject(144, "Export quotes")},
		Belts:  []core.Belt{coordOptedInBelt()}, AfkDuration: 100_000,
	})
	h.Script.add("triage", "ready to spec", "ready to spec", "ready to spec")
	h.Script.add("spec", "complete", "complete", "complete")
	h.Until("n := 0 ...", func() bool {
		n := 0
		for _, run := range h.nowRuns() {
			if run.State == core.RunStateWaiting {
				n++
			}
		}
		return n == 3
	})
	runs := h.coord.Runs()
	following, always, never := runs[0].ID, runs[1].ID, runs[2].ID
	h.coord.SetAfk(bgCtx, true)
	h.coord.SetRunAfk(bgCtx, always, core.AfkModeAlways)
	h.coord.SetRunAfk(bgCtx, never, core.AfkModeNever)
	h.Until("len(coordDeciding(decisionsNow(h))) == 2", func() bool { return len(coordDeciding(decisionsNow(h))) == 2 })
	deciding := coordDeciding(coordDecisions(h))
	kept := decisionFor(deciding, always)
	followed := decisionFor(deciding, following)
	if kept == nil || followed == nil {
		t.Fatalf("decisions in progress = %+v, want one for %s and one for %s", deciding, always, following)
	}
	h.coord.SetAfk(bgCtx, false)
	sim := h.coord.runner.(*runner.SimulatedRunner)
	if got := decisionOf(coordDecisions(h), followed.ID); got == nil || got.Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("followed decision = %+v, want superseded", got)
	} else if got.Detail == nil || *got.Detail != autopilotGlobalOffDetail {
		t.Fatalf("followed detail = %v, want the global-off detail", got.Detail)
	}
	if followed.Session == nil || sim.IsRunning(*followed.Session) {
		t.Fatal("the followed session is still running")
	}
	if got := decisionOf(coordDecisions(h), kept.ID); got == nil || got.Status != core.AfkDecisionStatusDeciding {
		t.Fatalf("kept decision = %+v, want still deciding", got)
	}
	if kept.Session == nil || !sim.IsRunning(*kept.Session) {
		t.Fatal("the kept session stopped")
	}
	if got := h.coord.Run(following); got == nil || got.Afk() != core.AfkModeGlobal {
		t.Fatalf("following run = %+v, want global", got)
	}
	if got := h.coord.Run(always); got == nil || got.Afk() != core.AfkModeAlways {
		t.Fatalf("always run = %+v, want always", got)
	}
	if got := h.coord.Run(never); got == nil || got.Afk() != core.AfkModeNever {
		t.Fatalf("never run = %+v, want never", got)
	}
	// Back on: the run that follows it is decided again; never stays out.
	h.coord.SetAfk(bgCtx, true)
	h.Until("len(coordDeciding(decisionsNow(h))) == 2", func() bool { return len(coordDeciding(decisionsNow(h))) == 2 })
	for range 3 {
		h.Tick()
	}
	if n := len(coordDeciding(coordDecisions(h))); n != 2 {
		t.Fatalf("deciding after switching back on = %d, want 2", n)
	}
	h.coord.Lock()
	allowsNever := h.coord.autopilot.allows(*h.coord.book.Run(never))
	h.coord.EndTurn()
	if allowsNever {
		t.Fatal("never run is allowed AFK")
	}
	if got := decisionOf(coordDecisions(h), followed.ID); got == nil || got.Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("followed decision after switching back on = %+v, want superseded", got)
	}
}

// coordDeciding returns the decisions still in progress.
func coordDeciding(decisions []core.AfkDecision) []core.AfkDecision {
	var out []core.AfkDecision
	for _, decision := range decisions {
		if decision.Status == core.AfkDecisionStatusDeciding {
			out = append(out, decision)
		}
	}
	return out
}

// decisionFor returns the decision for run in decisions, or nil.
func decisionFor(decisions []core.AfkDecision, runID string) *core.AfkDecision {
	for i := range decisions {
		if decisions[i].RunID == runID {
			return &decisions[i]
		}
	}
	return nil
}

// coordUnreviewed returns the decisions still to be checked, through the lock.
func coordUnreviewed(h *harness) []core.AfkDecision {
	h.coord.Lock()
	defer h.coord.EndTurn()
	return h.coord.autopilot.Unreviewed()
}
