//go:build integration

// Coordinator tests against the signed-in Codex CLI (BELTLINE_REAL_CODEX=1, model
// BELTLINE_REAL_CODEX_MODEL): a triage of one issue, and an answered stage resuming its checkout's own
// rollout, with no GitHub calls. These tests do not run in parallel.

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
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// TestRealCodexTriagesAnIssueAndReportsAnOutcome triages one issue with the Codex CLI and checks its outcome.
func TestRealCodexTriagesAnIssueAndReportsAnOutcome(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CODEX") != "1" {
		t.Skip("set BELTLINE_REAL_CODEX=1")
	}
	backend := core.BackendCodex
	c, paths, run, attempt := realOneIssueTriage(t, &backend, os.Getenv("BELTLINE_REAL_CODEX_MODEL"))
	realLogTriage(t, "Codex", c, run, attempt)
	realExpectTriaged(t, paths, run, attempt)
}

// TestRealCodexResumesTheCheckoutsRolloutAndOnlyThatOne resumes the checkout's own rollout and only that one: an
// answered stage resumes the checkout's own rollout; a checkout with none starts fresh and never sees another run's
// session. Each turn is one real Codex session on the test's own tmux server.
func TestRealCodexResumesTheCheckoutsRolloutAndOnlyThatOne(t *testing.T) {
	if os.Getenv("BELTLINE_REAL_CODEX") != "1" {
		t.Skip("set BELTLINE_REAL_CODEX=1")
	}
	ctx := context.Background()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, checkout := range []string{first, second} {
		if err := os.MkdirAll(checkout, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", checkout, err)
		}
		if !realGit(checkout, "init", "-q").OK() {
			t.Fatalf("git init in %s", checkout)
		}
		if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("# Tiny local repository\n"), 0o644); err != nil {
			t.Fatalf("write README: %v", err)
		}
	}
	tm := realTmux(t)
	agents := tm.runner()
	model := os.Getenv("BELTLINE_REAL_CODEX_MODEL")
	low := core.ThinkingLow
	turn := func(checkout string, number int, prompt string, resume bool) string {
		t.Helper()
		folder := filepath.Join(root, fmt.Sprintf("turn-%d", number))
		output := filepath.Join(folder, "result.md")
		spec := runner.NewSessionSpec(runner.SessionSpecArgs{
			Name:      runner.TmuxSessionName([]string{"real-codex", strconv.Itoa(number), realUUID()}),
			SessionID: realUUID(),
			Backend:   core.BackendCodex,
			Model:     model,
			Thinking:  &low,
			Prompt:    prompt + "\nWrite your result to " + output + ", ending with Outcome: done. Do not change any other files.",
			Directory: checkout, AttemptFolder: folder, Output: output, Outcomes: []string{"done"}, Resume: resume,
		})
		if err := agents.Start(ctx, spec); err != nil {
			t.Fatalf("start turn %d: %v", number, err)
		}
		realPoll(t, 300*time.Second, fmt.Sprintf("turn %d ends", number), func() bool {
			return agents.Status(ctx, spec).Kind != runner.SessionStatusRunning
		})
		status := agents.Status(ctx, spec)
		agents.Finish(ctx, spec)
		if !realFinishedZero(status) {
			t.Errorf("turn %d status = %+v, want finished with exit code 0", number, status)
		}
		text, _ := os.ReadFile(output)
		return string(text)
	}

	secret := realUUID()
	remembered := turn(first, 1, "Remember the private code "+secret+". Write 'remembered' in the result file.", false)
	if !strings.Contains(remembered, "Outcome: done") {
		t.Errorf("the first turn's result = %q, want Outcome: done", remembered)
	}
	rollout := runner.AdapterFor(core.BackendCodex).SessionToken(first)
	if rollout == nil {
		t.Fatal("the first checkout has no Codex rollout")
	}
	t.Logf("Codex rollout for the first checkout: %s", *rollout)
	if runner.AdapterFor(core.BackendCodex).SessionToken(second) != nil {
		t.Error("the second checkout has a Codex rollout, want none")
	}
	resumed := turn(first, 2, "Write the private code from the preceding turn to the result file.", true)
	if !strings.Contains(resumed, secret) {
		t.Errorf("the resumed turn's result does not contain the private code")
	}
	script := readText(t, filepath.Join(root, "turn-2", "run.sh"))
	if !strings.Contains(script, "'exec' 'resume'") || !strings.Contains(script, "'"+*rollout+"'") {
		t.Errorf("run.sh does not resume the first rollout: %s", script)
	}
	fresh := turn(second, 3, "Write any private code supplied earlier in this conversation, or 'unknown' if none was supplied. Do not read files to find it.", true)
	if strings.Contains(fresh, secret) {
		t.Error("the fresh checkout's turn saw the first checkout's private code")
	}
	if !strings.Contains(strings.ToLower(fresh), "unknown") {
		t.Errorf("the fresh checkout's result = %q, want unknown", fresh)
	}
}
