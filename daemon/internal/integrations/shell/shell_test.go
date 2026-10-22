package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMessageIsTheFirstLineOfStderrThenStdout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		r    Result
		want string
	}{
		{Result{Status: 1, Stderr: "  boom\nmore\n"}, "boom"},
		{Result{Status: 1, Stdout: "out\n"}, "out"},
		{Result{Status: 3}, "exit 3"},
		{Result{Status: 1, Stderr: "first\r\nsecond"}, "first\r\nsecond"},
	}
	for _, c := range cases {
		if got := c.r.Message(); got != c.want {
			t.Errorf("Message() = %q, want %q", got, c.want)
		}
	}
}

func TestPathPutsLocalBinFirstAndAddsSystemFolders(t *testing.T) {
	t.Parallel()
	got := PathFrom([]string{"/opt/homebrew/bin", "/home/u/.local/bin", "/usr/bin"}, "/home/u")
	want := "/home/u/.local/bin:/opt/homebrew/bin:/usr/bin:/usr/local/bin:/bin:/usr/sbin:/sbin:/home/linuxbrew/.linuxbrew/bin"
	if got != want {
		t.Fatalf("PathFrom = %q, want %q", got, want)
	}
}

func TestProcessRunnerRunsWithInputAndReportsStatus(t *testing.T) {
	t.Parallel()
	in := "hello"
	r := ProcessRunner{}.Run(context.Background(), "sh", []string{"-c", "cat; echo err >&2; exit 4"}, t.TempDir(), &in)
	if r.Status != 4 || r.Stdout != "hello" || strings.TrimSpace(r.Stderr) != "err" {
		t.Fatalf("got %+v", r)
	}
	missing := ProcessRunner{}.Run(context.Background(), "/nonexistent/tool", nil, "", nil)
	if missing.OK() {
		t.Fatalf("a missing tool succeeded: %+v", missing)
	}
}

func TestRecorderRecordsAndReplies(t *testing.T) {
	t.Parallel()
	rec := &Recorder{Reply: func(c Call) Result { return Result{Stdout: c.Line()} }}
	got := rec.Run(context.Background(), "gh", []string{"api", "x"}, "", nil)
	if got.Stdout != "gh api x" || len(rec.Calls()) != 1 {
		t.Fatalf("got %+v, calls %v", got, rec.Calls())
	}
}

func TestProcessRunnerDeadlineHoldsWithAChildKeepingStdoutOpen(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // room for a loaded machine to start the process
	defer cancel()
	start := time.Now()
	r := ProcessRunner{}.Run(ctx, "sh", []string{"-c", "sleep 6 & sleep 30"}, t.TempDir(), nil)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the deadline did not hold: returned after %s", elapsed)
	}
	if r.Status != 9 {
		t.Fatalf("a killed group reports SIGKILL, got status %d", r.Status)
	}
}

func TestProcessRunnerReturnsWhenAChildOutlivesItsParent(t *testing.T) {
	t.Parallel()
	r := ProcessRunner{}.Run(context.Background(), "sh", []string{"-c", "sleep 5 & echo hi"}, t.TempDir(), nil)
	if !r.OK() || strings.TrimSpace(r.Stdout) != "hi" {
		t.Fatalf("a finished command with a held pipe: %+v", r)
	}
}

func TestProcessRunnerReportsTheSignalNumberOfASignalledProcess(t *testing.T) {
	t.Parallel()
	r := ProcessRunner{}.Run(context.Background(), "sh", []string{"-c", "kill -9 $$"}, t.TempDir(), nil)
	if r.Status != 9 {
		t.Fatalf("status = %d, want 9", r.Status)
	}
}
