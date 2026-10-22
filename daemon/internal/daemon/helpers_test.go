package daemon

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// startedCall is one daemon start the command recorder saw.
type startedCall struct {
	mode    string
	opening bool
}

// commandRecorder records the daemon starts and the folders a command opened.
type commandRecorder struct {
	mu      sync.Mutex
	opened  []string
	started []startedCall
}

// Started returns the daemon starts so far.
func (r *commandRecorder) Started() []startedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]startedCall(nil), r.started...)
}

// Opened returns the folders a command opened so far.
func (r *commandRecorder) Opened() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.opened...)
}

// Clear forgets everything recorded.
func (r *commandRecorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = nil
	r.started = nil
}

// start records a daemon start.
func (r *commandRecorder) start(mode string, opening bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, startedCall{mode: mode, opening: opening})
}

// open records a folder a command opened.
func (r *commandRecorder) open(folder string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = append(r.opened, folder)
}

// textCapture collects text a command writes.
type textCapture struct {
	mu   sync.Mutex
	text string
}

// Append adds text.
func (c *textCapture) Append(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text += value
}

// Value returns everything appended so far.
func (c *textCapture) Value() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text
}

// commandOutcome is what one command invocation returned and wrote.
type commandOutcome struct {
	status int
	output string
	errors string
}

// runCommand runs the command with arguments, a fresh support folder and the given environment,
// recording daemon starts in rec. The standard error writer adds the "beltlined: " prefix the
// executable does.
func runCommand(t *testing.T, arguments []string, environment map[string]string, rec *commandRecorder,
	openDaemon func(folder string, runner shell.Runner) error) commandOutcome {
	t.Helper()
	support := t.TempDir()
	output := &textCapture{}
	errs := &textCapture{}
	if openDaemon == nil {
		openDaemon = func(string, shell.Runner) error { return nil }
	}
	status := RunCommand(Options{
		GOOS:               "darwin",
		Arguments:          arguments,
		ExecutablePath:     "/tmp/beltlined",
		Environment:        environment,
		SupportFolder:      support,
		LaunchAgentsFolder: filepath.Join(support, "LaunchAgents"),
		UID:                501,
		Runner:             &shell.Recorder{},
		RunDaemon: func(mode Mode, opening bool, _ shell.Runner) error {
			name := "live"
			if mode == ModeDemo {
				name = "demo"
			}
			rec.start(name, opening)
			return nil
		},
		OpenDaemon: openDaemon,
		Stdout:     output.Append,
		Stderr:     func(text string) { errs.Append("beltlined: " + text + "\n") },
	})
	return commandOutcome{status: status, output: output.Value(), errors: errs.Value()}
}
