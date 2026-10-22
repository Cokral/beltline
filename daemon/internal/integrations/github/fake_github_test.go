// Tests that FakeGitHub's values are copies: what it returns and what it keeps are not shared.

package github

import (
	"context"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestFakeGitHubReturnsCopies: a caller that changes an issue or pull request it was handed, or
// one it stored, does not change what the fake holds.
func TestFakeGitHubReturnsCopies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	issue := core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 1, Title: "One", Labels: []string{"agent"}, IsOpen: true}
	f := NewFakeGitHub([]core.Subject{issue}, nil, nil, nil)
	issue.Labels[0] = "caller"
	listed, err := f.Issues(ctx, "acme/web", "agent", nil)
	if err != nil || len(listed) != 1 {
		t.Fatalf("issues = %v, %v", listed, err)
	}
	if listed[0].Labels[0] != "agent" {
		t.Fatal("the fake stored the caller's slice")
	}
	listed[0].Labels[0] = "listed"
	read, err := f.Issue(ctx, "acme/web", 1)
	if err != nil || read.Labels[0] != "agent" {
		t.Fatalf("issue read = %+v, %v; the listing changed the fake", read, err)
	}
	read.Labels[0] = "read"
	if again, _ := f.Issue(ctx, "acme/web", 1); again.Labels[0] != "agent" {
		t.Fatal("a read changed the fake")
	}

	sha := "abc"
	pull := core.PullRequest{Number: 5, Repo: "acme/web", URL: "https://github.com/acme/web/pull/5", State: core.PullRequestStateOpen,
		HeadSha: &sha, Reviews: []core.PullRequestReview{{Author: "rob", State: core.PullRequestReviewStateApproved}}}
	f.PutPull(pull, "feature")
	sha = "caller"
	pull.Reviews[0].Author = "caller"
	got, err := f.PullRequest(ctx, "acme/web", 5)
	if err != nil || *got.HeadSha != "abc" || got.Reviews[0].Author != "rob" {
		t.Fatalf("pull = %+v, %v; the caller's change reached the fake", got, err)
	}
	*got.HeadSha = "read"
	got.Reviews[0].Author = "read"
	if again, _ := f.PullRequest(ctx, "acme/web", 5); *again.HeadSha != "abc" || again.Reviews[0].Author != "rob" {
		t.Fatal("a read changed the stored pull request")
	}
	onHead, err := f.PullRequestOnHead(ctx, "acme/web", "feature")
	if err != nil || onHead == nil {
		t.Fatalf("on head = %v, %v", onHead, err)
	}
	*onHead.HeadSha = "head"
	onHead.Reviews[0].Author = "head"
	if again, _ := f.PullRequest(ctx, "acme/web", 5); *again.HeadSha != "abc" || again.Reviews[0].Author != "rob" {
		t.Fatal("a branch read changed the stored pull request")
	}
	filtered, err := f.PullRequestWithEvents(ctx, "acme/web", 5, nil)
	if err != nil || filtered.Reviews != nil {
		t.Fatalf("filtered = %+v, %v; want the reviews dropped", filtered, err)
	}
	if again, _ := f.PullRequest(ctx, "acme/web", 5); again.Reviews == nil {
		t.Fatal("filtering dropped the stored reviews")
	}
}
