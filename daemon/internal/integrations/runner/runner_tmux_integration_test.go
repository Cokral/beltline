//go:build integration

package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// Runner-level tests: real tmux sessions, the
// stand-in agent, no coordinator.

func TestFastCodexAttemptsKeepTheirPaneAndTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for index := range 20 {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			folder := filepath.Join(root, "attempt")
			output := filepath.Join(folder, "triage.md")
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-fast-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: core.BackendCodex,
				Model: "fixture-model", Prompt: "Their answer: proceed. End " + output + " with one line.",
				Directory: root, AttemptFolder: folder, Output: output, Outcomes: []string{"ready to spec"}, Resume: index > 0,
			})
			runner := integrationRunner(t)
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, integrationTimeout, "the attempt finished", func() bool {
				return runner.Status(ctx, spec).Kind != SessionStatusRunning
			})
			status := runner.Status(ctx, spec)
			if status.Kind != SessionStatusFinished || status.ExitCode == nil || *status.ExitCode != 0 {
				t.Errorf("status = %+v, want finished with code 0", status)
			}
			if !runner.ConversationAlive(ctx, spec.Name) {
				t.Error("the pane was not kept")
			}
			runner.Finish(ctx, spec)
			if log := readTestText(filepath.Join(folder, "session.log")); !strings.Contains(log, "resumed with:") {
				t.Errorf("transcript lacks the resumed prompt:\n%s", log)
			}
			if !strings.Contains(readTestText(output), "Outcome: ready to spec") {
				t.Errorf("output lacks the outcome")
			}
		})
	}
}

// TestFailingSessionKeepsItsExitCodeAndLog checks that the exit code and the reason the
// log shows are what a hold reports.
func TestFailingSessionKeepsItsExitCodeAndLog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			folder := filepath.Join(root, "attempt")
			output := filepath.Join(folder, "triage.md")
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-fail-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: backend, Model: "fixture-model",
				Prompt: "EXIT-WITHOUT-OUTPUT. Write to " + output + ".", Directory: root, AttemptFolder: folder,
				Output: output, Outcomes: []string{"ready to spec"},
				Interactive: false,
			})
			if backend == core.BackendCopilot {
				state := filepath.Join(root, "copilot-state")
				spec.CopilotState = &state
			}
			runner := integrationRunner(t)
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, integrationTimeout, "the attempt ended", func() bool {
				return runner.Status(ctx, spec).Kind != SessionStatusRunning
			})
			status := runner.Status(ctx, spec)
			if status.Kind != SessionStatusFinished || status.ExitCode == nil || *status.ExitCode != 42 {
				t.Fatalf("status = %+v, want finished with code 42", status)
			}
			if got := strings.TrimSpace(readTestText(filepath.Join(folder, "exit"))); got != "42" {
				t.Errorf("exit file = %q", got)
			}
			runner.Finish(ctx, spec)
			if log := readTestText(filepath.Join(folder, "session.log")); !strings.Contains(log, "stand-in: test provider refused the request") {
				t.Errorf("log lacks the runner's last line:\n%s", log)
			}
		})
	}
}

// TestContinueEndsAndKeepsItsTranscript checks that the Continue session has
// its own pane, its scrollback is kept apart from the attempt's, and it ends.
func TestContinueEndsAndKeepsItsTranscript(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			folder := filepath.Join(root, "attempt")
			output := filepath.Join(folder, "triage.md")
			// Every stage starts interactively and ends on its handoff.
			token := core.NewUUID() + "#1"
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-continue-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: backend, Model: "fixture-model",
				Prompt: "End " + output + " with one line.\n" + core.BriefHandoff(folder, token), Directory: root, AttemptFolder: folder,
				Output: output, Outcomes: []string{"ready to spec"}, Interactive: true, CompletionToken: token,
			})
			if backend == core.BackendCopilot {
				state := filepath.Join(root, "copilot-state")
				spec.CopilotState = &state
			}
			runner := integrationRunner(t)
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, integrationTimeout, "the attempt ended", func() bool {
				return runner.Status(ctx, spec).Kind != SessionStatusRunning
			})
			runner.Finish(ctx, spec)
			original := readTestText(filepath.Join(folder, "session.log"))

			name, err := runner.ContinueSession(ctx, spec)
			if err != nil {
				t.Fatal(err)
			}
			if name != TmuxContinueName(spec.Name) {
				t.Fatalf("continue session = %q", name)
			}
			testkit.Eventually(t, integrationTimeout, "the continue session says it is ready", func() bool {
				return strings.Contains(tmuxPaneText(runner.socket, name), "stand-in Continue")
			})
			if !runner.ConversationAlive(ctx, name) {
				t.Fatal("the continue session is not alive")
			}
			runner.EndContinuing(ctx, spec)
			if got := readTestText(filepath.Join(folder, "continue.log")); !strings.Contains(got, "stand-in Continue in "+root) {
				t.Errorf("continue.log = %q", got)
			}
			if runner.ConversationAlive(ctx, name) {
				t.Error("the continue session is still alive")
			}
			if readTestText(filepath.Join(folder, "session.log")) != original {
				t.Error("the attempt's own transcript changed")
			}
		})
	}
}

