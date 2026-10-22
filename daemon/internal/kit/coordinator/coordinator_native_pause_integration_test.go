//go:build integration

// Coordinator tests that interrupt a live Pi, Copilot, Claude, Codex or Cursor turn and resume it with
// the prompt Continue. Each test is skipped unless its own BELTLINE_REAL_* variable is set. The
// stand-in agent does not count: a stand-in executable fails the test. The tests do not run in
// parallel, and go test -timeout bounds the run.

package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// TestNativePausePiInterruptKeepsTheSessionAndContinueRemembers checks that interrupting a Pi turn keeps the session and Continue remembers it.
func TestNativePausePiInterruptKeepsTheSessionAndContinueRemembers(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_PI") != "1" {
		t.Skip("set BELTLINE_REAL_PI=1")
	}
	realNativePause(t, core.BackendPi)
}

// TestNativePauseCopilotInterruptKeepsTheSessionAndContinueRemembers checks that interrupting a Copilot turn keeps the session and Continue remembers it.
func TestNativePauseCopilotInterruptKeepsTheSessionAndContinueRemembers(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_COPILOT") != "1" {
		t.Skip("set BELTLINE_REAL_COPILOT=1")
	}
	realNativePause(t, core.BackendCopilot)
}

// TestNativePauseClaudeInterruptKeepsTheSessionAndContinueRemembers checks that interrupting a Claude turn keeps the session and Continue remembers it.
func TestNativePauseClaudeInterruptKeepsTheSessionAndContinueRemembers(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CLAUDE") != "1" {
		t.Skip("set BELTLINE_REAL_CLAUDE=1")
	}
	realNativePause(t, core.BackendClaude)
}

// TestNativePauseCodexInterruptKeepsTheSessionAndContinueRemembers checks that interrupting a Codex turn keeps the session and Continue remembers it.
func TestNativePauseCodexInterruptKeepsTheSessionAndContinueRemembers(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CODEX") != "1" {
		t.Skip("set BELTLINE_REAL_CODEX=1")
	}
	realNativePause(t, core.BackendCodex)
}

// TestNativePauseCursorInterruptKeepsTheSessionAndContinueRemembers checks that interrupting a Cursor turn keeps the session and Continue remembers it.
func TestNativePauseCursorInterruptKeepsTheSessionAndContinueRemembers(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CURSOR") != "1" {
		t.Skip("set BELTLINE_REAL_CURSOR=1")
	}
	realNativePause(t, core.BackendCursor)
}

