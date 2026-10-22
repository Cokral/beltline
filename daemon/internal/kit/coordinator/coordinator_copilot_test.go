// Coordinator tests of a Copilot task: one that completes, routes once and survives a restart.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// copilotShell is a tmux and ps stand-in whose session is alive,
// shows a transcript and reports a pane and a process.
func copilotShell() *shell.Recorder {
	return &shell.Recorder{Reply: func(call shell.Call) shell.Result {
		switch {
		case slices.Contains(call.Args, "has-session"):
			return shell.Result{}
		case slices.Contains(call.Args, "capture-pane"):
			return shell.Result{Stdout: "copilot transcript\n"}
		case slices.Contains(call.Args, "list-panes"):
			return shell.Result{Stdout: "4242\n"}
		case call.Executable == "ps":
			return shell.Result{Stdout: "4100\n"}
		}
		return shell.Result{}
	}}
}

// copilotRunning is a Copilot triage run that is
// running its first attempt, with the coordinator that loaded it. The state folder of the
// fake Copilot is a temporary folder for the test.
func copilotRunning(t *testing.T, interactive bool, sh *shell.Recorder) (*harness, *Coordinator, core.Run, runner.SessionSpec) {
	t.Helper()
	t.Setenv("BELTLINE_COPILOT_STATE", t.TempDir())
	belt := defaultBelt()
	belt.Policies.CommentOnHold = false
	copilot := core.BackendCopilot
	belt.Stages[0].Runner = &copilot
	run := coordSampleRun(belt)
	const session = "bl-copilot-test"
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionStarted, Session: session, SessionID: "sid", Folder: "01-triage-1"}, sessionSampleT0)
	if interactive {
		yes := true
		run.Attempts[0].CopilotInteractive = &yes
	}
	run.Attempts[0].Settings = &core.SessionSettings{Model: "auto", Runner: &copilot}
	lines := 0
	run.Attempts[0].UsageCheckpoint = &core.UsageCheckpoint{Lines: &lines}
	h := newHarness(t, harnessOptions{
		Issues: []core.Subject{},
		Prepare: func(storage store.Storage) {
			if err := storage.SaveRun(run); err != nil {
				t.Fatalf("save run: %v", err)
			}
		},
		Runner: func(*harnessScript) runner.AgentRunner { return runner.NewTmuxRunner(sh) },
	})
	c := h.Coord()
	saved := c.Run(run.ID)
	if saved == nil {
		t.Fatal("the run was not loaded")
	}
	spec := c.Spec(*saved, saved.Attempts[0], "")
	if spec == nil {
		t.Fatal("no spec for the attempt")
	}
	return h, c, *saved, *spec
}

// copilotStateDir is the folder the fake Copilot writes its events to for spec's agent.
func copilotStateDir(spec runner.SessionSpec) string {
	return filepath.Join(runner.CopilotDefaultStateRoot(), spec.AgentName)
}

// TestATaskEventRoutesOnceAndASecondPollDoesNot: a Copilot task event routes the run once; a later tick routes nothing more.
func TestATaskEventRoutesOnceAndASecondPollDoesNot(t *testing.T) {
	sh := copilotShell()
	h, c, run, spec := copilotRunning(t, true, sh)
	state := copilotStateDir(spec)
	writeText(t, filepath.Join(state, "events.jsonl"), `{"type":"session.idle"}`+"\n")
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h.Tick()
	if got := c.Run(run.ID); got == nil || got.State != core.RunStateRunning {
		t.Fatalf("after an idle event the run is %+v, want running", got)
	}
	writeText(t, filepath.Join(state, "events.jsonl"), `{"type":"session.task_complete"}`+"\n")
	writeText(t, spec.Output, "Questions.\n\nOutcome: needs info\n")
	h.Tick()
	held := c.Run(run.ID)
	if held == nil || held.State != core.RunStateHeld || held.Reason == nil || *held.Reason != "needs info" {
		t.Fatalf("after the task completed the run is %+v, want held for needs info", held)
	}
	if len(held.Attempts) != 1 || held.Attempts[0].SessionID == nil || *held.Attempts[0].SessionID != "sid" {
		t.Fatalf("attempts = %+v, want one with session sid", held.Attempts)
	}
	h.Tick()
	if got := c.Run(run.ID); got == nil || got.State != core.RunStateHeld || len(got.Attempts) != 1 {
		t.Errorf("a second poll changed the run: %+v", got)
	}
	reloaded := h.RestartWith(runner.NewTmuxRunner(sh))
	reloaded.Tick(context.Background())
	reloaded.Idle()
	again := reloaded.Run(run.ID)
	if again == nil || again.State != core.RunStateHeld || again.Reason == nil || *again.Reason != "needs info" || len(again.Attempts) != 1 {
		t.Fatalf("after a restart the run is %+v, want held once", again)
	}
	if again.Attempts[0].SessionID == nil || *again.Attempts[0].SessionID != *held.Attempts[0].SessionID {
		t.Errorf("session id after restart = %v, want %v", again.Attempts[0].SessionID, held.Attempts[0].SessionID)
	}
	if again.Attempts[0].CopilotInteractive == nil || !*again.Attempts[0].CopilotInteractive {
		t.Errorf("copilotInteractive after restart = %v, want true", again.Attempts[0].CopilotInteractive)
	}
}

