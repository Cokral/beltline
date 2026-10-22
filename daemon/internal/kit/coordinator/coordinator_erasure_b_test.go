// The rest of the coordinator tests of run erasure: an erasure that
// waits for the cleanup, the busy sessions and a continue launch, a poll that admits a fresh run
// after an erasure, reviews and tickets that come back once, a schedule and a manual number that
// are not replayed, a failed erasure that a retry finishes, a restart that resumes an erasure,
// and a late completion or a tmux stop failure that leaves the replacement alone.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// simStarting starts a simulated session for spec, as the erasure tests start sessions by hand.
func simStarting(t *testing.T, sim *runner.SimulatedRunner, spec runner.SessionSpec) {
	t.Helper()
	if err := sim.Start(bgCtx, spec); err != nil {
		t.Fatalf("start %s: %v", spec.Name, err)
	}
}

// TestErasureWaitsForCleanupBusySessionsAndLeavesOtherSessions
func TestErasureWaitsForCleanupBusySessionsAndLeavesOtherSessions(t *testing.T) {
	t.Parallel()
	spaces := newH5m4GatedWorkspaces()
	h := newHarness(t, harnessOptions{Issues: []core.Subject{}})
	simRunner := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
	c := coordWith(t, h, func(s *Services) { s.Workspaces = spaces; s.Runner = simRunner })
	belt := c.Belts()[0].Belt
	target := addRun(c, belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 1, Title: "Erase me"})
	other := addRun(c, belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 2, Title: "Leave me"})
	c.Lock()
	c.sessions.start(bgCtx, target.ID, nil)
	c.sessions.start(bgCtx, other.ID, nil)
	c.EndTurn()
	session := runSession(c.Run(target.ID))
	otherSession := runSession(c.Run(other.ID))
	if session == "" || otherSession == "" {
		t.Fatal("the runs did not start sessions")
	}
	continuing := runner.TmuxContinueName(session)
	folder := c.Paths().AttemptFolder(*c.Run(target.ID), c.Run(target.ID).Attempts[0])
	spec := c.Spec(*c.Run(target.ID), c.Run(target.ID).Attempts[0], "")
	if spec == nil {
		t.Fatal("no spec")
	}
	spec.Name = continuing
	simStarting(t, simRunner, *spec)
	decision := core.AfkDecision{ID: "afk-1", RunID: target.ID, Session: new("bl-afk-race"), Backend: new(core.BackendPi),
		Situation: core.AfkSituationKindHold, Key: "h", Folder: "afk/01", StartedAt: jsonx.Date{Time: testkit.ContractTime}}
	c.Lock()
	c.autopilot.add(decision)
	c.EndTurn()
	afkFolder := filepath.Join(filepath.Dir(folder), "afk-session")
	afkAgent := "afk"
	simStarting(t, simRunner, runner.SessionSpec{Name: "bl-afk-race", SessionID: "afk", Backend: core.BackendPi, Model: "m",
		Prompt: "Decide.", Directory: folder, AttemptFolder: afkFolder, Output: filepath.Join(afkFolder, "decision.md"),
		Outcomes: []string{"leave"}, AgentID: &afkAgent})
	stop := newH5m4Latch()
	simRunner.BeforeStop = func(string) { stop.wait() }
	c.Drop(target.ID, nil)
	c.Lock()
	c.book.busy[target.ID] = struct{}{}
	c.EndTurn()
	erased := make(chan error, 1)
	go func() { erased <- c.Erase(bgCtx, target.ID) }()
	// The erasure cannot pass its wait for the busy session while the test holds the flag, so the
	// checks below hold whenever the erasure has got that far; no sleep is needed to see them.
	runFile := filepath.Join(c.Paths().RunFolder(target), "run.json")
	if _, err := os.Stat(runFile); err != nil {
		t.Fatalf("run.json gone while the erasure waits: %v", err)
	}
	if spaces.didStartRemove() {
		t.Fatal("the removal started before the cleanup finished")
	}
	spaces.cleanup.open()
	c.Lock()
	delete(c.book.busy, target.ID)
	c.EndTurn()
	if _, err := os.Stat(runFile); err != nil {
		t.Fatalf("run.json gone while a session is still busy: %v", err)
	}
	if !simRunner.IsRunning(continuing) || !simRunner.IsRunning("bl-afk-race") || !simRunner.IsRunning(otherSession) {
		t.Fatal("a session stopped before the erasure stopped it")
	}
	stop.open()
	spaces.removal.open()
	if err := <-erased; err != nil {
		t.Fatalf("erase: %v", err)
	}
	if c.Run(target.ID) != nil {
		t.Fatal("the erased run is still there")
	}
	if _, err := os.Stat(c.Paths().RunFolder(target)); err == nil {
		t.Fatal("the erased run's folder remains")
	}
	if simRunner.IsRunning(continuing) || simRunner.IsRunning("bl-afk-race") {
		t.Fatal("an erased run's session still runs")
	}
	if !simRunner.IsRunning(otherSession) {
		t.Fatal("the other run's session stopped")
	}
	if c.Run(other.ID) == nil {
		t.Fatal("the other run was erased")
	}
}

