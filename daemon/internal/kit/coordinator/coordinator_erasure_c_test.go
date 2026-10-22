// The last coordinator tests of run erasure: a restart resumes an
// erasure at whichever step it stopped, a late completion of the old session does not change the
// replacement, a tmux stop failure keeps the run until the session stops, and an erasure waits for a
// continue launch before it deletes the run.

package coordinator

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// decisionsOfRun is the decisions the log holds for run id.
func decisionsOfRun(decisions []core.AfkDecision, id string) []core.AfkDecision {
	var out []core.AfkDecision
	for _, decision := range decisions {
		if decision.RunID == id {
			out = append(out, decision)
		}
	}
	return out
}

// TestARestartResumesErasureBeforeTheNextPoll checks that a restart resumes an erasure before the next poll.
// (one subtest per step at which the erasure stopped).
func TestARestartResumesErasureBeforeTheNextPoll(t *testing.T) {
	t.Parallel()
	for step := range 5 {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			t.Parallel()
			sim := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
				func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
			h := newHarness(t, harnessOptions{Runner: func(*harnessScript) runner.AgentRunner { return sim }})
			first := coordWith(t, h, func(s *Services) { s.Runner = sim })
			run := addRun(first, first.Belts()[0].Belt, issue142())
			first.Lock()
			first.sessions.start(bgCtx, run.ID, nil)
			first.EndTurn()
			session := runSession(first.Run(run.ID))
			if session == "" {
				t.Fatal("no session")
			}
			first.Lock()
			first.book.update(run.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
			decision := core.AfkDecision{ID: "d", RunID: run.ID, Status: core.AfkDecisionStatusApplied,
				Situation: core.AfkSituationKindHold, Key: "k", Folder: "afk/01"}
			first.autopilot.add(decision)
			first.EndTurn()
			writeText(t, filepath.Join(first.Paths().RunFolder(run), "old.txt"), "OLD OUTPUT")
			writeText(t, filepath.Join(first.Paths().WorkFolder(run), "readme"), "work")
			pending := store.PendingErasure{ID: run.ID, Generation: run.Generation, Folder: run.Folder(), Repos: run.Repos,
				Sessions: []string{session}}
			pending.StoppedSessions = step > 0
			pending.RemovedWorkspace = step > 1
			pending.RemovedDecisions = step > 2
			pending.RemovedRun = step > 3
			if pending.RemovedWorkspace {
				_ = os.RemoveAll(first.Paths().WorkFolder(run))
			}
			if pending.RemovedDecisions {
				first.Lock()
				_ = first.autopilot.removeDecisions(run.ID)
				first.EndTurn()
			}
			if pending.RemovedRun {
				if err := h.storage.DeleteRunFolder(run.Folder()); err != nil {
					t.Fatalf("delete run folder: %v", err)
				}
			}
			if err := h.storage.SavePendingErasure(pending); err != nil {
				t.Fatalf("save pending: %v", err)
			}
			second := coordWith(t, h, func(s *Services) { s.Runner = sim })
			second.Lock()
			erasing := second.book.isErasing(run.ID)
			second.EndTurn()
			if !erasing {
				t.Fatal("the restart does not know the erasure is pending")
			}
			second.Tick(bgCtx)
			second.Idle()
			if _, ok := h.storage.PendingErasure(run.ID); ok {
				t.Fatal("the pending erasure survived the restart")
			}
			if _, err := os.Stat(filepath.Join(second.Paths().WorkFolder(run), "readme")); err == nil {
				t.Fatal("the work folder survived the restart")
			}
			if n := len(decisionsOfRun(second.ReadAfkDecisions(), run.ID)); n != 0 {
				t.Fatalf("decisions for the erased run = %d, want 0", n)
			}
			runs := second.Runs()
			if len(runs) != 1 || runs[0].Generation != 1 {
				t.Fatalf("runs after the restart = %+v, want one replacement at generation 1", runs)
			}
			if treeContains(second.Paths().Runs(), "OLD OUTPUT") {
				t.Fatal("the old output is still on disk")
			}
			if step == 0 && sim.IsRunning(session) {
				t.Fatal("the session still runs after the restart")
			}
			second.Tick(bgCtx)
			second.Idle()
			if n := len(second.Runs()); n != 1 {
				t.Fatalf("runs after another tick = %d, want 1", n)
			}
		})
	}
}