// TestMissingOutputFailsAndALegacyAttemptIsNotReplaced: a task that ends without its output fails the run; a legacy attempt is not replaced.
func TestMissingOutputFailsAndALegacyAttemptIsNotReplaced(t *testing.T) {
	sh := copilotShell()
	h, c, run, spec := copilotRunning(t, true, sh)
	state := copilotStateDir(spec)
	writeText(t, filepath.Join(state, "events.jsonl"), `{"type":"session.task_complete"}`+"\n")
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	h.Tick()
	failed := c.Run(run.ID)
	if failed == nil || failed.State != core.RunStateHeld || len(failed.Attempts) != 1 {
		t.Fatalf("failed run = %+v, want held with one attempt", failed)
	}
	if failed.Attempts[0].Detail == nil || !strings.Contains(*failed.Attempts[0].Detail, "triage.md") {
		t.Errorf("detail = %v, want the missing triage.md named", failed.Attempts[0].Detail)
	}
	if !coordSame(failed.Attempts[0].Session, run.Attempts[0].Session) {
		t.Errorf("session = %v, want %v", failed.Attempts[0].Session, run.Attempts[0].Session)
	}

	legacy := run
	legacy.Attempts[0].CopilotInteractive = nil
	legacy.Attempts[0].EndedAt = nil
	legacy.Attempts[0].Outcome = nil
	legacy.Attempts[0].Detail = nil
	legacy.State = core.RunStateRunning
	legacy.Reason = nil
	if err := h.storage.SaveRun(legacy); err != nil {
		t.Fatalf("save legacy: %v", err)
	}
	upgraded := h.RestartWith(runner.NewTmuxRunner(sh))
	upgraded.Tick(context.Background())
	upgraded.Idle()
	if got := upgraded.Run(run.ID); got == nil || got.State != core.RunStateRunning || len(got.Attempts) != 1 {
		t.Fatalf("upgraded run = %+v, want running with one attempt", got)
	}
	writeText(t, filepath.Join(spec.AttemptFolder, "exit"), "0\n")
	writeText(t, spec.Output, "Done.\n\nOutcome: ready to spec\n")
	upgraded.Tick(context.Background())
	upgraded.Idle()
	finished := upgraded.Run(run.ID)
	if finished == nil || len(finished.Attempts) == 0 {
		t.Fatalf("finished run = %+v", finished)
	}
	if finished.Attempts[0].SessionID == nil || *finished.Attempts[0].SessionID != "sid" {
		t.Errorf("session id = %v, want sid", finished.Attempts[0].SessionID)
	}
	for _, attempt := range finished.Attempts {
		if attempt.StageID == finished.Attempts[0].StageID && (attempt.SessionID == nil || *attempt.SessionID != "sid") {
			t.Errorf("a second attempt %s replaced the legacy one", attempt.ID)
		}
	}
}

