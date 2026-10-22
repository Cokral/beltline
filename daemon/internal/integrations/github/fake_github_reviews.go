// The review side of FakeGitHub: review requests, reviews and the requests a pull request holds.

package github

import (
	"context"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// RequestReview asks for reviewer's review on pull, as request. The reviewer is
// stored as given (lowercased); the default "you" is the
// caller's to pass.
func (f *FakeGitHub) RequestReview(pull core.Subject, reviewer, request string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestLocked(pull, reviewer, request)
}

// requestLocked is RequestReview with the lock held. The constructor calls it before the
// fake is shared, so it needs no lock there.
func (f *FakeGitHub) requestLocked(pull core.Subject, reviewer, request string) {
	key := fakeKey(pull.Repo, pull.Number)
	entry := f.reviews[key]
	if entry == nil {
		entry = &reviewEntry{pull: pull.Clone(), requests: map[string]string{}}
		f.reviews[key] = entry
	}
	entry.pull = pull.Clone()
	entry.requests[strings.ToLower(reviewer)] = request
	if _, ok := f.pulls[key]; !ok {
		f.pulls[key] = core.PullRequest{Number: pull.Number, URL: pull.URL, Repo: pull.Repo, State: core.PullRequestStateOpen}
	}
}

// WithdrawReview means the pull request no longer asks for reviewer's review. As
// with RequestReview, the reviewer is used as given.
func (f *FakeGitHub) WithdrawReview(repo string, number int, reviewer string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry := f.reviews[fakeKey(repo, number)]; entry != nil {
		delete(entry.requests, strings.ToLower(reviewer))
	}
}

// SetReviewRequestFailing makes review-request reads of repo fail (or not).
func (f *FakeGitHub) SetReviewRequestFailing(repo string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fail {
		f.failingReview[repo] = true
	} else {
		delete(f.failingReview, repo)
	}
}

// Approve records an approval by author at the time at (on commit, or the head when nil).
func (f *FakeGitHub) Approve(number int, author string, at time.Time, commit *string) {
	f.addReview(number, author, core.PullRequestReviewStateApproved, at, commit)
}

// SetReview records a review exactly as given, including a missing commit, time or id.
func (f *FakeGitHub) SetReview(number int, author string, state core.PullRequestReviewState, commit *string, at *time.Time, id *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number != number {
			continue
		}
		if pull.Reviews == nil {
			pull.Reviews = []core.PullRequestReview{}
		}
		review := core.PullRequestReview{ID: id, Author: author, State: state, Commit: commit}
		if at != nil {
			review.SubmittedAt = &jsonx.Date{Time: *at}
		}
		pull.Reviews = append(pull.Reviews, review)
		f.pulls[key] = pull
	}
}

// AddPullComment records a comment review by author at the time at.
func (f *FakeGitHub) AddPullComment(number int, author string, at time.Time) {
	f.addReview(number, author, core.PullRequestReviewStateCommented, at, nil)
}

// RequestChanges records a changes-requested review by author at the time at.
func (f *FakeGitHub) RequestChanges(number int, author string, at time.Time) {
	f.addReview(number, author, core.PullRequestReviewStateChangesRequested, at, nil)
}

// Dismiss dismisses author's last review.
func (f *FakeGitHub) Dismiss(number int, author string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number != number {
			continue
		}
		for i := len(pull.Reviews) - 1; i >= 0; i-- {
			if pull.Reviews[i].Author == author {
				pull.Reviews[i].State = core.PullRequestReviewStateDismissed
				break
			}
		}
		f.pulls[key] = pull
	}
}

// addReview appends a review by author at date, on commit or the head when commit is nil.
func (f *FakeGitHub) addReview(number int, author string, state core.PullRequestReviewState, at time.Time, commit *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number != number {
			continue
		}
		c := commit
		if c == nil {
			c = pull.HeadSha
		}
		id := core.NewUUID()
		review := core.PullRequestReview{ID: &id, Author: author, State: state, Commit: c, SubmittedAt: &jsonx.Date{Time: at}}
		if pull.Reviews == nil {
			pull.Reviews = []core.PullRequestReview{}
		}
		pull.Reviews = append(pull.Reviews, review)
		f.pulls[key] = pull
	}
}

// ReviewRequests lists the open pull requests in repo that ask for the current login's review.
func (f *FakeGitHub) ReviewRequests(_ context.Context, repo string, _ bool) ([]core.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing[repo] {
		return nil, core.NewFailure("gh pr list: could not reach " + repo)
	}
	out := []core.Subject{}
	for _, entry := range f.reviews {
		if _, ok := entry.requests[strings.ToLower(f.currentLogin)]; !ok {
			continue
		}
		if entry.pull.Repo == repo && entry.pull.IsOpen {
			out = append(out, entry.pull.Clone())
		}
	}
	sortSubjects(out)
	return out, nil
}

// LatestReviewRequest is the request id for reviewer on the pull request, or nil.
func (f *FakeGitHub) LatestReviewRequest(ctx context.Context, repo string, number int, reviewer string, _ bool) (*string, error) {
	if hook := f.BeforeLatestReview; hook != nil {
		hook(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Kind: FakeCallLatestReviewRequest, Repo: repo, Number: number, Reviewer: reviewer})
	if f.failingReview[repo] {
		return nil, core.NewFailure("could not read review requests in " + repo)
	}
	entry := f.reviews[fakeKey(repo, number)]
	if entry == nil {
		return nil, nil
	}
	if request, ok := entry.requests[strings.ToLower(reviewer)]; ok {
		return &request, nil
	}
	return nil, nil
}
