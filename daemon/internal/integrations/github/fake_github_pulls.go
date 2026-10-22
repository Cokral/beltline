// The pull request side of FakeGitHub: pull requests, their checks, heads and
// the holds that let a test pause pull request reads.

package github

import (
	"context"
	"strconv"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// SetPullState sets the state of the pull requests numbered number, in repo when it is set.
func (f *FakeGitHub) SetPullState(number int, state core.PullRequestState, repo *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number == number && (repo == nil || pull.Repo == *repo) {
			pull.State = state
			f.pulls[key] = pull
		}
	}
}

// SetHead gives the pull request numbered number a head commit (nil clears it).
func (f *FakeGitHub) SetHead(number int, sha *string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number == number {
			pull.HeadSha = sha
			f.pulls[key] = pull
		}
	}
}

// SetChecks sets what GitHub's checks say: failing of total, some of which may still run.
func (f *FakeGitHub) SetChecks(number, failing, total int, pending bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number != number {
			continue
		}
		if total == 0 {
			pull.Checks, pull.CheckFailures, pull.CheckTotal = nil, nil, nil
		} else {
			var state core.PullRequestCheckState
			switch {
			case pending:
				state = core.PullRequestCheckStatePending
			case failing > 0:
				state = core.PullRequestCheckStateFailing
			default:
				state = core.PullRequestCheckStatePassing
			}
			f0, t0 := failing, total
			pull.Checks, pull.CheckFailures, pull.CheckTotal = &state, &f0, &t0
		}
		f.pulls[key] = pull
	}
}

// SetConflicted sets whether the pull request numbered number conflicts.
func (f *FakeGitHub) SetConflicted(number int, conflicted bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, pull := range f.pulls {
		if pull.Number == number {
			c := conflicted
			pull.Conflicted = &c
			f.pulls[key] = pull
		}
	}
}

// SetBehind sets how far the head commit of pull request number is behind its base.
func (f *FakeGitHub) SetBehind(number, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pull := range f.pulls {
		if pull.Number != number || pull.HeadSha == nil {
			continue
		}
		f.pullsBehind[*pull.HeadSha] = count
	}
}

// PutPull adds a pull request as it is, such as one under review, opened from head.
func (f *FakeGitHub) PutPull(pull core.PullRequest, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pull = clonePull(withRepository(pull))
	key := fakeKey(pull.Repo, pull.Number)
	f.pulls[key] = pull
	f.heads[key] = head
}

// OpenPull opens a pull request from branch, as an agent would.
func (f *FakeGitHub) OpenPull(repo, branch string, state core.PullRequestState) core.PullRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openPullLocked(repo, branch, state)
}

// openPullLocked is OpenPull with the lock held.
func (f *FakeGitHub) openPullLocked(repo, branch string, state core.PullRequestState) core.PullRequest {
	f.nextPull++
	head := "demo-" + repo + "-" + strconv.Itoa(f.nextPull)
	base := "main"
	pull := core.PullRequest{
		Number: f.nextPull, URL: "https://github.com/" + repo + "/pull/" + strconv.Itoa(f.nextPull), State: state, Repo: repo,
		HeadSha: &head, BaseRef: &base,
	}
	key := fakeKey(repo, f.nextPull)
	f.pulls[key] = pull
	f.heads[key] = branch
	return clonePull(pull)
}

// CommentOnPullRequest records a comment on a pull request.
func (f *FakeGitHub) CommentOnPullRequest(_ context.Context, repo string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Kind: FakeCallCommentOnPullRequest, Repo: repo, Number: number, Body: body})
	return nil
}

// PullRequest reads a pull request by number.
func (f *FakeGitHub) PullRequest(_ context.Context, repo string, number int) (core.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pullLocked(repo, number)
}

// pullLocked is PullRequest with the lock held.
func (f *FakeGitHub) pullLocked(repo string, number int) (core.PullRequest, error) {
	if f.failing[repo] {
		return core.PullRequest{}, core.NewFailure("could not reach " + repo)
	}
	pull, ok := f.pulls[fakeKey(repo, number)]
	if !ok {
		return core.PullRequest{}, core.NewFailure("no pull request #" + strconv.Itoa(number))
	}
	return clonePull(pull), nil
}

// PullRequestOnHead is the newest pull request from branch; a branch with none is opened
// when opensPulls is set.
func (f *FakeGitHub) PullRequestOnHead(_ context.Context, repo, branch string) (*core.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onHeadLocked(repo, branch)
}

