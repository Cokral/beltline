package runner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// tmuxTestSocket is the server name the tests give the runner. Commands are
// recorded, never sent, so no tmux server is touched.
const tmuxTestSocket = "beltline-unit"

// tmuxTestRunner is a TmuxRunner on a recording shell, with the given answers.
func tmuxTestRunner(reply func(shell.Call) shell.Result) (TmuxRunner, *shell.Recorder) {
	rec := &shell.Recorder{Reply: reply}
	return TmuxRunner{shell: rec, socket: tmuxTestSocket}, rec
}

// tmuxTestLine is a recorded command as its words: the executable, then its arguments.
func tmuxTestLine(c shell.Call) []string {
	return append([]string{c.Executable}, c.Args...)
}

// tmuxTestLines is every recorded command, in order, as words.
func tmuxTestLines(rec *shell.Recorder) [][]string {
	var lines [][]string
	for _, c := range rec.Calls() {
		lines = append(lines, tmuxTestLine(c))
	}
	return lines
}

// tmuxTestWord reports whether any recorded command has word among its arguments.
func tmuxTestWord(lines [][]string, word string) bool {
	return slices.ContainsFunc(lines, func(line []string) bool { return slices.Contains(line, word) })
}

// tmuxTestSpec is the codex session the wrapper and launch tests use.
func tmuxTestSpec(root string) SessionSpec {
	return NewSessionSpec(SessionSpecArgs{
		Name: "bl-codex-test", SessionID: "id", Backend: core.BackendCodex, Model: "fixture-model", Prompt: "brief",
		Directory: root, AttemptFolder: filepath.Join(root, "attempt"), Output: filepath.Join(root, "output.md"),
		Outcomes: []string{"done"},
	})
}

// tmuxTestNoSession is the reply that makes has-session fail and everything else succeed.
func tmuxTestNoSession(c shell.Call) shell.Result {
	if slices.Contains(c.Args, "has-session") {
		return shell.Result{Status: 1}
	}
	return shell.Result{}
}

func TestBlankModelsNeverAllocateBackendOrTmux(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "blank")
			runner, rec := tmuxTestRunner(nil)
			blank := tmuxTestSpec(root)
			blank.Backend = backend
			blank.Model = " \n"
			if err := runner.Start(context.Background(), blank); err == nil {
				t.Error("start with a blank model succeeded")
			}
			if _, err := runner.ContinueSession(context.Background(), blank); err == nil {
				t.Error("continue with a blank model succeeded")
			}
			conversation := ConversationSpec{Name: "bl-empty", SessionID: "empty", Backend: backend, Model: "", Prompt: "No prompt should be sent", Directory: root, PromptFile: filepath.Join(root, "prompt.md")}
			if err := runner.StartConversation(context.Background(), conversation); err == nil {
				t.Error("conversation with a blank model started")
			}
			if n := len(rec.Calls()); n != 0 {
				t.Errorf("%d tmux commands ran, want none", n)
			}
			if runnerPathExists(root) {
				t.Errorf("%s was created", root)
			}
		})
	}
}

func TestLaunchWaitsUntilThePaneIsRetained(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner, rec := tmuxTestRunner(tmuxTestNoSession)
	spec := tmuxTestSpec(root)
	if err := runner.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	commands := tmuxTestLines(rec)
	if len(commands) != 3 {
		t.Fatalf("%d commands, want 3: %v", len(commands), commands)
	}
	launch := commands[0]
	if !slices.Contains(launch, "new-session") {
		t.Errorf("first command %v is not new-session", launch)
	}
	want := []string{"tmux", "-L", tmuxTestSocket, "set-option", "-t", tmuxExactPane(spec.Name), "remain-on-exit", "on"}
	if !slices.Equal(commands[1], want) {
		t.Errorf("retention = %v, want %v", commands[1], want)
	}
	if !slices.Equal(commands[2][:5], []string{"tmux", "-L", tmuxTestSocket, "wait-for", "-S"}) {
		t.Errorf("release = %v", commands[2])
	}
	channel := commands[2][len(commands[2])-1]
	wantLaunch := "tmux -L '" + tmuxTestSocket + "' wait-for '" + channel + "' && exec '" + filepath.Join(spec.AttemptFolder, "run.sh") + "'"
	if got := launch[len(launch)-1]; got != wantLaunch {
		t.Errorf("launch command = %q, want %q", got, wantLaunch)
	}
}

