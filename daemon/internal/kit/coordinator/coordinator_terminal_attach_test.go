// Tests of the terminal route's coordinator reads (TerminalAttachCommand and AssistantSessionName)
// against a runner that returns a known attach command.

package coordinator

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// terminalAttachRunner is an AgentRunner whose AttachCommand is scripted and
// which records each call. Its other methods come from the embedded simulated
// runner, which the coordinator's startup reconciliation calls.
type terminalAttachRunner struct {
	runner.AgentRunner
	mu      sync.Mutex
	command []string
	calls   []string
}

// AttachCommand records the call and returns the scripted command.
func (r *terminalAttachRunner) AttachCommand(_ context.Context, session string, readOnly bool) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	mode := "writable"
	if readOnly {
		mode = "read-only"
	}
	r.calls = append(r.calls, session+" "+mode)
	return r.command
}

func TestTerminalAttachCommandReadsTheRunnersCommandForTheSession(t *testing.T) {
	t.Parallel()
	fake := &terminalAttachRunner{command: []string{"tmux", "-L", "bl-test", "attach-session", "-r", "-t", "=bl-run"}}
	h := newHarness(t, harnessOptions{Runner: func(script *harnessScript) runner.AgentRunner {
		fake.AgentRunner = runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 }, time.Now, script.next)
		return fake
	}})
	c := h.Coord()

	got := c.TerminalAttachCommand(context.Background(), "bl-run", true)
	if !reflect.DeepEqual(got, fake.command) {
		t.Fatalf("attach command = %v, want %v", got, fake.command)
	}
	fake.command = nil
	if got := c.TerminalAttachCommand(context.Background(), "bl-run", false); got != nil {
		t.Fatalf("attach command for a session that is not running = %v, want nil", got)
	}
	want := []string{"bl-run read-only", "bl-run writable"}
	if !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("runner calls = %v, want %v", fake.calls, want)
	}
}