// realNativePause runs one turn that stays in generation and interrupts it, and the interrupted turn
// leaves its marks and keeps the session, and the conversation is resumed with Continue, which must write the secret
// the first turn was given.
func realNativePause(t *testing.T, backend core.Backend) {
	t.Helper()
	ctx := context.Background()
	if strings.Contains(backend.Executable(os.Getenv), "stand-in") {
		t.Fatalf("%s is the stand-in. Native pause needs the real CLI.", backend.Label())
	}
	root := t.TempDir()
	if !realGit(root, "init", "-q").OK() {
		t.Fatal("git init")
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Tiny local repository\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	tm := realTmux(t)
	agents := tm.runner()
	sessionID, err := agents.PrepareSessionID(ctx, backend, strings.ToLower(realUUID()), root)
	if err != nil {
		t.Fatalf("session id: %v", err)
	}
	model := realNativePauseModel(backend)
	secret := fmt.Sprintf("pause-%s-%s", backend, realPrefix(sessionID, 8))
	folder := filepath.Join(root, "attempt")
	output := filepath.Join(folder, "result.md")
	name := runner.TmuxSessionName([]string{"native-pause", string(backend), realPrefix(sessionID, 8)})
	t.Cleanup(func() { agents.Stop(ctx, name) })
	spec := runner.NewSessionSpec(runner.SessionSpecArgs{
		Name: name, SessionID: sessionID, Backend: backend, Model: model,
		Prompt:    realPausePrompt(secret, output),
		Directory: root, AttemptFolder: folder, Output: output, Outcomes: []string{"done"},
		Interactive: backend == core.BackendCopilot,
	})
	spec.UsageCheckpoint = agents.UsageCheckpoint(ctx, spec)
	t.Logf("Native pause %s model=%s session=%s", backend.Label(), model, sessionID)
	if err := agents.Start(ctx, spec); err != nil {
		t.Fatalf("start: %v", err)
	}
	realWaitUntilGenerating(t, tm, name, folder)
	interrupted := agents.Interrupt(ctx, spec)
	if interrupted.Kind != runner.SessionInterruptConfirmed {
		t.Fatalf("%s did not confirm the interrupt: %s", backend.Label(), interrupted.Message)
	}
	conversation := interrupted.Conversation
	var identity string
	if backend == core.BackendCodex {
		if conversation == nil {
			t.Fatal("Codex interrupt must capture the rollout")
		}
		identity = *conversation
	} else {
		if backend != core.BackendCopilot && (conversation == nil || *conversation != sessionID) {
			t.Errorf("the interrupt's conversation = %s, want %s", core.ValueOr(conversation, "<nil>"), sessionID)
		}
		identity = sessionID
		if conversation != nil {
			identity = *conversation
		}
	}
	if fileExistsAt(output) {
		t.Error("the interrupted turn wrote its result")
	}
	if marker := strings.TrimSpace(readText(t, filepath.Join(folder, "exit.interrupted"))); marker != "130" {
		t.Errorf("exit.interrupted = %q, want 130", marker)
	}
	if log := readText(t, filepath.Join(folder, "session.log")); !strings.Contains(log, "-- interrupted --") {
		t.Error("session.log does not record the interrupt")
	}

	spec.Prompt = core.RunPauseContinuePrompt
	spec.Resume = true
	spec.ResumeToken = &identity
	// Same boundary Continue uses: the interrupted turn's failure is not this turn's ending.
	spec.LogMark = runner.AdapterFor(backend).Mark(spec.Directory, spec.SessionID)
	if err := agents.Start(ctx, spec); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed := readText(t, filepath.Join(folder, "brief.md")); resumed != core.RunPauseContinuePrompt {
		t.Errorf("brief.md = %q, want the Continue prompt", resumed)
	} else if strings.Contains(resumed, secret) {
		t.Error("brief.md contains the secret")
	}
	script := readText(t, filepath.Join(folder, "run.sh"))
	if strings.Contains(script, secret) {
		t.Error("run.sh contains the secret")
	}
	var want []string
	switch backend {
	case core.BackendPi:
		want = []string{"'--session' '" + sessionID + "'"}
	case core.BackendClaude, core.BackendCursor:
		want = []string{"'--resume' '" + sessionID + "'"}
	case core.BackendCodex:
		want = []string{"'exec' 'resume'", "'" + identity + "'"}
	case core.BackendCopilot:
		want = []string{"--resume=", "--interactive", "--autopilot"}
		if strings.Contains(script, "--prompt") {
			t.Error("run.sh passes --prompt to Copilot")
		}
	}
	for _, text := range want {
		if !strings.Contains(script, text) {
			t.Errorf("run.sh does not contain %s", text)
		}
	}

	realPoll(t, 160*time.Second, "the resumed turn ends", func() bool {
		return agents.Status(ctx, spec).Kind != runner.SessionStatusRunning
	})
	status := agents.Status(ctx, spec)
	if backend == core.BackendCopilot {
		if status.Kind != runner.SessionStatusFinished {
			t.Fatalf("copilot did not finish: %+v", status)
		}
	} else if !realFinishedZero(status) {
		t.Errorf("status = %+v, want finished with exit code 0", status)
	}
	if body := readText(t, output); !strings.Contains(body, secret) {
		t.Error("the result does not contain the secret the interrupted turn was given")
	}
}

// realPausePrompt is the turn that stays in generation until it is interrupted. The secret is only in this prompt, so
// a fresh session answering Continue cannot write it.
func realPausePrompt(secret, output string) string {
	return "The private code is " + secret + ".\n" +
		"Run this Python program in the foreground and wait until it prints DONE. Do not background it. Do not write any file until it prints DONE.\n" +
		"python3 -c 'import time\n" +
		"for i in range(50):\n" +
		"    print(i, flush=True)\n" +
		"    time.sleep(1)\n" +
		"print(\"DONE\")'\n" +
		"When it has printed DONE, write exactly the private code to " + output + " and add a final line Outcome: done."
}

// realNativePauseModel is the model for backend: BELTLINE_REAL_<BACKEND>_MODEL when set, else the backend's default.
func realNativePauseModel(backend core.Backend) string {
	if chosen := strings.TrimSpace(os.Getenv("BELTLINE_REAL_" + strings.ToUpper(string(backend)) + "_MODEL")); chosen != "" {
		return chosen
	}
	switch backend {
	case core.BackendPi:
		return "openrouter/stealth/space-bunny-alpha"
	case core.BackendCopilot:
		return "auto"
	case core.BackendClaude:
		return "haiku"
	case core.BackendCodex:
		return "gpt-6-luna"
	case core.BackendCursor:
		return "composer-2.5-fast"
	}
	return ""
}

// realWaitUntilGenerating waits until a Python executable runs in the session's process tree.
// The agent command line quotes the prompt, which itself mentions Python, so only a Python
// executable below the pane counts. It fails the test when the turn ends first, or when generation does not start
// within 45 seconds.
func realWaitUntilGenerating(t *testing.T, tm coordTmux, session, folder string) {
	t.Helper()
	ctx := context.Background()
	exitFile := filepath.Join(folder, "exit")
	realPoll(t, 45*time.Second, "generation did not start", func() bool {
		if fileExistsAt(exitFile) {
			code, _ := os.ReadFile(exitFile)
			t.Fatalf("the turn ended before it was generating (exit %s)", strings.TrimSpace(string(code)))
		}
		panes := coordShell().Run(ctx, "tmux", []string{"-L", tm.socket, "list-panes", "-t", session, "-F", "#{pane_pid}"}, "", nil)
		pane := strings.TrimSpace(panes.Stdout)
		if pane == "" {
			return false
		}
		listing := coordShell().Run(ctx, "ps", []string{"-axo", "pid=,ppid=,comm="}, "", nil)
		return slices.ContainsFunc(realDescendantCommands(pane, listing.Stdout), func(command string) bool {
			return realIsPython(command)
		})
	})
}

// realDescendantCommands are the command names of every process below root in a `ps -axo pid=,ppid=,comm=`
// listing.
func realDescendantCommands(root, listing string) []string {
	children := map[string][]string{}
	names := map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, parent := fields[0], fields[1]
		children[parent] = append(children[parent], pid)
		names[pid] = strings.Join(fields[2:], " ")
	}
	var found []string
	stack := append([]string(nil), children[root]...)
	seen := map[string]bool{}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if name, ok := names[pid]; ok {
			found = append(found, name)
		}
		stack = append(stack, children[pid]...)
	}
	return found
}

// realIsPython reports whether a command names a Python executable.
func realIsPython(command string) bool {
	base := filepath.Base(command)
	return base == "python" || base == "python3" || base == "Python" || strings.HasPrefix(base, "python")
}
