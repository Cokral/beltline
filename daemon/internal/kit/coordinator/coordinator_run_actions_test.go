// Coordinator tests of run actions: the action context and its repeats, reads that write nothing, a
// missing checkout, the global pause and an erasure as inputs, overrule offers from the applied
// decisions, a Copilot interactive attempt, deciding with a drafted note, and the merge destination a
// join uses.

package coordinator

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// firstRun ticks until the harness has its first run and returns a copy of it (h.Run,
// after Harness(), which starts with the belt's run already admitted).
func firstRun(t *testing.T, h *harness) core.Run {
	t.Helper()
	// A forced intake poll admits the run without starting its first session.
	h.Turn(func() { h.coord.intake.pollTriggers(bgCtx, true) })
	run := h.Run()
	if run == nil {
		t.Fatal("no run was admitted")
	}
	return *run
}

// coordAction is the command's availability for the context.
func coordAction(t *testing.T, command core.RunCommand, context core.RunActionContext) core.RunAction {
	t.Helper()
	action := core.RunAvailabilityActionAt(command, context, core.RunActionTarget{})
	if action == nil {
		t.Fatalf("no availability for %s", command)
	}
	return *action
}

// coordUnscoped reports whether the action names no stage, leg, outcome, attempt or decision.
func coordUnscoped(action core.RunAction) bool {
	return action.Stage == nil && action.Leg == nil && action.Outcome == nil && action.Attempt == nil && action.Decision == nil
}

// inventory is every file under root with its bytes, keyed by the path below root.
func inventory(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		out[strings.TrimPrefix(path, root)] = string(data)
		return nil
	})
	return out
}

// runWithAttempt returns the first run after replacing its attempts with one session started by
// the engine and finished with outcome, then saves it (the fixtures the checkout and copilot tests build).
func runWithAttempt(t *testing.T, h *harness, change func(attempt *core.Attempt, stage core.Stage), outcome string) core.Run {
	t.Helper()
	run := firstRun(t, h)
	stage := *run.Stage()
	run.Attempts = nil
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionStarted, Session: "bl-test", SessionID: "id", Folder: "01-triage-1"}, testkit.ContractTime)
	if n := len(run.Attempts); n > 0 {
		attempt := &run.Attempts[n-1]
		backend := runnerBackend(stage)
		attempt.Settings = &core.SessionSettings{Model: stage.Model, Thinking: stage.Thinking, Runner: &backend}
		change(attempt, stage)
	}
	coordFinishWith(&run, outcome)
	if err := h.storage.SaveRun(run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	return run
}

// runnerBackend is the stage's runner, or pi when it has none.
func runnerBackend(stage core.Stage) core.Backend {
	if stage.Runner != nil {
		return *stage.Runner
	}
	return core.BackendPi
}

// coordFinishWith finishes the current agent session with outcome.
func coordFinishWith(run *core.Run, outcome string) {
	finishWith(run, outcome)
}

// TestCoordinatorContextGathersFactsAndRepeatsExactly
func TestCoordinatorContextGathersFactsAndRepeatsExactly(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run := firstRun(t, h)
	context := c.RunActionContext(run)
	if !reflect.DeepEqual(context.Run, run) {
		t.Fatal("the context does not carry the run")
	}
	if context.GlobalPause || context.Erasing {
		t.Fatalf("global pause %v, erasing %v; want neither", context.GlobalPause, context.Erasing)
	}
	listed := c.RunActions(run)
	if !reflect.DeepEqual(listed, context.Actions()) {
		t.Fatal("the listed actions differ from the context's")
	}
	if again := c.RunActions(run); !reflect.DeepEqual(listed, again) {
		t.Fatal("the listed actions change between reads")
	}
	for i := 0; i < len(core.AllRunCommand) && i < len(listed); i++ {
		if !coordUnscoped(listed[i]) {
			t.Fatalf("action %d (%s) is scoped; the command list leads with unscoped actions", i, listed[i].ID)
		}
	}
}

// TestReadsChangeNeitherTheRunNorItsFiles
func TestReadsChangeNeitherTheRunNorItsFiles(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run := firstRun(t, h)
	before := h.storage.LoadRuns()
	files := inventory(t, h.root)
	c.RunActions(run)
	c.RunActionContext(run)
	if !reflect.DeepEqual(h.storage.LoadRuns(), before) {
		t.Fatal("a read changed the saved runs")
	}
	if !reflect.DeepEqual(inventory(t, h.root), files) {
		t.Fatal("a read changed a file")
	}
	c.Tick(bgCtx)
	c.Idle()
	c.Lock()
	busy := len(c.book.busy)
	c.EndTurn()
	if busy != 0 {
		t.Fatalf("busy runs = %d, want 0", busy)
	}
}