// TestInterruptArchivesTheTurnAndConfirmsTheConversation checks that an interrupt archives the turn
// and confirms the conversation: an interrupt stops
// the running turn, keeps its scrollback and marks the exit as interrupted.
func TestInterruptArchivesTheTurnAndConfirmsTheConversation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			folder := filepath.Join(root, "attempt")
			output := filepath.Join(folder, "triage.md")
			token := core.NewUUID() + "#1"
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-interrupt-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: backend, Model: "fixture-model",
				Prompt:    "WAIT-FOR-PAUSE PARTIAL-OUTCOME End " + output + " with one line.\n" + core.BriefHandoff(folder, token),
				Directory: root, AttemptFolder: folder, Output: output, Outcomes: []string{"ready to spec"},
				Interactive: true, CompletionToken: token,
			})
			if backend == core.BackendCopilot {
				state := filepath.Join(root, "copilot-state")
				spec.CopilotState = &state
			}
			runner := integrationRunner(t)
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			ready := filepath.Join(folder, "ready-to-pause")
			testkit.Eventually(t, integrationTimeout, "the agent is generating", func() bool {
				return runnerPathExists(ready)
			})
			interrupt := runner.Interrupt(ctx, spec)
			if interrupt.Kind != SessionInterruptConfirmed {
				t.Fatalf("interrupt = %+v, want confirmed", interrupt)
			}
			if backend != core.BackendCodex && (interrupt.Conversation == nil || *interrupt.Conversation != spec.SessionID) {
				t.Errorf("conversation = %v, want the session id", interrupt.Conversation)
			}
			if !runnerPathExists(filepath.Join(folder, "exit.interrupted")) {
				t.Error("the interrupted exit was not quarantined")
			}
			if runnerPathExists(filepath.Join(folder, "exit")) {
				t.Error("an interrupted turn still reads as finished")
			}
			if log := readTestText(filepath.Join(folder, "session.log")); !strings.Contains(log, "-- interrupted --") {
				t.Errorf("log lacks the interrupted marker:\n%s", log)
			}
			if runner.ConversationAlive(ctx, spec.Name) {
				t.Error("the interrupted session is still alive")
			}
			// The partial output and its Outcome line never finish the interrupted turn.
			if status := runner.Status(ctx, spec); status.Kind == SessionStatusFinished {
				t.Errorf("status = %+v, the interrupted turn finished", status)
			}
		})
	}
}

// TestAnInteractiveStageTakesInputBeforeItHandsBack checks, for every runner,
// that a new stage stays open at its ready marker, that a line typed into its
// tmux session reaches the agent, and that only its handoff then finishes it.
func TestAnInteractiveStageTakesInputBeforeItHandsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			folder := filepath.Join(root, "attempt")
			output := filepath.Join(folder, "triage.md")
			token := core.NewUUID() + "#1"
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-steer-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: backend, Model: "fixture-model",
				Prompt:    "WAIT-FOR-STEER End " + output + " with one line.\n" + core.BriefHandoff(folder, token),
				Directory: root, AttemptFolder: folder, Output: output, Outcomes: []string{"ready to spec"},
				Interactive: true, CompletionToken: token,
			})
			if backend == core.BackendCopilot {
				state := filepath.Join(root, "copilot-state")
				spec.CopilotState = &state
			}
			runner := integrationRunner(t)
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			testkit.Eventually(t, integrationTimeout, "the agent is ready for input", func() bool {
				return strings.Contains(tmuxPaneText(runner.socket, spec.Name), "stand-in ready for steering")
			})
			if status := runner.Status(ctx, spec); status.Kind != SessionStatusRunning {
				t.Fatalf("status before input = %+v, want running", status)
			}
			typed := runner.tmux(ctx, []string{"send-keys", "-t", tmuxExactPane(spec.Name), "-l", "take the left branch"})
			entered := runner.tmux(ctx, []string{"send-keys", "-t", tmuxExactPane(spec.Name), "Enter"})
			if !typed.OK() || !entered.OK() {
				t.Fatalf("send-keys: %s %s", typed.Message(), entered.Message())
			}
			testkit.Eventually(t, integrationTimeout, "the agent handed back", func() bool {
				return runner.Status(ctx, spec).Kind == SessionStatusFinished
			})
			if got := readTestText(output); !strings.Contains(got, "steered: take the left branch") {
				t.Errorf("output = %q, want the typed line", got)
			}
			if !runner.ConversationAlive(ctx, spec.Name) {
				t.Error("the interface closed when it handed back")
			}
			runner.Finish(ctx, spec)
			if log := readTestText(filepath.Join(folder, "session.log")); !strings.Contains(log, "stand-in steered: take the left branch") {
				t.Errorf("transcript lacks the typed line:\n%s", log)
			}
			if runner.ConversationAlive(ctx, spec.Name) {
				t.Error("finishing left the session running")
			}
		})
	}
}

// - triageAndSpecRunInTmuxOnAReadOnlyCheckout: Coordinator run flow.
// - aSessionThatChangesItsReadOnlyCheckoutFails: Coordinator run flow.
// - abortingStopsTheSessionAndAnsweringContinuesIt, rerunningAfterAnAbortStartsAFreshTmuxSession:
// Coordinator (hold, answer and rerun).
// - parkingALiveSessionStopsItAndUnparkStartsANewOneWithTheEarlierWork (parkAndUnpark): Coordinator.
// - anAnsweredStageResumesItsSessionInTmux: Coordinator (answer flow).
// - theAfkAssistantApprovesInTmux: Autopilot (Coordinator).
// - pausingInterruptsGenerationAndResumeSendsContinue: Coordinator (pause and resume).
