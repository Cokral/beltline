// The argument lists and wrapper scripts of the interactive launches of Claude, Pi and Codex, and
// that the headless shapes older attempts still use are unchanged.

package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// claudeBypass is the permission bypass every Claude stage passes.
var claudeBypass = []string{"--allow-dangerously-skip-permissions", "--permission-mode", "bypassPermissions"}

func TestInteractiveArgumentsAreExactForEachAgent(t *testing.T) {
	t.Parallel()
	claude, pi, codex := ClaudeAdapter{}, PiAdapter{}, NewCodexAdapter(nil)
	token := "rollout-id"
	high, low := new(core.ThinkingHigh), new(core.ThinkingLow)
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"claude start", claude.InteractiveArguments("abc", "bl-x", " haiku ", new(core.ThinkingMax)),
			append([]string{"--session-id", "abc", "-n", "bl-x"}, append(claudeBypass, "--model", "haiku", "--effort", "max", "$PROMPT")...)},
		{"claude start bare", claude.InteractiveArguments("abc", "bl-x", "", nil),
			append([]string{"--session-id", "abc", "-n", "bl-x"}, append(claudeBypass, "$PROMPT")...)},
		{"claude resume", claude.InteractiveResumeArguments("abc", "bl-x", " haiku ", new(core.ThinkingMax), nil),
			append([]string{"--resume", "abc"}, append(claudeBypass, "--model", "haiku", "--effort", "max", "$PROMPT")...)},
		{"claude resume bare", claude.InteractiveResumeArguments("abc", "bl-x", "", nil, nil),
			append([]string{"--resume", "abc"}, append(claudeBypass, "$PROMPT")...)},
		{"pi start", pi.InteractiveArguments("abc", "bl-x", " model ", high),
			[]string{"--tui-mode", "fullscreen", "--approve", "--session-id", "abc", "--name", "bl-x", "--model", "model", "--thinking", "high", "$PROMPT"}},
		{"pi start bare", pi.InteractiveArguments("abc", "bl-x", "", nil),
			[]string{"--tui-mode", "fullscreen", "--approve", "--session-id", "abc", "--name", "bl-x", "$PROMPT"}},
		{"pi resume", pi.InteractiveResumeArguments("abc", "bl-x", " model ", high, &token),
			[]string{"--tui-mode", "fullscreen", "--approve", "--session", "abc", "--model", "model", "--thinking", "high", "$PROMPT"}},
		{"pi resume bare", pi.InteractiveResumeArguments("abc", "bl-x", "", nil, nil),
			[]string{"--tui-mode", "fullscreen", "--approve", "--session", "abc", "$PROMPT"}},
		{"codex start", codex.InteractiveArguments("id", "name", " gpt-test ", high),
			[]string{codexBypass, "-m", "gpt-test", "-c", "model_reasoning_effort=high", "$PROMPT"}},
		{"codex start bare", codex.InteractiveArguments("id", "name", " \n", nil),
			[]string{codexBypass, "$PROMPT"}},
		{"codex resume", codex.InteractiveResumeArguments("id", "name", "gpt-test", low, &token),
			[]string{"resume", "rollout-id", codexBypass, "-m", "gpt-test", "-c", "model_reasoning_effort=low", "$PROMPT"}},
		{"codex resume bare", codex.InteractiveResumeArguments("id", "name", "", nil, &token),
			[]string{"resume", "rollout-id", codexBypass, "$PROMPT"}},
		{"claude headless", claude.Arguments("abc", "bl-x", " haiku ", new(core.ThinkingMax)),
			append([]string{"-p", "--output-format", "text", "--session-id", "abc", "-n", "bl-x"}, append(claudeBypass, "--model", "haiku", "--effort", "max", "$PROMPT")...)},
		{"claude headless resume", claude.ResumeArguments("abc", "bl-x", "", nil, nil),
			append([]string{"-p", "--output-format", "text", "--resume", "abc"}, append(claudeBypass, "$PROMPT")...)},
		{"pi headless", pi.Arguments("abc", "bl-x", "model", high),
			[]string{"--print", "--approve", "--session-id", "abc", "--name", "bl-x", "--model", "model", "--thinking", "high", "$PROMPT"}},
		{"pi headless resume", pi.ResumeArguments("abc", "bl-x", "", nil, &token),
			[]string{"--print", "--approve", "--session", "abc", "$PROMPT"}},
		{"codex headless", codex.Arguments("id", "name", "gpt-test", nil),
			[]string{"exec", codexBypass, "--skip-git-repo-check", "-m", "gpt-test", "$PROMPT"}},
		{"codex headless resume", codex.ResumeArguments("id", "name", "", nil, &token),
			[]string{"exec", "resume", codexBypass, "--skip-git-repo-check", "rollout-id", "$PROMPT"}},
		{"codex continue", codex.ContinueArguments("id", "name", &token), []string{"resume", "rollout-id", codexBypass}},
	}
	for _, c := range cases {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if codex.InteractiveResumeArguments("id", "name", "gpt-test", nil, nil) != nil {
		t.Errorf("codex resume without a token is not nil")
	}
}

// TestInteractiveArgumentsNeverRunHeadless checks that no interactive shape carries a headless flag, whatever the options.
func TestInteractiveArgumentsNeverRunHeadless(t *testing.T) {
	t.Parallel()
	token := "rollout-id"
	for _, thinking := range []*core.Thinking{nil, new(core.ThinkingHigh)} {
		for _, model := range []string{"", "model"} {
			shapes := [][]string{
				ClaudeAdapter{}.InteractiveArguments("abc", "bl-x", model, thinking),
				ClaudeAdapter{}.InteractiveResumeArguments("abc", "bl-x", model, thinking, nil),
				PiAdapter{}.InteractiveArguments("abc", "bl-x", model, thinking),
				PiAdapter{}.InteractiveResumeArguments("abc", "bl-x", model, thinking, &token),
				NewCodexAdapter(nil).InteractiveArguments("id", "name", model, thinking),
				NewCodexAdapter(nil).InteractiveResumeArguments("id", "name", model, thinking, &token),
			}
			for _, args := range shapes {
				for _, headless := range []string{"-p", "--print", "--output-format", "exec", "--skip-git-repo-check"} {
					if slices.Contains(args, headless) {
						t.Errorf("interactive arguments carry %s: %v", headless, args)
					}
				}
			}
		}
	}
}

