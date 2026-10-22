package coordinator

import (
	"maps"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// propose returns the observations to save and the one event to deliver, without
// touching the cache. Only the observations of the selected event are the
// consuming ones. Every event is per pull
// request but approved, which waits for an approval on every open pull request;
// The first approved one is reported.
// Lock: runs with c.mu held.
func (w *Watches) propose(pulls []core.PullRequest, events map[core.WatchEvent]struct{}, run core.Run) (*watchSignal, map[string]WatchesObservation) {
	priority := []core.WatchEvent{
		core.WatchEventClosed, core.WatchEventMerged, core.WatchEventNewChanges, core.WatchEventChangesRequested,
		core.WatchEventConflicted, core.WatchEventChecksFailed, core.WatchEventApproved, core.WatchEventCommented, core.WatchEventBehind,
	}
	type pending struct {
		event  core.WatchEvent
		signal watchSignal
		// observations are the consuming observations, by watch key.
		observations map[string]WatchesObservation
	}
	perPull := watchesWithout(events, core.WatchEventApproved)
	saved := map[string]WatchesObservation{}
	var candidates []pending
	var open []core.PullRequest
	for _, pull := range pulls {
		if pull.State != core.PullRequestStateOpen {
			continue
		}
		open = append(open, pull)
		initial := w.observation(run, pull)
		observation := watchesCloneObservation(initial)
		event := w.signalPull(pull, perPull, &observation)
		recovered := watchesCloneObservation(initial)
		w.signalPull(pull, map[core.WatchEvent]struct{}{}, &recovered)
		saved[w.watchKey(run, pull)] = recovered
		if event != nil {
			reported := pull
			if *event == core.WatchEventCommented {
				reported.Reviews = make([]core.PullRequestReview, 0, len(observation.ReviewAuthors))
				for _, author := range observation.ReviewAuthors {
					reported.Reviews = append(reported.Reviews, core.PullRequestReview{Author: author, State: core.PullRequestReviewStateCommented, Commit: pull.HeadSha})
				}
			}
			candidates = append(candidates, pending{
				event:        *event,
				signal:       watchSignal{Event: *event, Pull: reported, Detail: w.detail(*event, reported, len(pulls))},
				observations: map[string]WatchesObservation{w.watchKey(run, pull): observation},
			})
		}
	}
	if _, wanted := events[core.WatchEventApproved]; wanted {
		if signal, observations := w.approvedAcross(open, saved, len(pulls), run); signal != nil {
			candidates = append(candidates, pending{event: core.WatchEventApproved, signal: *signal, observations: observations})
		}
	}
	if len(candidates) == 0 {
		return nil, saved
	}
	best := 0
	for i := range candidates {
		if watchesPriorityIndex(priority, candidates[i].event) < watchesPriorityIndex(priority, candidates[best].event) {
			best = i
		}
	}
	selected := candidates[best]
	maps.Copy(saved, selected.observations)
	return &selected.signal, saved
}

// approvedAcross is the approved event once every open pull request has an
// approval and at least one of them is new, with the observations that consume
// every new approval; nil while one of them has none. recovered holds each open
// pull request's observation by watch key. The detail names each pull request
// with its new approvers, or all its approvers when none is new.
// Lock: none (pure; it reads the observations it is given).
func (w *Watches) approvedAcross(open []core.PullRequest, recovered map[string]WatchesObservation, among int, run core.Run) (*watchSignal, map[string]WatchesObservation) {
	if len(open) == 0 {
		return nil, nil
	}
	observations := map[string]WatchesObservation{}
	details := make([]string, 0, len(open))
	fresh := false
	for _, pull := range open {
		approvals := pull.KeyedApprovals()
		if len(approvals) == 0 {
			return nil, nil
		}
		key := w.watchKey(run, pull)
		observation := watchesCloneObservation(recovered[key])
		var authors []string
		for _, keyed := range approvals {
			if _, seen := observation.Approvals[keyed.Key]; !seen {
				authors = append(authors, keyed.Review.Author)
				observation.Approvals[keyed.Key] = struct{}{}
			}
		}
		if len(authors) > 0 {
			fresh = true
			observation.ReviewAuthors = authors
			observations[key] = observation
		} else {
			for _, keyed := range approvals {
				authors = append(authors, keyed.Review.Author)
			}
		}
		reported := pull
		reported.Reviews = make([]core.PullRequestReview, 0, len(authors))
		for _, author := range authors {
			reported.Reviews = append(reported.Reviews, core.PullRequestReview{Author: author, State: core.PullRequestReviewStateApproved})
		}
		details = append(details, w.detail(core.WatchEventApproved, reported, among))
	}
	if !fresh {
		return nil, nil
	}
	return &watchSignal{Event: core.WatchEventApproved, Pull: open[0], Detail: strings.Join(details, "; ")}, observations
}

// watchesPriorityIndex is the position of event in the priority list.
// Lock: none (pure).
func watchesPriorityIndex(priority []core.WatchEvent, event core.WatchEvent) int {
	for i, p := range priority {
		if p == event {
			return i
		}
	}
	return len(priority)
}

// publish stores the saved observations in the cache once the save succeeded.
// Lock: runs with c.mu held.
func (w *Watches) publish(saved map[string]WatchesObservation, run core.Run) {
	for key, observation := range saved {
		w.observations[run.ID+"|"+key] = watchesCloneObservation(observation)
	}
}

// detail is what the watch reports, naming the repository only when it watches
// more than one pull request.
// Lock: none (pure).
func (w *Watches) detail(event core.WatchEvent, pull core.PullRequest, among int) string {
	where := ""
	if among > 1 {
		where = " in " + pull.Repo
	}
	number := "#" + strconv.Itoa(pull.Number)
	switch event {
	case core.WatchEventMerged, core.WatchEventClosed:
		return number + where
	case core.WatchEventConflicted:
		return number + where + " has merge conflicts"
	case core.WatchEventChecksFailed:
		return number + where + " checks failed (" + strconv.Itoa(core.ValueOf(pull.CheckFailures)) + " of " + strconv.Itoa(core.ValueOf(pull.CheckTotal)) + ")"
	case core.WatchEventBehind:
		base := core.ValueOr(pull.BaseRef, "its base branch")
		return number + where + " is " + strconv.Itoa(core.ValueOf(pull.Behind)) + " commits behind " + base
	case core.WatchEventApproved:
		var authors []string
		for _, review := range pull.Reviews {
			if review.State == core.PullRequestReviewStateApproved {
				authors = append(authors, review.Author)
			}
		}
		return number + where + " is approved by " + strings.Join(authors, ", ")
	case core.WatchEventChangesRequested:
		var authors []string
		for _, review := range pull.LatestReviews() {
			if review.State == core.PullRequestReviewStateChangesRequested {
				authors = append(authors, review.Author)
			}
		}
		return number + where + ": " + strings.Join(authors, ", ") + " asked for changes"
	case core.WatchEventCommented:
		return number + where + ": " + w.commentDetail(pull)
	case core.WatchEventNewChanges:
		return number + where + " has new changes"
	default:
		return "none of the other configured outcomes applied"
	}
}

// commentDetail is the summary of a pull request's comments.
// Lock: none (pure).
func (w *Watches) commentDetail(pull core.PullRequest) string {
	var authors []string
	for _, review := range pull.HeadReviews() {
		if review.State == core.PullRequestReviewStateCommented {
			authors = append(authors, review.Author)
		}
	}
	if len(authors) == 1 {
		return authors[0] + " commented"
	}
	return strconv.Itoa(len(authors)) + " reviewers commented"
}