// TestALateCompletionDoesNotChangeTheReplacement
func TestALateCompletionDoesNotChangeTheReplacement(t *testing.T) {
	t.Parallel()
	sim := runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return nil })
	h := newHarness(t, harnessOptions{Runner: func(*harnessScript) runner.AgentRunner { return sim }})
	c := coordWith(t, h, func(s *Services) { s.Runner = sim })
	run := addRun(c, c.Belts()[0].Belt, issue142())
	c.Lock()
	c.sessions.start(bgCtx, run.ID, nil)
	c.EndTurn()
	session := runSession(c.Run(run.ID))
	entered := newH5m4Signal()
	release := newH5m4Signal()
	sim.BeforeStatus = func(spec runner.SessionSpec) {
		if spec.Name == session {
			entered.fire()
			release.wait()
		}
	}
	checking := make(chan struct{})
	go func() {
		defer close(checking)
		c.Lock()
		c.sessions.check(bgCtx)
		c.EndTurn()
	}()
	entered.wait()
	c.Lock()
	c.book.update(run.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateDropped })
	c.EndTurn()
	if err := c.Erase(bgCtx, run.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}
	c.CheckNow(bgCtx)
	runs := c.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want the replacement", len(runs))
	}
	snapshot := runs[0]
	files := coordTree(t, c.Paths().RunFolder(snapshot))
	release.fire()
	<-checking
	after := c.Run(snapshot.ID)
	if after == nil || !reflect.DeepEqual(*after, snapshot) {
		t.Fatalf("the replacement changed after the late completion: %+v", after)
	}
	if !reflect.DeepEqual(coordTree(t, c.Paths().RunFolder(snapshot)), files) {
		t.Fatal("the replacement's files changed after the late completion")
	}
}

// TestATmuxStopFailureKeepsTheRunUntilTheSessionStops
func TestATmuxStopFailureKeepsTheRunUntilTheSessionStops(t *testing.T) {
	t.Parallel()
	shell := &tmuxStopShell{}
	spaces := newH5m4GatedWorkspaces()
	spaces.cleanup.open()
	spaces.removal.open()
	h := newHarness(t, harnessOptions{})
	c := coordWith(t, h, func(s *Services) {
		s.Workspaces = spaces
		s.Runner = runner.NewTmuxRunner(shell)
	})
	run := addRun(c, c.Belts()[0].Belt, issue142())
	c.Lock()
	c.book.update(run.ID, nil, nil, func(r *core.Run) {
		attempt := sessionSampleAttempt("build", 1, "01-build-1")
		session := "bl-live"
		attempt.Session = &session
		ended := jsonx.Date{Time: testkit.ContractTime}
		attempt.EndedAt = &ended
		r.Attempts = []core.Attempt{attempt}
		r.State = core.RunStateShipped
	})
	c.EndTurn()
	writeText(t, filepath.Join(c.Paths().RunFolder(*c.Run(run.ID)), "old.txt"), "OLD OUTPUT")
	busy := "session is busy"
	shell.setKillFail(&busy)
	if err := c.Erase(bgCtx, run.ID); err == nil || !strings.Contains(err.Error(), "session is busy") {
		t.Fatalf("erase error = %v, want the busy session", err)
	}
	if c.Run(run.ID) == nil || !coordIsErasing(c, run.ID) {
		t.Fatal("the run is gone or not marked erasing")
	}
	if spaces.didStartRemove() {
		t.Fatal("the workspace was removed while the session ran")
	}
	if _, err := os.Stat(filepath.Join(c.Paths().RunFolder(run), "old.txt")); err != nil {
		t.Fatal("the run's files were removed while the session ran")
	}
	c.CheckNow(bgCtx)
	if got := c.Runs(); len(got) != 1 || got[0].ID != run.ID || got[0].Generation != 0 {
		t.Fatalf("runs = %+v, want the same run at generation 0", got)
	}

	shell.setKillFail(nil)
	if err := c.Erase(bgCtx, run.ID); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if c.Run(run.ID) != nil {
		t.Fatal("the run is still there")
	}
	if _, pending := h.storage.PendingErasure(run.ID); pending {
		t.Fatal("the erasure is still pending")
	}
	if _, err := os.Stat(c.Paths().RunFolder(run)); err == nil {
		t.Fatal("the run folder remains")
	}
	c.CheckNow(bgCtx)
	if got := c.Runs(); len(got) != 1 || got[0].Generation != 1 {
		t.Fatalf("runs after the retry = %+v, want one replacement at generation 1", got)
	}
	if treeContains(c.Paths().Runs(), "OLD OUTPUT") {
		t.Fatal("the old output remains on disk")
	}
}