// onHeadLocked is PullRequestOnHead with the lock held.
func (f *FakeGitHub) onHeadLocked(repo, branch string) (*core.PullRequest, error) {
	if f.failing[repo] {
		return nil, core.NewFailure("could not reach " + repo)
	}
	var found *core.PullRequest
	for _, pull := range f.pulls {
		if pull.Repo != repo || f.heads[fakeKey(repo, pull.Number)] != branch {
			continue
		}
		if found == nil || pull.Number > found.Number {
			p := clonePull(pull)
			found = &p
		}
	}
	if found != nil {
		return found, nil
	}
	if f.opensPulls != nil {
		opened := f.openPullLocked(repo, branch, *f.opensPulls)
		return &opened, nil
	}
	return nil, nil
}

// HoldPulls makes the next pull-request reads wait until ReleasePulls.
func (f *FakeGitHub) HoldPulls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullsHeld = true
}

// ReleasePulls lets the held pull-request reads answer.
func (f *FakeGitHub) ReleasePulls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullsHeld = false
	if f.gate != nil {
		close(f.gate)
		f.gate = nil
	}
}

// WaitingPulls is how many reads are waiting on ReleasePulls.
func (f *FakeGitHub) WaitingPulls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitingPulls
}

// waitToAnswer blocks while pull-request reads are held. It returns the context's
// error when the wait is cancelled.
func (f *FakeGitHub) waitToAnswer(ctx context.Context) error {
	f.mu.Lock()
	if !f.pullsHeld {
		f.mu.Unlock()
		return nil
	}
	if f.gate == nil {
		f.gate = make(chan struct{})
	}
	gate := f.gate
	f.waitingPulls++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.waitingPulls--
		f.mu.Unlock()
	}()
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PullRequestWithEvents reads a pull request, waiting while reads are held.
func (f *FakeGitHub) PullRequestWithEvents(ctx context.Context, repo string, number int, events map[core.WatchEvent]struct{}) (core.PullRequest, error) {
	if err := f.waitToAnswer(ctx); err != nil {
		return core.PullRequest{}, err
	}
	pull, err := f.PullRequest(ctx, repo, number)
	if err != nil {
		return core.PullRequest{}, err
	}
	return filteredPull(pull, events), nil
}

// PullRequestOnHeadWithEvents reads the newest pull request from head, waiting while reads are held.
func (f *FakeGitHub) PullRequestOnHeadWithEvents(ctx context.Context, repo, head string, events map[core.WatchEvent]struct{}) (*core.PullRequest, error) {
	if err := f.waitToAnswer(ctx); err != nil {
		return nil, err
	}
	pull, err := f.PullRequestOnHead(ctx, repo, head)
	if err != nil || pull == nil {
		return nil, err
	}
	filtered := filteredPull(*pull, events)
	return &filtered, nil
}

// filteredPull drops the reviews unless the pull request is open and the events ask for them. The
// result is a copy: the caller's pull request is not changed.
func filteredPull(pull core.PullRequest, events map[core.WatchEvent]struct{}) core.PullRequest {
	pull = clonePull(pull)
	if pull.State != core.PullRequestStateOpen || !hasAnyIn(events, core.WatchEventReviewEvents) {
		pull.Reviews = nil
	}
	return pull
}

// clonePull is a deep copy of a pull request: its pointers and reviews are its own, so a caller
// that changes what it was handed does not change the fake's stored pull request.
func clonePull(pull core.PullRequest) core.PullRequest {
	pull.HeadSha = core.CopyPointer(pull.HeadSha)
	pull.BaseRef = core.CopyPointer(pull.BaseRef)
	pull.Checks = core.CopyPointer(pull.Checks)
	pull.CheckFailures = core.CopyPointer(pull.CheckFailures)
	pull.CheckTotal = core.CopyPointer(pull.CheckTotal)
	pull.Conflicted = core.CopyPointer(pull.Conflicted)
	pull.Behind = core.CopyPointer(pull.Behind)
	if pull.Reviews != nil {
		reviews := make([]core.PullRequestReview, len(pull.Reviews))
		for i, review := range pull.Reviews {
			review.ID = core.CopyPointer(review.ID)
			review.Commit = core.CopyPointer(review.Commit)
			review.SubmittedAt = core.CopyPointer(review.SubmittedAt)
			reviews[i] = review
		}
		pull.Reviews = reviews
	}
	return pull
}

// CommitsBehind is how far base is behind head, as recorded for head's commit.
func (f *FakeGitHub) CommitsBehind(_ context.Context, repo, base, head string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Kind: FakeCallCommitsBehind, Repo: repo, Base: base, Head: head})
	return f.pullsBehind[head], nil
}
