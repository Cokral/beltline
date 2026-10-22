package shell

import "testing"

// TestMessageForNamesAStoppedCommandByItsOperation: a stopped command's message names the
// tool and operation first, whatever it printed, and an ordinary failure keeps its own text.
func TestMessageForNamesAStoppedCommandByItsOperation(t *testing.T) {
	t.Parallel()
	timed := Result{Status: timedOutStatus, Stderr: "partial\n", Stopped: TimedOutReason}
	if got := timed.MessageFor("gh pr create"); got != "gh pr create: "+TimedOutReason {
		t.Errorf("timed out: message %q, want the operation and the timeout", got)
	}
	cancelled := Result{Status: cancelledStatus, Stdout: "partial\n", Stopped: CancelledReason}
	if got := cancelled.MessageFor("git diff"); got != "git diff: "+CancelledReason {
		t.Errorf("cancelled: message %q, want the operation and the cancellation", got)
	}
	if !timed.IsStopped() || !cancelled.IsStopped() {
		t.Error("a stopped command does not report IsStopped")
	}
	ordinary := Result{Status: 3, Stderr: "boom\n"}
	if got := ordinary.MessageFor("gh pr create"); got != "boom" || ordinary.IsStopped() {
		t.Errorf("ordinary failure: message %q, stopped %v; want its own text and not stopped", got, ordinary.IsStopped())
	}
}
