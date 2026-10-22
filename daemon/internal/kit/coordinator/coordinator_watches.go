package coordinator

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
)

// watchesGrace is how long a repository the branch changed nothing in, or a
// missing one, is given before the watch stops asking about it.
const watchesGrace = 10 * time.Minute

// watchesGraceSeconds is watchesGrace in the seconds the comparisons use.
const watchesGraceSeconds = 600.0

// WatchesObservation is what the watch has seen of a pull request.
type WatchesObservation = core.WatchObservation

// watchSignal is the one event a watch delivers, the pull request it came from
// (the first one for approved, which comes from all of them) and what it records
// .
type watchSignal struct {
	Event  core.WatchEvent
	Pull   core.PullRequest
	Detail string
}

// Watches watches the pull request under review, or the run's branch in every
// repository, for the events the stage's outcomes name. A None outcome is taken
// only when no other configured outcome applies. A part of Coordinator whose
// methods run with c.mu held; each await is a c.Suspend.
type Watches struct {
	// c is the coordinator the watches belong to (the back pointer).
	c      *Coordinator
	book   *RunBook
	github github.GitHub
	clock  Clock
	// lastCheck is the last check time per run and leg.
	lastCheck map[string]time.Time
	// bases resolves the base branch each repository's watch compares against.
	bases *Bases
	// observations caches the observations also saved on each run.
	observations map[string]WatchesObservation
}

// newWatches builds the watches part.
// Lock: runs during New, before the coordinator is shared.
func newWatches(c *Coordinator, book *RunBook, github github.GitHub, clock Clock) Watches {
	bases := &Bases{c: c, github: github, defaults: map[string]string{}, found: map[string]struct{}{}}
	return Watches{
		c: c, book: book, github: github, clock: clock, lastCheck: map[string]time.Time{},
		bases: bases, observations: map[string]WatchesObservation{},
	}
}

// check checks each watched pull request, at most once a minute unless force.
// Each leg of a run after a fork watches the pull request of its own branch.
// Lock: runs with c.mu held. Suspends: checkLeg for each run and leg.
func (w *Watches) check(ctx context.Context, force bool) {
	now := w.clock.Now()
	// The loop over book.runs sees the array as it was at the start. Only a run with a
	// running leg can have a pull request to check, so only those are copied.
	for _, run := range runBookCopiesWhere(w.book, watchesMayRun) {
		if !watchesRunIsLive(w.book, run) {
			continue
		}
		for _, leg := range run.WorkingLegs() {
			stage := leg.Stage(run)
			if leg.State != core.RunStateRunning || stage == nil || !watchesWatchesTarget(stage, core.WatchTargetPullRequest) {
				continue
			}
			w.checkLeg(ctx, force, run, leg, now)
		}
	}
	w.prune()
}

// watchesMayRun reports whether run may have a running leg, the only kind a watch pass checks.
// Lock: none (pure).
func watchesMayRun(run core.Run) bool { return runMayHaveLegIn(run, core.RunStateRunning) }

// watchesRunIsLive is the condition every watch pass checks before it looks at a run:
// the book lets it move on by itself.
// Lock: runs with c.mu held.
func watchesRunIsLive(book *RunBook, run core.Run) bool {
	return book.may(run, workAdvance)
}

// watchesWatchesTarget reports whether the stage watches target.
// Lock: none (pure).
func watchesWatchesTarget(stage *core.Stage, target core.WatchTarget) bool {
	found := stage.WatchTarget()
	return found != nil && *found == target
}

// watchesMixedReason is why a watch whose pull requests are all done, some merged
// and some closed without merging, holds the run.
// Lock: none (pure).
func watchesMixedReason(pulls []core.PullRequest) string {
	var merged, closed []string
	for _, pull := range pulls {
		name := "#" + strconv.Itoa(pull.Number) + " in " + pull.Repo
		if pull.State == core.PullRequestStateMerged {
			merged = append(merged, name)
		} else {
			closed = append(closed, name)
		}
	}
	return strings.Join(merged, ", ") + " merged, but " + strings.Join(closed, ", ") + " closed without merging"
}

