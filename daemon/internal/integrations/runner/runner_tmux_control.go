// The interrupt, suspend and resume parts of the TmuxRunner.

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Interrupt sends SIGINT to the pane's process group and waits for the wrapper's
// marker or Copilot's abort event, then archives the turn. An interactive
// interface other than Copilot's may only cancel its turn and stay open, so
// after about two seconds its session is ended: its conversation is already
// saved by the runner, and resuming continues it. Any other session that does
// not stop in about five seconds fails.
func (r TmuxRunner) Interrupt(ctx context.Context, spec SessionSpec) SessionInterrupt {
	marker := filepath.Join(spec.AttemptFolder, "interrupted")
	if runnerPathExists(marker) {
		r.quarantineExit(spec)
		r.Stop(ctx, spec.Name)
		return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: r.conversationIdentity(ctx, spec)}
	}
	if !r.tmux(ctx, []string{"has-session", "-t", tmuxExact(spec.Name)}).OK() {
		return SessionInterrupt{Kind: SessionInterruptFailed, Message: spec.Name + " is not running"}
	}
	r.signal(ctx, spec.Name, "INT")
	for tick := range 25 {
		if tick == 10 && spec.Interactive && spec.Backend != core.BackendCopilot {
			r.recordInterruption(spec)
		}
		if runnerPathExists(marker) || r.confirmInteractiveAbort(spec) {
			r.archiveInterruptedTurn(ctx, spec)
			r.quarantineExit(spec)
			r.Stop(ctx, spec.Name)
			return SessionInterrupt{Kind: SessionInterruptConfirmed, Conversation: r.conversationIdentity(ctx, spec)}
		}
		select {
		case <-ctx.Done():
			return SessionInterrupt{Kind: SessionInterruptFailed, Message: spec.Name + " did not stop"}
		case <-time.After(200 * time.Millisecond):
		}
	}
	return SessionInterrupt{Kind: SessionInterruptFailed, Message: spec.Name + " did not stop"}
}

// confirmInteractiveAbort writes the exit record the wrapper would have written,
// once Copilot's log shows the turn aborted.
func (r TmuxRunner) confirmInteractiveAbort(spec SessionSpec) bool {
	if !spec.Interactive || spec.Backend != core.BackendCopilot {
		return false
	}
	c := r.copilot(spec)
	if !c.didAbort(spec.AgentName, checkpointLines(spec.UsageCheckpoint), nil) {
		return false
	}
	exit := filepath.Join(spec.AttemptFolder, "exit")
	if !runnerPathExists(exit) {
		_ = os.WriteFile(exit, []byte("130\n"), 0o644)
	}
	return true
}

// recordInterruption writes the records the wrapper writes for a cancelled
// turn, for an interactive interface that cancelled its turn and stays open.
// The session is stopped once the turn is archived.
func (r TmuxRunner) recordInterruption(spec SessionSpec) {
	exit := filepath.Join(spec.AttemptFolder, "exit")
	if !runnerPathExists(exit) {
		_ = os.WriteFile(exit, []byte("130\n"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(spec.AttemptFolder, "interrupted"), []byte("interrupted\n"), 0o644)
}

// conversationIdentity is the id to resume the conversation by.
func (r TmuxRunner) conversationIdentity(ctx context.Context, spec SessionSpec) *string {
	if spec.Backend == core.BackendCodex {
		if id := r.UsageIdentity(ctx, spec); id != nil {
			return id
		}
		return spec.ResumeToken
	}
	id := spec.SessionID
	return &id
}

// archiveInterruptedTurn appends the pane's scrollback and an interrupted marker to session.log.
func (r TmuxRunner) archiveInterruptedTurn(ctx context.Context, spec SessionSpec) {
	capture := r.tmux(ctx, []string{"capture-pane", "-p", "-J", "-S", "-", "-t", tmuxExactPane(spec.Name)})
	url := filepath.Join(spec.AttemptFolder, "session.log")
	prior := ""
	if data, err := os.ReadFile(url); err == nil {
		prior = string(data)
	}
	segment := ""
	if capture.OK() {
		segment = capture.Stdout
	}
	separator := ""
	if prior != "" && segment != "" {
		separator = "\n"
	}
	_ = os.WriteFile(url, []byte(prior+separator+segment+"\n-- interrupted --\n"), 0o644)
}

// quarantineExit moves the exit file aside as exit.interrupted, so a cancelled turn does not read as finished.
func (r TmuxRunner) quarantineExit(spec SessionSpec) {
	exit := filepath.Join(spec.AttemptFolder, "exit")
	kept := filepath.Join(spec.AttemptFolder, "exit.interrupted")
	if !runnerPathExists(exit) {
		return
	}
	_ = os.Remove(kept)
	_ = os.Rename(exit, kept)
}

// Suspend freezes the session's process group, so its scrollback stays where it was.
func (r TmuxRunner) Suspend(ctx context.Context, session string) { r.signal(ctx, session, "STOP") }

// Resume wakes the session's frozen process group.
func (r TmuxRunner) Resume(ctx context.Context, session string) { r.signal(ctx, session, "CONT") }

// signal sends a signal (STOP, CONT or INT) to the process group of the session's first pane.
func (r TmuxRunner) signal(ctx context.Context, session string, action string) {
	pid := r.panePid(ctx, session)
	if pid == nil {
		return
	}
	group := r.shell.Run(ctx, "ps", []string{"-o", "pgid=", "-p", *pid}, "", nil)
	if !group.OK() {
		return
	}
	pgid := strings.TrimSpace(group.Stdout)
	if pgid == "" {
		return
	}
	// `-s NAME -- -PGID` is the one spelling macOS's kill and Linux's (procps) both read as a
	// process group: procps takes `-INT -123` as two signal options and signals nothing.
	_ = r.shell.Run(ctx, "kill", []string{"-s", action, "--", "-" + pgid}, "", nil)
}

// panePid is the shell's process id in the session's first pane, or nil when tmux has no such session.
func (r TmuxRunner) panePid(ctx context.Context, session string) *string {
	panes := r.tmux(ctx, []string{"list-panes", "-t", tmuxExactPane(session), "-F", "#{pane_pid}"})
	if !panes.OK() {
		return nil
	}
	pid := strings.TrimSpace(panes.Stdout)
	if pid == "" {
		return nil
	}
	return &pid
}

// runnerPathExists reports whether a file or folder exists at path.
func runnerPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