func TestWrapperUsesExecPromptAndCheckoutAndExitCode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	spec := tmuxTestSpec(root)
	script := tmuxScript(spec, nil)
	for _, want := range []string{
		"cd '" + root + "'",
		"'exec' '--dangerously-bypass-approvals-and-sandbox' '--skip-git-repo-check'",
		"\"$(cat '" + spec.AttemptFolder + "/brief.md')\"",
		"echo $code > '" + spec.AttemptFolder + "/exit'",
		"trap 'interrupted=1' INT",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("wrapper lacks %q:\n%s", want, script)
		}
	}
}

func TestContinueIsInteractiveInItsOwnSessionAndMissingRolloutStartsFresh(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner, rec := tmuxTestRunner(tmuxTestNoSession)
	spec := tmuxTestSpec(root)
	name, err := runner.ContinueSession(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bl-codex-test-continue" {
		t.Errorf("name = %q", name)
	}
	var command []string
	for _, line := range tmuxTestLines(rec) {
		if slices.Contains(line, "new-session") {
			command = line
		}
	}
	if command == nil {
		t.Fatal("no new-session command")
	}
	if !slices.Contains(command, root) {
		t.Errorf("new-session %v does not name the checkout", command)
	}
	checkout, _ := realPath(root)
	want := "cd '" + root + "' && exec '" + core.BackendCodex.Executable(os.Getenv) + "' '--dangerously-bypass-approvals-and-sandbox' '-c' 'projects={\"" + checkout + "\"={trust_level=\"trusted\"}}' '-m' 'fixture-model'"
	if got := command[len(command)-1]; got != want {
		t.Errorf("continue command = %q, want %q", got, want)
	}
	if strings.Contains(strings.Join(command, " "), "'exec'") {
		t.Errorf("continue runs a fresh exec: %v", command)
	}
}

func TestRetriesAndContinueUseTheCheckoutRollout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, ".codex", "sessions", "2025", "01", "01", "rollout-test.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := `{"type":"session_meta","payload":{"id":"rollout-id","cwd":"` + root + `"}}`
	if err := os.WriteFile(file, []byte(metadata+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := NewCodexAdapter(&root)
	spec := tmuxTestSpec(root)
	spec.Resume = true
	script := tmuxScript(spec, adapter)
	if !strings.Contains(script, "'exec' 'resume' '--dangerously-bypass-approvals-and-sandbox'") {
		t.Errorf("resume wrapper:\n%s", script)
	}
	if !strings.Contains(script, "'rollout-id' \"$(cat") {
		t.Errorf("resume wrapper does not resume the rollout:\n%s", script)
	}
	checkout, _ := realPath(root)
	want := "cd '" + root + "' && exec '" + core.BackendCodex.Executable(os.Getenv) + "' 'resume' 'rollout-id' '--dangerously-bypass-approvals-and-sandbox' '-c' 'projects={\"" + checkout + "\"={trust_level=\"trusted\"}}' '-m' 'fixture-model'"
	if got := tmuxContinueCommand(spec, adapter); got != want {
		t.Errorf("continue = %q, want %q", got, want)
	}
}

func TestCopilotContinueKeepsTheLongContextTier(t *testing.T) {
	t.Parallel()
	spec := tmuxTestSpec(t.TempDir())
	spec.Backend = core.BackendCopilot
	spec.Model = "claude-opus-4.6[long_context]"
	got := tmuxContinueCommand(spec, NewCopilotAdapter(nil))
	if !strings.HasSuffix(got, "'--model' 'claude-opus-4.6' '--context' 'long_context'") {
		t.Errorf("continue = %q, want the long-context tier", got)
	}
}

func TestExitFileDeterminesStatusWithoutRolloutParsing(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			spec := tmuxTestSpec(t.TempDir())
			spec.Backend = backend
			if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
				t.Fatal(err)
			}
			runner, _ := tmuxTestRunner(nil)
			for _, code := range []int32{0, 1, 127} {
				if err := os.WriteFile(filepath.Join(spec.AttemptFolder, "exit"), []byte(strconv.Itoa(int(code))+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				status := runner.Status(context.Background(), spec)
				if status.Kind != SessionStatusFinished || status.ExitCode == nil || *status.ExitCode != code {
					t.Errorf("exit %d: status = %+v", code, status)
				}
			}
		})
	}
}

