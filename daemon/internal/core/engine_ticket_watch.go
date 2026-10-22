package core

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TicketWatchQuiet is how long a change that can wait (a comment, a child added
// or moved) gathers others before the watch reports them together, in seconds.
const TicketWatchQuiet float64 = 300

// TicketWatchChanges are the changes on a ticket since the last observation.
type TicketWatchChanges struct {
	Closed   bool
	Added    []TicketStateChild
	Finished []TicketStateChild
	Reopened []TicketStateChild
	Moved    []TicketStateChild
	Comments []TicketStateComment
}

// ChildrenChanged is whether any child was added, finished, reopened or moved.
func (c TicketWatchChanges) ChildrenChanged() bool {
	return len(c.Added) > 0 || len(c.Finished) > 0 || len(c.Reopened) > 0 || len(c.Moved) > 0
}

// Shows reports whether event describes one of these changes.
func (c TicketWatchChanges) Shows(event TicketEvent) bool {
	switch event {
	case TicketEventTicketClosed:
		return c.Closed
	case TicketEventChildrenChanged:
		return c.ChildrenChanged()
	case TicketEventNewComment:
		return len(c.Comments) > 0
	default:
		panic("TicketWatchChanges.Shows: unknown event")
	}
}

// TicketWatchChangesSince are the changes on state since seen.
func TicketWatchChangesSince(seen TicketObservation, state TicketState) TicketWatchChanges {
	var changes TicketWatchChanges
	changes.Closed = !state.IsOpen
	for _, child := range state.Children {
		before, ok := seen.Children[child.Ref]
		if !ok {
			changes.Added = append(changes.Added, child)
			continue
		}
		switch {
		case before.IsOpen && !child.IsOpen:
			changes.Finished = append(changes.Finished, child)
		case !before.IsOpen && child.IsOpen:
			changes.Reopened = append(changes.Reopened, child)
		case child.IsOpen && before.Status != child.Status:
			changes.Moved = append(changes.Moved, child)
		}
	}
	for _, comment := range state.Comments {
		if _, counted := seen.Comments[comment.ID]; !counted {
			changes.Comments = append(changes.Comments, comment)
		}
	}
	return changes
}

// TicketWatchFinding is the event a ticket watch reports and its detail: the
// (TicketEvent, String) tuple TicketWatch.report returns.
type TicketWatchFinding struct {
	Event  TicketEvent
	Detail string
}

// TicketWatchReport is the event to report now, the most pressing first, and its
// detail; nil while there is none or it is still gathering. seen becomes the
// ticket as reported, or remembers when gathering began. events are the events
// the watch stage waits for. excluding is seconds to leave out of the quiet
// period.
func TicketWatchReport(seen *TicketObservation, state TicketState, events map[TicketEvent]struct{}, now time.Time, excluding float64) *TicketWatchFinding {
	changes := TicketWatchChangesSince(*seen, state)
	var event *TicketEvent
	for index := range ticketEventPriority {
		candidate := ticketEventPriority[index]
		if _, wanted := events[candidate]; wanted && changes.Shows(candidate) {
			event = &candidate
			break
		}
	}
	if event == nil {
		seen.PendingSince = nil
		return nil
	}
	if event.Gathers() {
		if seen.PendingSince == nil {
			since := jsonx.Date{Time: now}
			seen.PendingSince = &since
			return nil
		}
		elapsed := now.Sub(seen.PendingSince.Time).Seconds()
		if elapsed-excluding < TicketWatchQuiet {
			return nil
		}
	}
	*seen = NewTicketObservation(state)
	return &TicketWatchFinding{Event: *event, Detail: TicketWatchDetail(changes, state)}
}

// TicketWatchDetail is every change, one per line, then where the children stand.
func TicketWatchDetail(changes TicketWatchChanges, state TicketState) string {
	var lines []string
	if changes.Closed {
		status := "closed"
		if state.Status != "" {
			status = state.Status
		}
		lines = append(lines, "The ticket is "+status+".")
	}
	for _, child := range changes.Added {
		lines = append(lines, child.Ref+" added: "+child.Title+" ("+child.Status+")")
	}
	for _, child := range changes.Finished {
		lines = append(lines, child.Ref+" finished: "+child.Title+" ("+child.Status+")")
	}
	for _, child := range changes.Reopened {
		lines = append(lines, child.Ref+" reopened: "+child.Title+" ("+child.Status+")")
	}
	for _, child := range changes.Moved {
		lines = append(lines, child.Ref+" moved to "+child.Status+": "+child.Title)
	}
	for _, comment := range changes.Comments {
		body := strings.TrimSpace(comment.Body)
		line := comment.Author + " commented"
		if body == "" {
			line += "."
		} else {
			var quoted []string
			for _, piece := range outcomeParserLFSplit(body) {
				if piece != "" {
					quoted = append(quoted, "> "+piece)
				}
			}
			line += ":\n\n" + strings.Join(quoted, "\n") + "\n"
		}
		lines = append(lines, line)
	}
	if len(state.Children) > 0 {
		done := 0
		for _, child := range state.Children {
			if !child.IsOpen {
				done++
			}
		}
		lines = append(lines, strings.Join([]string{strconv.Itoa(done), "of", strconv.Itoa(len(state.Children)), "child tickets are done."}, " "))
	}
	return strings.Join(lines, "\n")
}

// TicketWatchSummary is where a watched ticket stands, as the run page says it.
func TicketWatchSummary(seen *TicketObservation, ref string) string {
	if seen == nil {
		return "Looking at " + ref + " for the first time."
	}
	refs := slices.Sorted(maps.Keys(seen.Children))
	status := ""
	if seen.Status != "" {
		status = " (" + seen.Status + ")"
	}
	if len(refs) == 0 {
		return "Watching " + ref + status + ". It has no child tickets yet."
	}
	done := 0
	for _, childRef := range refs {
		if !seen.Children[childRef].IsOpen {
			done++
		}
	}
	return "Watching " + ref + status + ": " + strconv.Itoa(done) + " of " + strconv.Itoa(len(refs)) + " child tickets done."
}
