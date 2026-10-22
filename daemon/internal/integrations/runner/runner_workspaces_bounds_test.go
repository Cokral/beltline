// Tests that the workspace's git and gh commands run under their fixed limits, and
// that a command which hits its limit fails with the timeout named for its tool.

package runner

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// limitOf is the limit a call's deadline was set to, to the minute.
func limitOf(c shell.Call) time.Duration {
	return time.Until(c.Deadline).Round(time.Minute)
}

// workspaceStall stalls each command whose arguments include word until its context
// ends, and answers every other command with ok.
type workspaceStall struct {
	word    string
	stalled shell.Result
	ok      shell.Result
}

func (s workspaceStall) Run(ctx context.Context, _ string, args []string, _ string, _ *string) shell.Result {
	for _, arg := range args {
		if arg == s.word {
			<-ctx.Done()
			return s.stalled
		}
	}
	return s.ok
}

// shortContext is a caller context that ends after a short time, long before any limit.
func shortContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// cloneSpec is a read-only base checkout of repo in a fresh work folder.
func cloneSpec(paths store.Paths, repo string) WorkspaceSpec {
	return NewWorkspaceSpec(WorkspaceSpecArgs{
		Repo: repo, Base: "main", Branch: "beltline/x",
		Checkout: WorkspaceCheckout{Kind: WorkspaceCheckoutKindBase},
		Folder:   filepath.Join(paths.Work(), "issues", "acme", "billing", "1"),
	})
}

