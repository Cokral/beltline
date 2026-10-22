// The launch parts of the TmuxRunner.

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// tmuxQuote single-quotes text for the shell.
func tmuxQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// tmuxScript is the wrapper script: it runs the agent, then records its exit
// code. SIGINT cancels the turn and writes `interrupted`, apart from a normal
// exit. An interactive spec runs the runner's own interface, a headless one its
// one-turn command. A nil adapter uses AdapterFor(spec.Backend).
func tmuxScript(spec SessionSpec, adapter AgentAdapter) string {
	if adapter == nil {
		adapter = AdapterFor(spec.Backend)
	}
	args := tmuxArguments(spec, adapter)
	briefPath := filepath.Join(spec.AttemptFolder, "brief.md")
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "$PROMPT" {
			quoted = append(quoted, `"$(cat `+tmuxQuote(briefPath)+`)"`)
		} else {
			quoted = append(quoted, tmuxQuote(arg))
		}
	}
	exitFile := tmuxQuote(filepath.Join(spec.AttemptFolder, "exit"))
	marker := tmuxQuote(filepath.Join(spec.AttemptFolder, "interrupted"))
	var state string
	if spec.CopilotState != nil {
		state = *spec.CopilotState
	} else {
		state = os.Getenv("BELTLINE_COPILOT_STATE")
	}
	export := ""
	if spec.Backend == core.BackendCopilot && state != "" {
		export = "export BELTLINE_COPILOT_STATE=" + tmuxQuote(state) + "\n"
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString(export)
	fmt.Fprintf(&b, "cd %s || { echo 126 > %s; exit 126; }\n", tmuxQuote(spec.Directory), exitFile)
	b.WriteString("interrupted=0\n")
	b.WriteString("# Foreground: a background job ignores SIGINT, so its own trap would never run.\n")
	b.WriteString("trap 'interrupted=1' INT\n")
	fmt.Fprintf(&b, "%s %s\n", tmuxQuote(spec.Backend.Executable(os.Getenv)), strings.Join(quoted, " "))
	b.WriteString("code=$?\n")
	b.WriteString("if [ \"$interrupted\" = 1 ] || [ \"$code\" = 130 ]; then\n")
	fmt.Fprintf(&b, "  echo 130 > %s\n", exitFile)
	fmt.Fprintf(&b, "  echo interrupted > %s\n", marker)
	b.WriteString("else\n")
	fmt.Fprintf(&b, "  echo $code > %s\n", exitFile)
	b.WriteString("fi\n")
	return b.String()
}

// tmuxArguments is the agent's command line for spec: interactive or headless,
// a start or a resume. Nil when an interactive resume has no conversation to
// resume, which Start refuses rather than starting a fresh one.
func tmuxArguments(spec SessionSpec, adapter AgentAdapter) []string {
	token := spec.ResumeToken
	if token == nil {
		token = adapter.SessionToken(spec.Directory)
	}
	switch {
	case spec.Interactive && spec.Resume:
		return trusting(spec.Backend, adapter.InteractiveResumeArguments(spec.SessionID, spec.AgentName, spec.Model, spec.Thinking, token), spec.Directory)
	case spec.Interactive:
		return trusting(spec.Backend, adapter.InteractiveArguments(spec.SessionID, spec.AgentName, spec.Model, spec.Thinking), spec.Directory)
	case spec.Resume:
		return adapter.ResumeArguments(spec.SessionID, spec.AgentName, spec.Model, spec.Thinking, token)
	}
	return adapter.Arguments(spec.SessionID, spec.AgentName, spec.Model, spec.Thinking)
}

// trusting is an interactive command line that trusts directory for this launch,
// where the runner would otherwise ask first (Codex), else arguments as they are.
func trusting(backend core.Backend, arguments []string, directory string) []string {
	if backend != core.BackendCodex {
		return arguments
	}
	return CodexTrusting(arguments, directory)
}

// PrepareSessionID is the runner's own id for a backend that hands one out (its
// create-session command), else the proposed id. The id must be a UUID.
func (r TmuxRunner) PrepareSessionID(ctx context.Context, backend core.Backend, proposed string, directory string) (string, error) {
	arguments := AdapterFor(backend).CreateSessionArguments()
	if arguments == nil {
		return proposed, nil
	}
	executable := backend.Executable(os.Getenv)
	command := strings.Join(append([]string{executable}, arguments...), " ")
	result := r.shell.Run(ctx, executable, arguments, directory, nil)
	if !result.OK() {
		return "", core.NewFailure(command + ": " + result.Message())
	}
	id := strings.TrimSpace(result.Stdout)
	// A UUID, not a title, terminal escape sequence or log.
	if !runnerUUIDPattern.MatchString(id) {
		return "", core.NewFailure(command + " returned an invalid session id")
	}
	return id, nil
}

// Start writes the brief and wrapper script, starts the tmux session on the
// wrapper, and holds launch until the pane is kept after exit. Refuses a session
// with no explicit model, and an interactive resume with no conversation to resume.
func (r TmuxRunner) Start(ctx context.Context, spec SessionSpec) error {
	if strings.TrimSpace(spec.Model) == "" {
		return core.NewFailure("Choose an explicit allowed model before launching " + spec.Backend.Label() + ".")
	}
	if tmuxArguments(spec, AdapterFor(spec.Backend)) == nil {
		return core.NewFailure(spec.Backend.Label() + " has no saved conversation to resume in this checkout.")
	}
	if err := os.MkdirAll(spec.AttemptFolder, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(spec.AttemptFolder, "brief.md"), []byte(spec.Prompt), 0o644); err != nil {
		return err
	}
	script := filepath.Join(spec.AttemptFolder, "run.sh")
	if err := os.WriteFile(script, []byte(tmuxScript(spec, nil)), 0o644); err != nil {
		return err
	}
	if err := os.Chmod(script, 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(spec.AttemptFolder, "exit"))
	_ = os.Remove(filepath.Join(spec.AttemptFolder, "interrupted"))
	_ = os.Remove(CompletionRecord(spec.AttemptFolder))
	// A fast headless agent can exit before set-option reaches tmux, losing
	// both the pane and its transcript. Hold launch until the pane is retained.
	// A unique channel also avoids consuming a signal from an earlier attempt.
	ready := "bl-start-" + core.NewUUID()
	command := "tmux -L " + tmuxQuote(r.socket) + " wait-for " + tmuxQuote(ready) + " && exec " + tmuxQuote(script)
	result := r.tmux(ctx, []string{
		"new-session", "-d", "-s", spec.Name, "-x", "200", "-y", "50", "-c", spec.Directory,
		"-e", "PATH=" + shell.Path(), command,
	})
	if !result.OK() {
		return core.NewFailure("tmux: " + result.Message())
	}
	retained := r.tmux(ctx, []string{"set-option", "-t", tmuxExactPane(spec.Name), "remain-on-exit", "on"})
	if !retained.OK() {
		r.Stop(ctx, spec.Name)
		return core.NewFailure("tmux: " + retained.Message())
	}
	released := r.tmux(ctx, []string{"wait-for", "-S", ready})
	if !released.OK() {
		r.Stop(ctx, spec.Name)
		return core.NewFailure("tmux: " + released.Message())
	}
	return nil
}

// copilot is the Copilot adapter for a spec, with its state folder.
func (r TmuxRunner) copilot(spec SessionSpec) CopilotAdapter {
	return NewCopilotAdapter(spec.CopilotState)
}
