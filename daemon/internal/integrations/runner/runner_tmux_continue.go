// The continue parts of the TmuxRunner.

package runner

import (
	"context"
	"os"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// tmuxContinueCommand is the shell command that continues a finished session in
// its checkout. Shared with the tests so rollout lookup and quoting can be tested
// with a temporary Codex home.
func tmuxContinueCommand(spec SessionSpec, adapter AgentAdapter) string {
	model := strings.TrimSpace(spec.Model)
	var explicitModel []string
	if model != "" {
		if spec.Backend == core.BackendCopilot {
			explicitModel = CopilotModelArguments(model)
		} else {
			flag := "--model"
			if spec.Backend == core.BackendCodex {
				flag = "-m"
			}
			explicitModel = []string{flag, model}
		}
	}
	token := adapter.SessionToken(spec.Directory)
	parts := append([]string{spec.Backend.Executable(os.Getenv)}, trusting(spec.Backend, adapter.ContinueArguments(spec.SessionID, spec.AgentName, token), spec.Directory)...)
	parts = append(parts, explicitModel...)
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, tmuxQuote(part))
	}
	return "cd " + tmuxQuote(spec.Directory) + " && exec " + strings.Join(quoted, " ")
}

// ContinueSession starts an interactive session that continues a finished agent
// session, and returns its tmux name. Returns the existing one when it is running.
// Refuses a session with no explicit model or a checkout that is gone.
func (r TmuxRunner) ContinueSession(ctx context.Context, spec SessionSpec) (string, error) {
	if strings.TrimSpace(spec.Model) == "" {
		return "", core.NewFailure("This historical session has no explicit model and cannot safely be continued.")
	}
	if !runnerIsDirectory(spec.Directory) {
		return "", core.NewFailure("the run's checkout is gone")
	}
	name := TmuxContinueName(spec.Name)
	adapter := AdapterFor(spec.Backend)
	// tmux can fall back to its own cwd if -c disappears between the
	// existence check and launch. The agent must never run there.
	command := tmuxContinueCommand(spec, adapter)
	if r.tmux(ctx, []string{"has-session", "-t", tmuxExact(name)}).OK() {
		return name, nil
	}
	result := r.tmux(ctx, []string{
		"new-session", "-d", "-s", name, "-c", spec.Directory, "-e", "PATH=" + shell.Path(), command,
	})
	if !result.OK() {
		return "", core.NewFailure("tmux: " + result.Message())
	}
	return name, nil
}

// runnerIsDirectory reports whether path exists and is a folder.
func runnerIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// runnerIsRealDirectory reports whether path is a folder itself, not a symbolic
// link to one: the directory check does not follow a link.
// Used where a directory is expected.
func runnerIsRealDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}