// watchesStillCurrent reports whether the leg still runs the stage and attempt a check
// read from run: a person may have moved, stopped or paused the run, or sent it to a fresh
// attempt (Send to keeps the operation), while GitHub or Jira was answering.
// Lock: c.mu held (reads the run book).
func watchesStillCurrent(book *RunBook, run core.Run, leg core.Leg) bool {
	current := book.Run(run.ID)
	if current == nil || current.Parking != nil || current.Generation != run.Generation || current.Operation != run.Operation ||
		(current.Pause != nil && current.Pause.BlocksProgress()) || book.isErasing(run.ID) {
		return false
	}
	currentLeg := current.LegByID(&leg.ID)
	if currentLeg == nil || currentLeg.State != core.RunStateRunning || !core.SamePointee(currentLeg.StageID, leg.StageID) {
		return false
	}
	before, now := watchesOpenAttempt(run, leg), watchesOpenAttempt(*current, *currentLeg)
	if before == nil || now == nil {
		return before == now
	}
	return before.ID == now.ID
}

// watchesOpenAttempt is the last open attempt of the leg's stage, or nil.
// Lock: none (pure).
func watchesOpenAttempt(run core.Run, leg core.Leg) *core.Attempt {
	stageID := core.ValueOr(leg.StageID, run.StageID)
	key := core.AttemptKey(stageID, &leg.ID)
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if attempt.IsOpen() && core.AttemptKey(attempt.StageID, attempt.Leg) == key {
			found := attempt
			return &found
		}
	}
	return nil
}

// watchesBranchName is the branch named in the watch's blocked reasons: the leg's, else the run's, else "the run's branch".
// Lock: none (pure).
func watchesBranchName(leg core.Leg, run core.Run) string {
	if branch := watchesLegBranch(leg, run); branch != nil {
		return *branch
	}
	return "the run's branch"
}

// watchesLegBranch is the leg's branch, else the run's.
// Lock: none (pure).
func watchesLegBranch(leg core.Leg, run core.Run) *string {
	if leg.Branch != nil {
		return leg.Branch
	}
	return run.Branch
}

// watchesWithout returns a copy of set without value.
// Lock: none (pure).
func watchesWithout(set map[core.WatchEvent]struct{}, value core.WatchEvent) map[core.WatchEvent]struct{} {
	out := make(map[core.WatchEvent]struct{}, len(set))
	for event := range set {
		if event != value {
			out[event] = struct{}{}
		}
	}
	return out
}

// watchesHas reports whether key is in set.
// Lock: none (pure).
func watchesHas(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// key is the per-leg key of a run's watch.
// Lock: none (pure).
func (w *Watches) key(run core.Run, leg core.Leg) string { return run.ID + "#" + leg.ID }

// terminal is the event a merged or closed pull request reports: its outcome
// when the stage has one, else None when configured, else the event itself, which the engine holds for someone.
// Lock: none (pure).
func (w *Watches) terminal(event core.WatchEvent, summary string, events map[core.WatchEvent]struct{}, catchAll bool, leg *core.Leg) core.Event {
	var legID *string
	if leg != nil {
		id := leg.ID
		legID = &id
	}
	if _, ok := events[event]; ok || !catchAll {
		detail := summary
		return core.Event{Kind: core.EventKindWatched, Watch: event, Detail: &detail, Leg: legID}
	}
	detail := watchesNoneDetail(summary)
	return core.Event{Kind: core.EventKindWatched, Watch: core.WatchEventNone, Detail: &detail, Leg: legID}
}

// watchesNoneDetail is what a None outcome records: which pull requests were seen,
// and that nothing else configured applied.
// Lock: none (pure).
func watchesNoneDetail(summary string) string {
	return summary + ": none of the other configured outcomes applied"
}

// forget drops what a run left behind: a watch that fired observes afresh.
// Lock: runs with c.mu held.
func (w *Watches) forget(run core.Run) {
	prefix := run.ID + "|"
	for key := range w.observations {
		if strings.HasPrefix(key, prefix) {
			delete(w.observations, key)
		}
	}
}

// prune drops the observations of runs that are gone or have finished.
// Lock: runs with c.mu held.
func (w *Watches) prune() {
	live := map[string]struct{}{}
	for _, run := range w.book.runs {
		if run.State.IsActive() {
			live[run.ID] = struct{}{}
		}
	}
	for key := range w.observations {
		// Splitting omits empty pieces, so the first piece is the first nonempty one.
		owner := ""
		for _, piece := range strings.Split(key, "|") {
			if piece != "" {
				owner = piece
				break
			}
		}
		if _, ok := live[owner]; !ok {
			delete(w.observations, key)
		}
	}
}