// codexTrustFlag is the quoted override that trusts directory for one launch.
func codexTrustFlag(directory string) string {
	checkout, _ := realPath(directory)
	return "'-c' " + tmuxQuote(`projects={"`+checkout+`"={trust_level="trusted"}}`)
}

// TestInteractiveWrapperScripts checks the wrapper of each interactive launch: the interface is started or resumed, and
// no headless flag or exec is run.
func TestInteractiveWrapperScripts(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	claude := claudeInteractiveSpec(t.TempDir(), false)
	claudeResume := claudeInteractiveSpec(t.TempDir(), true)
	pi := NewSessionSpec(SessionSpecArgs{
		Name: "bl-pi-test", SessionID: "pi-id", Backend: core.BackendPi, Model: "model", Thinking: new(core.ThinkingHigh),
		Prompt: "brief", Directory: t.TempDir(), Output: "output.md", Outcomes: []string{"done"}, Interactive: true,
	})
	pi.AttemptFolder = filepath.Join(pi.Directory, "attempt")
	piResume := pi
	piResume.Resume = true
	codex := codexInteractiveSpec(t, false)
	codexResume := codexInteractiveSpec(t, true)
	codexResume.ResumeToken = new("rollout-id")
	checkout := filepath.Join(home, "checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, home, "2026/10/01", checkout, "found", 5, `{"type":"session_meta","payload":{"id":"found","cwd":"`+checkout+`"}}`)
	codexDiscover := codexInteractiveSpec(t, true)
	codexDiscover.Directory = checkout

	brief := "\"$(cat '" + pi.AttemptFolder + "/brief.md')\""
	codexCommand := func(spec SessionSpec, words string) string {
		return tmuxQuote(core.BackendCodex.Executable(os.Getenv)) + " " + words + tmuxQuote(codexBypass) + " " + codexTrustFlag(spec.Directory) + ` "$(cat `
	}
	cases := []struct {
		name     string
		script   string
		contains []string
		absent   []string
	}{
		{"claude start", tmuxScript(claude, nil), []string{"'--session-id'", "'--allow-dangerously-skip-permissions'", "'--permission-mode'", "'bypassPermissions'"}, []string{"'-p'", "'--output-format'"}},
		{"claude resume", tmuxScript(claudeResume, nil), []string{"'--resume'", "'--allow-dangerously-skip-permissions'"}, []string{"'--session-id'", "'-p'"}},
		{"pi start", tmuxScript(pi, nil), []string{"'--tui-mode' 'fullscreen' '--approve' '--session-id' 'pi-id' '--name' 'bl-pi-test' '--model' 'model' '--thinking' 'high' " + brief}, []string{"'--print'"}},
		{"pi resume", tmuxScript(piResume, nil), []string{"'--tui-mode' 'fullscreen' '--approve' '--session' 'pi-id' '--model' 'model' '--thinking' 'high' " + brief}, []string{"'--print'"}},
		{"codex start", tmuxScript(codex, NewCodexAdapter(&home)), []string{codexCommand(codex, "")}, []string{"'exec'", "'--skip-git-repo-check'"}},
		{"codex resume token", tmuxScript(codexResume, NewCodexAdapter(&home)), []string{codexCommand(codexResume, "'resume' 'rollout-id' ")}, []string{"'exec'"}},
		{"codex resume discovers the checkout's rollout", tmuxScript(codexDiscover, NewCodexAdapter(&home)), []string{"'resume' 'found'"}, []string{"'exec'"}},
	}
	for _, c := range cases {
		for _, want := range c.contains {
			if !strings.Contains(c.script, want) {
				t.Errorf("%s: script lacks %q:\n%s", c.name, want, c.script)
			}
		}
		for _, bad := range c.absent {
			if strings.Contains(c.script, bad) {
				t.Errorf("%s: script carries %q:\n%s", c.name, bad, c.script)
			}
		}
	}
}

// claudeInteractiveSpec is an interactive Claude session spec in root, resuming it when resume is set.
func claudeInteractiveSpec(root string, resume bool) SessionSpec {
	return SessionSpec{
		Name:          "bl-claude-test",
		SessionID:     "claude-session",
		AgentName:     "bl-claude-test",
		Backend:       core.BackendClaude,
		Model:         "haiku",
		Prompt:        "brief",
		Directory:     root,
		AttemptFolder: filepath.Join(root, "attempt"),
		Output:        filepath.Join(root, "output.md"),
		Interactive:   true,
		Resume:        resume,
	}
}

// codexInteractiveSpec is a Codex interactive spec whose brief sits in its attempt folder.
func codexInteractiveSpec(t *testing.T, resume bool) SessionSpec {
	t.Helper()
	root := t.TempDir()
	attempt := filepath.Join(root, "attempt")
	if err := os.MkdirAll(attempt, 0o755); err != nil {
		t.Fatal(err)
	}
	return SessionSpec{
		Name:          "bl-test",
		SessionID:     "session",
		AgentName:     "name",
		Backend:       core.BackendCodex,
		Directory:     root,
		AttemptFolder: attempt,
		Interactive:   true,
		Resume:        resume,
	}
}
