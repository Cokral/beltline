// The run commands at the wire: each command's first accepted reply is its golden (the runScenarios
// in servertest_contract_runs_*.go), and here the reply's run is checked, a repeated command is refused
// or accepted as the app does, and the refusals leave the data folder alone.
package servertest

import (
	"context"
	"net/http"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// TestRunCommandsChangeTheRunAndRefuseARepeat serves each state-changing command's scenario, checks its
// golden, checks the run it answers, and refuses the same command a second time with its reason.
func TestRunCommandsChangeTheRunAndRefuseARepeat(t *testing.T) {
	t.Parallel()
	rows := []struct {
		contractCase string
		check        func(t *testing.T, after core.Run)
	}{
		{"runDecide", nil},
		{"runAbort", func(t *testing.T, after core.Run) {
			if after.State != core.RunStateHeld || after.Reason == nil || *after.Reason != "stopped by you" {
				t.Fatalf("abort: state %s reason %v, want held, stopped by you", after.State, after.Reason)
			}
		}},
		{"runDrop", func(t *testing.T, after core.Run) {
			if after.State != core.RunStateDropped || after.Reason == nil || *after.Reason != "dropped by you" {
				t.Fatalf("drop: state %s reason %v, want dropped, dropped by you", after.State, after.Reason)
			}
		}},
		{"runPark", func(t *testing.T, after core.Run) {
			if !after.IsButtonParked() {
				t.Fatal("park: the run is not parked")
			}
		}},
		{"runUnpark", func(t *testing.T, after core.Run) {
			if after.IsButtonParked() {
				t.Fatal("unpark: the run is still parked")
			}
		}},
		{"runPause", func(t *testing.T, after core.Run) {
			if after.Pause == nil || after.Pause.Phase != core.RunPausePhasePaused {
				t.Fatalf("pause: pause %+v, want paused", after.Pause)
			}
		}},
		{"runResumeRun", func(t *testing.T, after core.Run) {
			if after.Pause != nil {
				t.Fatal("resume-run: the run still has a pause")
			}
		}},
		{"runStartNow", func(t *testing.T, after core.Run) {
			if after.State != core.RunStateRunning {
				t.Fatalf("start-now: state %s, want running", after.State)
			}
		}},
	}
	for _, row := range rows {
		t.Run(row.contractCase, func(t *testing.T) {
			t.Parallel()
			f, reply := runScenarios[row.contractCase](t)
			checkRunCommand(t, f, reply, row.contractCase)
			run := decodeRunReply(t, reply)
			if row.check != nil {
				row.check(t, run)
			}
			body := []byte(nil)
			if row.contractCase == "runDecide" {
				body = mustJSON(t, api.RunDecideRequest{Outcome: "unknown", Note: new("again")})
			}
			if again := postRunCase(f, casePath(row.contractCase, run.ID, ""), body); again.Status != http.StatusUnprocessableEntity {
				t.Fatalf("a repeated %s: HTTP %d, want 422", row.contractCase, again.Status)
			}
		})
	}
}

// TestConcurrentClicksOnADropAcceptExactlyOne checks that two clicks that land together drop the run once.
func TestConcurrentClicksOnADropAcceptExactlyOne(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	path := casePath("runDrop", run.ID, "")
	reply := concurrentWinner(t, postTwice(f, path, nil), "runDrop")
	checkRunCommand(t, f, reply, "runDrop")
}

// TestResumeRestartsAHeldRunAndAcceptsTheSameResumeAgain checks the held run's resume: the note is trimmed,
// and the same resume sent again is accepted with the same state.
func TestResumeRestartsAHeldRunAndAcceptsTheSameResumeAgain(t *testing.T) {
	t.Parallel()
	f, reply := runScenarios["runResume"](t)
	checkRunCommand(t, f, reply, "runResume")
	after := decodeRunReply(t, reply)
	if after.State != core.RunStateQueued || after.Note == nil || *after.Note != "go on" {
		t.Fatalf("resume: state %s note %v, want queued with go on", after.State, after.Note)
	}
	body := mustJSON(t, api.RunResumeRequest{Stage: new(after.StageID), Note: new("  go on  "), Earlier: new(false)})
	repeated := postRunCase(f, casePath("runResume", after.ID, ""), body)
	if repeated.Status != http.StatusOK {
		t.Fatalf("a repeated resume: HTTP %d, want 200", repeated.Status)
	}
	if again := decodeRunReply(t, repeated); again.State != core.RunStateQueued || again.Note == nil || *again.Note != "go on" {
		t.Fatalf("a repeated resume: state %s note %v, want queued with go on", again.State, again.Note)
	}
}

// TestAnswerContinuesTheHeldSession checks that an answer continues the held attempt, and that the run
// is queued unless the held attempt is still its last.
func TestAnswerContinuesTheHeldSession(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "held with an answerable session", func(run core.Run) bool {
		return run.State == core.RunStateHeld && run.Answerable() != nil
	})
	held := run.Answerable()
	body := mustJSON(t, api.RunAnswerRequest{Answer: new("Use the second option.")})
	path := casePath("runAnswer", run.ID, "")
	reply := concurrentWinner(t, postTwice(f, path, body), "runAnswer")
	checkRunCommand(t, f, reply, "runAnswer")
	after := decodeRunReply(t, reply)
	if after.Continues == nil || *after.Continues != held.ID {
		t.Fatalf("continues %v, want %s", after.Continues, held.ID)
	}
	if last := after.LastAttempt(run.StageID); last != nil && last.ID == held.ID && after.State != core.RunStateQueued {
		t.Fatal("the held attempt is still the last one and the run is not queued")
	}
	if repeated := postRunCase(f, path, body); repeated.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a repeated answer: HTTP %d, want 422", repeated.Status)
	}
}

