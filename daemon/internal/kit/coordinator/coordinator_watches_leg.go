package coordinator

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// watchCheck is one leg's check: the run and leg it reads, when it runs, the
// operation that fences its late results, and the events its stage's outcomes name.
type watchCheck struct {
	run       core.Run
	leg       core.Leg
	legID     string
	operation int
	now       time.Time
	events    map[core.WatchEvent]struct{}
	catchAll  bool
}

// checkLeg checks one leg's pull requests and delivers the event the stage's
// outcomes name, once the matching observations are saved.
// Lock: runs with c.mu held. Suspends: find, toMerge, behind.
func (w *Watches) checkLeg(ctx context.Context, force bool, run core.Run, leg core.Leg, now time.Time) {
	stage := leg.Stage(run)
	if stage == nil || watchesHas(w.book.busy, run.ID) || watchesHas(w.book.busy, w.key(run, leg)) {
		return
	}
	legID := leg.ID
	operation := run.Operation
	if !run.Has(core.InputPullRequest) {
		w.book.apply(run.ID, core.Event{Kind: core.EventKindBlocked, Reason: stage.Name + " needs " + core.InputPullRequest.Label() + ", and this run has none", Leg: &legID}, nil, &operation, nil)
		return
	}
	key := w.key(run, leg)
	if !force {
		if last, ok := w.lastCheck[key]; ok && now.Sub(last).Seconds() < 60 {
			return
		}
	}
	w.lastCheck[key] = now
	w.book.busy[key] = struct{}{}
	defer delete(w.book.busy, key)

	c := watchCheck{run: run, leg: leg, legID: legID, operation: operation, now: now}
	c.events, c.catchAll = watchesConfigured(stage)
	found, err := w.find(ctx, run, &leg, c.events)
	if err != nil {
		return
	}
	attempt := watchesOpenAttempt(run, leg)
	if len(found) == 0 {
		w.noPullRequest(c, attempt)
		return
	}
	pulls := append([]core.PullRequest(nil), found...)
	first := watchesFirstPullRequest(run, attempt, now)
	missing := w.missingRepos(ctx, c, pulls)
	// Only a stage that watches for a behind base pays for the comparison.
	if len(missing) == 0 {
		w.readBehind(ctx, c, pulls)
	}
	if !watchesStillCurrent(w.book, run, leg) {
		return
	}
	w.book.update(run.ID, nil, &operation, func(r *core.Run) {
		r.PullRequests = watchesKeeping(r.PullRequests, pulls, leg)
		firstAt := jsonx.Date{Time: first}
		r.FirstPullRequestAt = &firstAt
	})
	summary := watchesSummary(pulls)
	if w.settle(c, pulls, missing, first, summary) {
		return
	}
	w.deliverSelected(c, pulls, summary)
}

// watchesConfigured returns the events a stage's outcomes name, without None,
// and whether a None outcome is configured (the catch-all).
// Lock: none (pure).
func watchesConfigured(stage *core.Stage) (map[core.WatchEvent]struct{}, bool) {
	configured := map[core.WatchEvent]struct{}{}
	for _, outcome := range stage.Outcomes {
		if event := core.WatchEvent(outcome.When); core.IsWatchEvent(event) {
			configured[event] = struct{}{}
		}
	}
	// None adds no GitHub fields: fetch only what the other outcomes need.
	events := watchesWithout(configured, core.WatchEventNone)
	_, catchAll := configured[core.WatchEventNone]
	return events, catchAll
}

// noPullRequest holds the run once the grace period has passed with no pull request
// from the leg's branch. A paused interval does not consume the watch grace period;
// operation fencing drops late results.
// Lock: runs with c.mu held.
func (w *Watches) noPullRequest(c watchCheck, attempt *core.Attempt) {
	if attempt == nil || !watchesStillCurrent(w.book, c.run, c.leg) {
		return
	}
	started := attempt.StartedAt.Time
	if c.now.Sub(started).Seconds()-core.RunPauseExcluded(c.run, started, c.now) > watchesGraceSeconds {
		reason := "no pull request from " + watchesBranchName(c.leg, c.run)
		w.book.apply(c.run.ID, core.Event{Kind: core.EventKindBlocked, Reason: reason, Leg: &c.legID}, nil, &c.operation, nil)
	}
}

// watchesFirstPullRequest is when the run's first pull request was seen in this watch
// attempt: the stored time when it falls inside the attempt, else now. The grace for
// missing repositories starts again with each watch attempt.
// Lock: none (pure).
func watchesFirstPullRequest(run core.Run, attempt *core.Attempt, now time.Time) time.Time {
	started := time.Time{}
	if attempt != nil {
		started = attempt.StartedAt.Time
	}
	if run.FirstPullRequestAt != nil && !run.FirstPullRequestAt.Before(started) {
		return run.FirstPullRequestAt.Time
	}
	return now
}

