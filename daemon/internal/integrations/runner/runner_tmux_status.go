// The status, finish and usage parts of the TmuxRunner.

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Status reports the session. An interactive launch with a token finishes only
// on its handoff, and fails when its process exits before handing back; an
// interactive Copilot launch, new or older, finishes on a new task event instead.
// Otherwise: the exit file, the backend's own log ending, else whether tmux still
// has the session.
func (r TmuxRunner) Status(ctx context.Context, spec SessionSpec) SessionStatus {
	if spec.Interactive && spec.CompletionToken != "" {
		return r.interactiveStatus(ctx, spec)
	}
	if spec.Interactive && spec.Backend == core.BackendCopilot {
		c := r.copilot(spec)
		if done := c.completion(spec.AgentName, checkpointLines(spec.UsageCheckpoint), nil); done != nil {
			if done.Kind == LogEndingFinished {
				return SessionStatus{Kind: SessionStatusFinished}
			}
			return SessionStatus{Kind: SessionStatusFailed, Message: done.Message}
		}
	}
	if code, ok := tmuxExitCode(spec); ok {
		return SessionStatus{Kind: SessionStatusFinished, ExitCode: code}
	}
	if ending := AdapterFor(spec.Backend).Ending(spec.Directory, spec.SessionID, spec.LogMark); ending != nil {
		if ending.Kind == LogEndingFinished {
			return SessionStatus{Kind: SessionStatusFinished}
		}
		return SessionStatus{Kind: SessionStatusFailed, Message: ending.Message}
	}
	if r.tmux(ctx, []string{"has-session", "-t", tmuxExact(spec.Name)}).OK() {
		return SessionStatus{Kind: SessionStatusRunning}
	}
	return SessionStatus{Kind: SessionStatusGone}
}

// interactiveStatus is the status of an interactive launch: finished once the
// handoff record holds its token (for Copilot, once its log records a task
// completion after the launch's checkpoint), while the interface stays open;
// failed when the process exits first; gone when tmux lost the session; else
// running. Becoming idle, asking a question or writing the output does not end it.
func (r TmuxRunner) interactiveStatus(ctx context.Context, spec SessionSpec) SessionStatus {
	if !spec.Backend.FinishesOnHandoff() {
		// A handoff record never ends a Copilot launch: its autopilot may still
		// be working, and stopping the session there loses that work.
		if done := r.copilot(spec).completion(spec.AgentName, checkpointLines(spec.UsageCheckpoint), nil); done != nil {
			if done.Kind == LogEndingFinished {
				return SessionStatus{Kind: SessionStatusFinished}
			}
			return SessionStatus{Kind: SessionStatusFailed, Message: done.Message}
		}
	} else if CompletionHandedBack(spec.AttemptFolder, spec.CompletionToken) {
		return SessionStatus{Kind: SessionStatusFinished}
	}
	if code, ok := tmuxExitCode(spec); ok {
		message := "exited before handing its work back"
		if code != nil {
			message = fmt.Sprintf("exited with code %d before handing its work back", *code)
		}
		return SessionStatus{Kind: SessionStatusFailed, Message: message}
	}
	if r.tmux(ctx, []string{"has-session", "-t", tmuxExact(spec.Name)}).OK() {
		return SessionStatus{Kind: SessionStatusRunning}
	}
	return SessionStatus{Kind: SessionStatusGone}
}

// tmuxExitCode is the code the wrapper recorded when the agent exited, nil when
// the record is unreadable; ok is false while there is no record.
func tmuxExitCode(spec SessionSpec) (code *int32, ok bool) {
	data, err := os.ReadFile(filepath.Join(spec.AttemptFolder, "exit"))
	// An empty record is one the wrapper is still writing.
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return nil, false
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 32); err == nil {
		value := int32(n)
		code = &value
	}
	return code, true
}

