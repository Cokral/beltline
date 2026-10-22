package core

import (
	"strings"
	"testing"
	"time"
)

// ticketWatchTestState is the ticket the TicketWatchTests start from: one child done,
// one in progress.
func ticketWatchTestState() TicketState {
	return TicketState{
		Status: "In Progress",
		IsOpen: true,
		Children: []TicketStateChild{
			{Ref: "PAY-41", Title: "One", Status: "Done", IsOpen: false},
			{Ref: "PAY-42", Title: "Two", Status: "In Progress", IsOpen: true},
		},
	}
}

// ticketWatchTestEvents is every TicketEvent.
func ticketWatchTestEvents() map[TicketEvent]struct{} {
	events := map[TicketEvent]struct{}{}
	for _, event := range AllTicketEvent {
		events[event] = struct{}{}
	}
	return events
}

// ticketWatchQuietDuration is TicketWatch.quiet as a duration.
func ticketWatchQuietDuration() time.Duration {
	return time.Duration(TicketWatchQuiet * float64(time.Second))
}

// TestAChildFinishingIsReportedOnceItIsQuiet
func TestAChildFinishingIsReportedOnceItIsQuiet(t *testing.T) {
	t.Parallel()
	state := ticketWatchTestState()
	seen := NewTicketObservation(state)
	done := ticketWatchTestState()
	done.Children[1].IsOpen = false
	done.Children[1].Status = "Done"
	events := ticketWatchTestEvents()
	if report := TicketWatchReport(&seen, done, events, testT0, 0); report != nil {
		t.Fatalf("reported before the quiet period: %+v", report)
	}
	report := TicketWatchReport(&seen, done, events, testT0.Add(ticketWatchQuietDuration()), 0)
	if report == nil || report.Event != TicketEventChildrenChanged {
		t.Fatalf("report = %+v, want childrenChanged", report)
	}
	if !strings.Contains(report.Detail, "PAY-42 finished: Two (Done)") {
		t.Fatalf("detail misses the finished child:\n%s", report.Detail)
	}
	if !strings.Contains(report.Detail, "2 of 2 child tickets are done.") {
		t.Fatalf("detail misses the count:\n%s", report.Detail)
	}
	// Back at the watch with nothing new, it waits.
	if again := TicketWatchReport(&seen, done, events, testT0.Add(2*ticketWatchQuietDuration()), 0); again != nil {
		t.Fatalf("reported again with nothing new: %+v", again)
	}
}

// TestAChildMovingIsAChange
func TestAChildMovingIsAChange(t *testing.T) {
	t.Parallel()
	state := ticketWatchTestState()
	seen := NewTicketObservation(state)
	moved := ticketWatchTestState()
	moved.Children[1].Status = "In Review"
	changes := TicketWatchChangesSince(seen, moved)
	if len(changes.Moved) != 1 || changes.Moved[0].Ref != "PAY-42" {
		t.Fatalf("moved = %+v, want PAY-42", changes.Moved)
	}
	events := ticketWatchTestEvents()
	TicketWatchReport(&seen, moved, events, testT0, 0)
	report := TicketWatchReport(&seen, moved, events, testT0.Add(ticketWatchQuietDuration()), 0)
	if report == nil || !strings.Contains(report.Detail, "PAY-42 moved to In Review") {
		t.Fatalf("report = %+v, want the move", report)
	}
}

// TestCommentsAndChildrenGatherForTheQuietPeriod
func TestCommentsAndChildrenGatherForTheQuietPeriod(t *testing.T) {
	t.Parallel()
	state := ticketWatchTestState()
	seen := NewTicketObservation(state)
	changed := ticketWatchTestState()
	changed.Comments = []TicketStateComment{{ID: "c1", Author: "Ana", Body: "Can billing keep its own numbers?"}}
	changed.Children = append(changed.Children, TicketStateChild{Ref: "PAY-43", Title: "Three", Status: "To Do", IsOpen: true})
	events := ticketWatchTestEvents()
	if report := TicketWatchReport(&seen, changed, events, testT0, 0); report != nil {
		t.Fatalf("reported while gathering: %+v", report)
	}
	if seen.PendingSince == nil || !seen.PendingSince.Equal(testT0) {
		t.Fatalf("pendingSince = %v, want %v", seen.PendingSince, testT0)
	}
	if report := TicketWatchReport(&seen, changed, events, testT0.Add(60*time.Second), 0); report != nil {
		t.Fatalf("reported after a minute: %+v", report)
	}
	report := TicketWatchReport(&seen, changed, events, testT0.Add(ticketWatchQuietDuration()), 0)
	if report == nil || report.Event != TicketEventChildrenChanged {
		t.Fatalf("report = %+v, want childrenChanged", report)
	}
	if !strings.Contains(report.Detail, "PAY-43 added: Three (To Do)") {
		t.Fatalf("detail misses the added child:\n%s", report.Detail)
	}
	if !strings.Contains(report.Detail, "Ana commented:\n\n> Can billing keep its own numbers?") {
		t.Fatalf("detail misses the quoted comment:\n%s", report.Detail)
	}
	if seen.PendingSince != nil {
		t.Fatal("pendingSince is kept after the report")
	}
	if _, ok := seen.Comments["c1"]; len(seen.Comments) != 1 || !ok {
		t.Fatalf("comments = %v, want c1", seen.Comments)
	}
}

// TestAClosedTicketComesFirst
func TestAClosedTicketComesFirst(t *testing.T) {
	t.Parallel()
	seen := NewTicketObservation(ticketWatchTestState())
	closed := ticketWatchTestState()
	closed.IsOpen = false
	closed.Status = "Won't do"
	closed.Children[1].IsOpen = false
	report := TicketWatchReport(&seen, closed, ticketWatchTestEvents(), testT0, 0)
	if report == nil || report.Event != TicketEventTicketClosed {
		t.Fatalf("report = %+v, want ticketClosed", report)
	}
}

// TestTheSummarySaysWhereTheChildrenStand
func TestTheSummarySaysWhereTheChildrenStand(t *testing.T) {
	t.Parallel()
	seen := NewTicketObservation(ticketWatchTestState())
	if got := TicketWatchSummary(&seen, "PAY-40"); got != "Watching PAY-40 (In Progress): 1 of 2 child tickets done." {
		t.Fatalf("summary = %q", got)
	}
	if got := TicketWatchSummary(nil, "PAY-40"); got != "Looking at PAY-40 for the first time." {
		t.Fatalf("summary without an observation = %q", got)
	}
}
