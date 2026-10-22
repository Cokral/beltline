package core

import "testing"

// TestBeltLibraryDetailIsTheTriggerSummaryThenTheStageCount: a belt with no trigger says so, and counts its stages.
func TestBeltLibraryDetailIsTheTriggerSummaryThenTheStageCount(t *testing.T) {
	t.Parallel()
	belt := Belt{Stages: []Stage{{}, {}}}
	if got := BeltLibraryDetail(belt); got != "Add at least one trigger · 2 stages" {
		t.Fatalf("detail = %q", got)
	}
}