// TestAbortAndDropOfQueuedAndPausedWork checks that an abort holds queued work without a new attempt, and a
// drop ends a paused run with its pause cleared and its attempts kept.
func TestAbortAndDropOfQueuedAndPausedWork(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	queued := findRun(t, f.Coordinator, "queued with an abortable route", func(run core.Run) bool {
		return run.State == core.RunStateQueued && canAbortRun(run)
	})
	reply := postRunCase(f, casePath("runAbort", queued.ID, ""), nil)
	if reply.Status != http.StatusOK {
		t.Fatalf("abort queued work: HTTP %d %s", reply.Status, reply.Text())
	}
	after := decodeRunReply(t, reply)
	if after.State != core.RunStateHeld || len(after.Attempts) != len(queued.Attempts) {
		t.Fatalf("after abort: state %s attempts %d, want held with %d attempts", after.State, len(after.Attempts), len(queued.Attempts))
	}

	running := findRun(t, f.Coordinator, "running at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateRunning && stageKind(run) == core.StageKindAgent
	})
	f.Coordinator.PauseRun(context.Background(), running.ID)
	if paused := f.Coordinator.ReadRun(running.ID); paused == nil || paused.Pause == nil {
		t.Fatal("the run did not pause")
	}
	reply = postRunCase(f, casePath("runDrop", running.ID, ""), nil)
	if reply.Status != http.StatusOK {
		t.Fatalf("drop a paused run: HTTP %d %s", reply.Status, reply.Text())
	}
	dropped := decodeRunReply(t, reply)
	if dropped.State != core.RunStateDropped || dropped.Pause != nil || len(dropped.Attempts) != len(running.Attempts) {
		t.Fatalf("after drop: state %s pause %v attempts %d", dropped.State, dropped.Pause, len(dropped.Attempts))
	}
}