// seedClone makes repo's clone look as an earlier run left it, so the next prepare fetches.
func seedClone(t *testing.T, paths store.Paths, repo string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(paths.Clone(repo), ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceCommandsRunUnderTheirLimits(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	seedClone(t, paths, "acme/billing")
	rec := &shell.Recorder{}
	if _, err := NewGitWorkspaces(paths, rec).Prepare(context.Background(), cloneSpec(paths, "acme/billing")); err != nil {
		t.Fatal(err)
	}
	calls := rec.Calls()
	if len(calls) == 0 || calls[0].Line() != "git fetch --prune origin" {
		t.Fatalf("calls = %v, want the fetch first", calls)
	}
	for i, call := range calls {
		if call.Deadline.IsZero() {
			t.Fatalf("%s has no deadline", call.Line())
		}
		want := shell.CommandLimit
		if i == 0 {
			want = shell.TransferLimit
		}
		if got := limitOf(call); got != want {
			t.Errorf("%s: limit %s, want %s", call.Line(), got, want)
		}
	}
}

func TestTheCloneOfANewRepositoryRunsGHUnderTheTransferLimit(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	rec := &shell.Recorder{}
	if _, err := NewGitWorkspaces(paths, rec).Prepare(context.Background(), cloneSpec(paths, "acme/billing")); err != nil {
		t.Fatal(err)
	}
	calls := rec.Calls()
	if len(calls) == 0 || calls[0].Executable != "gh" || calls[0].Args[0] != "repo" || calls[0].Args[1] != "clone" {
		t.Fatalf("calls = %v, want gh repo clone first", calls)
	}
	if got := limitOf(calls[0]); got != shell.TransferLimit {
		t.Errorf("gh repo clone: limit %s, want %s", got, shell.TransferLimit)
	}
}

func TestMergeAndRemovalCommandsRunUnderTheCommandLimit(t *testing.T) {
	t.Parallel()
	repos := []string{"acme/billing", "acme/web"}
	paths, folder := removalPlan(t, repos)
	rec := &shell.Recorder{Reply: func(c shell.Call) shell.Result {
		if len(c.Args) > 0 && c.Args[0] == "rev-list" {
			return shell.Result{Stdout: "1\n"}
		}
		return shell.Result{}
	}}
	workspaces := NewGitWorkspaces(paths, rec)
	ctx := context.Background()
	if result := workspaces.MergeLegs(ctx, []string{"beltline/leg"}, cloneSpec(paths, "acme/billing")); result.Kind != MergeResultMerged {
		t.Fatalf("merge = %+v, want merged", result)
	}
	if err := workspaces.Remove(ctx, repos, folder); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, call := range rec.Calls() {
		if call.Deadline.IsZero() {
			t.Fatalf("%s has no deadline", call.Line())
		}
		// The merge prepares the run's checkout first, and that fetch is a transfer.
		want := shell.CommandLimit
		if call.Args[0] == "fetch" {
			want = shell.TransferLimit
		}
		if got := limitOf(call); got != want {
			t.Errorf("%s: limit %s, want %s", call.Line(), got, want)
		}
		seen[call.Args[0]] = true
	}
	for _, sub := range []string{"rev-list", "merge", "worktree"} {
		if !seen[sub] {
			t.Errorf("no %s command was checked", sub)
		}
	}
}

func TestAWorkspaceFetchThatHitsItsLimitFailsWithTheTimeout(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	seedClone(t, paths, "acme/billing")
	stalled := workspaceStall{word: "fetch", stalled: shell.Result{Stdout: "From github.com:acme/billing\n"}}
	_, err := NewGitWorkspaces(paths, stalled).Prepare(shortContext(t), cloneSpec(paths, "acme/billing"))
	if err == nil || err.Error() != "git fetch: "+shell.TimedOutReason {
		t.Fatalf("prepare error = %v, want git fetch: command timed out", err)
	}
}

func TestAMergeThatHitsItsLimitIsAbortedAndNamedAsTimedOut(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	// rev-list counts one leg commit; the merge of it stalls after printing a conflict line.
	merge := workspaceStall{word: "--no-ff", ok: shell.Result{Stdout: "1\n"}, stalled: shell.Result{Status: 1, Stdout: "CONFLICT (content): Merge conflict in a.go\n"}}
	result := NewGitWorkspaces(paths, merge).MergeLegs(shortContext(t), []string{"beltline/leg"}, cloneSpec(paths, "acme/billing"))
	if result.Kind != MergeResultConflict || len(result.Names) != 1 || result.Names[0] != "git merge: "+shell.TimedOutReason {
		t.Fatalf("merge = %+v, want a conflict named as a timed-out git merge", result)
	}
}

func TestAMergeLegCountThatHitsItsLimitIsNotAnEmptyLeg(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	count := workspaceStall{word: "--count", stalled: shell.Result{Status: 1}}
	result := NewGitWorkspaces(paths, count).MergeLegs(shortContext(t), []string{"beltline/leg"}, cloneSpec(paths, "acme/billing"))
	if result.Kind == MergeResultNothing {
		t.Fatalf("a leg whose count timed out read as nothing to merge: %+v", result)
	}
	if result.Kind != MergeResultConflict || len(result.Names) != 1 || result.Names[0] != "git rev-list: "+shell.TimedOutReason {
		t.Fatalf("merge = %+v, want the timed-out rev-list named", result)
	}
}

func TestARemovalThatHitsItsLimitKeepsTheFolderAndNamesGit(t *testing.T) {
	t.Parallel()
	repos := []string{"acme/billing", "acme/web"}
	paths, folder := removalPlan(t, repos)
	stalled := workspaceStall{word: "remove", stalled: shell.Result{Status: 1}}
	err := NewGitWorkspaces(paths, stalled).Remove(shortContext(t), repos, folder)
	if err == nil || !strings.Contains(err.Error(), "git worktree: "+shell.TimedOutReason) {
		t.Fatalf("remove error = %v, want git worktree named as timed out", err)
	}
	if !runnerPathExists(folder) {
		t.Fatal("the run folder was removed after a worktree removal timed out")
	}
}

func TestAnOrdinaryWorkspaceFailureKeepsItsOwnMessage(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	seedClone(t, paths, "acme/billing")
	refused := &shell.Recorder{Reply: func(shell.Call) shell.Result { return shell.Result{Status: 1, Stderr: "boom\n"} }}
	_, err := NewGitWorkspaces(paths, refused).Prepare(context.Background(), cloneSpec(paths, "acme/billing"))
	if err == nil || err.Error() != "git fetch: boom" {
		t.Fatalf("prepare error = %v, want the git output unchanged", err)
	}
}

// cancelOnWord cancels the caller's context when a command whose arguments include word
// starts, and answers with stalled once that context has ended. It counts the merge aborts
// the runner was asked to run, so a test can see whether one started after the caller ended.
type cancelOnWord struct {
	word    string
	cancel  context.CancelFunc
	stalled shell.Result
	ok      shell.Result
	aborted int
}

func (c *cancelOnWord) Run(ctx context.Context, _ string, args []string, _ string, _ *string) shell.Result {
	for _, arg := range args {
		if arg == c.word {
			c.cancel()
			<-ctx.Done()
			return c.stalled
		}
	}
	if slices.Contains(args, "--abort") {
		c.aborted++
	}
	return c.ok
}

func TestAMergeLegCountThatIsCancelledIsNotAnEmptyLeg(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := &cancelOnWord{word: "--count", cancel: cancel, stalled: shell.Result{Status: 1}}
	result := NewGitWorkspaces(paths, count).MergeLegs(ctx, []string{"beltline/leg"}, cloneSpec(paths, "acme/billing"))
	if result.Kind == MergeResultNothing {
		t.Fatalf("a leg whose count was cancelled read as nothing to merge: %+v", result)
	}
	if result.Kind != MergeResultConflict || len(result.Names) != 1 || result.Names[0] != "git rev-list: "+shell.CancelledReason {
		t.Fatalf("merge = %+v, want the cancelled rev-list named as cancelled", result)
	}
}

func TestACancelledMergeStartsNoAbortAndIsNamedAsCancelled(t *testing.T) {
	t.Parallel()
	paths := store.Paths{Root: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// rev-list counts one leg commit; the merge of it is cancelled after printing a conflict line.
	merge := &cancelOnWord{word: "--no-ff", cancel: cancel, ok: shell.Result{Stdout: "1\n"}, stalled: shell.Result{Status: 1, Stdout: "CONFLICT (content): Merge conflict in a.go\n"}}
	result := NewGitWorkspaces(paths, merge).MergeLegs(ctx, []string{"beltline/leg"}, cloneSpec(paths, "acme/billing"))
	if result.Kind != MergeResultConflict || len(result.Names) != 1 || result.Names[0] != "git merge: "+shell.CancelledReason {
		t.Fatalf("merge = %+v, want a conflict named as a cancelled git merge", result)
	}
	// The abort is bound to the caller like every other command: an ended caller starts none.
	if merge.aborted != 0 {
		t.Fatalf("merge --abort reached the runner %d times after the caller cancelled, want 0", merge.aborted)
	}
}

func TestCleanUpRemovesWorktreesUnderTheCommandLimit(t *testing.T) {
	t.Parallel()
	repos := []string{"acme/billing", "acme/web"}
	paths, folder := removalPlan(t, repos)
	rec := &shell.Recorder{}
	NewGitWorkspaces(paths, rec).CleanUp(context.Background(), repos, folder)
	calls := rec.Calls()
	if len(calls) == 0 {
		t.Fatal("CleanUp ran no worktree removal")
	}
	for _, call := range calls {
		if call.Deadline.IsZero() {
			t.Fatalf("%s has no deadline", call.Line())
		}
		if call.Args[0] != "worktree" {
			t.Errorf("%s: CleanUp ran a command other than worktree removal", call.Line())
		}
		if got := limitOf(call); got != shell.CommandLimit {
			t.Errorf("%s: limit %s, want %s", call.Line(), got, shell.CommandLimit)
		}
	}
}
