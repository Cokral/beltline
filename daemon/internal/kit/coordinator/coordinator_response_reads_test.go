// Tests of the one-turn reads of the belts and agents responses: each returns what the separate reads
// return, taken together.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// TestResponseReadsMatchTheSeparateReads checks the belts and agents reads against the reads
// the responses used to make one at a time.
func TestResponseReadsMatchTheSeparateReads(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	c := h.Coord()

	belts := c.ReadBeltsReads("", "all")
	records := c.ReadBelts()
	if !reflect.DeepEqual(belts.Records, records) {
		t.Fatalf("belt records differ from ReadBelts")
	}
	if len(belts.Snapshots) != len(records) {
		t.Fatalf("%d snapshots for %d belts", len(belts.Snapshots), len(records))
	}
	for i, record := range records {
		snapshot := c.ReadBeltSnapshot(record)
		if !reflect.DeepEqual(belts.Snapshots[i], snapshot) {
			t.Fatalf("snapshot %d differs from ReadBeltSnapshot: %+v vs %+v", i, belts.Snapshots[i], snapshot)
		}
		context := c.BeltResourceContext(record, false)
		if !reflect.DeepEqual(snapshot.Context, context) {
			t.Fatalf("belt %s context differs from BeltResourceContext", record.Belt.ID)
		}
		if want := c.ConvertingLegacyKeys(record.Belt); !reflect.DeepEqual(snapshot.Draft, want) {
			t.Fatalf("belt %s draft differs from ConvertingLegacyKeys", record.Belt.ID)
		}
		if want := c.ReadSettings().Afk.Enabled; snapshot.GlobalAfkOn != want {
			t.Fatalf("belt %s global AFK %v, want %v", record.Belt.ID, snapshot.GlobalAfkOn, want)
		}
	}
	if want := c.ReadBeltBrowser("", "all"); !reflect.DeepEqual(belts.Browser, want) {
		t.Fatalf("belt browser differs from ReadBeltBrowser")
	}

	agents := c.ReadAgentsReads("", "all")
	agentRecords := c.ReadAgents()
	if !reflect.DeepEqual(agents.Records, agentRecords) {
		t.Fatalf("agent records differ from ReadAgents")
	}
	for _, record := range agentRecords {
		want := len(c.ReadAgentResource(record).Presentation.UsedBy)
		if agents.BeltCounts[record.Agent.ID] != want {
			t.Fatalf("agent %s belt count %d, want %d", record.Agent.ID, agents.BeltCounts[record.Agent.ID], want)
		}
	}
	if want := c.ReadAgentBrowser("", "all"); !reflect.DeepEqual(agents.Browser, want) {
		t.Fatalf("agent browser differs from ReadAgentBrowser")
	}
}

// libraryCardIDs is the ids of a library browser's cards, in order.
func libraryCardIDs(browser core.LibraryBrowser) []string {
	ids := make([]string, 0, len(browser.Items))
	for _, card := range browser.Items {
		ids = append(ids, card.ID)
	}
	return ids
}

