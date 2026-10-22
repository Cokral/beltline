// Coordinator tests of the AFK gates: turning a run's AFK off while an AFK call waits at a gate,
// re-enabling after a cancellation, a restart with AFK off, and the effort a Codex assistant
// session requests.

package coordinator

import (
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// coordFinished is a session that has exited with code 0.
func coordFinished() runner.SessionStatus {
	zero := int32(0)
	return runner.SessionStatus{Kind: runner.SessionStatusFinished, ExitCode: &zero}
}

// appliedCount counts the applied decisions.
func appliedCount(decisions []core.AfkDecision) int {
	return coordCount(decisions, func(d core.AfkDecision) bool { return d.Status == core.AfkDecisionStatusApplied })
}

// coordGated builds a harness whose AFK calls go through a holding runner, and runs it to its approval
// . The returned pointer is set when the runner is built.
func coordGated(t *testing.T) (*harness, **coordHolding) {
	t.Helper()
	var holding *coordHolding
	h := gateWith(t, func(script *harnessScript) runner.AgentRunner {
		holding = newH5m4Holding(script)
		return holding
	})
	return h, &holding
}

// TestTurningRunAfkOffDropsAPendingDecisionAndIgnoresALateResult
func TestTurningRunAfkOffDropsAPendingDecisionAndIgnoresALateResult(t *testing.T) {
	t.Parallel()
	h, holdingRef := coordGated(t)
	holding := *holdingRef
	id := h.Run().ID
	stage := h.Run().StageID
	h.coord.SetRunAfk(bgCtx, id, core.AfkModeAlways)
	h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
	holding.holdAt(gateStatus)
	ticking := tickUntil(h, holding, gateStatus)
	if !holding.waitUntil(t, gateStatus) {
		t.Fatal("the assistant's status call never reached its gate")
	}
	decision := decidingFor(coordDecisions(h), id)
	if decision == nil {
		t.Fatal("no decision in progress")
	}
	writeAfkDecision(t, "decide approved", *h.Run(), *decision, h.coord.Paths())
	holding.setStatus(coordFinished())
	h.coord.SetRunAfk(bgCtx, id, core.AfkModeNever)
	got := decisionOf(coordDecisions(h), decision.ID)
	if got == nil || got.Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("decision after turning AFK off = %+v, want superseded", got)
	}
	if got.Detail == nil || *got.Detail != autopilotTurnedOffReason {
		t.Fatalf("detail = %v, want the turned-off reason", got.Detail)
	}
	stopped := holding.stoppedSessions()
	if decision.Session == nil || !slices.Contains(stopped, *decision.Session) {
		t.Fatalf("stopped sessions = %v, want %v", stopped, decision.Session)
	}
	holding.releaseAll()
	<-ticking
	h.Tick()
	if got := decisionOf(coordDecisions(h), decision.ID); got == nil || got.Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("decision after the late result = %+v, want still superseded", got)
	}
	if run := h.Run(); run == nil || run.State != core.RunStateWaiting || run.StageID != stage {
		t.Fatalf("run = %+v, want waiting at %s", run, stage)
	}
	if attempt := h.Run().LastAttempt(stage); attempt != nil && attempt.Session != nil && slices.Contains(stopped, *attempt.Session) {
		t.Fatal("the run's own session was stopped")
	}
}

