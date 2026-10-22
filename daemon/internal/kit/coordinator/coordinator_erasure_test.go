// Coordinator tests of run erasure: erasing a finished run removes only that run's folders and
// decisions, waits for the cleanup and the busy sessions, leaves a replacement that a poll admits
// fresh, refuses an erasure of an active run, recovers an interrupted erasure after a restart, and
// waits for a continue launch.

package coordinator

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// gatedWorkspaces: cleanup and removal wait on latches, and removal can fail.
type gatedWorkspaces struct {
	cleanup       *coordLatch
	removal       *coordLatch
	mu            sync.Mutex
	failRemove    bool
	removeStarted bool
}

// newH5m4GatedWorkspaces builds the gated workspaces with cleanup and removal not yet released.
func newH5m4GatedWorkspaces() *gatedWorkspaces {
	return &gatedWorkspaces{cleanup: newH5m4Latch(), removal: newH5m4Latch()}
}

// didStartRemove reports whether a removal has begun.
func (w *gatedWorkspaces) didStartRemove() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.removeStarted
}

// setFailRemove makes the next removals fail with the busy folder error.
func (w *gatedWorkspaces) setFailRemove(fail bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failRemove = fail
}

// Prepare makes the checkout directory and returns the workspace folder.
func (w *gatedWorkspaces) Prepare(_ context.Context, spec runner.WorkspaceSpec) (string, error) {
	if err := os.MkdirAll(spec.CheckoutDirectory(), 0o755); err != nil {
		return "", err
	}
	return spec.Directory(), nil
}

// Fingerprint is empty: the gated workspaces track nothing.
func (w *gatedWorkspaces) Fingerprint(context.Context, string) string { return "" }

// Restore does nothing.
func (w *gatedWorkspaces) Restore(context.Context, string) {}

// CleanUp waits for the cleanup gate, then removes folder.
func (w *gatedWorkspaces) CleanUp(_ context.Context, _ []string, folder string) {
	w.cleanup.wait()
	_ = os.RemoveAll(folder)
}

// MergeLegs has nothing to merge.
func (w *gatedWorkspaces) MergeLegs(context.Context, []string, runner.WorkspaceSpec) runner.MergeResult {
	return runner.MergeResult{Kind: runner.MergeResultNothing}
}

// Remove marks the removal started, waits for its gate, then fails or removes folder.
func (w *gatedWorkspaces) Remove(_ context.Context, _ []string, folder string) error {
	w.mu.Lock()
	w.removeStarted = true
	w.mu.Unlock()
	w.removal.wait()
	w.mu.Lock()
	fail := w.failRemove
	w.mu.Unlock()
	if fail {
		return core.NewFailure("The work folder is busy.")
	}
	if _, err := os.Stat(folder); err == nil {
		return os.RemoveAll(folder)
	}
	return nil
}

// tmuxStopShell: kill-session fails while a kill error is set.
type tmuxStopShell struct {
	mu       sync.Mutex
	killFail *string
}

// setKillFail makes kill-session fail with message, or succeed for nil.
func (s *tmuxStopShell) setKillFail(message *string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.killFail = message
}

// Run answers tmux commands: kill-session fails while set, has-session finds the session.
func (s *tmuxStopShell) Run(_ context.Context, _ string, args []string, _ string, _ *string) shell.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(args, "kill-session") && s.killFail != nil {
		return shell.Result{Status: 1, Stderr: *s.killFail + "\n"}
	}
	return shell.Result{Status: 0}
}

// continueLaunchShell: has-session reports the continuation absent,
// and new-session waits at a gate and notes whether the run folder was deleted by then.
type continueLaunchShell struct {
	mu                  sync.Mutex
	commands            [][]string
	entered             *coordSignal
	open                *coordLatch
	runFolder           string
	createdAfterDeleted bool
}

// newH5m4ContinueLaunchShell builds the shell for the run folder that the tests watch.
func newH5m4ContinueLaunchShell() *continueLaunchShell {
	return &continueLaunchShell{entered: newH5m4Signal(), open: newH5m4Latch()}
}

// Run records the command and answers it as the continuation launch needs.
func (s *continueLaunchShell) Run(_ context.Context, executable string, args []string, _ string, _ *string) shell.Result {
	s.mu.Lock()
	s.commands = append(s.commands, append([]string{executable}, args...))
	folder := s.runFolder
	s.mu.Unlock()
	if slices.Contains(args, "has-session") {
		return shell.Result{Status: 1, Stderr: "can't find session\n"}
	}
	if slices.Contains(args, "new-session") {
		s.entered.fire()
		s.open.wait()
		if folder != "" {
			if _, err := os.Stat(filepath.Join(folder, "run.json")); err != nil {
				s.mu.Lock()
				s.createdAfterDeleted = true
				s.mu.Unlock()
			}
		}
		return shell.Result{Status: 0}
	}
	return shell.Result{Status: 0}
}

