// The pull request signal of the coordinator's watches.

package coordinator

import (
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// signalPull is the event one pull request shows, if any, given the observation
// so far; it clears only known recovered conditions.
// Lock: none (pure; it changes only the observation it is given).
func (w *Watches) signalPull(pull core.PullRequest, events map[core.WatchEvent]struct{}, observation *WatchesObservation) *core.WatchEvent {
	events = watchesWithout(events, core.WatchEventNone)
	if pull.State != core.PullRequestStateOpen {
		event := core.WatchEventClosed
		if pull.State == core.PullRequestStateMerged {
			event = core.WatchEventMerged
		}
		return &event
	}
	signal := readPullSignal(pull, observation)
	for _, c := range signal.candidates() {
		if !c.active {
			continue
		}
		if _, wanted := events[c.event]; !wanted {
			continue
		}
		_, reviewEvent := core.WatchEventReviewEvents[c.event]
		_, reported := observation.Reported[c.event]
		if c.event != core.WatchEventNewChanges && !reviewEvent && reported {
			continue
		}
		event := c.event
		signal.record(event, observation)
		return &event
	}
	return nil
}

// pullSignal is what one open pull request shows this poll, read against the
// observation it had when the poll began.
type pullSignal struct {
	approvals       []core.PullRequestKeyedReview
	comments        []keyedComment
	requesting      map[string]struct{}
	newRequests     map[string]struct{}
	head            *string
	pendingChange   bool
	conflicted      bool
	checksFailed    bool
	behindConfirmed bool
}

// keyedComment is a commented review on the head with the key the observation remembers it by.
type keyedComment struct {
	key    string
	review core.PullRequestReview
}

// pullCandidate is one event a pull request may show, and whether it shows it now.
type pullCandidate struct {
	event  core.WatchEvent
	active bool
}

// readPullSignal reads an open pull request against the observation and clears the
// known conditions it has recovered from. Recovery must not consume a newly active
// request, so the new requests are worked out before the recovered ones are cleared.
// Lock: none (pure; it changes only the observation it is given).
func readPullSignal(pull core.PullRequest, observation *WatchesObservation) pullSignal {
	signal := pullSignal{
		approvals: pendingApprovals(pull, observation),
		comments:  pendingComments(pull, observation),
	}
	signal.requesting = requestingReviewers(pull, observation)
	signal.newRequests = newReviewRequests(signal.requesting, observation)
	clearRecovered(pull, observation, signal.requesting)
	signal.behindConfirmed = behindConfirmed(pull, observation)
	signal.head = watchesNonempty(pull.HeadSha)
	// A pending head is delivered only when this poll actually saw it.
	// Recovery passes an empty event set, so it cannot consume the change.
	signal.pendingChange = signal.head != nil && observation.PendingHead != nil && *observation.PendingHead == *signal.head &&
		!core.SamePointee(signal.head, watchesNonempty(observation.HeadSha))
	signal.conflicted = pull.Conflicted != nil && *pull.Conflicted
	signal.checksFailed = pull.Checks != nil && *pull.Checks == core.PullRequestCheckStateFailing
	return signal
}

// candidates lists the events the pull request may show, in the order they are tried.
// Lock: none (pure).
func (s pullSignal) candidates() []pullCandidate {
	return []pullCandidate{
		{core.WatchEventNewChanges, s.pendingChange},
		{core.WatchEventChangesRequested, len(s.newRequests) > 0},
		{core.WatchEventConflicted, s.conflicted},
		{core.WatchEventChecksFailed, s.checksFailed},
		{core.WatchEventApproved, len(s.approvals) > 0},
		{core.WatchEventCommented, len(s.comments) > 0},
		{core.WatchEventBehind, s.behindConfirmed},
	}
}

// record writes what delivering event remembers: the head it saw, the approvals or
// comments it reports, or the requests now active.
// Lock: none (pure; it changes only the observation it is given).
func (s pullSignal) record(event core.WatchEvent, observation *WatchesObservation) {
	switch event {
	case core.WatchEventNewChanges:
		observation.HeadSha = s.head
		observation.PendingHead = nil
	case core.WatchEventApproved:
		observation.ReviewAuthors = nil
		for _, keyed := range s.approvals {
			observation.ReviewAuthors = append(observation.ReviewAuthors, keyed.Review.Author)
			observation.Approvals[keyed.Key] = struct{}{}
		}
	case core.WatchEventCommented:
		observation.ReviewAuthors = nil
		for _, keyed := range s.comments {
			observation.ReviewAuthors = append(observation.ReviewAuthors, keyed.review.Author)
			observation.Comments[keyed.key] = struct{}{}
		}
	case core.WatchEventChangesRequested:
		observation.Requesting = s.requesting
	default:
		observation.Reported[event] = struct{}{}
	}
}

// pendingApprovals is the approvals of the pull request the observation has not seen.
// Lock: none (pure).
func pendingApprovals(pull core.PullRequest, observation *WatchesObservation) []core.PullRequestKeyedReview {
	var approvals []core.PullRequestKeyedReview
	for _, keyed := range pull.KeyedApprovals() {
		if _, seen := observation.Approvals[keyed.Key]; !seen {
			approvals = append(approvals, keyed)
		}
	}
	return approvals
}

// pendingComments is the commented reviews on the head that the observation has not seen.
// Lock: none (pure).
func pendingComments(pull core.PullRequest, observation *WatchesObservation) []keyedComment {
	var comments []keyedComment
	for index, review := range pull.HeadReviews() {
		key := pull.ReviewKey(review, index)
		if review.State != core.PullRequestReviewStateCommented {
			continue
		}
		if _, seen := observation.Comments[key]; seen {
			continue
		}
		comments = append(comments, keyedComment{key: key, review: review})
	}
	return comments
}

// requestingReviewers is the reviewers asking for changes, by lower-cased name: the
// observation's own set when the pull request carries no reviews, else its latest reviews.
// Lock: none (pure).
func requestingReviewers(pull core.PullRequest, observation *WatchesObservation) map[string]struct{} {
	if pull.Reviews == nil {
		return core.CopySet(observation.Requesting)
	}
	requesting := map[string]struct{}{}
	for _, review := range pull.LatestReviews() {
		if review.State == core.PullRequestReviewStateChangesRequested {
			requesting[strings.ToLower(review.Author)] = struct{}{}
		}
	}
	return requesting
}

// newReviewRequests is the requesting reviewers that the observation did not have.
// Lock: none (pure).
func newReviewRequests(requesting map[string]struct{}, observation *WatchesObservation) map[string]struct{} {
	newRequests := map[string]struct{}{}
	for name := range requesting {
		if _, was := observation.Requesting[name]; !was {
			newRequests[name] = struct{}{}
		}
	}
	return newRequests
}

// clearRecovered drops the recorded conditions the pull request no longer shows:
// requests that ended, and a conflict, failing checks or a base lag that cleared.
// Lock: none (pure; it changes only the observation it is given).
func clearRecovered(pull core.PullRequest, observation *WatchesObservation, requesting map[string]struct{}) {
	for name := range observation.Requesting {
		if _, now := requesting[name]; !now {
			delete(observation.Requesting, name)
		}
	}
	if len(requesting) == 0 {
		delete(observation.Reported, core.WatchEventChangesRequested)
	}
	if pull.Conflicted != nil && !*pull.Conflicted {
		delete(observation.Reported, core.WatchEventConflicted)
	}
	if pull.Checks != nil && *pull.Checks == core.PullRequestCheckStatePassing {
		delete(observation.Reported, core.WatchEventChecksFailed)
	}
	if pull.Behind != nil && *pull.Behind == 0 {
		delete(observation.Reported, core.WatchEventBehind)
		observation.BehindSeen = false
	}
}

// behindConfirmed reports a base lag seen on two polls in a row, and records this
// poll's lag. A base branch that is merely catching up is left to do so: only a
// pull request that is behind on two checks in a row is reported.
// Lock: none (pure; it changes only the observation it is given).
func behindConfirmed(pull core.PullRequest, observation *WatchesObservation) bool {
	behind := core.ValueOf(pull.Behind)
	confirmed := behind > 0 && observation.BehindSeen
	observation.BehindSeen = behind > 0
	return confirmed
}
