//go:build integration

package runner

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// retentionProbe is the shell a launch test runs tmux through. It records
// whether the agent's exit file already exists when the pane is retained.
type retentionProbe struct {
	exitFile string

	mu                    sync.Mutex
	exitedBeforeRetention bool
}

// Run records the exit-file check at the retention command, then runs the command.
func (p *retentionProbe) Run(ctx context.Context, executable string, args []string, dir string, input *string) shell.Result {
	if slices.Contains(args, "set-option") && slices.Contains(args, "remain-on-exit") {
		p.mu.Lock()
		p.exitedBeforeRetention = runnerPathExists(p.exitFile)
		p.mu.Unlock()
	}
	return shell.ProcessRunner{}.Run(ctx, executable, args, dir, input)
}

// TestFastAgentCannotExitBeforeItsPaneIsRetained checks, for every runner, that
// the pane is kept before the agent can exit, that the brief reaches the agent,
// which hands back while its interface stays open, and that finishing keeps the
// transcript and ends the session.
func TestFastAgentCannotExitBeforeItsPaneIsRetained(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			root := t.TempDir()
			output := filepath.Join(root, "triage.md")
			attempt := filepath.Join(root, "attempt")
			token := core.NewUUID() + "#1"
			spec := NewSessionSpec(SessionSpecArgs{
				Name: "bl-launch-" + core.NewUUID(), SessionID: core.NewUUID(), Backend: backend, Model: "fixture-model",
				Prompt:    "End " + output + " with one line: Outcome: ready to spec\n" + core.BriefHandoff(attempt, token),
				Directory: root, AttemptFolder: attempt, Output: output, Outcomes: []string{"ready to spec"},
				Interactive: true, CompletionToken: token,
			})
			if backend == core.BackendCopilot {
				state := filepath.Join(root, "copilot-state")
				spec.CopilotState = &state
			}
			probe := &retentionProbe{exitFile: filepath.Join(attempt, "exit")}
			runner := TmuxRunner{shell: probe, socket: integrationSocket(t)}
			if err := runner.Start(ctx, spec); err != nil {
				t.Fatal(err)
			}
			probe.mu.Lock()
			exited := probe.exitedBeforeRetention
			probe.mu.Unlock()
			if exited {
				t.Error("the agent exited before its pane was retained")
			}
			testkit.Eventually(t, integrationTimeout, "the agent handed back", func() bool {
				return runner.Status(ctx, spec).Kind != SessionStatusRunning
			})
			if status := runner.Status(ctx, spec); status.Kind != SessionStatusFinished || status.ExitCode != nil {
				t.Errorf("status = %+v, want finished without a code", status)
			}
			// The interface stays open after handing back, until the daemon ends it.
			if !runner.ConversationAlive(ctx, spec.Name) || runnerPathExists(filepath.Join(attempt, "exit")) {
				t.Fatal("the agent's interface closed when it handed back")
			}
			runner.Finish(ctx, spec)
			transcript := readTestText(filepath.Join(attempt, "session.log"))
			if !strings.Contains(transcript, "stand-in agent in") {
				t.Errorf("transcript lacks the agent's output:\n%s", transcript)
			}
			if !strings.Contains(readTestText(output), "Outcome: ready to spec") {
				t.Errorf("output %s lacks the outcome", output)
			}
			if runner.ConversationAlive(ctx, spec.Name) {
				t.Error("the session is still alive after finish")
			}
		})
	}
}
