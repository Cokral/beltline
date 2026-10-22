package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestPullRequestDecodesRepoDefault checks that a pull request saved before
// there was more than one repository reads with no repository and no checks.
func TestPullRequestDecodesRepoDefault(t *testing.T) {
	t.Parallel()
	var p PullRequest
	testDecode(t, `{"number":7,"url":"https://github.com/acme/billing/pull/7","state":"open"}`, &p)
	if d := testDiffEmpty(PullRequest{Number: 7, URL: "https://github.com/acme/billing/pull/7", State: PullRequestStateOpen}, p); d != "" {
		t.Fatalf("legacy pull request: %s", d)
	}
	if p.Checks != nil || p.Reviews != nil {
		t.Errorf("optional keys not nil: %+v", p)
	}
}

// testReviewPull is a pull request whose head is h1, with three reviews: two on
// the head by the same author (in any letter case), one on an older commit.
func testReviewPull() PullRequest {
	head, old := "h1", "h0"
	approvedAt := jsonx.Date{Time: time.Date(2026, 7, 22, 2, 18, 0, 0, time.UTC)}
	return PullRequest{Number: 9, URL: "u", State: PullRequestStateOpen, HeadSha: &head, Reviews: []PullRequestReview{
		{Author: "Alice", State: PullRequestReviewStateApproved, Commit: &head, SubmittedAt: &approvedAt},
		{ID: new("r2"), Author: "bob", State: PullRequestReviewStateCommented, Commit: &old},
		{ID: new("r3"), Author: "alice", State: PullRequestReviewStateChangesRequested, Commit: &head},
	}}
}

// TestPullRequestReviewsOnTheHead checks that only reviews on the head count
// as head reviews, and that the latest one per author (ignoring case) is kept, sorted.
func TestPullRequestReviewsOnTheHead(t *testing.T) {
	t.Parallel()
	pull := testReviewPull()
	if got := pull.HeadReviews(); len(got) != 2 {
		t.Fatalf("head reviews = %d, want 2", len(got))
	}
	latest := pull.LatestReviews()
	if len(latest) != 1 || latest[0].State != PullRequestReviewStateChangesRequested || latest[0].Author != "alice" {
		t.Errorf("latest reviews = %+v", latest)
	}
	noHead := PullRequest{Reviews: pull.Reviews}
	if got := noHead.HeadReviews(); len(got) != 0 {
		t.Errorf("reviews without a head = %d, want none", len(got))
	}
}

// TestPullRequestApprovalKeys checks the keys of approvals: a review's id when
// it has one, and otherwise its place, author and submission time, which keep
// their place when a neighbour is dismissed.
func TestPullRequestApprovalKeys(t *testing.T) {
	t.Parallel()
	pull := testReviewPull()
	approvals := pull.KeyedApprovals()
	if len(approvals) != 1 || approvals[0].Key == "" {
		t.Fatalf("approvals = %+v", approvals)
	}
	wantKey := "0:Alice:" + reviewDouble(reviewUnixSeconds(time.Date(2026, 7, 22, 2, 18, 0, 0, time.UTC)))
	if approvals[0].Key != wantKey {
		t.Errorf("key = %q, want %q", approvals[0].Key, wantKey)
	}
	unsubmitted := PullRequest{Reviews: []PullRequestReview{{Author: "carol", State: PullRequestReviewStateApproved}}}
	if got := unsubmitted.KeyedApprovals(); len(got) != 1 || got[0].Key != "0:carol:0.0" {
		t.Errorf("unsubmitted approval = %+v", got)
	}
	withID := PullRequest{Reviews: []PullRequestReview{{ID: new("r9"), Author: "dan", State: PullRequestReviewStateApproved}}}
	if got := withID.KeyedApprovals(); len(got) != 1 || got[0].Key != "r9" {
		t.Errorf("approval with an id = %+v", got)
	}
}

// TestReviewDoubleFormat checks the float description the review
// keys use: a whole number keeps ".0", and a fraction is the shortest form.
func TestReviewDoubleFormat(t *testing.T) {
	t.Parallel()
	cases := map[float64]string{0: "0.0", 1.5: "1.5", 1785000000: "1785000000.0", 0.25: "0.25"}
	for in, want := range cases {
		if got := reviewDouble(in); got != want {
			t.Errorf("reviewDouble(%v) = %q, want %q", in, got, want)
		}
	}
}
