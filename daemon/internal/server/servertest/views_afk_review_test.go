// Contract tests of the AFK review: the review marks the named decisions, or all, as checked and
// returns the history.

package servertest

import (
	"slices"
	"testing"

	"encoding/json"
)

// TestAfkReviewMarksTheNamedDecisionsOrAllAsCheckedAndReturnsTheHistory checks that review marks the named decisions, or all, as checked and returns the history.
// afkReviewMarksTheNamedDecisionsOrAllAsCheckedAndReturnsTheHistory.
func TestAfkReviewMarksTheNamedDecisionsOrAllAsCheckedAndReturnsTheHistory(t *testing.T) {
	t.Parallel()
	f := Start(t, Options{})
	before := needingReview(f)
	if len(before) == 0 {
		t.Fatal("the contract log holds no decision that needs review")
	}
	first := before[0]
	reply := f.Do(Request{Method: "POST", Path: "/api/v1/afk/review", Token: ContractToken,
		Body: []byte(`{"ids":["` + first + `"]}`)})
	CheckGolden(t, "afkReview", "afk-review.json", reply, f.Root)
	var response struct {
		Decisions []struct {
			ID          string `json:"id"`
			NeedsReview bool   `json:"needsReview"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(reply.Body, &response); err != nil {
		t.Fatal(err)
	}
	for _, decision := range response.Decisions {
		if decision.ID == first && decision.NeedsReview {
			t.Fatalf("%s still needs review after it was marked", first)
		}
	}
	if got, want := needingReview(f), before[1:]; !slices.Equal(got, want) {
		t.Fatalf("after the named review %v, want %v", got, want)
	}

	all := f.Do(Request{Method: "POST", Path: "/api/v1/afk/review", Token: ContractToken, Body: []byte(`{}`)})
	if all.Status != 200 {
		t.Fatalf("review all: status %d", all.Status)
	}
	if got := needingReview(f); len(got) != 0 {
		t.Fatalf("after reviewing all, %v still need review", got)
	}
}

// needingReview are the ids of the coordinator's AFK decisions that still need review, in stored order.
func needingReview(f *Fixture) []string {
	var ids []string
	for _, decision := range f.Coordinator.ReadAfkDecisions() {
		if decision.NeedsReview() {
			ids = append(ids, decision.ID)
		}
	}
	return ids
}