// TestSendToMovesARunningRouteAndRefusesBadTargetsWithoutChangingTheRun: Send to moves a running route to
// another stage, stopping its attempt. An unknown stage or route is refused with nothing changed on disk.
func TestSendToMovesARunningRouteAndRefusesBadTargetsWithoutChangingTheRun(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "running at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateRunning && stageKind(run) == core.StageKindAgent
	})
	other := ""
	for _, stage := range run.Stages {
		if stage.ID != run.StageID {
			other = stage.ID
			break
		}
	}
	path := casePath("runResume", run.ID, "")
	before := inventory(t, f.Root)
	for name, body := range map[string][]byte{
		"unknown stage": mustJSON(t, api.RunResumeRequest{Stage: new("missing"), Earlier: new(false)}),
		"unknown route": mustJSON(t, api.RunResumeRequest{Stage: new(other), Earlier: new(false), Leg: new("r:missing")}),
	} {
		if reply := postRunCase(f, path, body); reply.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: HTTP %d, want 422", name, reply.Status)
		}
	}
	if after := inventory(t, f.Root); !sameInventory(before, after) {
		t.Fatal("a refused send to changed the data folder")
	}

	reply := postRunCase(f, path, mustJSON(t, api.RunResumeRequest{Stage: new(other), Note: new("  carry on  "), Earlier: new(false)}))
	if reply.Status != http.StatusOK {
		t.Fatalf("send to %s: HTTP %d %s", other, reply.Status, reply.Text())
	}
	after := decodeRunReply(t, reply)
	if after.StageID != other || after.State != core.RunStateQueued || after.Note == nil || *after.Note != "carry on" {
		t.Fatalf("after send to: stage %s state %s note %v", after.StageID, after.State, after.Note)
	}
	if closed := after.ClosedAttempts(run.StageID, nil); len(closed) == 0 {
		t.Fatal("the displaced stage has no closed attempt")
	}
}

// TestRetryContinuationRefusesAWrongRouteAndQueuesTheMainOne checks the shared refusal for a wrong route, that
// the refusal changes neither the run nor the data folder, and that the main route queues the retry.
func TestRetryContinuationRefusesAWrongRouteAndQueuesTheMainOne(t *testing.T) {
	t.Parallel()
	f, reply := runScenarios["runRetryContinuation"](t)
	checkRunCommand(t, f, reply, "runRetryContinuation")
	after := decodeRunReply(t, reply)
	var status core.AttemptInterruptionContinuationStatus
	for _, attempt := range after.Attempts {
		if attempt.Interruption != nil && attempt.Interruption.Continuation != nil {
			status = attempt.Interruption.Continuation.Status
		}
	}
	if status != core.AttemptInterruptionContinuationStatusQueued {
		t.Fatalf("continuation status %q, want queued", status)
	}
	if again := postRunCase(f, casePath("runRetryContinuation", after.ID, ""), mustJSON(t, api.RunLegRequest{Leg: new("main")})); again.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a repeated retry: HTTP %d, want 422", again.Status)
	}

	// A wrong route is refused with the shared refusal, and nothing changes.
	f.Coordinator.Idle()
	before := *f.Coordinator.ReadRun(after.ID)
	files := inventory(t, f.Root)
	wrong := postRunCase(f, casePath("runRetryContinuation", after.ID, ""), mustJSON(t, api.RunLegRequest{Leg: new("no-such-route")}))
	if wrong.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a wrong route: HTTP %d, want 422", wrong.Status)
	}
	expected := core.RunAvailabilityRetryContinuationRefusal(f.Coordinator.RunActionContext(before), new("no-such-route"))
	if expected == nil || errorMessage(t, wrong) != *expected {
		t.Fatalf("a wrong route's message = %q, want the shared refusal %v", errorMessage(t, wrong), expected)
	}
	if got := *f.Coordinator.ReadRun(after.ID); !sameRun(got, before) {
		t.Fatal("a refused retry changed the run")
	}
	if got := inventory(t, f.Root); !sameInventory(got, files) {
		t.Fatal("a refused retry changed the data folder")
	}
}

