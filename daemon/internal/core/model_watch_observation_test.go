package core

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestOldObservationsDecodeWithoutAPendingHead checks that an observation
// saved before pending heads and approval visits reads with neither, and it
// round-trips once a pending head is set.
func TestOldObservationsDecodeWithoutAPendingHead(t *testing.T) {
	t.Parallel()
	const saved = `{"pullNumber":201,"headSha":"abc","arrival":"watch-1","reported":[],"behindSeen":true,"approvals":["alice"],"comments":[],"requesting":[],"reviewAuthors":[]}`
	var decoded WatchObservation
	testDecode(t, saved, &decoded)
	if decoded.HeadSha == nil || *decoded.HeadSha != "abc" {
		t.Errorf("headSha = %v", decoded.HeadSha)
	}
	if decoded.PendingHead != nil {
		t.Errorf("pendingHead = %v, want nil", *decoded.PendingHead)
	}
	if !decoded.BehindSeen {
		t.Error("behindSeen lost")
	}
	if _, ok := decoded.Approvals["alice"]; !ok || len(decoded.Approvals) != 1 {
		t.Errorf("approvals = %v, want alice", decoded.Approvals)
	}
	if decoded.ApprovalVisit != nil {
		t.Errorf("approvalVisit = %v, want nil", *decoded.ApprovalVisit)
	}
	pending := decoded
	pending.PendingHead = new("def")
	data, err := jsonx.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	var again WatchObservation
	testDecode(t, string(data), &again)
	if d := testDiffEmpty(pending, again); d != "" {
		t.Fatalf("round trip: %s", d)
	}
}

// TestWatchObservationKeepingApprovals checks that approvals survive a later
// visit to the same approval visit, and are cleared when the visit changes.
func TestWatchObservationKeepingApprovals(t *testing.T) {
	t.Parallel()
	observation := NewWatchObservation(WatchObservationArgs{PullNumber: 1, ApprovalVisit: new("a1")})
	observation.Approvals = map[string]struct{}{"review-1": {}}
	if kept := observation.KeepingApprovals(new("a1")); len(kept.Approvals) != 1 {
		t.Errorf("same visit kept %v", kept.Approvals)
	}
	later := observation.KeepingApprovals(new("a2"))
	if len(later.Approvals) != 0 || later.ApprovalVisit == nil || *later.ApprovalVisit != "a2" {
		t.Errorf("a later visit = approvals %v visit %v", later.Approvals, later.ApprovalVisit)
	}
	if len(observation.Approvals) != 1 {
		t.Error("KeepingApprovals changed the original observation")
	}
	saved := NewWatchObservation(WatchObservationArgs{PullNumber: 1})
	saved.Approvals = map[string]struct{}{"review-2": {}}
	if kept := saved.KeepingApprovals(new("a3")); len(kept.Approvals) != 1 || *kept.ApprovalVisit != "a3" {
		t.Errorf("a file without a visit = approvals %v visit %v, want the keys kept", kept.Approvals, kept.ApprovalVisit)
	}
}

// TestWatchObservationApprovalVisitRestartsAfterRunningAgain checks that the
// visit is the run of attempts at the stage, and that an attempt which ran the
// watch again starts a new one. Attempts at other stages are not part of it.
func TestWatchObservationApprovalVisitRestartsAfterRunningAgain(t *testing.T) {
	t.Parallel()
	again := "Running it again"
	attempt := func(id, stage string, route *string) Attempt {
		return Attempt{ID: id, StageID: stage, Route: route, Folder: id}
	}
	run := Run{StageID: "watch", Attempts: []Attempt{
		attempt("build-1", "build", nil),
		attempt("watch-1", "watch", nil),
		attempt("watch-2", "watch", &again),
		attempt("watch-3", "watch", nil),
	}}
	if got := WatchObservationApprovalVisit(run); got == nil || *got != "watch-3" {
		t.Errorf("visit = %v, want watch-3", got)
	}
	if got := WatchObservationApprovalVisit(Run{StageID: "watch"}); got != nil {
		t.Errorf("no attempts = %v, want nil", *got)
	}
	run.Attempts = run.Attempts[:3]
	if got := WatchObservationApprovalVisit(run); got == nil || *got != "watch-1" {
		t.Errorf("visit before the rerun = %v, want watch-1", got)
	}
}

// TestNewTicketObservationCountsEverythingSeen checks that every comment and
// child in the state counts as seen, and that a repeated child keeps its first status.
func TestNewTicketObservationCountsEverythingSeen(t *testing.T) {
	t.Parallel()
	state := TicketState{Status: "In Progress", IsOpen: true,
		Comments: []TicketStateComment{{ID: "c1", Author: "a", Body: "hi"}, {ID: "c2", Author: "b"}},
		Children: []TicketStateChild{
			{Ref: "#9", Title: "one", Status: "open", IsOpen: true},
			{Ref: "#9", Title: "two", Status: "closed", IsOpen: false},
		}}
	seen := NewTicketObservation(state)
	if seen.Status != "In Progress" || len(seen.Comments) != 2 {
		t.Errorf("observation = %+v", seen)
	}
	if d := testDiffEmpty(map[string]TicketObservationSeen{"#9": {Status: "open", IsOpen: true}}, seen.Children); d != "" {
		t.Errorf("children: %s", d)
	}
	if seen.PendingSince != nil {
		t.Error("a fresh observation has a pending change")
	}
}