// coordIsErasing reports whether run id is being erased, through the lock.
func coordIsErasing(c *Coordinator, id string) bool {
	c.Lock()
	defer c.EndTurn()
	return c.book.isErasing(id)
}

// TestAReviewAndATicketEachComeBackOnce
func TestAReviewAndATicketEachComeBackOnce(t *testing.T) {
	t.Parallel()
	reviewBelt := core.DefaultBelt("reviews", "Review requests", []core.TriggerEntry{{ID: "reviews",
		Trigger: core.Trigger{Kind: core.TriggerKindReviews, Reviews: new(core.DefaultReviewFilter())}, Repos: []string{"acme/web"}}},
		flowTriageApproval(core.TriggerKindReviews))
	pull := core.Subject{Kind: core.SubjectKindPullRequest, Repo: "acme/web", Number: 57, Title: "Move checkout", Body: "Uses the new API.",
		URL: "https://github.com/acme/web/pull/57", Base: new("develop"), Occurrence: new("100"), IsOpen: true}
	review := newHarness(t, harnessOptions{Issues: []core.Subject{}, Belts: []core.Belt{reviewBelt},
		Reviews: []github.FakeReviewRequest{{Pull: pull, Request: "100"}}})
	review.coord.Lock()
	review.coord.intake.pollTriggers(bgCtx, true)
	review.coord.EndTurn()
	review.coord.Idle()
	original := *review.Run()
	review.coord.Lock()
	review.coord.book.update(original.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	review.coord.EndTurn()
	reviewCalls := len(review.github.Calls())
	if err := review.coord.Erase(bgCtx, original.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if n := len(review.coord.Runs()); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
	if n := len(review.github.Calls()); n != reviewCalls {
		t.Fatal("the erasure called GitHub")
	}
	review.coord.Lock()
	review.coord.intake.pollTriggers(bgCtx, true)
	review.coord.intake.pollTriggers(bgCtx, true)
	review.coord.EndTurn()
	runs := review.coord.Runs()
	if len(runs) != 1 || runs[0].ID != "reviews:acme/web#57@100" || runs[0].Generation != 1 || runs[0].StageID != runs[0].Stages[0].ID {
		t.Fatalf("review runs = %+v, want one fresh run", runs)
	}
	for _, attempt := range runs[0].Attempts {
		if attempt.Outcome != nil || attempt.Session != nil {
			t.Fatal("the fresh review run carries history")
		}
	}

	// The ticket half: an unassigned Jira ticket comes back once.
	ticket := core.Subject{Kind: core.SubjectKindJira, Repo: "", Key: "PAY-31", Title: "Refund part of a payment",
		Body: "Up to what was paid.", URL: "https://acme.atlassian.net/browse/PAY-31", IsOpen: true}
	jiraBelt := flowBelt(core.TriggerKindJira, "", flowTriageApproval(core.TriggerKindJira))
	jira := newHarness(t, harnessOptions{Issues: []core.Subject{}, Belts: []core.Belt{jiraBelt}, Tickets: []core.Subject{ticket}})
	jira.Tick()
	ticketRun := *jira.Run()
	jira.Turn(func() {
		jira.coord.book.update(ticketRun.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	})
	jiraCalls := len(jira.jira.Calls())
	if err := jira.coord.Erase(bgCtx, ticketRun.ID); err != nil {
		t.Fatalf("erase ticket: %v", err)
	}
	if n := len(jira.coord.Runs()); n != 0 {
		t.Fatalf("ticket runs = %d, want 0", n)
	}
	if n := len(jira.jira.Calls()); n != jiraCalls {
		t.Fatal("the erasure called Jira")
	}
	jira.Turn(func() {
		jira.coord.intake.pollTriggers(bgCtx, true)
		jira.coord.intake.pollTriggers(bgCtx, true)
	})
	jira.coord.Idle()
	again := jira.coord.Runs()
	if len(again) != 1 || again[0].ID != ticketRun.ID || again[0].Generation != 1 || again[0].StageID != again[0].Stages[0].ID {
		t.Fatalf("ticket runs = %+v, want the one fresh run", again)
	}
	for _, attempt := range again[0].Attempts {
		if attempt.Outcome != nil || attempt.Session != nil {
			t.Fatal("the fresh ticket run carries history")
		}
	}
}

// TestAFailedEraseStaysVisibleAndARetryFinishes
func TestAFailedEraseStaysVisibleAndARetryFinishes(t *testing.T) {
	t.Parallel()
	spaces := newH5m4GatedWorkspaces()
	spaces.cleanup.open()
	spaces.removal.open()
	simRunner := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
	h := newHarness(t, harnessOptions{})
	c := coordWith(t, h, func(s *Services) { s.Workspaces = spaces; s.Runner = simRunner })
	run := addRun(c, c.Belts()[0].Belt, issue142())
	c.Lock()
	c.sessions.start(bgCtx, run.ID, nil)
	c.book.update(run.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	c.EndTurn()
	failure := "tmux did not stop"
	simRunner.StopError = &failure
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "tmux did not stop") {
		t.Fatalf("erase error = %v, want the stop failure", err)
	}
	if c.Run(run.ID) == nil || !coordIsErasing(c, run.ID) {
		t.Fatal("the run is gone or not marked erasing")
	}
	if _, err := os.Stat(c.Paths().RunFolder(run)); err != nil {
		t.Fatal("the run folder was removed after a failed stop")
	}
	c.CheckNow(bgCtx)
	if got := c.Runs(); len(got) != 1 || got[0].ID != run.ID || got[0].Generation != 0 {
		t.Fatalf("runs after the failed erase = %+v, want the same run at generation 0", got)
	}

	simRunner.StopError = nil
	spaces.setFailRemove(true)
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "work folder") {
		t.Fatalf("erase error = %v, want the busy work folder", err)
	}
	if c.Run(run.ID) == nil {
		t.Fatal("the run is gone after a failed workspace removal")
	}
	c.CheckNow(bgCtx)
	if n := len(c.Runs()); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}

	spaces.setFailRemove(false)
	decisions := c.Paths().AfkDecisions()
	if err := os.Remove(decisions); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(decisions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "AFK") {
		t.Fatalf("erase error = %v, want the AFK save failure", err)
	}
	if c.Run(run.ID) == nil {
		t.Fatal("the run is gone after a failed AFK save")
	}
	if err := os.RemoveAll(decisions); err != nil {
		t.Fatal(err)
	}

	parent := filepath.Dir(c.Paths().RunFolder(run))
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "saved folder") {
		t.Fatalf("erase error = %v, want the saved folder failure", err)
	}
	if c.Run(run.ID) == nil {
		t.Fatal("the run is gone after a failed deletion")
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}

	_ = os.RemoveAll(c.Paths().WorkFolder(run))
	if err := c.Erase(bgCtx, run.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if c.Run(run.ID) != nil {
		t.Fatal("the run is still there after the retry")
	}
	if _, pending := h.storage.PendingErasure(run.ID); pending {
		t.Fatal("the erasure is still pending")
	}
	c.CheckNow(bgCtx)
	if got := c.Runs(); len(got) != 1 || got[0].Generation != 1 {
		t.Fatalf("runs after the retry = %+v, want one replacement at generation 1", got)
	}
}

// TestAnInFlightReviewCheckDoesNotJoinUntilErasureFinishes checks that an in-flight review check does not join until the erasure finishes:
// a review check that is in flight when its run is erased does not bring the run back until the erasure
// has finished, and then the request comes back once, as a fresh run. The in-flight check is held at its
// review read (the fake's BeforeLatestReview hook, setReviewGate), and the erasure at the removal
// of the workspace.
func TestAnInFlightReviewCheckDoesNotJoinUntilErasureFinishes(t *testing.T) {
	t.Parallel()
	pull := core.Subject{Kind: core.SubjectKindPullRequest, Repo: "acme/web", Number: 57, Title: "Move checkout", Body: "Uses the new API.",
		URL: "https://github.com/acme/web/pull/57", Base: new("develop"), Occurrence: new("100"), IsOpen: true}
	spaces := newH5m4GatedWorkspaces()
	spaces.cleanup.open()
	h := newHarness(t, harnessOptions{Issues: []core.Subject{}, Belts: []core.Belt{flowBelt(core.TriggerKindReviews, "", nil)},
		Reviews: []github.FakeReviewRequest{{Pull: pull, Request: "100"}}})
	c := h.withServices(t, func(s *Services) { s.Workspaces = spaces })
	c.Tick(bgCtx)
	c.Idle()
	if len(c.Runs()) != 1 {
		t.Fatalf("runs after the first tick = %d, want 1", len(c.Runs()))
	}
	original := c.Runs()[0]
	c.Lock()
	c.book.update(original.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
	c.EndTurn()

	entered := newH5m4Latch()
	gate := newH5m4Latch()
	h.github.BeforeLatestReview = func(context.Context) {
		entered.open()
		gate.wait()
	}
	checked := make(chan struct{})
	go func() {
		defer close(checked)
		c.CheckNow(bgCtx)
	}()
	<-entered.ch
	erased := make(chan error, 1)
	go func() { erased <- c.Erase(bgCtx, original.ID) }()
	testkit.Eventually(t, 20*time.Second, "the erasure starts removing the workspace", spaces.didStartRemove)
	gate.open()
	<-checked
	if runs := c.Runs(); len(runs) != 1 || runs[0].ID != original.ID {
		t.Fatalf("runs while the erasure runs = %+v, want the original run", runs)
	}
	spaces.removal.open()
	if err := <-erased; err != nil {
		t.Fatalf("erase: %v", err)
	}
	if len(c.Runs()) == 0 {
		c.CheckNow(bgCtx)
	}
	runs := c.Runs()
	if len(runs) != 1 || runs[0].ID != original.ID || runs[0].Generation != 1 || runs[0].StageID != runs[0].Stages[0].ID {
		t.Fatalf("runs after the erasure = %+v, want the request back once as a fresh run", runs)
	}
}
