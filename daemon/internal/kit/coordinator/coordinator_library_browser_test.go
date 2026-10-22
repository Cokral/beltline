// Coordinator tests of the library browser's belt cards: pinned looks, cap stages, and paused
// separated from archived. The saves go through Library.SaveBelt and SaveAgent, which is the same
// write the coordinator makes.

package coordinator

import (
	"strconv"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

func TestCoordinatorCardsUsePinnedLooksCapStagesAndSeparatePausedFromArchived(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	c := h.Coord()

	pinned := c.LatestAgent("triage").Agent
	visual := cloneAgent(pinned)
	icon := core.StageIconWrench
	visual.Icon = &icon
	visual.Tint = core.TintRose
	if _, err := c.SaveAgent(visual); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	definition := core.NewBeltFromTrigger(core.BeltFromTriggerArgs{
		ID: "visual-belt", Name: "Visual belt", Trigger: core.TriggerKindManual.Fresh(), Repos: []string{}, Stages: []core.Stage{},
	})
	for index := 0; index < 9; index++ {
		stage := core.DefaultsStageForAgent(pinned, nil, nil, nil, core.TriggerKindManual)
		stage.ID = "visual-" + strconv.Itoa(index)
		stage.Name = "Stage " + strconv.Itoa(index)
		definition.Stages = append(definition.Stages, stage)
	}
	savedLibraryBelt(t, h, definition)
	h.Turn(func() { c.library.SetBeltArchived("visual-belt", true) })

	warned := core.NewBeltFromTrigger(core.BeltFromTriggerArgs{
		ID: "warned-belt", Name: "Warned belt", Trigger: core.TriggerKindManual.Fresh(), Repos: []string{}, Stages: []core.Stage{},
	})
	warned.Stages = []core.Stage{{ID: "missing-stage", Type: core.StageTypeAgent, Agent: &core.AgentRef{ID: "missing-agent", Version: 1}, Outcomes: []core.Outcome{}}}
	warnedSaved := savedLibraryBelt(t, h, warned)
	if len(c.Warnings(warnedSaved)) == 0 {
		t.Errorf("the warned belt has no warnings")
	}
	if record := c.Belt("warned-belt"); record == nil || !record.EffectivelyPaused() {
		t.Errorf("the warned belt is not paused")
	}

	archivedBrowser := c.ReadBeltBrowser("", "archived")
	archivedCard := findCard(t, archivedBrowser.Items, "visual-belt")
	if archivedCard.Status != "Archived" {
		t.Errorf("archived status = %q", archivedCard.Status)
	}
	if len(archivedCard.Stages) != 7 {
		t.Errorf("stage looks = %d, want 7", len(archivedCard.Stages))
	}
	glyph := pinned.Glyph()
	for _, look := range archivedCard.Stages {
		if look.Icon != glyph || look.Tint != pinned.Tint {
			t.Errorf("a stage look is %v %v, want the pinned %v %v", look.Icon, look.Tint, glyph, pinned.Tint)
		}
	}

	liveBrowser := c.ReadBeltBrowser("", "all")
	for _, card := range liveBrowser.Items {
		if card.ID == "visual-belt" {
			t.Errorf("the archived belt is listed under all")
		}
	}
	if got := findCard(t, liveBrowser.Items, "warned-belt").Status; got != "Paused" {
		t.Errorf("warned status = %q, want Paused", got)
	}
	if count := filterCount(liveBrowser.Filters, "paused"); count != 1 {
		t.Errorf("paused count = %d, want 1", count)
	}
}

// findCard returns the card with id, failing the test when there is none.
func findCard(t *testing.T, cards []core.LibraryCard, id string) core.LibraryCard {
	t.Helper()
	for _, card := range cards {
		if card.ID == id {
			return card
		}
	}
	t.Fatalf("no card %q", id)
	return core.LibraryCard{}
}

// filterCount is the count of the filter with id.
func filterCount(filters []core.LibraryFilter, id string) int {
	for _, filter := range filters {
		if filter.ID == id {
			return filter.Count
		}
	}
	return -1
}