// setRunFolder tells the shell which run folder a created continuation must still find.
func (s *continueLaunchShell) setRunFolder(folder string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runFolder = folder
}

// enteredCh closes when a new-session call reaches the gate.
func (s *continueLaunchShell) enteredCh() <-chan struct{} { return s.entered.ch }

// release lets the held new-session call finish.
func (s *continueLaunchShell) release() { s.open.open() }

// createdAfterRunDeleted reports whether a continuation was created after its run folder was deleted.
func (s *continueLaunchShell) createdAfterRunDeleted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createdAfterDeleted
}

// commandList is a copy of every command the shell was asked to run.
func (s *continueLaunchShell) commandList() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.commands...)
}

// coordTree is every regular file under root, keyed by path, with its bytes.
func coordTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			out[path] = string(data)
		}
		return nil
	})
	return out
}

// treeContains reports whether any file under root has text in it.
func treeContains(root, text string) bool {
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found {
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), text) {
			found = true
		}
		return nil
	})
	return found
}

// coordWith is a coordinator over the harness's folder whose services change as change says.
func coordWith(t *testing.T, h *harness, change func(s *Services)) *Coordinator {
	t.Helper()
	return h.withServices(t, change)
}

// addRun adds a run of belt on subject to c's book, without saving it apart from the book's own save.
func addRun(c *Coordinator, belt core.Belt, subject core.Subject) core.Run {
	var added core.Run
	c.Lock()
	added = c.book.add(core.NewRun(core.RunArgs{Belt: belt, Subject: subject, Now: testkit.ContractTime}))
	c.EndTurn()
	return added
}

// issue142 is the default issue on acme/billing, as the erasure tests name it.
func issue142() core.Subject {
	return core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Export invoices as CSV", Labels: []string{"agent"}, IsOpen: true}
}

// TestActiveRunsCannotBeErased
func TestActiveRunsCannotBeErased(t *testing.T) {
	t.Parallel()
	for _, state := range []core.RunState{core.RunStateQueued, core.RunStateRunning, core.RunStateWaiting, core.RunStateHeld, core.RunStateParked} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{Issues: []core.Subject{}})
			c := h.Coord()
			belt := c.Belts()[0].Belt
			added := addRun(c, belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 7, Title: "Keep"})
			if state != core.RunStateQueued {
				c.Lock()
				c.book.update(added.ID, nil, nil, func(r *core.Run) { r.State = state })
				c.EndTurn()
			}
			folder := c.Paths().RunFolder(added)
			err := c.Erase(bgCtx, added.ID)
			if err == nil || !strings.Contains(err.Error(), "Drop") {
				t.Fatalf("erase error = %v, want a refusal that mentions Drop", err)
			}
			if got := c.Run(added.ID); got == nil || got.State != state {
				t.Fatalf("state after the refusal = %+v, want %s", got, state)
			}
			if _, statErr := os.Stat(filepath.Join(folder, "run.json")); statErr != nil {
				t.Fatalf("run.json gone: %v", statErr)
			}
			if _, ok := h.storage.PendingErasure(added.ID); ok {
				t.Fatal("an erasure was recorded for an active run")
			}
		})
	}
}