// TestLibraryReadsShowEditsMadeOnDiskWithoutATick: the configurator writes belts and agents straight into the
// data folder. The next library reads show the new versions, the new items and the removals, with no tick and
// no reconcile call in between, and with no conversation ever started.
func TestLibraryReadsShowEditsMadeOnDiskWithoutATick(t *testing.T) {
	t.Parallel()
	storage, conversations := assistantSetup(t)
	root := storage.Paths.Root
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})
	if n := len(c.ReadBeltsReads("", "all").Records); n != 0 {
		t.Fatalf("a fresh library has %d belts", n)
	}

	coordWrite(t, root, "belts/tasks/v1.json", encodeFor(t, assistantBelt(t, 1)))
	belts := c.ReadBeltsReads("", "all")
	if len(belts.Records) != 1 || belts.Records[0].Belt.Version != 1 || belts.Records[0].Belt.Name != "Tasks" {
		t.Fatalf("a new belt on disk is not in the next read: %+v", belts.Records)
	}
	if ids := libraryCardIDs(belts.Browser); len(ids) != 1 || ids[0] != "tasks" {
		t.Fatalf("the belt browser lists %v, want [tasks]", ids)
	}

	revised := assistantBelt(t, 2)
	revised.Name = "Tasks, revised"
	coordWrite(t, root, "belts/tasks/v2.json", encodeFor(t, revised))
	belts = c.ReadBeltsReads("", "all")
	if len(belts.Records) != 1 || belts.Records[0].Belt.Version != 2 || belts.Records[0].Belt.Name != "Tasks, revised" {
		t.Fatalf("a newer belt version on disk is not in the next read: %+v", belts.Records)
	}
	if browser := belts.Browser.Items; len(browser) != 1 || browser[0].Version != 2 || browser[0].Name != "Tasks, revised" {
		t.Fatalf("the belt browser shows %+v, want version 2 with the new name", browser)
	}
	if got := c.ReadBelt("tasks"); got == nil || got.Belt.Version != 2 {
		t.Fatalf("ReadBelt does not show version 2: %+v", got)
	}

	revisedHelper := assistantHelper(2)
	revisedHelper.Name = "Helper, revised"
	coordWrite(t, root, "agents/helper/v2.md", []byte(core.AgentFileWrite(revisedHelper)))
	detail, found := c.ReadAgentDetail("helper")
	if !found {
		t.Fatalf("the helper agent is missing from the detail read")
	}
	if got := detail.Resource.Agent(); got.Version != 2 || got.Name != "Helper, revised" {
		t.Fatalf("the detail shows version %d %q, want version 2 with the new name", got.Version, got.Name)
	}
	if got := c.ReadAgentsReads("", "all").Records; !hasAgentVersion(got, "helper", 2) {
		t.Fatalf("the agent list does not show helper version 2")
	}

	fresh := assistantHelper(1)
	fresh.ID = "fresh"
	fresh.Key = "fresh"
	fresh.Name = "Fresh helper"
	coordWrite(t, root, "agents/fresh/v1.md", []byte(core.AgentFileWrite(fresh)))
	agents := c.ReadAgentsReads("", "all")
	if !hasAgentVersion(agents.Records, "fresh", 1) {
		t.Fatalf("a new agent on disk is not in the agent list")
	}
	if ids := libraryCardIDs(agents.Browser); !slices.Contains(ids, "fresh") {
		t.Fatalf("the agent browser lists %v, without fresh", ids)
	}
	if _, found := c.ReadAgentDetail("fresh"); !found {
		t.Fatalf("a new agent on disk has no detail")
	}

	if err := os.RemoveAll(filepath.Join(root, "belts", "tasks")); err != nil {
		t.Fatalf("remove belt: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "agents", "fresh")); err != nil {
		t.Fatalf("remove agent: %v", err)
	}
	if n := len(c.ReadBeltsReads("", "all").Records); n != 0 {
		t.Fatalf("a removed belt is still listed: %d belts", n)
	}
	if c.ReadBelt("tasks") != nil {
		t.Fatalf("ReadBelt still returns the removed belt")
	}
	if agents := c.ReadAgentsReads("", "all"); hasAgentVersion(agents.Records, "fresh", 1) || slices.Contains(libraryCardIDs(agents.Browser), "fresh") {
		t.Fatalf("a removed agent is still listed")
	}
	if _, found := c.ReadAgentDetail("fresh"); found {
		t.Fatalf("a removed agent still has a detail")
	}
}

// TestLibraryReadsShowEditsAfterTheConversationStops: a conversation that was started and stopped leaves the
// library reads current with the folder, as a conversation that never ran does.
func TestLibraryReadsShowEditsAfterTheConversationStops(t *testing.T) {
	t.Parallel()
	storage, conversations := assistantSetup(t)
	root := storage.Paths.Root
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})
	if err := c.StartAssistant(context.Background(), "Change the helper"); err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	stopAssistant(c)

	revised := assistantHelper(2)
	revised.Name = "Helper, after the stop"
	coordWrite(t, root, "agents/helper/v2.md", []byte(core.AgentFileWrite(revised)))
	detail, found := c.ReadAgentDetail("helper")
	if !found || detail.Resource.Agent().Version != 2 || detail.Resource.Agent().Name != "Helper, after the stop" {
		t.Fatalf("after the conversation stopped, the detail is %+v", detail.Resource.Agent())
	}
	if !hasAgentVersion(c.ReadAgentsReads("", "all").Records, "helper", 2) {
		t.Fatalf("after the conversation stopped, the agent list does not show helper version 2")
	}
}

