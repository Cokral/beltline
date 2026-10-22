package coordinator

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
)

// TicketWatches watches the issue or ticket a run is about, for the events the
// stage's outcomes name: its status and comments, and the tickets under it. A
// part of Coordinator whose methods run with c.mu held; each await is a c.Suspend.
type TicketWatches struct {
	// c is the coordinator the ticket watches belong to (the back pointer).
	c      *Coordinator
	book   *RunBook
	github github.GitHub
	jira   jira.Jira
	clock  Clock
	// lastCheck is the last check time per run and leg.
	lastCheck map[string]time.Time
}

// newTicketWatches builds the ticket watches part.
// Lock: runs during New, before the coordinator is shared.
func newTicketWatches(c *Coordinator, book *RunBook, github github.GitHub, jira jira.Jira, clock Clock) TicketWatches {
	return TicketWatches{c: c, book: book, github: github, jira: jira, clock: clock, lastCheck: map[string]time.Time{}}
}

// check checks each watched ticket, at most once a minute unless force.
// Lock: runs with c.mu held. Suspends: checkLeg for each run and leg.
func (t *TicketWatches) check(ctx context.Context, force bool) {
	now := t.clock.Now()
	// Only a run with a running leg can have a watch to check, so only those are copied.
	for _, run := range runBookCopiesWhere(t.book, watchesMayRun) {
		if !watchesRunIsLive(t.book, run) {
			continue
		}
		for _, leg := range run.WorkingLegs() {
			stage := leg.Stage(run)
			if leg.State != core.RunStateRunning || stage == nil || !watchesWatchesTarget(stage, core.WatchTargetTicket) {
				continue
			}
			t.checkLeg(ctx, force, run, leg, now)
		}
	}
	live := map[string]struct{}{}
	for _, run := range t.book.runs {
		if run.State.IsActive() {
			live[run.ID] = struct{}{}
		}
	}
	for key := range t.lastCheck {
		owner := key
		if at := strings.Index(key, "|"); at >= 0 {
			owner = key[:at]
		}
		if !watchesHas(live, owner) {
			delete(t.lastCheck, key)
		}
	}
}

// checkLeg reads one leg's issue or ticket and delivers the event the stage's
// outcomes name. The first look only learns what is there already.
// Lock: runs with c.mu held. Suspends: read.
func (t *TicketWatches) checkLeg(ctx context.Context, force bool, run core.Run, leg core.Leg, now time.Time) {
	stage := leg.Stage(run)
	if stage == nil || watchesHas(t.book.busy, run.ID) {
		return
	}
	legID := leg.ID
	operation := run.Operation
	if !run.Has(core.InputTicket) {
		t.book.apply(run.ID, core.Event{Kind: core.EventKindBlocked, Reason: stage.Name + " needs " + core.InputTicket.Label() + ", and this run has none", Leg: &legID}, nil, &operation, nil)
		return
	}
	key := run.ID + "|" + leg.ID
	if !force {
		if last, ok := t.lastCheck[key]; ok && now.Sub(last).Seconds() < 60 {
			return
		}
	}
	t.lastCheck[key] = now
	t.book.busy[run.ID] = struct{}{}
	defer delete(t.book.busy, run.ID)
	state, err := t.read(ctx, run)
	if err != nil {
		runID := run.ID
		t.book.note("Could not read "+run.Subject.Ref()+": "+err.Error(), &runID)
		return
	}
	// A person may have moved the run while GitHub or Jira was answering.
	if !watchesStillCurrent(t.book, run, leg) {
		return
	}
	current := t.book.Run(run.ID)
	// The first look only learns what is there already.
	before, ok := current.TicketObservations[stage.ID]
	if !ok {
		t.book.update(run.ID, nil, &operation, func(r *core.Run) {
			if r.TicketObservations == nil {
				r.TicketObservations = map[string]core.TicketObservation{}
			}
			r.TicketObservations[stage.ID] = core.NewTicketObservation(state)
		})
		return
	}
	seen := ticketObservationCopy(before)
	events := map[core.TicketEvent]struct{}{}
	for _, outcome := range stage.Outcomes {
		if event := core.TicketEvent(outcome.When); core.IsTicketEvent(event) {
			events[event] = struct{}{}
		}
	}
	since := t.clock.Now()
	if seen.PendingSince != nil {
		since = seen.PendingSince.Time
	}
	excluded := core.RunPauseExcluded(*current, since, t.clock.Now())
	finding := core.TicketWatchReport(&seen, state, events, t.clock.Now(), excluded)
	if !reflect.DeepEqual(seen, before) {
		t.book.update(run.ID, nil, &operation, func(r *core.Run) {
			if r.TicketObservations == nil {
				r.TicketObservations = map[string]core.TicketObservation{}
			}
			r.TicketObservations[stage.ID] = seen
		})
	}
	if finding != nil {
		detail := finding.Detail
		t.book.apply(run.ID, core.Event{Kind: core.EventKindTicketWatched, Ticket: finding.Event, Detail: &detail, Leg: &legID}, nil, &operation, nil)
	}
}

// read returns the current state of the run's issue or ticket.
// Lock: runs with c.mu held. Suspends: github.issueState or jira.ticketState.
func (t *TicketWatches) read(ctx context.Context, run core.Run) (core.TicketState, error) {
	about := run.Subject.About()
	var state core.TicketState
	var err error
	switch {
	case about != nil && about.Kind == core.ResourceRefKindIssue:
		t.c.Suspend(func() { state, err = t.github.IssueState(ctx, about.Repo, about.Number) })
	case about != nil && about.Kind == core.ResourceRefKindTicket:
		t.c.Suspend(func() { state, err = t.jira.TicketState(ctx, about.Key) })
	default:
		return core.TicketState{}, core.NewFailure(run.Subject.Ref() + " is not an issue or ticket")
	}
	return state, err
}

// ticketObservationCopy copies a ticket observation, its comment set and its children, so the copy shares nothing with the original.
// Lock: none (pure).
func ticketObservationCopy(o core.TicketObservation) core.TicketObservation {
	out := o
	out.Comments = core.CopySet(o.Comments)
	out.Children = make(map[string]core.TicketObservationSeen, len(o.Children))
	maps.Copy(out.Children, o.Children)
	out.PendingSince = core.CopyPointer(o.PendingSince)
	return out
}
