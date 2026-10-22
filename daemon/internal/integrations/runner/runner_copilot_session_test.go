// Tests of the Copilot adapter at the TmuxRunner level: a live task, a legacy headless attempt, an exit
// without a task, an abort, and attaching twice. They run TmuxRunner on a recorded shell whose tmux
// answers as a session that is alive or gone.

package runner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// sessionCommands answers tmux has-session by whether
// the session is alive, capture-pane returns a transcript, list-panes a pid and
// ps a parent pid. Safe for concurrent use.
type sessionCommands struct {
	mu       sync.Mutex
	commands [][]string
	alive    bool
}

func newSessionCommands() *sessionCommands { return &sessionCommands{alive: true} }

func (s *sessionCommands) Run(_ context.Context, executable string, args []string, _ string, _ *string) shell.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, append([]string{executable}, args...))
	switch {
	case slices.Contains(args, "has-session"):
		if s.alive {
			return shell.Result{}
		}
		return shell.Result{Status: 1, Stderr: "can't find session\n"}
	case slices.Contains(args, "capture-pane"):
		return shell.Result{Stdout: "copilot transcript\n"}
	case slices.Contains(args, "list-panes"):
		return shell.Result{Stdout: "4242\n"}
	case executable == "ps":
		return shell.Result{Stdout: "4100\n"}
	}
	return shell.Result{}
}

// Calls returns every command so far, as words.
func (s *sessionCommands) Calls() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.commands...)
}

// SetAlive sets whether tmux reports the session.
func (s *sessionCommands) SetAlive(alive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alive = alive
}

// copilotSessionSpec is a Copilot attempt
// under root, interactive or headless, with a usage checkpoint of lines lines.
func copilotSessionSpec(root string, interactive bool, lines int) SessionSpec {
	state := filepath.Join(root, "state")
	agent := "bl-copilot"
	spec := NewSessionSpec(SessionSpecArgs{
		Name: "bl-copilot", SessionID: "sid", Backend: core.BackendCopilot, Model: "auto", Prompt: "brief",
		Directory: root, AttemptFolder: filepath.Join(root, "attempt"), Output: filepath.Join(root, "attempt", "triage.md"),
		Outcomes: []string{"ready to spec", "needs info"}, AgentName: &agent,
		Interactive: interactive,
	})
	spec.CopilotState = &state
	spec.UsageCheckpoint = &core.UsageCheckpoint{Lines: new(lines)}
	return spec
}