// TestAMissingCheckoutRefusesContinueWithTheSharedReason
func TestAMissingCheckoutRefusesContinueWithTheSharedReason(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	run := runWithAttempt(t, h, func(*core.Attempt, core.Stage) {}, "needs info")
	c := h.RestartWith(idleRunner())
	saved := c.Run(run.ID)
	if saved == nil {
		t.Fatal("the run is gone")
	}
	context := c.RunActionContext(*saved)
	var offered []core.RunAction
	for _, action := range context.Actions() {
		if action.ID == string(core.RunCommandContinueSession) && !coordUnscoped(action) {
			offered = append(offered, action)
		}
	}
	if len(offered) != 1 || offered[0].Enabled {
		t.Fatalf("continue offers = %+v, want one disabled", offered)
	}
	if offered[0].Reason == nil || *offered[0].Reason != "the run's checkout is gone" {
		t.Fatalf("reason = %v, want the checkout reason", offered[0].Reason)
	}
	if refusal := c.ContinueSessionRefusal(*saved, saved.Attempts[0]); refusal == nil || *refusal != "the run's checkout is gone" {
		t.Fatalf("refusal = %v, want the checkout reason", refusal)
	}
}

// idleRunner is a simulated runner that runs nothing and picks no outcome.
func idleRunner() runner.AgentRunner {
	return runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
}

// TestGlobalPauseAndErasingAreTheCoordinatorInputs
func TestGlobalPauseAndErasingAreTheCoordinatorInputs(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run := firstRun(t, h)
	c.SetGloballyPaused(bgCtx, true)
	paused := c.RunActionContext(run)
	if !paused.GlobalPause {
		t.Fatal("the global pause is not in the context")
	}
	if coordAction(t, core.RunCommandStartNow, paused).Enabled || coordAction(t, core.RunCommandPause, paused).Enabled {
		t.Fatal("start now or pause is enabled while the app is paused")
	}
	c.SetGloballyPaused(bgCtx, false)
	c.Lock()
	c.book.markErasing(run.ID)
	c.EndTurn()
	defer func() {
		c.Lock()
		c.book.clearErasing(run.ID)
		c.EndTurn()
	}()
	erasing := c.RunActionContext(run)
	if !erasing.Erasing {
		t.Fatal("the erasure is not in the context")
	}
	// Erase itself waits for the erasure under way, as the app's did.
	for _, command := range core.AllRunCommand {
		if command == core.RunCommandErase {
			continue
		}
		action := coordAction(t, command, erasing)
		if action.Enabled {
			t.Fatalf("%s is enabled while erasing", command)
		}
		if action.Reason == nil || *action.Reason != core.RunAvailabilityErasing {
			t.Fatalf("%s reason = %v, want the erasing reason", command, action.Reason)
		}
	}
}

