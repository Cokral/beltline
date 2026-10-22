// The GitHub protocol and the shared helpers of the GitHub adapters.

package github

import (
	"context"
	"errors"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// GitHub is everything the app asks of GitHub. GHCLI talks to gh; tests and
// demo mode use FakeGitHub. Every method blocks; callers that must not wait run
// it in a goroutine.
type GitHub interface {
	Issues(ctx context.Context, repo, label string, assignee *string) ([]core.Subject, error)
	Issue(ctx context.Context, repo string, number int) (core.Subject, error)
	Comment(ctx context.Context, repo string, number int, body string) error
	// ReviewRequests are the open pull requests in repo that ask for your review,
	// and with teams, for a team you are on. Their Occurrence is not set.
	ReviewRequests(ctx context.Context, repo string, teams bool) ([]core.Subject, error)
	// LatestReviewRequest is the id of the latest review request on the pull
	// request for reviewer, or with teams also for a team; nil when there is none.
	LatestReviewRequest(ctx context.Context, repo string, number int, reviewer string, teams bool) (*string, error)
	CommentOnPullRequest(ctx context.Context, repo string, number int, body string) error
	PullRequest(ctx context.Context, repo string, number int) (core.PullRequest, error)
	// PullRequestOnHead is the newest pull request from branch, whatever its
	// state; nil when there is none.
	PullRequestOnHead(ctx context.Context, repo, branch string) (*core.PullRequest, error)
	PullRequestWithEvents(ctx context.Context, repo string, number int, events map[core.WatchEvent]struct{}) (core.PullRequest, error)
	PullRequestOnHeadWithEvents(ctx context.Context, repo, head string, events map[core.WatchEvent]struct{}) (*core.PullRequest, error)
	CommitsBehind(ctx context.Context, repo, base, head string) (int, error)
	DefaultBranch(ctx context.Context, repo string) (string, error)
	Login(ctx context.Context) (string, error)
	// Blockers are the open issues that block this one, as refs: "#n", or
	// "owner/repo#n" in another repository.
	Blockers(ctx context.Context, repo string, number int) ([]string, error)
	// IssueState is its state, comments and sub-issues, for a ticket watch.
	IssueState(ctx context.Context, repo string, number int) (core.TicketState, error)
	// CommitsAhead is how many commits head has that base doesn't: 0 when the
	// branch doesn't exist, nil when GitHub doesn't say.
	CommitsAhead(ctx context.Context, repo, base, head string) (*int, error)
	// BranchExists is whether branch is on the repository: nil when GitHub doesn't say.
	BranchExists(ctx context.Context, repo, branch string) (*bool, error)
}

// failure returns the core.Failure err carries, if it is one.
func failure(err error) (core.Failure, bool) {
	var f core.Failure
	if errors.As(err, &f) {
		return f, true
	}
	return core.Failure{}, false
}
