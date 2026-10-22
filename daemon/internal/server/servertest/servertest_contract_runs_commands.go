// The runs contract cases that change a run: its decisions, answers, pauses, parks, aborts, drops and
// starts. Each case is registered in servertest_contract_runs.go with the rest of the runs area.
package servertest

import (
	"net/http"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// scenarioDecide decides the first waiting gate with its first outcome and a note (run-decide.json).
func scenarioDecide(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "waiting at a gate", func(run core.Run) bool {
		return run.State == core.RunStateWaiting && stageKind(run) == core.StageKindGate
	})
	outcome := run.Stage().Outcomes[0].Name
	body := mustJSON(t, api.RunDecideRequest{Outcome: outcome, Note: new("Looks right.")})
	return g, postRunCase(g, casePath("runDecide", run.ID, ""), body)
}

// scenarioResume resumes the first held run that has a session to answer, from its own stage with a note (run-resume.json).
func scenarioResume(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "held with an answerable session", func(run core.Run) bool {
		return run.State == core.RunStateHeld && run.Answerable() != nil
	})
	body := mustJSON(t, api.RunResumeRequest{Stage: new(run.StageID), Note: new("  go on  "), Earlier: new(false)})
	return g, postRunCase(g, casePath("runResume", run.ID, ""), body)
}

// scenarioAnswer answers the first held run's session (run-answer.json).
func scenarioAnswer(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "held with an answerable session", func(run core.Run) bool {
		return run.State == core.RunStateHeld && run.Answerable() != nil
	})
	body := mustJSON(t, api.RunAnswerRequest{Answer: new("Use the second option.")})
	return g, postRunCase(g, casePath("runAnswer", run.ID, ""), body)
}

// scenarioAbort aborts the first running run's route that can be aborted (run-abort.json).
func scenarioAbort(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "running with an abortable route", func(run core.Run) bool {
		return run.State == core.RunStateRunning && canAbortRun(run)
	})
	return g, postRunCase(g, casePath("runAbort", run.ID, ""), nil)
}

// scenarioDrop drops the first queued agent run (run-drop.json).
func scenarioDrop(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	return g, postRunCase(g, casePath("runDrop", run.ID, ""), nil)
}

// parkableRun is the first queued run that can be parked from its button.
func parkableRun(t testing.TB, g *Fixture) core.Run {
	return findRun(t, g.Coordinator, "queued and parkable", func(run core.Run) bool {
		return run.State == core.RunStateQueued && run.CanButtonPark()
	})
}

// scenarioPark parks the first parkable run (run-park.json).
func scenarioPark(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := parkableRun(t, g)
	return g, postRunCase(g, casePath("runPark", run.ID, ""), nil)
}

// scenarioUnpark parks the first parkable run and then unparks it; the reply is the unpark (run-unpark.json).
func scenarioUnpark(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := parkableRun(t, g)
	if parked := postRunCase(g, casePath("runPark", run.ID, ""), nil); parked.Status != http.StatusOK {
		t.Fatalf("park: HTTP %d", parked.Status)
	}
	return g, postRunCase(g, casePath("runUnpark", run.ID, ""), nil)
}

// scenarioPause pauses the first running watch that is not paused (run-pause.json).
func scenarioPause(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "running at a watch and not paused", func(run core.Run) bool {
		return run.State == core.RunStateRunning && stageKind(run) == core.StageKindWatch && run.Pause == nil
	})
	return g, postRunCase(g, casePath("runPause", run.ID, ""), nil)
}

// scenarioResumeRun pauses a running watch, then resumes the first other paused run (run-resume-run.json).
func scenarioResumeRun(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	first := findRun(t, g.Coordinator, "running at a watch and not paused", func(run core.Run) bool {
		return run.State == core.RunStateRunning && stageKind(run) == core.StageKindWatch && run.Pause == nil
	})
	if paused := postRunCase(g, casePath("runPause", first.ID, ""), nil); paused.Status != http.StatusOK {
		t.Fatalf("pause: HTTP %d", paused.Status)
	}
	afterPause := g.Coordinator.ReadRun(first.ID)
	if afterPause == nil || afterPause.Pause == nil {
		t.Fatal("the paused run has no pause")
	}
	pausedRun := findRun(t, g.Coordinator, "paused by another pause", func(run core.Run) bool {
		return pausedAt(run) && run.Pause.Operation != afterPause.Pause.Operation
	})
	return g, postRunCase(g, casePath("runResumeRun", pausedRun.ID, ""), nil)
}

// scenarioRetryContinuation records a failed continuation on the running run's open attempt, then
// asks for it again on the main leg (run-retry-continuation.json).
func scenarioRetryContinuation(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "running with an open attempt and not paused", func(run core.Run) bool {
		current := run.CurrentAttempt()
		return run.State == core.RunStateRunning && current != nil && current.IsOpen() && run.Pause == nil
	})
	attemptID := run.CurrentAttempt().ID
	if !g.Coordinator.UpdateRunForContract(run.ID, func(saved *core.Run) {
		for i := range saved.Attempts {
			if saved.Attempts[i].ID != attemptID {
				continue
			}
			operation := "op"
			saved.Attempts[i].Interruption = &core.AttemptInterruption{
				Operation: operation, LegID: "main",
				Confirmation: core.AttemptInterruptionConfirmationConfirmed,
				Continuation: new(core.NewAttemptInterruptionContinuation(operation, core.AttemptInterruptionContinuationStatusFailed, new("could not start"), nil)),
			}
		}
	}) {
		t.Fatal("the run could not be changed")
	}
	body := mustJSON(t, api.RunLegRequest{Leg: new("main")})
	return g, postRunCase(g, casePath("runRetryContinuation", run.ID, ""), body)
}

// scenarioStartNow starts the first queued agent run now (run-start-now.json).
func scenarioStartNow(t testing.TB) (*Fixture, Reply) {
	g := Start(t, Options{})
	run := findRun(t, g.Coordinator, "queued at an agent stage", func(run core.Run) bool {
		return run.State == core.RunStateQueued && stageKind(run) == core.StageKindAgent
	})
	return g, postRunCase(g, casePath("runStartNow", run.ID, ""), nil)
}