// TestTurningRunAfkOffDuringARunnerCallKeepsTheRun
func TestTurningRunAfkOffDuringARunnerCallKeepsTheRun(t *testing.T) {
	t.Parallel()
	for _, gate := range []coordGates{gateStart, gateStatus, gateReply} {
		t.Run(string(gate), func(t *testing.T) {
			t.Parallel()
			h, holdingRef := coordGated(t)
			holding := *holdingRef
			id := h.Run().ID
			stage := h.Run().StageID
			if gate == gateStart {
				holding.holdAt(gateStart)
			}
			h.coord.SetRunAfk(bgCtx, id, core.AfkModeAlways)
			var ticking <-chan struct{}
			if gate == gateStart {
				if !holding.waitUntil(t, gateStart) {
					t.Fatal("the assistant's start never reached its gate")
				}
				done := make(chan struct{})
				go func() { defer close(done); h.coord.Tick(bgCtx) }()
				ticking = done
			} else {
				h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
				holding.holdAt(gate)
				if gate == gateReply {
					holding.setStatus(coordFinished())
				}
				ticking = tickUntil(h, holding, gate)
				if !holding.waitUntil(t, gate) {
					t.Fatalf("the assistant's %s call never reached its gate", gate)
				}
			}
			decision := decidingFor(coordDecisions(h), id)
			if decision == nil {
				t.Fatal("no decision in progress")
			}
			reply := runner.SimulatedAFKDecision("decide approved")
			holding.setReply(&reply)
			holding.setStatus(coordFinished())
			writeAfkDecision(t, "decide approved", *h.Run(), *decision, h.coord.Paths())
			h.coord.SetRunAfk(bgCtx, id, core.AfkModeNever)
			holding.releaseAll()
			<-ticking
			for range 3 {
				h.Tick()
			}
			got := decisionOf(coordDecisions(h), decision.ID)
			if got == nil || got.Status != core.AfkDecisionStatusSuperseded {
				t.Fatalf("decision = %+v, want superseded", got)
			}
			if got.Detail == nil || *got.Detail != autopilotTurnedOffReason {
				t.Fatalf("detail = %v, want the turned-off reason", got.Detail)
			}
			if run := h.Run(); run == nil || run.State != core.RunStateWaiting || run.StageID != stage {
				t.Fatalf("run = %+v, want waiting at %s", run, stage)
			}
			if n := appliedCount(coordDecisions(h)); n != 0 {
				t.Fatalf("applied decisions = %d, want 0", n)
			}
		})
	}
}

// sortedOf returns the given strings sorted.
func sortedOf(values ...string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

// TestARestartFollowsAPendingDecisionOnlyWhileTheRunStaysOn
func TestARestartFollowsAPendingDecisionOnlyWhileTheRunStaysOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{Belts: []core.Belt{coordOptedInBelt()}, AfkDuration: 100_000})
	h.Script.add("triage", "ready to spec")
	h.Script.add("spec", "complete")
	h.Until("run := h.nowRun(); return run != nil && run.State == core.RunStateWaiting", func() bool { run := h.nowRun(); return run != nil && run.State == core.RunStateWaiting })
	h.coord.SetAfk(bgCtx, true)
	h.Until("len(coordDeciding(decisionsNow(h))) == 1", func() bool { return len(coordDeciding(decisionsNow(h))) == 1 })
	id := h.Run().ID
	stage := h.Run().StageID
	decision := decidingFor(coordDecisions(h), id)
	if decision == nil {
		t.Fatal("no decision in progress")
	}
	writeAfkDecision(t, "decide approved", *h.Run(), *decision, h.coord.Paths())
	setAfk(h, func(afk *core.AfkSettings) { afk.Enabled = false })
	pick := func(runner.SessionSpec) *string { choice := "decide approved"; return &choice }
	off := h.RestartWith(runner.NewSimulatedRunner(
		func(runner.SessionSpec) float64 { return 0 },
		func() time.Time { return testkit.ContractTime },
		pick,
	))
	off.Tick(bgCtx)
	off.Idle()
	got := decisionOf(off.ReadAfkDecisions(), decision.ID)
	if got == nil || got.Status != core.AfkDecisionStatusSuperseded {
		t.Fatalf("decision after the restart = %+v, want superseded", got)
	}
	if got.Detail == nil || *got.Detail != autopilotGlobalOffDetail {
		t.Fatalf("detail = %v, want the global-off detail", got.Detail)
	}
	if run := off.Run(id); run == nil || run.State != core.RunStateWaiting || run.StageID != stage {
		t.Fatalf("run = %+v, want waiting at %s", run, stage)
	}
	if n := appliedCount(off.ReadAfkDecisions()); n != 0 {
		t.Fatalf("applied decisions = %d, want 0", n)
	}
}
