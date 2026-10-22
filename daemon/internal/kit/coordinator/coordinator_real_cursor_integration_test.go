//go:build integration

// (BELTLINE_REAL_CURSOR=1, model BELTLINE_REAL_CURSOR_MODEL): real
// Cursor chats on a tiny local repository, checking chat allocation, one-turn completion, resume and isolation. It
// needs the tester's Cursor sign-in and makes no GitHub calls, pushes or issues.

package coordinator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// TestRealCursorFileWritingResumeAndIsolation checks file writing, resume and isolation with the Cursor CLI.
func TestRealCursorFileWritingResumeAndIsolation(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CURSOR") != "1" {
		t.Skip("set BELTLINE_REAL_CURSOR=1")
	}
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	version := coordShell().Run(ctx, core.BackendCursor.Executable(os.Getenv), []string{"--version"}, "", nil)
	if !version.OK() {
		t.Fatalf("cursor --version failed: %s", version.Stderr)
	}
	t.Logf("Tested Cursor CLI: %s", strings.TrimSpace(version.Stdout))
	if !realGit(root, "init", "-q").OK() {
		t.Fatal("git init")
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Tiny local repository\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	tm := realTmux(t)
	agents := tm.runner()
	firstID, err := agents.PrepareSessionID(ctx, core.BackendCursor, realUUID(), root)
	if err != nil {
		t.Fatalf("first chat id: %v", err)
	}
	secondID, err := agents.PrepareSessionID(ctx, core.BackendCursor, realUUID(), root)
	if err != nil {
		t.Fatalf("second chat id: %v", err)
	}
	if firstID == secondID {
		t.Error("two chats got the same id")
	}
	model := os.Getenv("BELTLINE_REAL_CURSOR_MODEL")
	turn := func(id string, number int, prompt string, resume bool) string {
		t.Helper()
		folder := filepath.Join(root, fmt.Sprintf("turn-%d", number))
		output := filepath.Join(folder, "result.md")
		spec := runner.NewSessionSpec(runner.SessionSpecArgs{
			Name:      runner.TmuxSessionName([]string{"real-cursor", id, strconv.Itoa(number)}),
			SessionID: id,
			Backend:   core.BackendCursor,
			Model:     model,
			Prompt:    prompt + "\nWrite your result to " + output + ", ending with Outcome: done. Do not change any other files.",
			Directory: root, AttemptFolder: folder, Output: output, Outcomes: []string{"done"}, Resume: resume,
		})
		if err := agents.Start(ctx, spec); err != nil {
			t.Fatalf("start turn %d: %v", number, err)
		}
		// The loop checks the status once a second for 240 seconds and fails when the turn never ends.
		realPoll(t, 240*time.Second, "Cursor print invocation must exit after one turn", func() bool {
			return agents.Status(ctx, spec).Kind != runner.SessionStatusRunning
		})
		if status := agents.Status(ctx, spec); !realFinishedZero(status) {
			t.Errorf("turn %d status = %+v, want finished with exit code 0", number, status)
		}
		agents.Finish(ctx, spec)
		text := readText(t, output)
		if !strings.Contains(text, "Outcome: done") {
			t.Errorf("turn %d result = %q, want Outcome: done", number, text)
		}
		if usage := agents.Usage(ctx, spec); usage != nil {
			t.Errorf("turn %d usage = %+v, want none", number, usage)
		}
		return text
	}

	secret := realUUID()
	turn(firstID, 1, "Remember the private code "+secret+". Write 'remembered' in the result file.", false)
	resumed := turn(firstID, 2, "Write the private code from the preceding turn to the result file.", true)
	if !strings.Contains(resumed, secret) {
		t.Error("the resumed chat does not have the private code")
	}
	isolated := turn(secondID, 3, "Write any private code supplied earlier in this chat, or 'unknown' if none was supplied. Do not read files to find it.", false)
	if strings.Contains(isolated, secret) {
		t.Error("the isolated chat saw the private code")
	}
	if !strings.Contains(strings.ToLower(isolated), "unknown") {
		t.Errorf("the isolated chat's result = %q, want unknown", isolated)
	}
	gh := github.NewFakeGitHub(nil, nil, nil, nil)
	if calls := gh.Calls(); len(calls) != 0 {
		t.Errorf("GitHub was called: %v", calls)
	}
}
