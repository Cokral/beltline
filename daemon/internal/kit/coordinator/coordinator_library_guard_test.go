package coordinator

import (
	"context"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestALibraryCommandOnARepositoryInFlightRefusesTheNextOne checks the guard that keeps two library
// commands off one clone: while a command on a repository is in flight, another is refused with
// the reason, and the repository's actions carry that reason.
func TestALibraryCommandOnARepositoryInFlightRefusesTheNextOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	shared := core.LibraryOrigin{Repository: "owner/shared", Path: ""}
	if _, err := c.ApplySettingsPatch(SettingsPatch{LibraryRepositories: &[]core.LibraryRepository{
		{Repository: shared.Repository, Role: core.LibraryRoleShared, Publish: core.LibraryPublishPullRequest},
	}}); err != nil {
		t.Fatalf("settings: %v", err)
	}
	// Stand in for a command that is between its Suspends.
	c.Lock()
	c.libraryBusy[shared.Repository] = struct{}{}
	c.libraryRepoRevision++
	c.EndTurn()

	want := "A library command on owner/shared is still running."
	if _, err := c.RefreshLibraryRepo(context.Background(), shared); err == nil || err.Error() != want {
		t.Fatalf("refresh while busy: err %v, want %q", err, want)
	}
	if _, err := c.ShareLibraryItem(context.Background(), shared, "belt", "nightly"); err == nil || err.Error() != want {
		t.Fatalf("share while busy: err %v, want %q", err, want)
	}
	repos := c.LibraryRepos()
	if len(repos) != 1 || len(repos[0].Actions) == 0 {
		t.Fatalf("repositories %+v", repos)
	}
	for _, action := range repos[0].Actions {
		if action.Enabled || action.Reason == nil || !strings.Contains(*action.Reason, "still running") {
			t.Fatalf("action %s while busy: enabled %v reason %v", action.ID, action.Enabled, action.Reason)
		}
	}

	// Once the command is over, the mark is gone and the repository's refresh is offered again.
	c.Lock()
	delete(c.libraryBusy, shared.Repository)
	c.libraryRepoRevision++
	c.EndTurn()
	for _, action := range c.LibraryRepos()[0].Actions {
		if !action.Enabled {
			t.Fatalf("action %s after the command: still refused (%v)", action.ID, action.Reason)
		}
	}
}
