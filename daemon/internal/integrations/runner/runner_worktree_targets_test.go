package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// The rules of WorktreeTargets: route names, duplicate names, symlinks to the
// same checkout, real-checkout filtering and folders that are kept on disk.

// worktreeTestRun is a run over repos with the given forks.
func worktreeTestRun(repos []string, forks []core.Fork) core.Run {
	return core.Run{Repos: repos, Forks: forks}
}

// worktreeTestWork is a work folder with a name holding spaces and unicode.
func worktreeTestWork(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "a b · 工作 (notes)")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// placeWorktree makes the worktree directory of a leg (or the run's own when leg
// is empty), with one child checkout per repository when there are several.
func placeWorktree(t *testing.T, repos []string, work, leg string, children []string) string {
	t.Helper()
	base := work
	if leg != "" {
		base = filepath.Join(work, leg)
	}
	folder := filepath.Join(base, "worktree")
	if len(repos) > 1 {
		names := children
		if names == nil {
			for _, repo := range repos {
				names = append(names, filepath.Base(WorkspaceSpecCheckout(repo, repos, folder)))
			}
		}
		for _, name := range names {
			if err := os.MkdirAll(filepath.Join(folder, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	} else if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	return folder
}

// targetFolders is each target's folder, canonical, in order.
func targetFolders(targets []WorktreeTarget) []string {
	var out []string
	for _, target := range targets {
		out = append(out, worktreeCanonical(target.Folder))
	}
	return out
}

// targetTitles is each target's title, in order.
func targetTitles(targets []WorktreeTarget) []string {
	var out []string
	for _, target := range targets {
		out = append(out, target.Title)
	}
	return out
}

func TestMainOnlyRouteOnlyAndSeveralKeepOrderPaths(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	forks := []core.Fork{{ID: "size", Routes: []core.Route{{ID: "zeta", Name: "Zebra"}, {ID: "alpha", Name: "Alpha"}}}}
	run := worktreeTestRun([]string{"acme/app"}, forks)
	main := placeWorktree(t, run.Repos, work, "", nil)
	found := WorktreeTargetsFor(run, work)
	if !reflect.DeepEqual(targetTitles(found), []string{WorktreeTargetsAction}) {
		t.Fatalf("titles = %v", targetTitles(found))
	}
	if !reflect.DeepEqual(targetFolders(found), []string{worktreeCanonical(main)}) {
		t.Fatalf("folders = %v", targetFolders(found))
	}

	if err := os.RemoveAll(main); err != nil {
		t.Fatal(err)
	}
	route := placeWorktree(t, run.Repos, work, "alpha", nil)
	found = WorktreeTargetsFor(run, work)
	if !reflect.DeepEqual(targetTitles(found), []string{WorktreeTargetsAction}) {
		t.Fatalf("route-only titles = %v", targetTitles(found))
	}
	if !reflect.DeepEqual(targetFolders(found), []string{worktreeCanonical(route)}) {
		t.Fatalf("route-only folders = %v", targetFolders(found))
	}

	mainAgain := placeWorktree(t, run.Repos, work, "", nil)
	zebra := placeWorktree(t, run.Repos, work, "zeta", nil)
	found = WorktreeTargetsFor(run, work)
	want := []string{"Open in VS Code · Main", "Open in VS Code · Alpha", "Open in VS Code · Zebra"}
	if !reflect.DeepEqual(targetTitles(found), want) {
		t.Fatalf("titles = %v, want %v", targetTitles(found), want)
	}
	if !reflect.DeepEqual(targetFolders(found), []string{worktreeCanonical(mainAgain), worktreeCanonical(route), worktreeCanonical(zebra)}) {
		t.Fatalf("folders = %v", targetFolders(found))
	}
	for _, target := range found {
		if target.Detail() != target.Folder {
			t.Errorf("detail %q, folder %q", target.Detail(), target.Folder)
		}
	}
}

func TestDuplicateAndUnknownRouteNamesIncludeTheLeg(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	forks := []core.Fork{{ID: "size", Routes: []core.Route{{ID: "beta", Name: "Fix"}, {ID: "alpha", Name: "Fix"}}}}
	run := worktreeTestRun([]string{"acme/app"}, forks)
	placeWorktree(t, run.Repos, work, "beta", nil)
	placeWorktree(t, run.Repos, work, "alpha", nil)
	placeWorktree(t, run.Repos, work, "mystery", nil)
	titles := targetTitles(WorktreeTargetsFor(run, work))
	want := []string{
		"Open in VS Code · Fix · r:alpha",
		"Open in VS Code · Fix · r:beta",
		"Open in VS Code · r:mystery",
	}
	if !reflect.DeepEqual(titles, want) {
		t.Fatalf("titles = %v, want %v", titles, want)
	}
	seen := map[string]bool{}
	for _, title := range titles {
		if seen[title] {
			t.Errorf("title %q repeats", title)
		}
		seen[title] = true
	}
}

func TestARouteFolderThatLinksToTheMainCheckoutIsOneChoice(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	run := worktreeTestRun([]string{"acme/app"}, []core.Fork{{ID: "size", Routes: []core.Route{{ID: "alpha", Name: "Alpha"}}}})
	main := placeWorktree(t, run.Repos, work, "", nil)
	route := filepath.Join(work, "alpha")
	if err := os.MkdirAll(route, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(main, filepath.Join(route, "worktree")); err != nil {
		t.Fatal(err)
	}
	found := WorktreeTargetsFor(run, work)
	if !reflect.DeepEqual(targetTitles(found), []string{WorktreeTargetsAction}) {
		t.Fatalf("titles = %v", targetTitles(found))
	}
	if !reflect.DeepEqual(targetFolders(found), []string{worktreeCanonical(main)}) {
		t.Fatalf("folders = %v", targetFolders(found))
	}
}

func TestSingleAndSeveralRepositoryFoldersAreTheWorktreeItself(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	one := worktreeTestRun([]string{"acme/app"}, nil)
	single := placeWorktree(t, one.Repos, work, "", nil)
	found := WorktreeTargetsFor(one, work)
	if len(found) != 1 || found[0].Folder != single {
		t.Fatalf("single = %+v, want %s", found, single)
	}
	if filepath.Base(single) != "worktree" {
		t.Errorf("folder %q is not named worktree", single)
	}

	if err := os.RemoveAll(single); err != nil {
		t.Fatal(err)
	}
	repos := []string{"acme/app", "other/app"}
	many := worktreeTestRun(repos, nil)
	shared := placeWorktree(t, repos, work, "", nil)
	if got := filepath.Base(WorkspaceSpecCheckout("acme/app", repos, shared)); got != "acme--app" {
		t.Errorf("acme/app checkout = %q", got)
	}
	if got := filepath.Base(WorkspaceSpecCheckout("other/app", repos, shared)); got != "other--app" {
		t.Errorf("other/app checkout = %q", got)
	}
	found = WorktreeTargetsFor(many, work)
	if !reflect.DeepEqual(targetFolders(found), []string{worktreeCanonical(shared)}) {
		t.Fatalf("several folders = %v", targetFolders(found))
	}
}

func TestOnlyRealWorktreeCheckoutsAreOffered(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	run := worktreeTestRun([]string{"acme/app"}, nil)
	if got := WorktreeTargetsFor(run, work); len(got) != 0 {
		t.Fatalf("empty work offered %v", got)
	}

	// A plain file named worktree is not a folder to open.
	worktree := filepath.Join(work, "worktree")
	if err := os.WriteFile(worktree, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeTargetsFor(run, work); len(got) != 0 {
		t.Fatalf("a plain file was offered: %v", got)
	}
	if err := os.Remove(worktree); err != nil {
		t.Fatal(err)
	}

	// The read-only checkouts folder is never a route.
	if err := os.MkdirAll(filepath.Join(work, "readonly"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeTargetsFor(run, work); len(got) != 0 {
		t.Fatalf("readonly was offered: %v", got)
	}

	// Several repositories: a worktree folder with no checkout of its repositories is not offered.
	multi := worktreeTestRun([]string{"acme/app", "acme/web"}, nil)
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeTargetsFor(multi, work); len(got) != 0 {
		t.Fatalf("an empty worktree folder was offered: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(worktree, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeTargetsFor(multi, work); len(got) != 0 {
		t.Fatalf("a folder that is no checkout was offered: %v", got)
	}
	if err := os.MkdirAll(filepath.Join(worktree, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := targetTitles(WorktreeTargetsFor(multi, work)); !reflect.DeepEqual(got, []string{WorktreeTargetsAction}) {
		t.Fatalf("titles = %v", got)
	}
}

func TestTargetsFollowTheFoldersOnDisk(t *testing.T) {
	t.Parallel()
	work := worktreeTestWork(t)
	run := worktreeTestRun([]string{"acme/app"}, nil)
	if got := WorktreeTargetsFor(run, work); len(got) != 0 {
		t.Fatalf("offered before the folder exists: %v", got)
	}
	folder := placeWorktree(t, run.Repos, work, "", nil)
	found := WorktreeTargetsFor(run, work)
	if len(found) != 1 || found[0].Folder != folder {
		t.Fatalf("found = %+v, want %s", found, folder)
	}
	if err := os.RemoveAll(folder); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeTargetsFor(run, work); len(got) != 0 {
		t.Fatalf("offered after the folder was removed: %v", got)
	}
}
