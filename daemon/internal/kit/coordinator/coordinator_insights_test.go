package coordinator

import (
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestInsightsArchivingHidesAndRestoresABelt checks that the stored archive flag reaches the Insights view:
// archiving takes the belt out of the default picker, the opt-in offers it again, and unarchiving restores
// its place in the default picker.
func TestInsightsArchivingHidesAndRestoresABelt(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	c := h.Coord()
	id := savedLibraryBelt(t, h, defaultLibraryBelt()).ID
	inPicker := func(showArchived bool) bool {
		view, ok := c.ReadInsightsView(core.InsightsQuery{Period: core.InsightsPeriodAll, ShowArchived: showArchived}, nil, nil)
		if !ok {
			t.Fatalf("the Insights view refused a query with no belt")
		}
		return slices.ContainsFunc(view.Belts, func(belt core.InsightsBelt) bool { return belt.ID == id })
	}
	if !inPicker(false) {
		t.Fatalf("an active belt is missing from the picker")
	}
	c.SetBeltArchived(id, true)
	if inPicker(false) {
		t.Fatalf("an archived belt is still in the default picker")
	}
	if !inPicker(true) {
		t.Fatalf("the opt-in does not offer an archived belt")
	}
	c.SetBeltArchived(id, false)
	if !inPicker(false) {
		t.Fatalf("unarchiving did not restore the belt to the default picker")
	}
}
