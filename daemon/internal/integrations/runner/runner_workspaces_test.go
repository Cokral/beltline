package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

func TestSameNamedRepositoriesHaveDistinctCheckoutPaths(t *testing.T) {
	t.Parallel()
	directory := filepath.Join("/work", "worktree")
	repos := []string{"app/shared", "infra/shared"}
	if got := filepath.Base(WorkspaceSpecCheckout(repos[0], repos, directory)); got != "app--shared" {
		t.Errorf("app/shared checkout = %q", got)
	}
	if got := filepath.Base(WorkspaceSpecCheckout(repos[1], repos, directory)); got != "infra--shared" {
		t.Errorf("infra/shared checkout = %q", got)
	}
	if got := WorkspaceSpecCheckout(repos[0], []string{repos[0]}, directory); got != directory {
		t.Errorf("single repository checkout = %q, want %q", got, directory)
	}
}

// removalPlan is a run's folder with a readonly and a worktree checkout of each repository.
func removalPlan(t *testing.T, repos []string) (store.Paths, string) {
	t.Helper()
	paths := store.Paths{Root: t.TempDir()}
	for _, repo := range repos {
		gitDir := filepath.Join(paths.Clone(repo), ".git")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	folder := filepath.Join(paths.Work(), "issues", "acme", "billing", "142")
	for _, repo := range repos {
		for _, name := range []string{"readonly", "worktree"} {
			checkout := WorkspaceSpecCheckout(repo, repos, filepath.Join(folder, name))
			if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return paths, folder
}

func TestRemovalDropsWorktreesAndKeepsTheClone(t *testing.T) {
	t.Parallel()
	repos := []string{"acme/billing", "acme/web"}
	paths, folder := removalPlan(t, repos)
	rec := &shell.Recorder{}
	workspaces := NewGitWorkspaces(paths, rec)
	if err := workspaces.Remove(context.Background(), repos, folder); err != nil {
		t.Fatal(err)
	}
	commands := rec.Calls()
	if len(commands) == 0 {
		t.Fatal("no git commands ran")
	}
	for _, c := range commands {
		if c.Executable != "git" || len(c.Args) < 3 || c.Args[0] != "worktree" || c.Args[1] != "remove" || c.Args[2] != "--force" {
			t.Errorf("command %s is not a forced worktree removal", c.Line())
		}
	}
	if runnerPathExists(folder) {
		t.Errorf("run folder %s is still there", folder)
	}
	for _, repo := range repos {
		if !runnerPathExists(filepath.Join(paths.Clone(repo), ".git", "HEAD")) {
			t.Errorf("clone of %s lost its .git", repo)
		}
	}
	// Removing again finds nothing to do.
	if err := workspaces.Remove(context.Background(), repos, folder); err != nil {
		t.Fatal(err)
	}
	if len(rec.Calls()) != len(commands) {
		t.Errorf("a second removal ran %d more commands", len(rec.Calls())-len(commands))
	}
}

func TestFolderWorkspacesPrepareAndRemoveAPlainFolder(t *testing.T) {
	t.Parallel()
	folder := filepath.Join(t.TempDir(), "run")
	spec := NewWorkspaceSpec(WorkspaceSpecArgs{Repo: "acme/app", Base: "main", Branch: "b", Folder: folder, Checkout: WorkspaceCheckout{Kind: WorkspaceCheckoutKindBase}})
	directory, err := FolderWorkspaces{}.Prepare(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if directory != filepath.Join(folder, "readonly") || !runnerIsDirectory(directory) {
		t.Fatalf("directory = %s", directory)
	}
	if got := (FolderWorkspaces{}).MergeLegs(context.Background(), []string{"a", "b"}, spec); got.Kind != MergeResultNothing {
		t.Errorf("merge = %+v, want nothing", got)
	}
	if err := (FolderWorkspaces{}).Remove(context.Background(), []string{"acme/app"}, folder); err != nil {
		t.Fatal(err)
	}
	if runnerPathExists(folder) {
		t.Error("the folder is still there")
	}
}
