package core

import "testing"

// TestStagePanelWatchCardsOfWaitsForOpenPullRequests checks one card per pull request in the run's order:
// an open one waits for a review and a merge, a merged one says so, and each carries the run's title and branch.
func TestStagePanelWatchCardsOfWaitsForOpenPullRequests(t *testing.T) {
	t.Parallel()
	run := engineRunningWatch()
	run.Branch = new("beltline/issue-142")
	run.PullRequests = []PullRequest{
		{Repo: "acme/billing", Number: 7, URL: "https://github.com/acme/billing/pull/7", State: PullRequestStateOpen},
		{Repo: "acme/api", Number: 9, URL: "https://github.com/acme/api/pull/9", State: PullRequestStateMerged},
	}

	cards := StagePanelWatchCardsOf(run, nil)
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	first := cards[0]
	if first.ID != "acme/billing" || first.Title != "billing#7" || first.URL != run.PullRequests[0].URL {
		t.Errorf("first card = %+v", first)
	}
	if first.State != "It waits for a review and a merge." {
		t.Errorf("open state = %q", first.State)
	}
	wantSubtitle := []InlineSpan{
		{Kind: InlineSpanKindText, Text: "Export invoices as CSV · branch "},
		{Kind: InlineSpanKindCode, Text: "beltline/issue-142"},
	}
	if len(first.Subtitle) != len(wantSubtitle) || first.Subtitle[0] != wantSubtitle[0] || first.Subtitle[1] != wantSubtitle[1] {
		t.Errorf("subtitle = %+v, want %+v", first.Subtitle, wantSubtitle)
	}
	if got := cards[1].State; got != "The pull request is merged." {
		t.Errorf("merged state = %q", got)
	}
}

// TestStagePanelWatchCardsOfUsesTheRunBranchWithoutAnOverride checks that a run with no branch of its own
// shows the branch it would take, and that a run with no pull requests has no cards.
func TestStagePanelWatchCardsOfUsesTheRunBranchWithoutAnOverride(t *testing.T) {
	t.Parallel()
	run := engineRunningWatch()
	run.Branch = nil
	run.PullRequests = []PullRequest{{Repo: "acme/billing", Number: 7, State: PullRequestStateOpen}}

	cards := StagePanelWatchCardsOf(run, nil)
	if len(cards) != 1 || cards[0].Subtitle[1].Text != RunBranch(run) {
		t.Fatalf("cards = %+v, want the run's own branch %q", cards, RunBranch(run))
	}

	run.PullRequests = nil
	if cards := StagePanelWatchCardsOf(run, nil); cards == nil || len(cards) != 0 {
		t.Errorf("no pull requests: cards = %#v, want an empty non-nil slice", cards)
	}
}