// TestRestartKeepsTheBoundaryAndDoesNotConsumeAFinishedTaskTwice: a restart resumes the boundary and does not count an old task event again.
func TestRestartKeepsTheBoundaryAndDoesNotConsumeAFinishedTaskTwice(t *testing.T) {
	sh := copilotShell()
	h, c, run, spec := copilotRunning(t, true, sh)
	const old = `{"type":"session.task_complete","data":{"summary":"previous"}}`
	state := copilotStateDir(spec)
	writeText(t, filepath.Join(state, "events.jsonl"), old+"\n")
	coordUpdate(c, run.ID, func(r *core.Run) {
		name := spec.AgentName
		lines := 1
		r.Attempts[0].UsageCheckpoint = &core.UsageCheckpoint{SessionID: &name, Lines: &lines}
	})
	restarted := h.RestartWith(runner.NewTmuxRunner(sh))
	saved := restarted.Run(run.ID)
	if saved == nil {
		t.Fatal("the run is gone after the restart")
	}
	again := restarted.Spec(*saved, saved.Attempts[0], "")
	if again == nil {
		t.Fatal("no spec after the restart")
	}
	if !again.Interactive {
		t.Error("the restarted spec is not interactive")
	}
	if again.UsageCheckpoint == nil || again.UsageCheckpoint.Lines == nil || *again.UsageCheckpoint.Lines != 1 {
		t.Errorf("usage checkpoint = %+v, want lines 1", again.UsageCheckpoint)
	}
	if saved.Attempts[0].Session == nil || again.AgentName != *saved.Attempts[0].Session {
		t.Errorf("agent name = %q, want the attempt's session", again.AgentName)
	}
	if again.SessionID != "sid" {
		t.Errorf("session id = %q, want sid", again.SessionID)
	}
	restarted.Tick(context.Background())
	restarted.Idle()
	if got := restarted.Run(run.ID); got == nil || got.State != core.RunStateRunning {
		t.Fatalf("after the old event the run is %+v, want running", got)
	}
	writeText(t, filepath.Join(state, "events.jsonl"), old+"\n"+`{"type":"session.task_complete"}`+"\n")
	writeText(t, filepath.Join(spec.AttemptFolder, "triage.md"), "Questions.\n\nOutcome: needs info\n")
	restarted.Tick(context.Background())
	restarted.Idle()
	held := restarted.Run(run.ID)
	if held == nil || held.State != core.RunStateHeld {
		t.Fatalf("after the new task the run is %+v, want held", held)
	}
	restarted.Tick(context.Background())
	restarted.Idle()
	if got := restarted.Run(run.ID); !coordSame(got, held) {
		t.Errorf("a later tick changed the held run")
	}
}

// TestContinuingInTheSessionThenSendingNilFollowsTheSameConversation: an answer sent with no text continues the same Copilot conversation.
func TestContinuingInTheSessionThenSendingNilFollowsTheSameConversation(t *testing.T) {
	t.Parallel()
	copilot := core.BackendCopilot
	belt := defaultBelt()
	belt.Stages[0].Runner = &copilot
	run := coordSampleRun(belt)
	coordFinish(&run, new("needs info"))
	run.Attempts[0].CopilotInteractive = new(true)
	lines := 2
	run.Attempts[0].UsageCheckpoint = &core.UsageCheckpoint{Lines: &lines}
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindAnswered}, sessionSampleT0)
	if run.State != core.RunStateQueued {
		t.Fatalf("state = %s, want queued", run.State)
	}
	if run.Continues == nil || *run.Continues != "triage-1" {
		t.Fatalf("continues = %v, want triage-1", run.Continues)
	}
	if run.Note != nil {
		t.Errorf("note = %v, want none", run.Note)
	}
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionStarted, Session: "bl-next", SessionID: "sid", Folder: "02"}, sessionSampleT0)
	if len(run.Attempts) < 2 {
		t.Fatalf("attempts = %d, want 2", len(run.Attempts))
	}
	if run.Attempts[1].Continues == nil || *run.Attempts[1].Continues != "triage-1" {
		t.Errorf("attempt continues = %v, want triage-1", run.Attempts[1].Continues)
	}
	if run.Attempts[1].AgentName == nil || *run.Attempts[1].AgentName != "bl-test" {
		t.Errorf("agent name = %v, want bl-test", run.Attempts[1].AgentName)
	}
	before := run
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindAnswered}, sessionSampleT0)
	if !coordSame(run, before) {
		t.Errorf("a second nil answer changed the run")
	}
	again := new("Again.\n\nOutcome: ready to spec")
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionFinished, Output: again}, sessionSampleT0)
	if run.StageID != "spec" {
		t.Fatalf("stage = %s, want spec", run.StageID)
	}
	core.EngineAdvance(&run, core.Event{Kind: core.EventKindSessionFinished, Output: again}, sessionSampleT0)
	if run.StageID != "spec" {
		t.Errorf("stage after a repeated finish = %s, want spec", run.StageID)
	}
}
