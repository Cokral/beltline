//go:build integration

package runner

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// TestMergeLegsMergesNothingMergesAndReportsConflicts runs GitWorkspaces.MergeLegs
// on a real clone: no branches, a leg with no commits, two legs that merge, and
// two legs that conflict, which the merge is aborted for.
func TestMergeLegsMergesNothingMergesAndReportsConflicts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := store.Paths{Root: filepath.Join(root, "data")}
	origin := seedOrigin(t, root)
	const repo = "acme/billing"
	clone := paths.Clone(repo)
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "clone", "-q", origin, clone)
	gitIn(t, clone, "branch", "leg-empty", "origin/main")
	leg := func(branch, file, text string) {
		t.Helper()
		gitIn(t, clone, "checkout", "-q", "-b", branch, "origin/main")
		if err := os.WriteFile(filepath.Join(clone, file), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, clone, "add", ".")
		gitIn(t, clone, "commit", "-qm", branch)
		gitIn(t, clone, "checkout", "-q", "--detach", "origin/main")
	}
	leg("leg-a", "a.txt", "a\n")
	leg("leg-b", "b.txt", "b\n")
	leg("leg-c", "README.md", "c\n")
	leg("leg-d", "README.md", "d\n")

	folder := filepath.Join(paths.Work(), "run")
	workspaces := NewGitWorkspaces(paths, nil)
	ctx := context.Background()
	into := NewWorkspaceSpec(WorkspaceSpecArgs{
		Repo: repo, Base: "main", Branch: "beltline/run", Folder: folder, Repos: []string{repo},
		Checkout: WorkspaceCheckout{Kind: WorkspaceCheckoutKindWorktree},
	})

	if got := workspaces.MergeLegs(ctx, nil, into); got.Kind != MergeResultNothing {
		t.Fatalf("no branches: %+v", got)
	}
	if got := workspaces.MergeLegs(ctx, []string{"leg-empty"}, into); got.Kind != MergeResultNothing {
		t.Fatalf("a leg with no commits: %+v", got)
	}

	if got := workspaces.MergeLegs(ctx, []string{"leg-a", "leg-b"}, into); got.Kind != MergeResultMerged || !reflect.DeepEqual(got.Names, []string{"leg-a", "leg-b"}) {
		t.Fatalf("two legs: %+v", got)
	}
	checkout := into.CheckoutDirectory()
	for _, file := range []string{"a.txt", "b.txt"} {
		if !runnerPathExists(filepath.Join(checkout, file)) {
			t.Errorf("%s is missing after the merge", file)
		}
	}

	got := workspaces.MergeLegs(ctx, []string{"leg-c", "leg-d"}, into)
	if got.Kind != MergeResultConflict || !reflect.DeepEqual(got.Names, []string{"README.md"}) {
		t.Fatalf("conflicting legs: %+v, want a conflict on README.md", got)
	}
	if status := gitIn(t, checkout, "status", "--porcelain"); status != "" {
		t.Errorf("the checkout is not clean after the abort: %q", status)
	}
	mergeHead := shell.ProcessRunner{}.Run(ctx, "git", []string{"rev-parse", "-q", "--verify", "MERGE_HEAD"}, checkout, nil)
	if mergeHead.OK() {
		t.Errorf("a merge is still in progress: %s", strings.TrimSpace(mergeHead.Stdout))
	}
}
