package api

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// TestBeltSnapshotSendsTheStoredBeltOnlyWhenItDiffersFromTheDraft checks the rule the web app relies on: a
// snapshot carries the stored definition only when the draft is not that definition (legacy stage keys filled in).
func TestBeltSnapshotSendsTheStoredBeltOnlyWhenItDiffersFromTheDraft(t *testing.T) {
	t.Parallel()
	record := store.BeltRecord{Belt: core.Belt{ID: "belt-a", Name: "Stored"}}

	same := beltSnapshotFromReads(record, coordinator.BeltSnapshotReads{Draft: record.Belt})
	if same.Belt != nil {
		t.Fatalf("a draft equal to the stored belt still sent the stored belt: %+v", same.Belt)
	}
	if same.Draft.ID != "belt-a" {
		t.Fatalf("draft id = %q", same.Draft.ID)
	}

	filled := record.Belt
	filled.Name = "Filled"
	differs := beltSnapshotFromReads(record, coordinator.BeltSnapshotReads{Draft: filled})
	if differs.Belt == nil || differs.Belt.Name != "Stored" {
		t.Fatalf("a draft that differs must send the stored belt, got %+v", differs.Belt)
	}
	if differs.Draft.Name != "Filled" {
		t.Fatalf("draft name = %q, want the draft's", differs.Draft.Name)
	}
}
