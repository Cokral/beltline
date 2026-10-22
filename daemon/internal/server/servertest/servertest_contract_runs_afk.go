// The runs contract cases for the AFK mode, stage adjustments, erasing and tasks. Each case is
// registered in servertest_contract_runs.go with the rest of the runs area.
package servertest

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// scenarioAdjustStage adjusts the first queued agent run's stage model and brief (run-adjust-stage.json).
func scenarioAdjustStage(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	stage := run.Stage()
	models := g.Coordinator.AdjustmentModels(run, *stage, nil)
	if len(models) == 0 {
		t.Fatal("the stage has no admitted model to adjust to")
	}
	body := mustJSON(t, api.RunAdjustStageRequest{Stage: stage.ID, Model: models[0].ID, Brief: new("A run-local brief.")})
	return g, postRunCase(g, casePath("runAdjustStage", run.ID, ""), body)
}

// scenarioAdjustSwitch switches the first queued agent run's stage to another runner (run-adjust-switch.json).
func scenarioAdjustSwitch(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	body := mustJSON(t, api.RunAdjustSwitchRequest{Stage: run.Stage().ID, Runner: "copilot", Model: new("not-admitted"), Thinking: new("max")})
	return g, postRunCase(g, casePath("runAdjustSwitch", run.ID, ""), body)
}

// scenarioAfk sets the first active, not queued run to the other AFK mode (run-afk.json).
func scenarioAfk(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "active and not queued", func(run core.Run) bool {
		return run.State.IsActive() && run.State != core.RunStateQueued
	})
	mode := core.AfkModeAlways
	if run.Afk() == core.AfkModeAlways {
		mode = core.AfkModeNever
	}
	body := mustJSON(t, api.RunAfkRequest{Mode: string(mode)})
	return g, postRunCase(g, casePath("runAfk", run.ID, ""), body)
}

// scenarioErase erases the first finished run (run-erase.json).
func scenarioErase(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "finished", func(run core.Run) bool {
		return !run.State.IsActive()
	})
	return g, postRunCase(g, casePath("runErase", run.ID, ""), nil)
}

// scenarioOverruleAfk restarts an applied AFK decision on the first active agent run with a session
// (run-overrule-afk.json).
func scenarioOverruleAfk(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "active at an agent stage with a session", func(run core.Run) bool {
		return run.State.IsActive() && stageKind(run) == core.StageKindAgent && hasSessionAttempt(run)
	})
	decision := appliedOverruleDecision(t, g, run)
	body := mustJSON(t, api.RunOverruleAfkRequest{Decision: decision.ID, Stage: run.StageID, Comments: "Do it my way.", AcceptedWarning: true})
	return g, postRunCase(g, casePath("runOverruleAfk", run.ID, ""), body)
}

// appliedOverruleDecision records an applied AFK decision on the run's last attempt with a session,
// and adds it to the AFK log. It is the decision the overrule tests restart from.
func appliedOverruleDecision(t testing.TB, g *Fixture, run core.Run) core.AfkDecision {
	t.Helper()
	decision := core.NewAfkDecision(core.AfkDecisionArgs{
		ID: "contract-overrule", Run: run,
		Situation: core.AfkSituation{Kind: core.AfkSituationKindApproval, Key: "contract-overrule"},
		StartedAt: jsonx.Date{Time: ContractNow.Add(-10 * time.Second)},
	})
	decision.Status = core.AfkDecisionStatusApplied
	if !g.Coordinator.UpdateRunForContract(run.ID, func(saved *core.Run) {
		for i := len(saved.Attempts) - 1; i >= 0; i-- {
			if saved.Attempts[i].Session != nil {
				id := decision.ID
				saved.Attempts[i].AfkDecision = &id
				return
			}
		}
	}) {
		t.Fatal("the run could not be changed")
	}
	g.Coordinator.AddAfkDecisionForContract(decision)
	return decision
}

// scenarioTasks starts a task on the first manual entry of the first unpaused belt that has one (tasks.json).
func scenarioTasks(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	belt, entry := manualBeltEntry(t, g)
	body := mustJSON(t, api.RunTaskRequest{Belt: belt.ID, Entry: new(entry.ID), Repo: entry.Repos[0], Title: "  A task  ", Body: "  Body  "})
	return g, postRunCase(g, casePath("tasks", "", ""), body)
}

// manualBeltEntry is the first unpaused belt with a manual trigger, and its first manual trigger.
func manualBeltEntry(t testing.TB, g *Fixture) (core.Belt, core.TriggerEntry) {
	t.Helper()
	for _, record := range g.Coordinator.ReadBelts() {
		if record.EffectivelyPaused() {
			continue
		}
		for _, entry := range record.Belt.Triggers {
			if entry.Trigger.Kind == core.TriggerKindManual && len(entry.Repos) > 0 {
				return record.Belt, entry
			}
		}
	}
	t.Fatal("the contract demo has no unpaused belt with a manual trigger")
	return core.Belt{}, core.TriggerEntry{}
}