// TestAWriteOfTheLibraryDoesNotHideAnEditMadeOnDisk: an edit made on disk before the coordinator's own library
// write is loaded by that write, so the write does not record it as already seen.
func TestAWriteOfTheLibraryDoesNotHideAnEditMadeOnDisk(t *testing.T) {
	t.Parallel()
	storage, conversations := assistantSetup(t)
	if _, err := storage.SaveNewVersion(assistantBelt(t, 1)); err != nil {
		t.Fatalf("save belt: %v", err)
	}
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})
	revised := assistantHelper(2)
	revised.Name = "Helper, edited on disk"
	coordWrite(t, storage.Paths.Root, "agents/helper/v2.md", []byte(core.AgentFileWrite(revised)))

	c.SetBeltArchived("tasks", true)

	if detail, found := c.ReadAgentDetail("helper"); !found || detail.Resource.Agent().Version != 2 {
		t.Fatalf("the library write hid the edit made on disk: %+v", detail.Resource.Agent())
	}
}

// hasAgentVersion reports whether records holds the agent id at version.
func hasAgentVersion(records []core.AgentRecord, id string, version int) bool {
	return slices.ContainsFunc(records, func(record core.AgentRecord) bool {
		return record.Agent.ID == id && record.Agent.Version == version
	})
}

// TestBeltsReadsListTheLiveBeltsInListOrder checks that the belts response names the belts
// that are not archived, in the order the library lists them.
func TestBeltsReadsListTheLiveBeltsInListOrder(t *testing.T) {
	t.Parallel()
	reads := BeltsReads{Records: []store.BeltRecord{
		{Belt: core.Belt{ID: "first"}},
		{Belt: core.Belt{ID: "archived"}, Archived: true},
		{Belt: core.Belt{ID: "second"}, Paused: true},
	}}
	if got, want := reads.LiveIDs(), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live ids = %v, want %v", got, want)
	}
	if got := (BeltsReads{}).LiveIDs(); got == nil || len(got) != 0 {
		t.Fatalf("no belts gave %v, want an empty list", got)
	}
}

// TestBeltsReadContextsFollowTheLibraryAndSettings checks the belts read's memoised contexts against
// fresh ones across the writes they depend on: a belt paused, an agent its stages pin archived, and
// the allowed models changed.
func TestBeltsReadContextsFollowTheLibraryAndSettings(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("one", "One", "acme/app"), liveTestBelt("two", "Two", "acme/web"))
	c := h.Coord()
	check := func(when string) {
		t.Helper()
		reads := c.ReadBeltsReads("", "all")
		for i, record := range reads.Records {
			if want := c.BeltResourceContext(record, false); !reflect.DeepEqual(reads.Snapshots[i].Context, want) {
				t.Fatalf("%s: belt %s context %+v, want %+v", when, record.Belt.ID, reads.Snapshots[i].Context, want)
			}
		}
	}
	check("first read")
	check("unchanged")
	records := c.ReadBelts()
	if len(records) == 0 {
		t.Fatal("the harness has no belts")
	}
	if _, err := c.PauseBeltCommand(records[0].Belt.ID, true); err != nil {
		t.Fatal(err)
	}
	check("belt paused")
	archived := false
	for _, stage := range records[0].Belt.Stages {
		if stage.Agent != nil {
			c.SetAgentArchived(stage.Agent.ID, true)
			archived = true
			break
		}
	}
	if !archived {
		t.Fatal("the belt pins no agent")
	}
	check("agent archived")
	_ = c.SetModelAllowed("sonnet", core.BackendClaude, true) // a model the catalog lacks still changes the settings or is refused; either way the read must follow
	check("models changed")
}