// writeCopilotEvents writes the events log the Copilot CLI keeps for spec's agent.
func writeCopilotEvents(t *testing.T, spec SessionSpec, events string) {
	t.Helper()
	folder := filepath.Join(*spec.CopilotState, spec.AgentName)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestALiveTaskCompletesWithoutTheProcessExiting(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := newSessionCommands()
	runner := TmuxRunner{shell: commands, socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, true, 0)
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Output, []byte("Questions.\n\nOutcome: needs info\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idle := `{"type":"session.idle"}` + "\n"
	writeCopilotEvents(t, spec, idle)
	for range 3 {
		if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusRunning {
			t.Fatalf("status = %+v, want running", got)
		}
	}
	writeCopilotEvents(t, spec, idle+`{"type":"session.task_complete"}`+"\n")
	for range 2 {
		if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusFinished || got.ExitCode != nil {
			t.Fatalf("status = %+v, want finished without an exit code", got)
		}
	}
	if !commands.aliveNow() {
		t.Fatal("the session ended with its task")
	}
	runner.Finish(context.Background(), spec)
	log, err := os.ReadFile(filepath.Join(spec.AttemptFolder, "session.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "copilot transcript") {
		t.Fatalf("session.log = %q", log)
	}
	if !commandsContain(commands, "kill-session") {
		t.Fatal("Finish did not stop the session")
	}
}

// TestACopilotLaunchFinishesOnItsTaskNotOnAHandoff checks that a Copilot launch
// with a completion token keeps running after a handoff record while its autopilot
// has not completed the task, ignores a task completion from before its
// checkpoint, and finishes on a new one with the CLI still open.
func TestACopilotLaunchFinishesOnItsTaskNotOnAHandoff(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := TmuxRunner{shell: newSessionCommands(), socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, true, 1)
	spec.CompletionToken = "sid#1"
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Output, []byte("Questions.\n\nOutcome: needs info\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CompletionRecord(spec.AttemptFolder), []byte("sid#1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	earlier := `{"type":"session.task_complete"}` + "\n" + `{"type":"session.idle"}` + "\n"
	writeCopilotEvents(t, spec, earlier)
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusRunning {
		t.Fatalf("handed back, task still going: status = %+v, want running", got)
	}
	writeCopilotEvents(t, spec, earlier+`{"type":"session.task_complete"}`+"\n")
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusFinished || got.ExitCode != nil {
		t.Fatalf("task complete: status = %+v, want finished without an exit code", got)
	}
}

func TestALegacyHeadlessAttemptFinishesOnlyWhenItExits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := TmuxRunner{shell: newSessionCommands(), socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, false, 0)
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCopilotEvents(t, spec, `{"type":"session.task_complete"}`+"\n")
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusRunning {
		t.Fatalf("status = %+v, want running", got)
	}
	if err := os.WriteFile(filepath.Join(spec.AttemptFolder, "exit"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusFinished || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Fatalf("status = %+v, want finished with exit 0", got)
	}
	script := tmuxScript(spec, nil)
	if !strings.Contains(script, "--interactive") || strings.Contains(script, "--prompt") {
		t.Fatalf("headless script = %q", script)
	}
}

func TestExitAndLossStillApplyWhenTheTaskDidNotComplete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := newSessionCommands()
	runner := TmuxRunner{shell: commands, socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, true, 0)
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Output, []byte("only a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusRunning {
		t.Fatalf("status = %+v, want running", got)
	}
	exitFile := filepath.Join(spec.AttemptFolder, "exit")
	if err := os.WriteFile(exitFile, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusFinished || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("status = %+v, want finished with exit 1", got)
	}
	if err := os.Remove(exitFile); err != nil {
		t.Fatal(err)
	}
	commands.SetAlive(false)
	if got := runner.Status(context.Background(), spec); got.Kind != SessionStatusGone {
		t.Fatalf("status = %+v, want gone", got)
	}
}

func TestAnAbortConfirmsCancellationWhileTheCLIStaysUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := newSessionCommands()
	runner := TmuxRunner{shell: commands, socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, true, 1)
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCopilotEvents(t, spec, `{"type":"session.task_complete"}`+"\n"+`{"type":"abort"}`+"\n")
	interrupt := runner.Interrupt(context.Background(), spec)
	if interrupt.Kind != SessionInterruptConfirmed || interrupt.Conversation == nil || *interrupt.Conversation != "sid" {
		t.Fatalf("interrupt = %+v, want confirmed for sid", interrupt)
	}
	marker, err := os.ReadFile(filepath.Join(spec.AttemptFolder, "exit.interrupted"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(marker)) != "130" {
		t.Fatalf("exit.interrupted = %q, want 130", marker)
	}
	if !commands.aliveNow() {
		t.Fatal("the CLI did not stay up")
	}
}

func TestAttachingTwiceDoesNotStartASecondConversation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	commands := newSessionCommands()
	runner := TmuxRunner{shell: commands, socket: tmuxTestSocket}
	spec := copilotSessionSpec(root, true, 0)
	first, err := runner.ContinueSession(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.ContinueSession(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != TmuxContinueName(spec.Name) {
		t.Fatalf("continue names = %q, %q, want %q", first, second, TmuxContinueName(spec.Name))
	}
	for _, c := range commands.Calls() {
		if slices.Contains(c, "new-session") {
			t.Fatalf("a session was created: %v", c)
		}
	}
}

// commandsContain reports whether any recorded command has word among its arguments.
func commandsContain(s *sessionCommands, word string) bool {
	return slices.ContainsFunc(s.Calls(), func(c []string) bool { return slices.Contains(c, word) })
}

// aliveNow reads the alive flag.
func (s *sessionCommands) aliveNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive
}
