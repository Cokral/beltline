//go:build integration

package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// gitIn runs git in dir and fails the test when it does not succeed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	result := shell.ProcessRunner{}.Run(context.Background(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...), dir, nil)
	if !result.OK() {
		t.Fatalf("git %s in %s: %s", strings.Join(args, " "), dir, result.Message())
	}
	return strings.TrimSpace(result.Stdout)
}

// seedOrigin makes a bare origin with one commit on main and returns its path.
func seedOrigin(t *testing.T, root string) string {
	t.Helper()
	origin := filepath.Join(root, "origin.git")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", origin)
	seed := filepath.Join(root, "seed")
	gitIn(t, root, "clone", "-q", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-qm", "init")
	gitIn(t, seed, "push", "-q", "origin", "main")
	return origin
}

// TestWorkspacesCheckOutEveryRepositoryAndRestoreReadOnly checks that every repository is checked out read-only,
// in one run folder, that the restore undoes a change, and that removal drops the worktrees.
func TestWorkspacesCheckOutEveryRepositoryAndRestoreReadOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := store.Paths{Root: filepath.Join(root, "data")}
	origin := seedOrigin(t, root)
	repos := []string{"acme/billing", "acme/infra"}
	for _, repo := range repos {
		if err := os.MkdirAll(filepath.Dir(paths.Clone(repo)), 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, root, "clone", "-q", origin, paths.Clone(repo))
	}
	folder := filepath.Join(paths.Work(), "run")
	workspaces := NewGitWorkspaces(paths, nil)
	ctx := context.Background()
	for _, repo := range repos {
		spec := NewWorkspaceSpec(WorkspaceSpecArgs{
			Repo: repo, Base: "main", Branch: "beltline/run", Folder: folder, Repos: repos,
			Checkout: WorkspaceCheckout{Kind: WorkspaceCheckoutKindBase},
		})
		directory, err := workspaces.Prepare(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(folder, "readonly"); directory != want {
			t.Errorf("directory = %s, want %s", directory, want)
		}
		checkout := spec.CheckoutDirectory()
		if !runnerPathExists(filepath.Join(checkout, ".git")) {
			t.Errorf("%s is not a checkout", checkout)
		}
		if head, base := gitIn(t, checkout, "rev-parse", "HEAD"), gitIn(t, paths.Clone(repo), "rev-parse", "origin/main"); head != base {
			t.Errorf("%s HEAD = %s, want origin/main %s", repo, head, base)
		}
	}

	before := workspaces.Fingerprint(ctx, filepath.Join(folder, "readonly"))
	infra := filepath.Join(folder, "readonly", "infra")
	if err := os.WriteFile(filepath.Join(infra, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if after := workspaces.Fingerprint(ctx, filepath.Join(folder, "readonly")); after == before {
		t.Error("a change to a checkout left its fingerprint the same")
	}
	workspaces.Restore(ctx, filepath.Join(folder, "readonly"))
	if restored := workspaces.Fingerprint(ctx, filepath.Join(folder, "readonly")); restored != before {
		t.Error("restore did not put the checkout back")
	}

	// A worktree on the run's branch is made from the base and is on that branch.
	worktree := NewWorkspaceSpec(WorkspaceSpecArgs{
		Repo: repos[0], Base: "main", Branch: "beltline/run", Folder: folder, Repos: repos,
		Checkout: WorkspaceCheckout{Kind: WorkspaceCheckoutKindWorktree},
	})
	if _, err := workspaces.Prepare(ctx, worktree); err != nil {
		t.Fatal(err)
	}
	if branch := gitIn(t, worktree.CheckoutDirectory(), "rev-parse", "--abbrev-ref", "HEAD"); branch != "beltline/run" {
		t.Errorf("worktree branch = %s", branch)
	}

	if err := workspaces.Remove(ctx, repos, folder); err != nil {
		t.Fatal(err)
	}
	if runnerPathExists(folder) {
		t.Errorf("run folder %s is still there", folder)
	}
	for _, repo := range repos {
		list := gitIn(t, paths.Clone(repo), "worktree", "list", "--porcelain")
		if strings.Contains(list, folder) {
			t.Errorf("%s still lists a worktree in the run folder", repo)
		}
	}
}