// TestErasureWaitsForAContinueLaunchBeforeDeleting
func TestErasureWaitsForAContinueLaunchBeforeDeleting(t *testing.T) {
	t.Parallel()
	spaces := newH5m4GatedWorkspaces()
	spaces.cleanup.open()
	shell := newH5m4ContinueLaunchShell()
	h := newHarness(t, harnessOptions{Issues: []core.Subject{}})
	c := coordWith(t, h, func(s *Services) {
		s.Workspaces = spaces
		s.Runner = runner.NewTmuxRunner(shell)
	})
	added := addRun(c, c.Belts()[0].Belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Export invoices as CSV"})
	c.Lock()
	c.book.update(added.ID, nil, nil, func(r *core.Run) {
		attempt := sessionSampleAttempt(r.StageID, 1, "01-triage-1")
		session := "bl-hist"
		attempt.Session = &session
		sessionID := "hist"
		attempt.SessionID = &sessionID
		ended := jsonx.Date{Time: testkit.ContractTime}
		attempt.EndedAt = &ended
		model := "model"
		if stage := r.Stage(); stage != nil {
			model = stage.Model
		}
		attempt.Settings = &core.SessionSettings{Model: model}
		r.Attempts = []core.Attempt{attempt}
		r.State = core.RunStateHeld
	})
	c.EndTurn()
	run := c.Run(added.ID)
	if run == nil {
		t.Fatal("the run is gone")
	}
	attempt := run.Attempts[0]
	spec := c.Spec(*run, attempt, "")
	if spec == nil {
		t.Fatal("no spec for the attempt")
	}
	if err := os.MkdirAll(spec.Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	shell.setRunFolder(c.Paths().RunFolder(*run))

	launching := make(chan error, 1)
	go func() {
		_, err := c.ContinueSession(bgCtx, run.ID, attempt)
		launching <- err
	}()
	<-shell.enteredCh()
	c.Drop(run.ID, nil)
	erasing := make(chan error, 1)
	go func() { erasing <- c.Erase(bgCtx, run.ID) }()

	if spaces.didStartRemove() {
		t.Fatal("the workspace was removed during the continue launch")
	}
	if coordIsErasing(c, run.ID) {
		t.Fatal("the erasure started during the continue launch")
	}
	if _, err := os.Stat(filepath.Join(c.Paths().RunFolder(*run), "run.json")); err != nil {
		t.Fatalf("run.json gone during the continue launch: %v", err)
	}
	c.Lock()
	continuing := c.sessions.continuing
	_, stillContinuing := continuing[run.ID]
	c.EndTurn()
	if !stillContinuing {
		t.Fatal("the run is not marked as continuing")
	}
	shell.release()
	if err := <-launching; err != nil {
		t.Fatalf("continue: %v", err)
	}
	spaces.removal.open()
	if err := <-erasing; err != nil {
		t.Fatalf("erase: %v", err)
	}
	if shell.createdAfterRunDeleted() {
		t.Fatal("the continue session was created after the run was deleted")
	}
	if c.Run(run.ID) != nil {
		t.Fatal("the run is still there")
	}
	if _, err := os.Stat(c.Paths().RunFolder(*run)); err == nil {
		t.Fatal("the run folder remains")
	}
	commands := shell.commandList()
	continueName := runner.TmuxContinueName("bl-hist")
	created, killed := -1, -1
	for i, command := range commands {
		if slices.Contains(command, "new-session") && slices.Contains(command, continueName) {
			created = i
		}
		if slices.Contains(command, "kill-session") && slices.Contains(command, "="+continueName) {
			killed = i
		}
	}
	if created < 0 || killed < 0 || created >= killed {
		t.Fatalf("commands = %v, want the continue created before it is killed", commands)
	}
}