// TestAdjustStageSavesThisRunOnly
func TestAdjustStageSavesThisRunOnly(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	stage := run.Stage()
	models := f.Coordinator.AdjustmentModels(run, *stage, nil)
	if len(models) == 0 {
		t.Fatal("the stage has no admitted model")
	}
	model := models[0]
	body := mustJSON(t, api.RunAdjustStageRequest{Stage: stage.ID, Model: model.ID, Brief: new("A run-local brief.")})
	path := casePath("runAdjustStage", run.ID, "")
	accepted := concurrentReusable(t, postTwice(f, path, body), "runAdjustStage", 2)
	for _, reply := range accepted {
		checkRunCommand(t, f, reply, "runAdjustStage")
	}
	after := decodeRunReply(t, accepted[0])
	stageAfter := findStageIn(after, stage.ID)
	if stageAfter == nil || stageAfter.Model != model.ID {
		t.Fatalf("the run's stage model = %v, want %s", stageAfter, model.ID)
	}
	if stageAfter.Brief != "A run-local brief." {
		t.Fatalf("the run's stage brief = %q", stageAfter.Brief)
	}
	adjustments := len(after.Adjustments)
	repeated := postRunCase(f, path, body)
	if repeated.Status != http.StatusOK {
		t.Fatalf("a repeated adjustment: HTTP %d, want 200", repeated.Status)
	}
	if got := len(f.Coordinator.ReadRun(run.ID).Adjustments); got != adjustments {
		t.Fatalf("adjustments %d, want %d", got, adjustments)
	}
	belt := f.Coordinator.ReadBelt(run.BeltID)
	if belt == nil {
		t.Fatal("the belt is gone")
	}
	beltStage := findBeltStage(belt.Belt, stage.ID)
	if beltStage == nil || (beltStage.Model == model.ID && beltStage.Brief == "A run-local brief.") {
		t.Fatal("the belt's stage took the run's adjustment")
	}
}

// TestAdjustSwitchKeepsAnAdmittedModelAndDropsAThinkingLevelTheRunnerCannotTake
func TestAdjustSwitchKeepsAnAdmittedModelAndDropsAThinkingLevelTheRunnerCannotTake(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	before := inventory(t, f.Root)
	body := mustJSON(t, api.RunAdjustSwitchRequest{Stage: run.Stage().ID, Runner: "copilot", Model: new("not-admitted"), Thinking: new("max")})
	reply := postRunCase(f, casePath("runAdjustSwitch", run.ID, ""), body)
	checkRunCommand(t, f, reply, "runAdjustSwitch")
	var switched api.RunAdjustSwitchResponse
	if err := routes.JSONDecode(reply.Body, &switched); err != nil {
		t.Fatalf("switch response: %v", err)
	}
	if switched.Model == "not-admitted" {
		t.Fatal("the switch kept a model the runner does not admit")
	}
	if !sameInventory(inventory(t, f.Root), before) {
		t.Fatal("a switch is a draft and writes nothing")
	}
}

// TestAfkPersistsTheRunChoice
func TestAfkPersistsTheRunChoice(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "active and not queued", func(run core.Run) bool {
		return run.State.IsActive() && run.State != core.RunStateQueued
	})
	mode := core.AfkModeAlways
	if run.Afk() == core.AfkModeAlways {
		mode = core.AfkModeNever
	}
	body := mustJSON(t, api.RunAfkRequest{Mode: string(mode)})
	path := casePath("runAfk", run.ID, "")
	accepted := concurrentReusable(t, postTwice(f, path, body), "runAfk", 2)
	checkRunCommand(t, f, accepted[0], "runAfk")
	checkRunCommand(t, f, accepted[1], "runAfk")
	if got := decodeRunReply(t, accepted[0]).Afk(); got != mode {
		t.Fatalf("afk %s, want %s", got, mode)
	}
	if again := postRunCase(f, path, body); again.Status != http.StatusOK {
		t.Fatalf("a repeated afk: HTTP %d, want 200", again.Status)
	}
}