// TestOverruleOffersComeFromTheRunsAppliedDecisions
func TestOverruleOffersComeFromTheRunsAppliedDecisions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	run := firstRun(t, h)
	coordFinishWith(&run, "needs info")
	attemptID := run.Attempts[len(run.Attempts)-1].ID
	overruleID := "overrule-1"
	run.Attempts[len(run.Attempts)-1].AfkDecision = &overruleID
	if err := h.storage.SaveRun(run); err != nil {
		t.Fatalf("save run: %v", err)
	}
	c := h.RestartWith(idleRunner())
	decision := core.AfkDecision{ID: overruleID, RunID: run.ID, Situation: core.AfkSituationKindApproval, Key: overruleID,
		Status: core.AfkDecisionStatusApplied, StartedAt: jsonxDateAt(testkit.ContractTime.Add(-10 * time.Second)), Folder: ""}
	c.Lock()
	c.autopilot.add(decision)
	c.EndTurn()
	saved := c.Run(run.ID)
	if saved == nil {
		t.Fatal("the run is gone")
	}
	offers := c.OverruleOffers(*saved)
	if len(offers) != 1 || offers[0].Decision != overruleID || offers[0].Attempt != attemptID || len(offers[0].Destinations) == 0 {
		t.Fatalf("offers = %+v, want the one decision at its attempt with destinations", offers)
	}
	context := c.RunActionContext(*saved)
	if !coordAction(t, core.RunCommandOverruleAfk, context).Enabled {
		t.Fatal("overrule is not enabled")
	}
	target := core.RunActionTarget{Stage: new("triage"), Decision: &overruleID}
	if got := core.RunAvailabilityActionAt(core.RunCommandOverruleAfk, context, target); got == nil || !got.Enabled {
		t.Fatalf("overrule at triage = %+v, want enabled", got)
	}
	overruled := core.AfkOverrule{Comments: "no", StageID: "triage", StageName: "Triage", At: jsonxDateAt(testkit.ContractTime),
		AttemptID: attemptID, Phase: core.AfkOverrulePhaseRestarted}
	c.Lock()
	c.autopilot.change(decision.ID, func(d *core.AfkDecision) { d.Overrule = &overruled })
	c.EndTurn()
	after := c.OverruleOffers(*saved)
	if len(after) == 0 || after[0].Refusal == nil || *after[0].Refusal != "This decision was already overruled." {
		t.Fatalf("offers after the overrule = %+v, want the already-overruled refusal", after)
	}
	if got := core.RunAvailabilityActionAt(core.RunCommandOverruleAfk, c.RunActionContext(*saved), core.RunActionTarget{Decision: &overruleID}); got == nil || got.Enabled {
		t.Fatalf("overrule after the overrule = %+v, want disabled", got)
	}
}

// TestACopilotInteractiveAttemptStillOffersAnswerAndContinue
func TestACopilotInteractiveAttemptStillOffersAnswerAndContinue(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	copilot := core.BackendCopilot
	run := runWithAttempt(t, h, func(attempt *core.Attempt, stage core.Stage) {
		attempt.CopilotInteractive = new(true)
		attempt.Settings = &core.SessionSettings{Model: stage.Model, Thinking: stage.Thinking, Runner: &copilot}
	}, "needs info")
	c := h.RestartWith(idleRunner())
	saved := c.Run(run.ID)
	if saved == nil {
		t.Fatal("the run is gone")
	}
	context := c.RunActionContext(*saved)
	if !coordAction(t, core.RunCommandAnswer, context).Enabled {
		t.Fatal("answer is not enabled")
	}
	answerable := saved.Answerable()
	if answerable == nil {
		t.Fatal("nothing to answer")
	}
	target := core.RunActionTarget{Stage: new("triage"), Attempt: &answerable.ID}
	if got := core.RunAvailabilityActionAt(core.RunCommandAnswer, context, target); got == nil || !got.Enabled {
		t.Fatalf("answer at the attempt = %+v, want enabled", got)
	}
	var offered []core.RunAction
	for _, action := range context.Actions() {
		if action.ID == string(core.RunCommandAnswer) && !coordUnscoped(action) {
			offered = append(offered, action)
		}
	}
	if len(offered) != 1 || offered[0].Attempt == nil || *offered[0].Attempt != answerable.ID {
		t.Fatalf("answer offers = %+v, want one at %s", offered, answerable.ID)
	}
}

// TestAMergeWithNoBranchNamedGoesIntoTheCheckoutLegsBranch
func TestAMergeWithNoBranchNamedGoesIntoTheCheckoutLegsBranch(t *testing.T) {
	t.Parallel()
	run := sessionSampleRun()
	run.Branch = new("run-branch")
	run.MergeInto = nil
	run.MergeCheckout = nil
	if got := mergeDestination(run); got != "run-branch" {
		t.Fatalf("destination = %q, want run-branch", got)
	}
	run.Branch = nil
	if got := mergeDestination(run); got != core.RunBranch(run) {
		t.Fatalf("destination without a run branch = %q, want %q", got, core.RunBranch(run))
	}
	// A route that never made a branch of its own merges into the one its folder gets.
	run.MergeCheckout = new("r:build")
	if got := mergeDestination(run); got != core.RunBranch(run)+"-build" {
		t.Fatalf("destination for the build checkout = %q, want %q", got, core.RunBranch(run)+"-build")
	}
	run.MergeInto = new("named")
	if got := mergeDestination(run); got != "named" {
		t.Fatalf("destination named = %q, want named", got)
	}
}

// jsonxDateAt is a wire date at t.
func jsonxDateAt(t time.Time) jsonx.Date { return jsonx.Date{Time: t} }
