// Tests of the Cursor allocation and launch. The launch tests run TmuxRunner.

package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

func TestExecutableResolvesDefaultOrNonemptyEnvironmentOverride(t *testing.T) {
	t.Parallel()
	override := os.Getenv("BELTLINE_CURSOR")
	want := "cursor-agent"
	if override != "" {
		want = override
	}
	env := func(key string) string {
		if key == "BELTLINE_CURSOR" {
			return override
		}
		return ""
	}
	if got := core.BackendCursor.Executable(env); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	if core.BackendCursor.Label() != "Cursor CLI" {
		t.Fatalf("label = %q", core.BackendCursor.Label())
	}
	// Cursor takes a level through the model variant, so every level is offered.
	if len(core.BackendCursor.SupportedThinking()) != len(core.AllThinking) {
		t.Fatalf("cursor thinking levels = %d", len(core.BackendCursor.SupportedThinking()))
	}
	high := core.ThinkingHigh
	if got := core.BackendCursor.Thinking(&high); got == nil || *got != core.ThinkingHigh {
		t.Fatalf("cursor high = %v", got)
	}
	for _, backend := range core.AllBackend {
		if backend.ThinkingNote() != nil {
			t.Errorf("%s has a thinking note", backend)
		}
	}
}

func TestArgumentsIgnoreThinkingAndPreserveTrimmedModel(t *testing.T) {
	t.Parallel()
	for _, thinking := range core.AllThinking {
		for _, model := range []string{" \n", "  model[high] \n"} {
			thinking, model := thinking, model
			t.Run(string(thinking)+"/"+strings.TrimSpace(model), func(t *testing.T) {
				t.Parallel()
				adapter := CursorAdapter{}
				args := adapter.Arguments("chat", "name", model, &thinking)
				trimmed := strings.TrimSpace(model)
				var modelArgs []string
				if trimmed != "" {
					modelArgs = []string{"--model", trimmed}
				}
				mustEqual(t, append(append([]string{"--print", "--output-format", "text", "--force", "--trust", "--resume", "chat"}, modelArgs...), "$PROMPT"), args)
				mustEqual(t, args, adapter.ResumeArguments("chat", "name", model, &thinking, nil))
				interactive := adapter.ConversationArguments("chat", "name", model, &thinking)
				mustEqual(t, append(append([]string{"--force", "--trust", "--resume", "chat"}, modelArgs...), "$PROMPT"), interactive)
				mustEqual(t, []string{"--force", "--trust", "--resume", "chat"}, adapter.ContinueArguments("chat", "name", nil))
				const context = "claude-opus-5-5-250k-high"
				if !slices.Contains(adapter.Arguments("chat", "name", context, new(core.ThinkingLow)), context) {
					t.Errorf("start drops the variant")
				}
				if !slices.Contains(adapter.ConversationArguments("chat", "name", context, new(core.ThinkingMax)), context) {
					t.Errorf("conversation drops the variant")
				}
				if slices.Contains(adapter.ContinueArguments("chat", "name", nil), "--model") || slices.Contains(adapter.ContinueArguments("chat", "name", nil), context) {
					t.Errorf("continue carries a model")
				}
				dir := t.TempDir()
				if adapter.Ending(dir, "chat", 0) != nil || adapter.Reply(dir, "chat", 0) != nil {
					t.Errorf("cursor reports an ending or reply")
				}
				if adapter.Usage(dir, "chat", "name", 0, 0) != nil {
					t.Errorf("cursor reports usage")
				}
				if adapter.Mark(dir, "chat") != 0 || adapter.UsageMark(dir, "chat", "name") != 0 {
					t.Errorf("cursor reports a mark")
				}
			})
		}
	}
}

// chatCommandRecorder answers tmux with status 1 (no session alive),
// and every other command with reply.
func chatCommandRecorder(reply shell.Result) *shell.Recorder {
	return &shell.Recorder{Reply: func(c shell.Call) shell.Result {
		if c.Executable == "tmux" {
			return shell.Result{Status: 1}
		}
		return reply
	}}
}

func TestInvalidAllocationNeverLaunches(t *testing.T) {
	t.Parallel()
	outputs := []string{"", "not a chat", "12345678-1234-1234-1234-123456789abc\nextra", "\x1b[0m"}
	for _, output := range outputs {
		t.Run(fmt.Sprintf("%q", output), func(t *testing.T) {
			t.Parallel()
			rec := chatCommandRecorder(shell.Result{Stdout: output})
			runner := TmuxRunner{shell: rec, socket: tmuxTestSocket}
			_, err := runner.PrepareSessionID(context.Background(), core.BackendCursor, "proposed", "/tmp/space here")
			var failure core.Failure
			if !errors.As(err, &failure) {
				t.Fatalf("err = %v, want a Failure", err)
			}
			calls := rec.Calls()
			if len(calls) != 1 || !reflect.DeepEqual(calls[0].Args, []string{"create-chat"}) {
				t.Fatalf("calls = %v, want one create-chat", calls)
			}
		})
	}
}

func TestInvalidAssistantAllocationNeverLaunchesTmux(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rec := chatCommandRecorder(shell.Result{})
	runner := TmuxRunner{shell: rec, socket: tmuxTestSocket}
	thinking := core.ThinkingHigh
	spec := ConversationSpec{
		Name: "bl-assistant-test", SessionID: "proposed", Backend: core.BackendCursor,
		Model: "fixture-model", Thinking: &thinking, Prompt: "hello",
		Directory: root, PromptFile: filepath.Join(root, "prompt.md"),
	}
	if err := runner.StartConversation(context.Background(), spec); err == nil {
		t.Fatal("an invalid allocation started a conversation")
	}
	calls := rec.Calls()
	if len(calls) != 2 { // has-session, then create-chat
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	for _, c := range calls {
		if c.Executable == "tmux" && len(c.Args) > 2 && c.Args[2] == "new-session" {
			t.Fatalf("tmux started a session: %v", c.Args)
		}
	}
}

func TestAllocationUsesOverrideAndWorkspaceAndRejectsCommandFailure(t *testing.T) {
	t.Parallel()
	directory := "/tmp/space here"
	const id = "3F2504E0-4F89-41D3-9A0C-0305E82C3301"
	rec := &shell.Recorder{Reply: func(shell.Call) shell.Result { return shell.Result{Stdout: "\n" + id + "\n"} }}
	got, err := TmuxRunner{shell: rec, socket: tmuxTestSocket}.PrepareSessionID(context.Background(), core.BackendCursor, "unused", directory)
	if err != nil || got != id {
		t.Fatalf("PrepareSessionID = %q, %v, want %q", got, err, id)
	}
	calls := rec.Calls()
	if calls[0].Executable != core.BackendCursor.Executable(os.Getenv) || calls[0].Dir != directory {
		t.Fatalf("first call = %+v", calls[0])
	}
	failed := &shell.Recorder{Reply: func(shell.Call) shell.Result { return shell.Result{Status: 1, Stderr: "sign in first"} }}
	if _, err := (TmuxRunner{shell: failed, socket: tmuxTestSocket}).PrepareSessionID(context.Background(), core.BackendCursor, "unused", directory); err == nil {
		t.Fatal("a failed create-chat was accepted")
	}
}
