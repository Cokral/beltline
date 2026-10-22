// Tests that the gh commands run under their fixed limit, and that a read which hits
// the limit fails rather than reading as a missing answer.

package github

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// limitOf is the limit a call's deadline was set to, to the minute.
func limitOf(c shell.Call) time.Duration {
	return time.Until(c.Deadline).Round(time.Minute)
}

func TestGHCommandsRunUnderTheCommandLimit(t *testing.T) {
	t.Parallel()
	rec := &shell.Recorder{Reply: func(shell.Call) shell.Result { return shell.Result{Stdout: "[]"} }}
	gh := NewGHCLI(rec)
	ctx := context.Background()
	if _, err := gh.Issues(ctx, "acme/billing", "agent", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := gh.Login(ctx); err != nil {
		t.Fatal(err)
	}
	calls := rec.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	for _, call := range calls {
		if call.Deadline.IsZero() {
			t.Fatalf("%s has no deadline", call.Line())
		}
		if got := limitOf(call); got != shell.CommandLimit {
			t.Errorf("%s: limit %s, want %s", call.Line(), got, shell.CommandLimit)
		}
	}
}

func TestAGHCallWithAnEndedCallerStartsNoCommand(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := &shell.Recorder{}
	if _, err := NewGHCLI(rec).Login(ctx); err == nil {
		t.Fatal("a cancelled caller got an answer")
	}
	if n := len(rec.Calls()); n != 0 {
		t.Fatalf("a cancelled caller started %d commands", n)
	}
}

func TestAGHReadThatHitsItsLimitFailsRatherThanParsingItsOutput(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// gh printed part of a page before it stalled; the read must not parse it.
	stalled := shell.Stalled{Reply: shell.Result{Stdout: `[{"number":1,"title":"partial`}}
	issues, err := NewGHCLI(stalled).Issues(ctx, "acme/billing", "agent", nil)
	if !isFailure(err) || issues != nil {
		t.Fatalf("issues %v, err %v; want a failure and no issues", issues, err)
	}
	if got := err.Error(); got != "gh issue list: "+shell.TimedOutReason {
		t.Fatalf("err = %q, want the command named as timed out", got)
	}
}

// TestAnOptionalReadThatTimesOutIsNotA404: a timed-out read does not become "no such
// branch" because its command line names a 404-like ref.
func TestAnOptionalReadThatTimesOutIsNotA404(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	stalled := shell.Stalled{Reply: shell.Result{Status: 1, Stderr: "gh: Not Found (HTTP 404)"}}
	exists, err := NewGHCLI(stalled).BranchExists(ctx, "a/b", "fix-404")
	if err == nil || exists != nil {
		t.Fatalf("exists %v, err %v; want the timeout as the error", exists, err)
	}
	if !strings.HasSuffix(err.Error(), shell.TimedOutReason) {
		t.Fatalf("err = %q, want the timeout", err.Error())
	}
}

// cancelOnRun cancels its caller's context when it starts, then answers reply once that
// context has ended, as a command the caller gave up on does.
type cancelOnRun struct {
	cancel context.CancelFunc
	reply  shell.Result
}

func (c cancelOnRun) Run(ctx context.Context, _ string, _ []string, _ string, _ *string) shell.Result {
	c.cancel()
	<-ctx.Done()
	return c.reply
}

// TestAnOptionalReadThatIsCancelledIsNotA404: a read the caller cancelled is no answer
// either, even when its command line names a 404-like ref, and it is named as cancelled.
func TestAnOptionalReadThatIsCancelledIsNotA404(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelling := cancelOnRun{cancel: cancel, reply: shell.Result{Status: 1, Stderr: "gh: Not Found (HTTP 404)"}}
	exists, err := NewGHCLI(cancelling).BranchExists(ctx, "a/b", "fix-404")
	if err == nil || exists != nil {
		t.Fatalf("exists %v, err %v; want the cancellation as the error", exists, err)
	}
	if !strings.HasSuffix(err.Error(), shell.CancelledReason) {
		t.Fatalf("err = %q, want the cancellation", err.Error())
	}
}