// Finish saves the pane's scrollback to session.log (appending after a turn a
// pause interrupted, or on a Continue prompt) and stops the session.
func (r TmuxRunner) Finish(ctx context.Context, spec SessionSpec) {
	capture := r.tmux(ctx, []string{"capture-pane", "-p", "-J", "-S", "-", "-t", tmuxExactPane(spec.Name)})
	url := filepath.Join(spec.AttemptFolder, "session.log")
	piece := ""
	if capture.OK() {
		piece = capture.Stdout
	}
	prior, readErr := os.ReadFile(url)
	resumed := spec.Prompt == core.RunPauseContinuePrompt || strings.HasSuffix(string(prior), "-- interrupted --\n")
	if resumed && readErr == nil && len(prior) > 0 {
		_ = os.WriteFile(url, []byte(string(prior)+"\n"+piece), 0o644)
	} else if capture.OK() {
		_ = os.WriteFile(url, []byte(piece), 0o644)
	}
	r.Stop(ctx, spec.Name)
}

// LiveSessionNames is every session name the private server has, from one list-sessions
// call. A server that is not running has none, as has-session would find.
// Lock: none (runs tmux).
func (r TmuxRunner) LiveSessionNames(ctx context.Context) map[string]bool {
	names := map[string]bool{}
	listed := r.tmux(ctx, []string{"list-sessions", "-F", "#{session_name}"})
	if !listed.OK() {
		return names
	}
	for _, name := range strings.Split(listed.Stdout, "\n") {
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// EndContinuing saves the Continue session's scrollback to continue.log and stops it.
func (r TmuxRunner) EndContinuing(ctx context.Context, spec SessionSpec) {
	name := TmuxContinueName(spec.Name)
	if !r.tmux(ctx, []string{"has-session", "-t", tmuxExact(name)}).OK() {
		return
	}
	capture := r.tmux(ctx, []string{"capture-pane", "-p", "-J", "-S", "-", "-t", tmuxExactPane(name)})
	if capture.OK() {
		_ = os.WriteFile(filepath.Join(spec.AttemptFolder, "continue.log"), []byte(capture.Stdout), 0o644)
	}
	r.Stop(ctx, name)
}

// Reply is the last assistant reply in the attempt's own session log.
func (r TmuxRunner) Reply(ctx context.Context, spec SessionSpec) *string {
	return AdapterFor(spec.Backend).Reply(spec.Directory, spec.SessionID, spec.LogMark)
}

// usageAdapter is the adapter that reads a session's usage: Copilot's own, else the backend's.
func (r TmuxRunner) usageAdapter(spec SessionSpec) AgentAdapter {
	if spec.Backend == core.BackendCopilot {
		return r.copilot(spec)
	}
	return AdapterFor(spec.Backend)
}

// Usage is the attempt's usage from the backend's log, counted from the checkpoint.
func (r TmuxRunner) Usage(ctx context.Context, spec SessionSpec) *core.Usage {
	return r.usageAdapter(spec).UsageWithCheckpoint(spec.Directory, spec.SessionID, spec.AgentName, spec.LogMark, spec.UsageMark, spec.UsageCheckpoint)
}

// UsageMark is the backend's usage counter before launch.
func (r TmuxRunner) UsageMark(ctx context.Context, spec SessionSpec) int {
	return r.usageAdapter(spec).UsageMark(spec.Directory, spec.SessionID, spec.AgentName)
}

// UsageCheckpoint is the backend's counters and identity before launch.
func (r TmuxRunner) UsageCheckpoint(ctx context.Context, spec SessionSpec) *core.UsageCheckpoint {
	return r.usageAdapter(spec).UsageCheckpoint(spec.Directory, spec.SessionID, spec.AgentName, spec.UsageCheckpoint)
}

// UsageIdentity is the native session id the attempt's log can be told apart by, or nil.
func (r TmuxRunner) UsageIdentity(ctx context.Context, spec SessionSpec) *string {
	if spec.Backend == core.BackendCopilot {
		c := r.copilot(spec)
		return c.exactIdentity(spec.AgentName, nil)
	}
	return AdapterFor(spec.Backend).UsageIdentity(spec.Directory, spec.UsageCheckpoint)
}