// TestOverruleAfkRestartsAnAppliedDecision
func TestOverruleAfkRestartsAnAppliedDecision(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "active at an agent stage with a session", func(run core.Run) bool {
		return run.State.IsActive() && stageKind(run) == core.StageKindAgent && hasSessionAttempt(run)
	})
	decision := appliedOverruleDecision(t, f, run)
	body := mustJSON(t, api.RunOverruleAfkRequest{Decision: decision.ID, Stage: run.StageID, Comments: "Do it my way.", AcceptedWarning: true})
	replies := postTwice(f, casePath("runOverruleAfk", run.ID, ""), body)
	var accepted []Reply
	for _, reply := range replies {
		if reply.Status == http.StatusOK {
			accepted = append(accepted, reply)
		}
		if reply.Status != http.StatusOK && reply.Status != http.StatusUnprocessableEntity {
			t.Fatalf("unexpected response %d", reply.Status)
		}
	}
	if len(accepted) == 0 {
		t.Fatal("no overrule was accepted")
	}
	for _, reply := range accepted {
		checkRunCommand(t, f, reply, "runOverruleAfk")
	}
	after := decodeRunReply(t, accepted[0])
	if after.Restart == nil || after.Restart.DecisionID != decision.ID {
		t.Fatalf("restart %+v, want decision %s", after.Restart, decision.ID)
	}
	if after.Restart.Phase != core.RestartIntentPhaseApplied {
		t.Fatalf("restart phase %s, want applied", after.Restart.Phase)
	}
	if again := postRunCase(f, casePath("runOverruleAfk", run.ID, ""), body); again.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a repeated overrule: HTTP %d, want 422", again.Status)
	}
}

// TestEraseRemovesAFinishedRunAndItsGolden
func TestEraseRemovesAFinishedRunAndItsGolden(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	run := findRun(t, f.Coordinator, "finished", func(run core.Run) bool {
		return !run.State.IsActive()
	})
	path := casePath("runErase", run.ID, "")
	accepted := concurrentReusable(t, postTwice(f, path, nil), "runErase", 2)
	for _, reply := range accepted {
		checkRunCommand(t, f, reply, "runErase")
	}
	var erased api.EraseResponse
	if err := routes.JSONDecode(accepted[0].Body, &erased); err != nil {
		t.Fatalf("erase response: %v", err)
	}
	if erased.ID != run.ID || !erased.Erased {
		t.Fatalf("erase response %+v, want %s erased", erased, run.ID)
	}
	if f.Coordinator.ReadRun(run.ID) != nil {
		t.Fatal("the erased run is still there")
	}
	repeated := postRunCase(f, path, nil)
	if repeated.Status != http.StatusOK {
		t.Fatalf("a repeated erase: HTTP %d, want 200", repeated.Status)
	}
	var again api.EraseResponse
	if err := routes.JSONDecode(repeated.Body, &again); err != nil || !again.Erased {
		t.Fatalf("a repeated erase did not answer erased: %+v %v", again, err)
	}
	CheckGolden(t, "runErase", caseEndpoint("runErase").GoldenName, repeated, f.Root)
	if gone := getCase(f, casePath("run", run.ID, "")); gone.Status != http.StatusNotFound {
		t.Fatalf("the erased run: HTTP %d, want 404", gone.Status)
	}
}

// findStageIn is the stage with id in run, or nil.
func findStageIn(run core.Run, id string) *core.Stage {
	for i := range run.Stages {
		if run.Stages[i].ID == id {
			return &run.Stages[i]
		}
	}
	return nil
}

// findBeltStage is the stage with id in belt, or nil.
func findBeltStage(belt core.Belt, id string) *core.Stage {
	for i := range belt.Stages {
		if belt.Stages[i].ID == id {
			return &belt.Stages[i]
		}
	}
	return nil
}

// sameInventory reports whether two inventories hold the same files with the same bytes.
func sameInventory(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for path, data := range a {
		if other, ok := b[path]; !ok || other != data {
			return false
		}
	}
	return true
}