func TestSuspendingSignalsTheSessionsProcessGroupAndResumingWakesIt(t *testing.T) {
	t.Parallel()
	runner, rec := tmuxTestRunner(func(c shell.Call) shell.Result {
		if slices.Contains(c.Args, "list-panes") {
			return shell.Result{Stdout: "4242\n"}
		}
		if c.Executable == "ps" {
			return shell.Result{Stdout: "4100\n"}
		}
		return shell.Result{}
	})
	runner.Suspend(context.Background(), "bl-issues")
	runner.Resume(context.Background(), "bl-issues")
	commands := tmuxTestLines(rec)
	if len(commands) < 6 {
		t.Fatalf("commands = %v", commands)
	}
	expectPane := []string{"tmux", "-L", tmuxTestSocket, "list-panes", "-t", "=bl-issues:", "-F", "#{pane_pid}"}
	if !slices.Equal(commands[0], expectPane) {
		t.Errorf("pane lookup = %v", commands[0])
	}
	if !slices.Equal(commands[1], []string{"ps", "-o", "pgid=", "-p", "4242"}) {
		t.Errorf("group lookup = %v", commands[1])
	}
	if !slices.Equal(commands[2], []string{"kill", "-s", "STOP", "--", "-4100"}) {
		t.Errorf("suspend = %v", commands[2])
	}
	if !slices.Equal(commands[3], expectPane) {
		t.Errorf("resume pane lookup = %v", commands[3])
	}
	if !slices.Equal(commands[5], []string{"kill", "-s", "CONT", "--", "-4100"}) {
		t.Errorf("resume = %v", commands[5])
	}
}

func TestASessionTmuxDoesNotHaveIsLeftAlone(t *testing.T) {
	t.Parallel()
	runner, rec := tmuxTestRunner(func(c shell.Call) shell.Result {
		if slices.Contains(c.Args, "list-panes") {
			return shell.Result{Status: 1, Stderr: "can't find session"}
		}
		return shell.Result{}
	})
	runner.Suspend(context.Background(), "bl-gone")
	runner.Resume(context.Background(), "bl-gone")
	commands := tmuxTestLines(rec)
	if len(commands) != 2 {
		t.Fatalf("commands = %v, want two pane lookups", commands)
	}
	for _, line := range commands {
		if !slices.Contains(line, "list-panes") {
			t.Errorf("command %v is not a pane lookup", line)
		}
	}
}

func TestEnsureStoppedThrowsWhenTheSessionSurvivesKill(t *testing.T) {
	t.Parallel()
	runner, rec := tmuxTestRunner(func(c shell.Call) shell.Result {
		if slices.Contains(c.Args, "kill-session") {
			return shell.Result{Status: 1, Stderr: "operation not permitted\n"}
		}
		if slices.Contains(c.Args, "has-session") {
			return shell.Result{}
		}
		return shell.Result{}
	})
	if err := runner.EnsureStopped(context.Background(), "bl-live"); err == nil {
		t.Fatal("EnsureStopped succeeded while the session survived")
	}
	lines := tmuxTestLines(rec)
	if !tmuxTestWord(lines, "=bl-live") || !tmuxTestWord(lines, "kill-session") || !tmuxTestWord(lines, "has-session") {
		t.Errorf("commands = %v", lines)
	}
}

