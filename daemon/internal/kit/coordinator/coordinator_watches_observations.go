// The observation half of the coordinator's watches.

package coordinator

import (
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// watchesCloneObservation copies an observation, its sets and its lists, so the copy shares nothing with the original.
// Lock: none (pure).
func watchesCloneObservation(o WatchesObservation) WatchesObservation {
	out := o
	out.HeadSha = core.CopyPointer(o.HeadSha)
	out.Arrival = core.CopyPointer(o.Arrival)
	out.PendingHead = core.CopyPointer(o.PendingHead)
	out.ApprovalVisit = core.CopyPointer(o.ApprovalVisit)
	out.Reported = map[core.WatchEvent]struct{}{}
	for event := range o.Reported {
		out.Reported[event] = struct{}{}
	}
	out.Approvals = core.CopySet(o.Approvals)
	out.Comments = core.CopySet(o.Comments)
	out.Requesting = core.CopySet(o.Requesting)
	out.ReviewAuthors = append([]string(nil), o.ReviewAuthors...)
	return out
}

// watchesNonempty returns a head GitHub actually reported; empty is as good as missing.
// Lock: none (pure).
func watchesNonempty(sha *string) *string {
	if sha == nil || *sha == "" {
		return nil
	}
	return sha
}

// observation is what the watch has seen of pull since the run came to it.
// Lock: runs with c.mu held.
func (w *Watches) observation(run core.Run, pull core.PullRequest) WatchesObservation {
	arrival := watchesArrival(run, nil)
	visit := core.WatchObservationApprovalVisit(run)
	var prior *WatchesObservation
	if cached, ok := w.observations[w.observationKey(run, pull)]; ok {
		copied := watchesCloneObservation(cached)
		prior = &copied
	} else if saved, ok := run.WatchObservations[w.watchKey(run, pull)]; ok {
		copied := watchesCloneObservation(saved)
		prior = &copied
	}
	sha := watchesNonempty(pull.HeadSha)
	if sha == nil {
		if prior != nil && prior.PullNumber == pull.Number && core.SamePointee(prior.Arrival, arrival) {
			return watchesCloneObservation(prior.KeepingApprovals(visit))
		}
		return core.NewWatchObservation(core.WatchObservationArgs{PullNumber: pull.Number, Arrival: arrival, ApprovalVisit: visit})
	}
	if prior != nil && prior.PullNumber == pull.Number && core.SamePointee(prior.Arrival, arrival) {
		baseline := watchesNonempty(prior.HeadSha)
		known := watchesNonempty(prior.PendingHead)
		if known == nil {
			known = baseline
		}
		if known != nil && *known == *sha {
			return watchesCloneObservation(prior.KeepingApprovals(visit))
		}
		// Compare with the last known head before rearming the other observations.
		// Learning the first head is a baseline, so approval keys carry over.
		if baseline == nil {
			return w.baselineObservation(pull, *sha, arrival, visit, prior, run)
		}
		if *sha == *baseline {
			return core.NewWatchObservation(core.WatchObservationArgs{PullNumber: pull.Number, HeadSha: baseline, Arrival: arrival, ApprovalVisit: visit})
		}
		moved := core.NewWatchObservation(core.WatchObservationArgs{PullNumber: pull.Number, HeadSha: baseline, Arrival: arrival, ApprovalVisit: visit})
		pending := *sha
		moved.PendingHead = &pending
		return moved
	}
	return w.baselineObservation(pull, *sha, arrival, visit, prior, run)
}

// baselineObservation is the first nonempty head of this visit. It does not
// report new changes; approvals already submitted qualify, comments from before arrival do not.
// Lock: none (pure).
func (w *Watches) baselineObservation(pull core.PullRequest, sha string, arrival *string, visit *string,
	prior *WatchesObservation, run core.Run) WatchesObservation {
	head := sha
	fresh := core.NewWatchObservation(core.WatchObservationArgs{PullNumber: pull.Number, HeadSha: &head, Arrival: arrival, ApprovalVisit: visit})
	var known *string
	if prior != nil {
		known = watchesNonempty(prior.PendingHead)
		if known == nil {
			known = watchesNonempty(prior.HeadSha)
		}
	}
	if prior != nil && prior.PullNumber == pull.Number && known != nil && *known == sha {
		fresh.Comments = core.CopySet(prior.Comments)
		fresh.Requesting = core.CopySet(prior.Requesting)
		// A new arrival is a new visit, so its approvals start clear.
		if core.SamePointee(prior.Arrival, arrival) {
			fresh.Approvals = prior.KeepingApprovals(visit).Approvals
		}
	} else if prior == nil || (prior.PullNumber == pull.Number && known == nil) {
		// First poll excludes comments predating arrival, not comments that
		// arrived between entering the watch and this poll. Submission time
		// does not exclude an approval.
		started := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
		if arrival != nil {
			for _, attempt := range run.Attempts {
				if attempt.ID == *arrival {
					started = attempt.StartedAt.Time
					break
				}
			}
		}
		for index, review := range pull.HeadReviews() {
			submitted := time.Time{}
			if review.SubmittedAt != nil {
				submitted = review.SubmittedAt.Time
			}
			if submitted.After(started) {
				continue
			}
			if review.State == core.PullRequestReviewStateCommented {
				fresh.Comments[pull.ReviewKey(review, index)] = struct{}{}
			}
		}
		if prior != nil && prior.PullNumber == pull.Number && core.SamePointee(prior.Arrival, arrival) {
			fresh.Approvals = prior.KeepingApprovals(visit).Approvals
		}
	}
	return watchesCloneObservation(fresh)
}

// watchesArrival is the first of the run's latest attempts in a row at its current stage.
// Lock: none (pure).
func watchesArrival(run core.Run, leg *core.Leg) *string {
	var found *string
	if leg == nil {
		for i := len(run.Attempts) - 1; i >= 0 && run.Attempts[i].StageID == run.StageID; i-- {
			id := run.Attempts[i].ID
			found = &id
		}
		return found
	}
	if leg.StageID == nil {
		return nil
	}
	legID := leg.ID
	want := core.LegFromID(&legID)
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if attempt.StageID != *leg.StageID || !core.SamePointee(attempt.Leg, want) {
			break
		}
		id := attempt.ID
		found = &id
	}
	return found
}

// watchKey is the stage and repository a watch observes.
// Lock: none (pure).
func (w *Watches) watchKey(run core.Run, pull core.PullRequest) string {
	return run.StageID + "|" + pull.Repo
}

// observationKey is the cache key of a pull request's observation on a run.
// Lock: none (pure).
func (w *Watches) observationKey(run core.Run, pull core.PullRequest) string {
	return run.ID + "|" + w.watchKey(run, pull)
}
