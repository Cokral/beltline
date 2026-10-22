// The card checks of the library browser: the belt and agent cards, which
// the coordinator test reads through the library.

package core

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// libraryCardTestBelt is a manual belt with the given number of agent stages, each
// pinning the triage agent.
func libraryCardTestBelt(id string, stages int) Belt {
	belt := NewBeltFromTrigger(BeltFromTriggerArgs{
		ID: id, Name: id, Trigger: TriggerKindManual.Fresh(), Repos: []string{}, Stages: []Stage{},
	})
	for i := 0; i < stages; i++ {
		belt.Stages = append(belt.Stages, Stage{ID: fmt.Sprintf("%s-%d", id, i), Type: StageTypeAgent,
			Agent: &AgentRef{ID: "triage", Version: 1}, Outcomes: []Outcome{}})
	}
	return belt
}

// libraryCardTestAgent is the triage agent the belts pin, with a tint of its own.
func libraryCardTestAgent() *Agent {
	agent := Agent{ID: "triage", Name: "Triage", Version: 2, Brief: "Read the issue.", Runner: BackendPi, Model: "", Tint: TintRose}
	return &agent
}

// libraryCardTestStatuses maps each card id to its status.
func libraryCardTestStatuses(browser LibraryBrowser) map[string]string {
	statuses := map[string]string{}
	for _, card := range browser.Items {
		statuses[card.ID] = card.Status
	}
	return statuses
}

func TestLibraryBeltCardsLetArchiveWinOverPauseAndFilterByIt(t *testing.T) {
	t.Parallel()
	agentOf := func(*AgentRef) *Agent { return libraryCardTestAgent() }
	belts := []LibraryBeltState{
		{Belt: libraryCardTestBelt("live", 1)},
		{Belt: libraryCardTestBelt("paused", 1), Paused: true},
		{Belt: libraryCardTestBelt("archived", 1), Archived: true, Paused: true},
	}
	browser := LibraryBeltBrowserOf(belts, agentOf, "", "all")

	statuses := libraryCardTestStatuses(browser)
	if statuses["live"] != "Running" || statuses["paused"] != "Paused" {
		t.Errorf("statuses = %v, want live Running and paused Paused", statuses)
	}
	if got := libraryBrowserTestIDs(browser.Items); !reflect.DeepEqual(got, []string{"live", "paused"}) {
		t.Errorf("all lists %v, want the live belts only", got)
	}
	archived := LibraryBeltBrowserOf(belts, agentOf, "", "archived")
	if got := libraryBrowserTestIDs(archived.Items); !reflect.DeepEqual(got, []string{"archived"}) {
		t.Errorf("archived lists %v, want [archived]", got)
	}
	if status := archived.Items[0].Status; status != "Archived" {
		t.Errorf("archived status = %q, want Archived", status)
	}
	if browser.Actions[0].Label != libraryBrowserActionNewBelt {
		t.Errorf("action = %q, want New belt", browser.Actions[0].Label)
	}
}

func TestLibraryBeltCardShowsAtMostSevenStageLooksWithThePinnedAgent(t *testing.T) {
	t.Parallel()
	agent := libraryCardTestAgent()
	agentOf := func(*AgentRef) *Agent { return agent }
	browser := LibraryBeltBrowserOf([]LibraryBeltState{{Belt: libraryCardTestBelt("wide", 9)}}, agentOf, "", "all")
	card := browser.Items[0]
	if len(card.Stages) != libraryBrowserStageLimit {
		t.Fatalf("stage looks = %d, want %d", len(card.Stages), libraryBrowserStageLimit)
	}
	glyph := agent.Glyph()
	for _, look := range card.Stages {
		if look.Icon != glyph || look.Tint != agent.Tint {
			t.Errorf("look = %v %v, want the pinned %v %v", look.Icon, look.Tint, glyph, agent.Tint)
		}
	}
	if card.Detail != "9 stages" {
		t.Errorf("detail = %q, want 9 stages", card.Detail)
	}
}

func TestLibraryAgentCardsReadUsageForStatusFilterAndDetail(t *testing.T) {
	t.Parallel()
	used := AgentRecord{Agent: *libraryCardTestAgent()}
	unused := AgentRecord{Agent: Agent{ID: "spare", Name: "Spare", Version: 1, Runner: BackendPi, Model: "gpt"}}
	archived := AgentRecord{Agent: Agent{ID: "old", Name: "Old", Version: 1, Runner: BackendPi}, Archived: true}
	usage := []AgentUsage{{BeltID: "b", BeltName: "Build belt", Version: 1}, {BeltID: "c", BeltName: "Other", Version: 1}}
	browser := LibraryAgentBrowserOf([]LibraryAgentState{
		{Record: used, UsedBy: usage, Glyph: StageIconWrench},
		{Record: unused, Glyph: StageIconChip},
		{Record: archived, Glyph: StageIconChip},
	}, "", "all")

	statuses := libraryCardTestStatuses(browser)
	if statuses["triage"] != "In use" || statuses["spare"] != "Unused" {
		t.Errorf("statuses = %v", statuses)
	}
	if got := libraryBrowserTestIDs(browser.Items); !reflect.DeepEqual(got, []string{"spare", "triage"}) {
		t.Errorf("all lists %v, want the live agents by name", got)
	}
	archivedBrowser := LibraryAgentBrowserOf([]LibraryAgentState{{Record: archived, Glyph: StageIconChip}}, "", "archived")
	if status := archivedBrowser.Items[0].Status; status != "Archived" {
		t.Errorf("archived status = %q, want Archived", status)
	}
	triage := browser.Items[1]
	if want := "Pi · default model · Used by 2 belts"; triage.Detail != want {
		t.Errorf("detail = %q, want %q", triage.Detail, want)
	}
	if triage.Summary != "Read the issue." {
		t.Errorf("summary = %q", triage.Summary)
	}
	if count := browser.Filters[1].Count; count != 1 {
		t.Errorf("used count = %d, want 1", count)
	}
	if browser.Actions[0].Label != agentActionsLabelNew {
		t.Errorf("action = %q, want New agent", browser.Actions[0].Label)
	}
}

func TestLibraryBrowserBriefSummaryKeepsTheFirstLineAndCutsLongBriefs(t *testing.T) {
	t.Parallel()
	if got := LibraryBrowserBriefSummary("  First line  \nSecond line"); got != "First line" {
		t.Errorf("summary = %q, want the trimmed first line", got)
	}
	long := strings.Repeat("a", 200)
	got := LibraryBrowserBriefSummary(long)
	if want := strings.Repeat("a", 177) + "…"; got != want {
		t.Errorf("long summary = %d characters, want 177 and an ellipsis", len([]rune(got)))
	}
	exact := strings.Repeat("b", 180)
	if got := LibraryBrowserBriefSummary(exact); got != exact {
		t.Errorf("a 180-character brief is cut")
	}
}