func TestEnsureStoppedTreatsAnAbsentSessionAsStopped(t *testing.T) {
	t.Parallel()
	for _, stderr := range []string{
		"can't find session: bl-gone",
		"session not found: bl-gone",
		"no server running on /tmp/tmux-1/beltline",
		"error connecting to /tmp/tmux-1/beltline (No such file or directory)",
	} {
		t.Run(stderr, func(t *testing.T) {
			t.Parallel()
			runner, rec := tmuxTestRunner(func(c shell.Call) shell.Result {
				if slices.Contains(c.Args, "kill-session") {
					return shell.Result{Status: 1, Stderr: stderr + "\n"}
				}
				return shell.Result{}
			})
			if err := runner.EnsureStopped(context.Background(), "bl-gone"); err != nil {
				t.Fatalf("EnsureStopped: %v", err)
			}
			if tmuxTestWord(tmuxTestLines(rec), "has-session") {
				t.Error("an absent session was checked again")
			}
		})
	}
}

func TestEnsureStoppedAcceptsASessionThatExitsBeforeTheCheck(t *testing.T) {
	t.Parallel()
	runner, _ := tmuxTestRunner(func(c shell.Call) shell.Result {
		if slices.Contains(c.Args, "kill-session") {
			return shell.Result{Status: 1, Stderr: "session is busy\n"}
		}
		if slices.Contains(c.Args, "has-session") {
			return shell.Result{Status: 1, Stderr: "can't find session: bl-race\n"}
		}
		return shell.Result{}
	})
	if err := runner.EnsureStopped(context.Background(), "bl-race"); err != nil {
		t.Fatalf("EnsureStopped: %v", err)
	}
}

func TestSocketIsBeltlineWhenTheVariableIsUnset(t *testing.T) {
	t.Parallel()
	if got := tmuxSocketName(map[string]string{}); got != "beltline" {
		t.Errorf("unset: %q", got)
	}
	if got := tmuxSocketName(map[string]string{"BELTLINE_TMUX_SOCKET": "beltline-test-9"}); got != "beltline-test-9" {
		t.Errorf("set: %q", got)
	}
	want := "beltline"
	if value, ok := os.LookupEnv("BELTLINE_TMUX_SOCKET"); ok {
		want = value
	}
	if got := TmuxSocket(); got != want {
		t.Errorf("TmuxSocket() = %q, want %q", got, want)
	}
}

func TestAttachUsesAnExactTargetForExistenceAndAttachment(t *testing.T) {
	t.Parallel()
	runner, rec := tmuxTestRunner(nil)
	got := runner.AttachCommand(context.Background(), "bl-live", true)
	want := []string{"tmux", "-L", tmuxTestSocket, "attach-session", "-r", "-t", "=bl-live"}
	if !slices.Equal(got, want) {
		t.Errorf("attach = %v, want %v", got, want)
	}
	if lines := tmuxTestLines(rec); len(lines) != 1 || !slices.Equal(lines[0], []string{"tmux", "-L", tmuxTestSocket, "has-session", "-t", "=bl-live"}) {
		t.Errorf("commands = %v", lines)
	}
}

func TestStoppingASessionNamesItExactlyNotByPrefix(t *testing.T) {
	t.Parallel()
	runner, rec := tmuxTestRunner(func(c shell.Call) shell.Result {
		if slices.Contains(c.Args, "has-session") {
			return shell.Result{Status: 1, Stderr: "can't find session: bl-1\n"}
		}
		return shell.Result{}
	})
	if err := runner.EnsureStopped(context.Background(), "bl-1"); err != nil {
		t.Fatal(err)
	}
	lines := tmuxTestLines(rec)
	if len(lines) != 1 || !slices.Equal(lines[0], []string{"tmux", "-L", tmuxTestSocket, "kill-session", "-t", "=bl-1"}) {
		t.Errorf("commands = %v", lines)
	}
}

func TestEnsureStoppedReportsWhenTmuxItselfCannotBeAsked(t *testing.T) {
	t.Parallel()
	runner, _ := tmuxTestRunner(func(c shell.Call) shell.Result {
		return shell.Result{Status: 127, Stderr: "Could not start tmux: missing\n"}
	})
	if err := runner.EnsureStopped(context.Background(), "bl-live"); err == nil {
		t.Fatal("EnsureStopped succeeded when tmux could not run")
	}
}
