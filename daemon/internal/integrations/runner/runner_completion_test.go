package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// completionTestSpec is an interactive launch of backend waiting for token's handoff.
func completionTestSpec(t *testing.T, backend core.Backend, token string) SessionSpec {
	t.Helper()
	spec := tmuxTestSpec(t.TempDir())
	spec.Backend, spec.Interactive, spec.CompletionToken = backend, true, token
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	return spec
}

// completionTestAlive answers every tmux command, has-session included: the CLI is still open.
func completionTestAlive(shell.Call) shell.Result { return shell.Result{} }

// TestAWrittenOutputWithoutAHandoffKeepsRunning checks that an interactive
// session whose output and Outcome line are written keeps running until it hands back.
func TestAWrittenOutputWithoutAHandoffKeepsRunning(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			spec := completionTestSpec(t, backend, "id#1")
			testkit.WriteFile(t, spec.Output, "# Output\n\nOutcome: done\n")
			runner, _ := tmuxTestRunner(completionTestAlive)
			if status := runner.Status(context.Background(), spec); status.Kind != SessionStatusRunning {
				t.Fatalf("status = %+v, want running", status)
			}
		})
	}
}

// TestTheCurrentHandoffFinishesWhileTheCLIStaysOpen checks that the launch's
// own token finishes it with no exit code while tmux still has the session, on
// every runner that finishes on its handoff.
func TestTheCurrentHandoffFinishesWhileTheCLIStaysOpen(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		if !backend.FinishesOnHandoff() {
			continue
		}
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			spec := completionTestSpec(t, backend, "id#2")
			testkit.WriteFile(t, CompletionRecord(spec.AttemptFolder), "id#2\n")
			runner, _ := tmuxTestRunner(completionTestAlive)
			status := runner.Status(context.Background(), spec)
			if status.Kind != SessionStatusFinished || status.ExitCode != nil {
				t.Fatalf("status = %+v, want finished without an exit code", status)
			}
		})
	}
}

// TestAStaleOrPartialHandoffIsIgnored checks that an earlier launch's token, a
// record still being written and an empty token never finish a launch.
func TestAStaleOrPartialHandoffIsIgnored(t *testing.T) {
	t.Parallel()
	for _, record := range []string{"id#1\n", "id#", ""} {
		spec := completionTestSpec(t, core.BackendClaude, "id#2")
		testkit.WriteFile(t, CompletionRecord(spec.AttemptFolder), record)
		runner, _ := tmuxTestRunner(completionTestAlive)
		if status := runner.Status(context.Background(), spec); status.Kind != SessionStatusRunning {
			t.Errorf("record %q: status = %+v, want running", record, status)
		}
	}
	if CompletionHandedBack(t.TempDir(), "") {
		t.Error("an empty token was handed back")
	}
}

// TestExitingOrVanishingBeforeTheHandoffFails checks that a CLI that exits, even
// cleanly, before handing back fails with its code, and one tmux lost is gone.
func TestExitingOrVanishingBeforeTheHandoffFails(t *testing.T) {
	t.Parallel()
	spec := completionTestSpec(t, core.BackendPi, "id#1")
	testkit.WriteFile(t, spec.Output, "# Output\n\nOutcome: done\n")
	testkit.WriteFile(t, filepath.Join(spec.AttemptFolder, "exit"), "0\n")
	runner, _ := tmuxTestRunner(completionTestAlive)
	status := runner.Status(context.Background(), spec)
	if status.Kind != SessionStatusFailed || !strings.Contains(status.Message, "code 0 before handing its work back") {
		t.Fatalf("exited: status = %+v, want failed", status)
	}
	gone := completionTestSpec(t, core.BackendPi, "id#1")
	runner, _ = tmuxTestRunner(tmuxTestNoSession)
	if status := runner.Status(context.Background(), gone); status.Kind != SessionStatusGone {
		t.Fatalf("vanished: status = %+v, want gone", status)
	}
}

// TestAHeadlessAttemptStillFinishesOnItsExit checks that an older headless
// attempt keeps finishing on its exit code, with no handoff.
func TestAHeadlessAttemptStillFinishesOnItsExit(t *testing.T) {
	t.Parallel()
	spec := tmuxTestSpec(t.TempDir())
	testkit.WriteFile(t, filepath.Join(spec.AttemptFolder, "exit"), "0\n")
	runner, _ := tmuxTestRunner(completionTestAlive)
	status := runner.Status(context.Background(), spec)
	if status.Kind != SessionStatusFinished || status.ExitCode == nil || *status.ExitCode != 0 {
		t.Fatalf("status = %+v, want finished with code 0", status)
	}
}

// TestALaunchClearsAnEarlierHandoff checks that starting a launch removes the
// handoff record a previous launch of the attempt left.
func TestALaunchClearsAnEarlierHandoff(t *testing.T) {
	t.Parallel()
	spec := completionTestSpec(t, core.BackendCursor, "id#2")
	testkit.WriteFile(t, CompletionRecord(spec.AttemptFolder), "id#1\n")
	runner, _ := tmuxTestRunner(tmuxTestNoSession)
	if err := runner.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if runnerPathExists(CompletionRecord(spec.AttemptFolder)) {
		t.Fatal("the earlier handoff survived the launch")
	}
}

// TestAnInteractiveResumeWithoutAConversationIsRefused checks that a resume the
// runner cannot point at its conversation never starts a fresh one.
func TestAnInteractiveResumeWithoutAConversationIsRefused(t *testing.T) {
	t.Parallel()
	spec := completionTestSpec(t, core.BackendCodex, "id#1")
	spec.Resume = true
	if tmuxArguments(spec, NewCodexAdapter(new(t.TempDir()))) != nil {
		t.Fatal("Codex resumes without a rollout")
	}
	runner, rec := tmuxTestRunner(tmuxTestNoSession)
	runner.shell = rec
	if err := runner.Start(context.Background(), spec); err == nil {
		t.Fatal("a resume with no conversation launched")
	}
	if len(rec.Calls()) != 0 {
		t.Fatalf("tmux was asked: %v", tmuxTestLines(rec))
	}
}
