package shell

import (
	"context"
	"sync"
	"testing"
	"time"
)

// deadlineRunner records the deadline of each call's context and answers with
// reply. It never waits, so the limit is inspected rather than waited for.
type deadlineRunner struct {
	mu        sync.Mutex
	deadlines []time.Time
	reply     Result
}

func (d *deadlineRunner) Run(ctx context.Context, _ string, _ []string, _ string, _ *string) Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		d.deadlines = append(d.deadlines, deadline)
	} else {
		d.deadlines = append(d.deadlines, time.Time{})
	}
	return d.reply
}

// remaining is how long until the call's deadline, or zero when it has none.
func (d *deadlineRunner) remaining(t *testing.T) time.Duration {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.deadlines) != 1 {
		t.Fatalf("calls = %d, want 1", len(d.deadlines))
	}
	if d.deadlines[0].IsZero() {
		t.Fatal("the command has no deadline")
	}
	return time.Until(d.deadlines[0])
}

// waitingRunner blocks until its context ends, then returns reply: a command that
// is still running when the limit or the caller stops it.
type waitingRunner struct {
	reply Result
	// started, when set, receives once the command has started.
	started chan struct{}
	mu      sync.Mutex
	calls   int
}

func (w *waitingRunner) Run(ctx context.Context, _ string, _ []string, _ string, _ *string) Result {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	if w.started != nil {
		w.started <- struct{}{}
	}
	<-ctx.Done()
	return w.reply
}

// near reports whether got is within slack of want.
func near(got, want, slack time.Duration) bool {
	return got > want-slack && got <= want+slack
}

func TestBudgetGivesTransfersTheLongerLimit(t *testing.T) {
	t.Parallel()
	transfers := [][]string{
		{"git", "clone", "-q", "https://example.com/r.git", "dir"},
		{"git", "-C", "/work", "fetch", "-q", "origin"},
		{"git", "-c", "user.name=Beltline", "-C", "/work", "push", "-q", "origin", "HEAD"},
		{"git", "--git-dir=/work/.git", "fetch"},
		{"git", "--git-dir", "/work/.git", "push"},
		{"gh", "repo", "clone", "acme/library", "dir"},
	}
	for _, args := range transfers {
		if got := Limit(args[0], args[1:]); got != TransferLimit {
			t.Errorf("Limit(%v) = %s, want %s", args, got, TransferLimit)
		}
	}
	ordinary := [][]string{
		{"git", "-C", "/work", "rev-parse", "HEAD"},
		{"git", "-c", "user.name=Beltline", "commit", "-q"},
		{"git", "worktree", "remove", "--force", "/work/x"},
		{"git", "merge", "--no-edit"},
		{"git"},
		{"gh", "pr", "create"},
		{"gh", "repo", "view", "acme/library"},
		{"gh", "api", "user"},
		{"acli", "jira", "workitem", "search"},
		{"acli", "jira", "auth", "status"},
	}
	for _, args := range ordinary {
		if got := Limit(args[0], args[1:]); got != CommandLimit {
			t.Errorf("Limit(%v) = %s, want %s", args, got, CommandLimit)
		}
	}
	if CommandLimit != 2*time.Minute || TransferLimit != 10*time.Minute {
		t.Errorf("the limits are %s and %s, want 2m and 10m", CommandLimit, TransferLimit)
	}
}

func TestGitSubcommandSkipsGlobalOptions(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args []string
		want string
	}{
		"plain":        {[]string{"fetch", "origin"}, "fetch"},
		"dir":          {[]string{"-C", "/x", "push"}, "push"},
		"config":       {[]string{"-c", "a=b", "-c", "c=d", "commit"}, "commit"},
		"flag":         {[]string{"--no-pager", "log"}, "log"},
		"git dir join": {[]string{"--git-dir=/x/.git", "clone"}, "clone"},
		"only options": {[]string{"-C", "/x"}, ""},
		"none":         {nil, ""},
	}
	for name, c := range cases {
		if got := GitSubcommand(c.args); got != c.want {
			t.Errorf("%s: GitSubcommand(%v) = %q, want %q", name, c.args, got, c.want)
		}
	}
}

func TestBoundedGivesEachCommandItsLimitAsADeadline(t *testing.T) {
	t.Parallel()
	const slack = 5 * time.Second
	ctx := context.Background()

	ordinary := &deadlineRunner{}
	Bounded(ctx, ordinary, "gh", []string{"issue", "list"}, "", nil)
	if got := ordinary.remaining(t); !near(got, CommandLimit, slack) {
		t.Errorf("gh issue list deadline in %s, want about %s", got, CommandLimit)
	}

	transfer := &deadlineRunner{}
	Bounded(ctx, transfer, "git", []string{"-C", "/x", "fetch", "origin"}, "", nil)
	if got := transfer.remaining(t); !near(got, TransferLimit, slack) {
		t.Errorf("git fetch deadline in %s, want about %s", got, TransferLimit)
	}
}

