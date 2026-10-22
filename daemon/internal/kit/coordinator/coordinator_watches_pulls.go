// The pull request half of the coordinator's watches.

package coordinator

import (
	"context"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// watchesHasPull reports whether one of the pull requests is in repo.
// Lock: none (pure).
func watchesHasPull(pulls []core.PullRequest, repo string) bool {
	return slices.ContainsFunc(pulls, func(pull core.PullRequest) bool { return pull.Repo == repo })
}

// watchesAnyState reports whether one pull request has state.
// Lock: none (pure).
func watchesAnyState(pulls []core.PullRequest, state core.PullRequestState) bool {
	return slices.ContainsFunc(pulls, func(pull core.PullRequest) bool { return pull.State == state })
}

// watchesAllState reports whether every pull request has state.
// Lock: none (pure).
func watchesAllState(pulls []core.PullRequest, state core.PullRequestState) bool {
	return !slices.ContainsFunc(pulls, func(pull core.PullRequest) bool { return pull.State != state })
}

// watchesClonePull is a deep copy of a pull request as GitHub answered it: the reviews and the
// pointer fields are copied, so the run keeps what it saw when a later read changes the source
// .
// Lock: none (pure).
func watchesClonePull(pull core.PullRequest) core.PullRequest {
	copied := pull
	copied.HeadSha = core.CopyPointer(pull.HeadSha)
	copied.BaseRef = core.CopyPointer(pull.BaseRef)
	copied.Checks = core.CopyPointer(pull.Checks)
	copied.CheckFailures = core.CopyPointer(pull.CheckFailures)
	copied.CheckTotal = core.CopyPointer(pull.CheckTotal)
	copied.Conflicted = core.CopyPointer(pull.Conflicted)
	copied.Behind = core.CopyPointer(pull.Behind)
	if pull.Reviews != nil {
		copied.Reviews = make([]core.PullRequestReview, len(pull.Reviews))
		for i, review := range pull.Reviews {
			reviewCopy := review
			reviewCopy.ID = core.CopyPointer(review.ID)
			reviewCopy.Commit = core.CopyPointer(review.Commit)
			reviewCopy.SubmittedAt = core.CopyPointer(review.SubmittedAt)
			copied.Reviews[i] = reviewCopy
		}
	}
	return copied
}

// watchesKeeping keeps the known pull requests of a leg, and the first entry for
// the run's own branch's repository. Two legs never share a pull request.
// Lock: none (pure).
func watchesKeeping(known []core.PullRequest, pulls []core.PullRequest, leg core.Leg) []core.PullRequest {
	result := append([]core.PullRequest(nil), known...)
	legID := leg.ID
	ownsLeg := core.LegFromID(&legID) != nil
	for _, pull := range pulls {
		at := -1
		for i, existing := range result {
			if existing.Repo == pull.Repo && (!ownsLeg || existing.Number == pull.Number) {
				at = i
				break
			}
		}
		if at >= 0 {
			result[at] = pull
			continue
		}
		seen := slices.ContainsFunc(result, func(existing core.PullRequest) bool {
			return existing.Repo == pull.Repo && existing.Number == pull.Number
		})
		if !seen {
			result = append(result, pull)
		}
	}
	return result
}

// behind measures how far a pull request is behind its base, on every check
// . Nil when GitHub cannot answer.
// Lock: runs with c.mu held. Suspends: github.commitsBehind.
func (w *Watches) behind(ctx context.Context, pull core.PullRequest, run core.Run) *int {
	if pull.HeadSha == nil || pull.BaseRef == nil {
		return nil
	}
	sha, base := *pull.HeadSha, *pull.BaseRef
	var count int
	var err error
	w.c.Suspend(func() { count, err = w.github.CommitsBehind(ctx, pull.Repo, base, sha) })
	if err != nil || count < 0 {
		return nil
	}
	return &count
}

// find returns the pull requests a leg watches: the ones from its own branch, or the run's subject's.
// Lock: runs with c.mu held. Suspends: github.pullRequest.
func (w *Watches) find(ctx context.Context, run core.Run, leg *core.Leg, events map[core.WatchEvent]struct{}) ([]core.PullRequest, error) {
	if about := run.Subject.About(); about != nil && about.Kind == core.ResourceRefKindPullRequest {
		var pull core.PullRequest
		var err error
		w.c.Suspend(func() { pull, err = w.github.PullRequestWithEvents(ctx, about.Repo, about.Number, events) })
		if err != nil {
			return nil, err
		}
		pull.Repo = about.Repo
		return []core.PullRequest{watchesClonePull(pull)}, nil
	}
	var branch *string
	if leg != nil && leg.Branch != nil {
		branch = leg.Branch
	} else {
		branch = run.Branch
	}
	if branch == nil {
		return nil, nil
	}
	var legID *string
	if leg != nil {
		id := leg.ID
		legID = &id
	}
	var pulls []core.PullRequest
	for _, repo := range run.Repos {
		var found *core.PullRequest
		var err error
		var known *core.PullRequest
		if core.LegFromID(legID) == nil {
			for i := range run.PullRequests {
				if run.PullRequests[i].Repo == repo {
					known = &run.PullRequests[i]
					break
				}
			}
		}
		if known != nil {
			number := known.Number
			var pull core.PullRequest
			w.c.Suspend(func() { pull, err = w.github.PullRequestWithEvents(ctx, repo, number, events) })
			if err == nil {
				found = &pull
			}
		} else {
			head := *branch
			w.c.Suspend(func() { found, err = w.github.PullRequestOnHeadWithEvents(ctx, repo, head, events) })
		}
		if err != nil {
			return nil, err
		}
		if found != nil {
			pull := watchesClonePull(*found)
			pull.Repo = repo
			pulls = append(pulls, pull)
		}
	}
	return pulls, nil
}

// toMerge returns the repositories among repos where branch has commits its base
// does not; one GitHub cannot answer for keeps its place.
// Lock: runs with c.mu held. Suspends: base, github.commitsAhead.
func (w *Watches) toMerge(ctx context.Context, repos []string, run core.Run, branch string) []string {
	var result []string
	for _, repo := range repos {
		base := w.base(ctx, run, repo)
		if base == nil {
			result = append(result, repo)
			continue
		}
		var ahead *int
		var err error
		baseName := *base
		w.c.Suspend(func() { ahead, err = w.github.CommitsAhead(ctx, repo, baseName, branch) })
		if err != nil || ahead == nil || *ahead != 0 {
			result = append(result, repo)
		}
	}
	return result
}

// base returns the branch the run's work merges into in repo, or nil.
// Lock: runs with c.mu held. Suspends: bases.base.
func (w *Watches) base(ctx context.Context, run core.Run, repo string) *string {
	name, err := w.bases.base(ctx, run, repo)
	if err != nil {
		return nil
	}
	return &name
}