// TestErasureRemovesOnlyTheSelectedRun
func TestErasureRemovesOnlyTheSelectedRun(t *testing.T) {
	t.Parallel()
	for _, state := range []core.RunState{core.RunStateShipped, core.RunStateDropped} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			c := h.Coord()
			otherBelt := core.DefaultBelt("other", "Other", []core.TriggerEntry{manualEntry("other", "acme/billing")}, nil)
			if _, err := c.SaveBelt(otherBelt); err != nil {
				t.Fatalf("save belt: %v", err)
			}
			c.Tick(bgCtx)
			c.Idle()
			first := *h.Run()
			belt := c.Belts()[0].Belt
			second := addRun(c, belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 143, Title: "Keep me"})
			third := addRun(c, otherBelt, core.Subject{Kind: core.SubjectKindManual, Repo: "acme/billing", Key: "9", Title: "Elsewhere"})
			secondState := core.RunStateShipped
			if state == core.RunStateShipped {
				secondState = core.RunStateDropped
			}
			c.Lock()
			c.book.update(first.ID, nil, nil, func(r *core.Run) {
				r.State = state
				if len(r.Attempts) > 0 {
					cost := 2_000_000_000
					total := 40
					r.Attempts[0].Usage = &core.Usage{CostNanos: &cost, TotalTokens: &total}
					outcome := "from-before"
					r.Attempts[0].Outcome = &outcome
				}
			})
			c.book.update(second.ID, nil, nil, func(r *core.Run) { r.State = secondState })
			c.book.update(third.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
			c.EndTurn()
			writeText(t, filepath.Join(c.Paths().RunFolder(first), "01-triage-1", "spec.md"), "OLD OUTPUT")
			writeText(t, filepath.Join(c.Paths().RunFolder(first), "01-triage-1", "artifacts", "export.csv"), "artifact")
			writeText(t, filepath.Join(c.Paths().WorkFolder(first), "worktree", "README"), "checkout")
			writeText(t, filepath.Join(c.Paths().RunFolder(second), "note.txt"), "kept")
			gone := core.AfkDecision{ID: "gone", RunID: first.ID, Status: core.AfkDecisionStatusApplied, Session: new("bl-afk-gone"),
				Situation: core.AfkSituationKindApproval, Key: "gate", Folder: "afk/01", StartedAt: jsonx.Date{Time: testkit.ContractTime}}
			stays := core.AfkDecision{ID: "stays", RunID: second.ID, Status: core.AfkDecisionStatusApplied,
				Situation: core.AfkSituationKindApproval, Key: "gate", Folder: "afk/01", StartedAt: jsonx.Date{Time: testkit.ContractTime}}
			c.Lock()
			c.autopilot.add(gone)
			c.autopilot.add(stays)
			c.book.note("about the erased run", &first.ID)
			c.book.note("about the kept run", &second.ID)
			c.EndTurn()
			library := coordTree(t, c.Paths().Belts())
			maps.Copy(library, coordTree(t, c.Paths().Agents()))
			settingsBefore, _ := os.ReadFile(c.Paths().Settings())
			calls := len(h.github.Calls())
			if err := c.Erase(bgCtx, first.ID); err != nil {
				t.Fatalf("erase: %v", err)
			}
			if c.Run(first.ID) != nil || c.Run(second.ID) == nil || c.Run(third.ID) == nil || c.Run(third.ID).State != core.RunStateShipped {
				t.Fatal("the wrong runs are left")
			}
			if _, err := os.Stat(c.Paths().RunFolder(first)); err == nil {
				t.Fatal("the erased run's folder remains")
			}
			if _, err := os.Stat(c.Paths().WorkFolder(first)); err == nil {
				t.Fatal("the erased run's work folder remains")
			}
			if _, err := os.Stat(filepath.Join(c.Paths().RunFolder(second), "note.txt")); err != nil {
				t.Fatalf("the kept run lost its note: %v", err)
			}
			if got := decisionIDs(coordDecisions(h)); !slices.Equal(got, []string{"stays"}) {
				t.Fatalf("decisions = %v, want [stays]", got)
			}
			for _, notice := range h.coord.Notices() {
				if notice.RunID != nil && *notice.RunID == first.ID {
					t.Fatal("a notice about the erased run remains")
				}
			}
			if !noticeText(h.coord, "about the kept run") {
				t.Fatal("the kept run's notice is gone")
			}
			after := coordTree(t, c.Paths().Belts())
			maps.Copy(after, coordTree(t, c.Paths().Agents()))
			if len(after) != len(library) {
				t.Fatalf("library files = %d, want %d", len(after), len(library))
			}
			for k, v := range library {
				if after[k] != v {
					t.Fatalf("library file %s changed", k)
				}
			}
			if settingsAfter, _ := os.ReadFile(c.Paths().Settings()); string(settingsAfter) != string(settingsBefore) {
				t.Fatal("the settings changed")
			}
			if n := len(h.github.Calls()); n != calls {
				t.Fatalf("GitHub calls = %d, want %d", n, calls)
			}
			for _, record := range h.storage.LoadBelts() {
				if record.Belt.ID == "other" && record.LastFired() != nil {
					t.Fatal("the other belt's schedule moved")
				}
			}
			if _, pending := h.storage.PendingErasure(first.ID); pending {
				t.Fatal("the erasure is still pending")
			}
			// A restart sees the same runs and decisions.
			restarted := h.RestartWith(durationRunner2(h.Script, 0))
			if got := runIDs(restarted.Runs()); !slices.Equal(got, sortedOf(second.ID, third.ID)) {
				t.Fatalf("runs after restart = %v", got)
			}
			if got := decisionIDs(restarted.ReadAfkDecisions()); !slices.Equal(got, []string{"stays"}) {
				t.Fatalf("decisions after restart = %v", got)
			}
		})
	}
}

// decisionIDs is the decisions' ids, in log order.
func decisionIDs(decisions []core.AfkDecision) []string {
	ids := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		ids = append(ids, decision.ID)
	}
	return ids
}

// runIDs is the runs' ids, sorted.
func runIDs(runs []core.Run) []string {
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return sortedOf(ids...)
}

// noticeText reports whether a notice with exactly text is recorded.
func noticeText(c *Coordinator, text string) bool {
	return slices.ContainsFunc(c.Notices(), func(notice Notice) bool { return notice.Text == text })
}
