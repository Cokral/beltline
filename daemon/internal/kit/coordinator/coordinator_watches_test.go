// Tests of the watch's signal functions on pull requests built here, with no run fixture, no GitHub and
// no tick.

package coordinator

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// pureWatches is a watch with no coordinator: the signal functions read and
// write only the observations they are given and the cache.
func pureWatches() *Watches {
	return &Watches{observations: map[string]WatchesObservation{}}
}

// eventSet builds a set of events.
func eventSet(events ...core.WatchEvent) map[core.WatchEvent]struct{} {
	set := map[core.WatchEvent]struct{}{}
	for _, event := range events {
		set[event] = struct{}{}
	}
	return set
}

// reviewOf is a review by author in state on commit.
func reviewOf(author string, state core.PullRequestReviewState, commit string) core.PullRequestReview {
	return core.PullRequestReview{Author: author, State: state, Commit: new(commit)}
}

// TestCaughtUpBranchRearmsBehindWithoutReportingRecovery
func TestCaughtUpBranchRearmsBehindWithoutReportingRecovery(t *testing.T) {
	t.Parallel()
	w := pureWatches()
	events := eventSet(core.WatchEventBehind)
	observation := core.NewWatchObservation(core.WatchObservationArgs{PullNumber: 201, HeadSha: new("a")})
	pull := core.PullRequest{Number: 201, URL: "u", State: core.PullRequestStateOpen, HeadSha: new("a"), BaseRef: new("main"), Behind: new(2)}
	signal := func() core.WatchEvent { return core.ValueOf(w.signalPull(pull, events, &observation)) }
	if got := signal(); got != "" {
		t.Fatalf("first behind poll = %q, want nothing", got)
	}
	if got := signal(); got != core.WatchEventBehind {
		t.Fatalf("second behind poll = %q, want behind", got)
	}
	if got := signal(); got != "" {
		t.Fatalf("third behind poll = %q, want nothing", got)
	}
	pull.Behind = new(0)
	if got := signal(); got != "" {
		t.Fatalf("caught up = %q, want nothing", got)
	}
	pull.Behind = new(1)
	if got := signal(); got != "" {
		t.Fatalf("one behind after catching up = %q, want nothing", got)
	}
	if got := signal(); got != core.WatchEventBehind {
		t.Fatalf("behind again = %q, want behind", got)
	}
}

// TestChangesRequestedRecoveryDoesNotConsumeNewReviewers
func TestChangesRequestedRecoveryDoesNotConsumeNewReviewers(t *testing.T) {
	t.Parallel()
	w := pureWatches()
	observation := core.NewWatchObservation(core.WatchObservationArgs{PullNumber: 201, HeadSha: new("a")})
	events := eventSet(core.WatchEventChangesRequested)
	pull := core.PullRequest{Number: 201, URL: "u", State: core.PullRequestStateOpen, HeadSha: new("a"), Reviews: []core.PullRequestReview{}}
	signal := func(want core.WatchEvent) {
		t.Helper()
		if got := core.ValueOf(w.signalPull(pull, events, &observation)); got != want {
			t.Fatalf("signal = %q, want %q", got, want)
		}
	}
	pull.Reviews = []core.PullRequestReview{reviewOf("carol", core.PullRequestReviewStateChangesRequested, "a")}
	signal(core.WatchEventChangesRequested)
	pull.Reviews = append(pull.Reviews, reviewOf("carol", core.PullRequestReviewStateChangesRequested, "a"))
	signal("")
	// One reviewer recovers while a different reviewer first asks for changes.
	pull.Reviews = append(pull.Reviews, reviewOf("carol", core.PullRequestReviewStateCommented, "a"), reviewOf("bot", core.PullRequestReviewStateChangesRequested, "a"))
	signal(core.WatchEventChangesRequested)
	signal("")
	pull.Reviews = append(pull.Reviews, reviewOf("bot", core.PullRequestReviewStateDismissed, "a"))
	signal("")
	pull.Reviews = append(pull.Reviews, reviewOf("bot", core.PullRequestReviewStateChangesRequested, "a"))
	signal(core.WatchEventChangesRequested)
}
