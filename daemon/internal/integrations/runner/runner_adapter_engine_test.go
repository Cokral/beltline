// Tests of the agent adapters that drive the engine. The wrapper-script assertions are in the
// TmuxRunner tests.

package runner

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

func TestPiRepliesAreTheLastTextAfterTheMark(t *testing.T) {
	t.Parallel()
	first := `{"type":"message","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"Outcome: needs info"}]}}`
	tool := `{"type":"message","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"thinking","thinking":"…"},{"type":"toolCall"}]}}`
	last := `{"type":"message","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"Done.\n\nOutcome: complete"}]}}`
	if got := PiReplyOf(strings.Join([]string{first, tool, last}, "\n"), 0); got == nil || *got != "Done.\n\nOutcome: complete" {
		t.Fatalf("reply = %v", got)
	}
	if got := PiReplyOf(strings.Join([]string{first, tool}, "\n"), 0); got == nil || *got != "Outcome: needs info" {
		t.Fatalf("reply = %v", got)
	}
	if got := PiReplyOf(strings.Join([]string{first, tool}, "\n"), 1); got != nil {
		t.Fatalf("reply after 1 = %v", *got)
	}
}

func TestAContinuedSessionResumesTheAgentsSession(t *testing.T) {
	t.Parallel()
	copilot := CopilotAdapter{StateRoot: t.TempDir()}.ResumeArguments("abc", "bl-x-1", "", nil, nil)
	if !hasPrefixArgs(copilot, []string{"--interactive", "$PROMPT"}) {
		t.Errorf("copilot resume = %v", copilot)
	}
	if !slices.Contains(copilot, "--autopilot") || !slices.Contains(copilot, "--resume=bl-x-1") {
		t.Errorf("copilot resume = %v", copilot)
	}
	if slices.Contains(copilot, "--prompt") {
		t.Errorf("copilot resume carries --prompt: %v", copilot)
	}
	for _, argument := range copilot {
		if strings.HasPrefix(argument, "--name") {
			t.Errorf("copilot resume carries %s", argument)
		}
	}
}

func TestPiSessionFoldersFollowPisNaming(t *testing.T) {
	t.Parallel()
	if got := filepath.Base(PiSessionDirectory("/Users/test/Repositories/beltline")); got != "--Users-test-Repositories-beltline--" {
		t.Fatalf("folder = %q", got)
	}
}

// TestEveryRunnerStartsAndResumesStagesInItsInterface checks, for every runner,
// that a stage's interactive start and resume carry the brief last, the
// session's identity and automatic permissions, and none of the headless flags.
func TestEveryRunnerStartsAndResumesStagesInItsInterface(t *testing.T) {
	t.Parallel()
	headless := []string{"--print", "-p", "--output-format", "exec", "--skip-git-repo-check"}
	identity := map[core.Backend][2]string{
		core.BackendCopilot: {"--name=bl-x", "--resume=bl-x"},
		core.BackendPi:      {"--session-id", "--session"},
		core.BackendClaude:  {"--session-id", "--resume"},
		core.BackendCodex:   {"", "rollout"},
		core.BackendCursor:  {"--resume", "--resume"},
	}
	permission := map[core.Backend]string{
		core.BackendCopilot: "--allow-all", core.BackendPi: "--approve", core.BackendClaude: "bypassPermissions",
		core.BackendCodex: codexBypass, core.BackendCursor: "--force",
	}
	for _, backend := range core.AllBackend {
		adapter := AdapterFor(backend)
		rollout := "rollout"
		start := adapter.InteractiveArguments("id", "bl-x", "model", nil)
		resume := adapter.InteractiveResumeArguments("id", "bl-x", "model", nil, &rollout)
		for index, arguments := range [][]string{start, resume} {
			if len(arguments) == 0 || (arguments[len(arguments)-1] != "$PROMPT" && !slices.Contains(arguments, "$PROMPT")) {
				t.Errorf("%s: no brief in %v", backend, arguments)
			}
			for _, flag := range headless {
				if slices.Contains(arguments, flag) {
					t.Errorf("%s: headless %s in %v", backend, flag, arguments)
				}
			}
			if want := identity[backend][index]; want != "" && !slices.Contains(arguments, want) {
				t.Errorf("%s: %v lacks the session identity %s", backend, arguments, want)
			}
			if !slices.Contains(arguments, permission[backend]) {
				t.Errorf("%s: %v lacks %s", backend, arguments, permission[backend])
			}
		}
	}
}