// missingRepos lists the repositories a pull request is expected in but none was found,
// dropping those the leg's branch changed nothing in, since they have nothing to merge.
// Lock: runs with c.mu held. Suspends: toMerge.
func (w *Watches) missingRepos(ctx context.Context, c watchCheck, pulls []core.PullRequest) []string {
	expected := c.run.Repos
	if c.run.Subject.Kind == core.SubjectKindPullRequest {
		expected = []string{c.run.Subject.Repo}
	}
	var missing []string
	for _, repo := range expected {
		if !watchesHasPull(pulls, repo) {
			missing = append(missing, repo)
		}
	}
	// A repository the leg's branch changed nothing in has nothing to merge.
	if len(missing) > 0 && c.run.Subject.Kind != core.SubjectKindPullRequest {
		if branch := watchesLegBranch(c.leg, c.run); branch != nil {
			missing = w.toMerge(ctx, missing, c.run, *branch)
		}
	}
	return missing
}

// readBehind fills in whether each open pull request is behind its base, for a stage
// that watches for a behind base.
// Lock: runs with c.mu held. Suspends: behind.
func (w *Watches) readBehind(ctx context.Context, c watchCheck, pulls []core.PullRequest) {
	if _, ok := c.events[core.WatchEventBehind]; !ok {
		return
	}
	for index := range pulls {
		if pulls[index].State == core.PullRequestStateOpen {
			pulls[index].Behind = w.behind(ctx, pulls[index], c.run)
		}
	}
}

// watchesSummary names the pull requests a watch saw: one number, or each number
// with its repository.
// Lock: none (pure).
func watchesSummary(pulls []core.PullRequest) string {
	summary := "#" + strconv.Itoa(pulls[0].Number)
	if len(pulls) > 1 {
		parts := make([]string, 0, len(pulls))
		for _, pull := range pulls {
			parts = append(parts, "#"+strconv.Itoa(pull.Number)+" in "+pull.Repo)
		}
		summary = strings.Join(parts, ", ")
	}
	return summary
}

// settle ends the check once the pull requests have reached a final state, and
// reports whether it did: all closed without merging, a missing repository past its
// grace, all merged, or some merged and the rest closed (neither outcome fits, so it
// holds the run). Otherwise the watch goes on and the selected event is delivered.
// Lock: runs with c.mu held.
func (w *Watches) settle(c watchCheck, pulls []core.PullRequest, missing []string, first time.Time, summary string) bool {
	// Closed only once every pull request is closed without merging; one closed
	// beside open ones leaves the watch on the open ones.
	if watchesAllState(pulls, core.PullRequestStateClosed) {
		if w.book.apply(c.run.ID, w.terminal(core.WatchEventClosed, summary, c.events, c.catchAll, &c.leg), nil, &c.operation, nil) {
			w.forget(c.run)
		}
		return true
	}
	if len(missing) > 0 {
		if c.now.Sub(first).Seconds()-core.RunPauseExcluded(c.run, first, c.now) > watchesGraceSeconds {
			reason := "no pull request from " + watchesBranchName(c.leg, c.run) + " in " + strings.Join(missing, ", ")
			w.book.apply(c.run.ID, core.Event{Kind: core.EventKindBlocked, Reason: reason, Leg: &c.legID}, nil, &c.operation, nil)
		}
		return true
	}
	if watchesAllState(pulls, core.PullRequestStateMerged) {
		if w.book.apply(c.run.ID, w.terminal(core.WatchEventMerged, summary, c.events, c.catchAll, &c.leg), nil, &c.operation, nil) {
			w.forget(c.run)
		}
		return true
	}
	// Some merged and the rest closed: nothing is left to watch, and neither outcome fits.
	if !watchesAnyState(pulls, core.PullRequestStateOpen) {
		reason := watchesMixedReason(pulls)
		if w.book.apply(c.run.ID, core.Event{Kind: core.EventKindBlocked, Reason: reason, Leg: &c.legID}, nil, &c.operation, nil) {
			w.forget(c.run)
		}
		return true
	}
	return false
}

// deliverSelected delivers the event the watch selects, or None for a catch-all, with
// the observations that record it. Baseline and delivery are authoritative only after
// they are saved, so the selected event and the observations share one write.
// Lock: runs with c.mu held.
func (w *Watches) deliverSelected(c watchCheck, pulls []core.PullRequest, summary string) {
	selected, saved := w.propose(pulls, c.events, c.run)
	record := func(r *core.Run) {
		if r.WatchObservations == nil {
			r.WatchObservations = map[string]core.WatchObservation{}
		}
		for key, observation := range saved {
			r.WatchObservations[key] = watchesCloneObservation(observation)
		}
	}
	var persisted bool
	switch {
	case selected != nil:
		detail := selected.Detail
		persisted = w.book.apply(c.run.ID, core.Event{Kind: core.EventKindWatched, Watch: selected.Event, Detail: &detail, Leg: &c.legID}, nil, &c.operation, record)
	case c.catchAll:
		detail := watchesNoneDetail(summary)
		persisted = w.book.apply(c.run.ID, core.Event{Kind: core.EventKindWatched, Watch: core.WatchEventNone, Detail: &detail, Leg: &c.legID}, nil, &c.operation, record)
	default:
		persisted = w.book.update(c.run.ID, nil, &c.operation, record)
	}
	if persisted {
		w.publish(saved, c.run)
	}
}