func TestBoundedKeepsAnEarlierCallerDeadlineAndCapsALaterOne(t *testing.T) {
	t.Parallel()
	const slack = 5 * time.Second

	earlier, cancelEarlier := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelEarlier()
	sooner := &deadlineRunner{}
	Bounded(earlier, sooner, "acli", []string{"jira"}, "", nil)
	if got := sooner.remaining(t); !near(got, 30*time.Second, slack) {
		t.Errorf("an earlier caller deadline: command deadline in %s, want about 30s", got)
	}

	later, cancelLater := context.WithTimeout(context.Background(), time.Hour)
	defer cancelLater()
	capped := &deadlineRunner{}
	Bounded(later, capped, "acli", []string{"jira"}, "", nil)
	if got := capped.remaining(t); !near(got, CommandLimit, slack) {
		t.Errorf("a later caller deadline: command deadline in %s, want about %s", got, CommandLimit)
	}
}

func TestBoundedCancellationReachesTheCommandAndIsNamedAsCancelled(t *testing.T) {
	t.Parallel()
	parent, cancel := context.WithCancel(context.Background())
	runner := &waitingRunner{reply: Result{Stdout: "partial"}, started: make(chan struct{}, 1)}
	done := make(chan Result, 1)
	go func() { done <- bounded(parent, runner, CommandLimit, "git", []string{"fetch"}, "", nil) }()
	<-runner.started
	cancel()
	result := <-done
	if result.Stopped != CancelledReason || result.Message() != CancelledReason || result.TimedOut() {
		t.Fatalf("cancelled command: %+v, message %q", result, result.Message())
	}
	if result.OK() || result.Stdout != "partial" {
		t.Fatalf("cancelled command lost its output or succeeded: %+v", result)
	}
}

func TestBoundedStartsNothingForACallerThatHasEnded(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	for _, c := range []struct {
		name   string
		ctx    context.Context
		reason string
	}{
		{"cancelled", cancelled, CancelledReason},
		{"past its deadline", expired, TimedOutReason},
	} {
		runner := &waitingRunner{}
		result := Bounded(c.ctx, runner, "gh", []string{"api", "user"}, "", nil)
		if runner.calls != 0 {
			t.Errorf("%s: the command was started", c.name)
		}
		if result.Stopped != c.reason || result.OK() {
			t.Errorf("%s: result %+v, want stopped as %s", c.name, result, c.reason)
		}
	}
}

func TestBoundedTimeoutNamesTheTimeoutBeforeAnyOutputAndIsNotZero(t *testing.T) {
	t.Parallel()
	// The runner returns a clean status after its context expired, with output
	// printed before the limit: the timeout still decides the result.
	runner := &waitingRunner{reply: Result{Status: 0, Stdout: "[]\n", Stderr: "warning: partial\n"}}
	result := bounded(context.Background(), runner, 20*time.Millisecond, "acli", []string{"jira", "workitem", "search"}, "", nil)
	if !result.TimedOut() || result.OK() {
		t.Fatalf("a timed-out command: %+v", result)
	}
	if result.Status != timedOutStatus {
		t.Errorf("status = %d, want %d", result.Status, timedOutStatus)
	}
	if result.Message() != TimedOutReason {
		t.Errorf("message = %q, want %q", result.Message(), TimedOutReason)
	}
	if result.Stdout != "[]\n" || result.Stderr != "warning: partial\n" {
		t.Errorf("the timed-out command lost its output: %+v", result)
	}
}

func TestBoundedStopsARealCommandAtItsLimit(t *testing.T) {
	t.Parallel()
	start := time.Now()
	// The limit leaves room for a loaded machine to start the shell and print before it.
	result := bounded(context.Background(), ProcessRunner{}, 2*time.Second, "sh", []string{"-c", "echo started; sleep 30"}, t.TempDir(), nil)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the limit did not stop the command: returned after %s", elapsed)
	}
	if !result.TimedOut() || result.Message() != TimedOutReason || result.Status != timedOutStatus {
		t.Fatalf("a command stopped at its limit: %+v", result)
	}
	if result.Stdout != "started\n" {
		t.Errorf("the output before the limit was lost: %q", result.Stdout)
	}
}

func TestBoundedLeavesOrdinaryResultsAlone(t *testing.T) {
	t.Parallel()
	ok := Bounded(context.Background(), ProcessRunner{}, "sh", []string{"-c", "echo fine"}, t.TempDir(), nil)
	if !ok.OK() || ok.Stdout != "fine\n" || ok.Stopped != "" {
		t.Fatalf("a successful command: %+v", ok)
	}
	failed := Bounded(context.Background(), ProcessRunner{}, "sh", []string{"-c", "echo nope >&2; exit 3"}, t.TempDir(), nil)
	if failed.Status != 3 || failed.Message() != "nope" || failed.TimedOut() {
		t.Fatalf("an ordinary failure: %+v, message %q", failed, failed.Message())
	}
	missing := Bounded(context.Background(), ProcessRunner{}, "/nonexistent/tool", nil, "", nil)
	if missing.Status != 127 || missing.Stopped != "" {
		t.Fatalf("a missing tool: %+v", missing)
	}
}