func TestStartsAndResumesWithThinking(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		for _, thinking := range core.AllThinking {
			backend, thinking := backend, thinking
			t.Run(string(backend)+"/"+string(thinking), func(t *testing.T) {
				t.Parallel()
				adapter := AdapterFor(backend)
				flag := "--reasoning-effort"
				switch backend {
				case core.BackendPi:
					flag = "--thinking"
				case core.BackendClaude:
					flag = "--effort"
				}
				rollout := "rollout"
				commands := [][]string{
					adapter.Arguments("id", "bl-x", " model ", &thinking),
					adapter.ResumeArguments("id", "bl-x", " model ", &thinking, nil),
					adapter.InteractiveArguments("id", "bl-x", " model ", &thinking),
					adapter.InteractiveResumeArguments("id", "bl-x", " model ", &thinking, &rollout),
				}
				for _, arguments := range commands {
					switch {
					case backend == core.BackendCursor:
						// Cursor's level is the model variant the session is given.
						for _, argument := range arguments {
							if strings.HasPrefix(argument, "--reasoning") || argument == "--effort" || argument == "--thinking" {
								t.Errorf("cursor carries a level flag: %v", arguments)
							}
						}
					case backend.Thinking(&thinking) != nil:
						level := *backend.Thinking(&thinking)
						if backend == core.BackendCodex {
							if !slices.Contains(arguments, "model_reasoning_effort="+string(level)) {
								t.Errorf("codex level missing: %v", arguments)
							}
						} else {
							index := slices.Index(arguments, flag)
							if index < 0 || index+1 >= len(arguments) || arguments[index+1] != string(level) {
								t.Errorf("%s %s missing: %v", flag, level, arguments)
							}
						}
					default:
						if slices.Contains(arguments, flag) {
							t.Errorf("%s passed for an unset level: %v", flag, arguments)
						}
					}
					modelFlag := "--model"
					if backend == core.BackendCodex {
						modelFlag = "-m"
					}
					index := slices.Index(arguments, modelFlag)
					if index < 0 || index+1 >= len(arguments) || arguments[index+1] != "model" {
						t.Errorf("model missing: %v", arguments)
					}
				}
			})
		}
	}
}

func TestUnsetThinkingUsesTheCLIsDefault(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		backend := backend
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()
			adapter := AdapterFor(backend)
			rollout := "rollout"
			commands := [][]string{
				adapter.Arguments("id", "bl-x", "", nil),
				adapter.ResumeArguments("id", "bl-x", "", nil, nil),
				adapter.InteractiveArguments("id", "bl-x", "", nil),
				adapter.InteractiveResumeArguments("id", "bl-x", "", nil, &rollout),
			}
			for _, arguments := range commands {
				for _, flag := range []string{"--effort", "--thinking", "--reasoning-effort"} {
					if slices.Contains(arguments, flag) {
						t.Errorf("%s passed: %v", flag, arguments)
					}
				}
				for _, argument := range arguments {
					if strings.HasPrefix(argument, "model_reasoning_effort=") {
						t.Errorf("reasoning effort passed: %v", arguments)
					}
				}
			}
		})
	}
}

func TestManualContinuationDoesNotChangeThinking(t *testing.T) {
	t.Parallel()
	for _, backend := range core.AllBackend {
		arguments := AdapterFor(backend).ContinueArguments("id", "bl-x", nil)
		for _, flag := range []string{"--effort", "--thinking", "--reasoning-effort"} {
			if slices.Contains(arguments, flag) {
				t.Errorf("%s: %s passed: %v", backend, flag, arguments)
			}
		}
	}
}

func TestCopilotStartsInteractiveAndKeepsAutopilot(t *testing.T) {
	t.Parallel()
	adapter := CopilotAdapter{StateRoot: t.TempDir()}
	arguments := adapter.Arguments("id", "bl-x", "", nil)
	if !hasPrefixArgs(arguments, []string{"--interactive", "$PROMPT", "--allow-all", "--no-ask-user", "--autopilot"}) {
		t.Fatalf("start = %v", arguments)
	}
	if !slices.Contains(arguments, "--name=bl-x") || slices.Contains(arguments, "--prompt") {
		t.Fatalf("start = %v", arguments)
	}
	conversation := adapter.ConversationArguments("id", "bl-x", "", nil)
	if slices.Contains(conversation, "--autopilot") || !slices.Contains(conversation, "--interactive") {
		t.Fatalf("conversation = %v", conversation)
	}
	continuing := adapter.ContinueArguments("id", "bl-x", nil)
	if len(continuing) != 2 || continuing[0] != "--resume=bl-x" || continuing[1] != "--allow-all" {
		t.Fatalf("continue = %v", continuing)
	}
	if slices.Contains(continuing, "--autopilot") || slices.Contains(continuing, "--interactive") {
		t.Fatalf("continue = %v", continuing)
	}
}
