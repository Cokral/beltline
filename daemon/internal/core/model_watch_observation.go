//
// Its sets are saved as sorted arrays; see setOf.

package core

import (
	"slices"
)

// WatchObservation is persisted per watch stage and repository so resumes and
// restarts do not report the same review or the same head twice. HeadSha is the
// baseline established by the first nonempty head of this visit, or advanced
// when a new-changes event is delivered. PendingHead is a later head not yet
// delivered; a newer unreported head replaces it. A missing PendingHead in an
// older file means there is nothing waiting.
//
// Approvals holds review keys already delivered in this approval visit. A key
// stays for repeated polls and an app restart, including while the head is
// missing and when the first known head arrives. It is dropped when that known
// head changes, or when a new visit starts: the watch is run again, or the run
// leaves and comes back. ApprovalVisit is the attempt that opened the visit. A
// file saved before the field existed leaves it absent and keeps its keys.
// Comments are not part of this visit.
//
// Decoding requires every key but headSha, arrival, pendingHead and
// approvalVisit.
type WatchObservation struct {
	PullNumber int `json:"pullNumber"`
	// HeadSha is the last head accepted as the baseline. Empty is treated as unknown.
	HeadSha *string `json:"headSha,omitzero"`
	Arrival *string `json:"arrival,omitzero"`
	// Reported is the watch events already reported in this visit.
	Reported      setOf[WatchEvent] `json:"reported"`
	BehindSeen    bool              `json:"behindSeen"`
	Approvals     setOf[string]     `json:"approvals"`
	Comments      setOf[string]     `json:"comments"`
	Requesting    setOf[string]     `json:"requesting"`
	ReviewAuthors []string          `json:"reviewAuthors"`
	// PendingHead is the latest head that differs from the baseline and has not been delivered.
	PendingHead *string `json:"pendingHead,omitzero"`
	// ApprovalVisit is the attempt that opened the approval visit these keys belong to.
	// Absent on a file saved before approval visits were recorded.
	ApprovalVisit *string `json:"approvalVisit,omitzero"`
}

// WatchObservationArgs are the parameters of NewWatchObservation.
type WatchObservationArgs struct {
	PullNumber    int
	HeadSha       *string
	Arrival       *string
	BehindSeen    bool
	PendingHead   *string
	ApprovalVisit *string
}

// NewWatchObservation creates an observation with empty sets.
func NewWatchObservation(a WatchObservationArgs) WatchObservation {
	return WatchObservation{
		PullNumber: a.PullNumber, HeadSha: a.HeadSha, Arrival: a.Arrival, Reported: map[WatchEvent]struct{}{},
		BehindSeen: a.BehindSeen, Approvals: map[string]struct{}{}, Comments: map[string]struct{}{},
		Requesting: map[string]struct{}{}, PendingHead: a.PendingHead, ApprovalVisit: a.ApprovalVisit,
	}
}

// WatchObservationApprovalVisit is the attempt that opened the current approval
// visit: consecutive attempts at the run's stage are one visit, and an outcome
// that runs the watch again starts a new one.
func WatchObservationApprovalVisit(run Run) *string {
	var streak []Attempt
	for i := len(run.Attempts) - 1; i >= 0 && run.Attempts[i].StageID == run.StageID; i-- {
		streak = append(streak, run.Attempts[i])
	}
	slices.Reverse(streak)
	if len(streak) == 0 {
		return nil
	}
	start := streak[0].ID
	for index := 1; index < len(streak); index++ {
		if streak[index-1].Route != nil && *streak[index-1].Route == "Running it again" {
			start = streak[index].ID
		}
	}
	return &start
}

// KeepingApprovals is this observation with its approvals kept, unless visit is
// a later approval visit than the one recorded (then they are cleared). A
// missing ApprovalVisit keeps the keys: the file predates the field.
func (o WatchObservation) KeepingApprovals(visit *string) WatchObservation {
	copied := o
	if visit != nil && o.ApprovalVisit != nil && *o.ApprovalVisit != *visit {
		copied.Approvals = map[string]struct{}{}
	}
	if visit != nil {
		v := *visit
		copied.ApprovalVisit = &v
	}
	return copied
}
