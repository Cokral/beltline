// Coordinator tests of the resource-change rows: each command reports exactly the resources it
// changes. Each row's command runs, then the test waits for the spawned work. A row passes when the
// resources its turns report are exactly the expected set. The rows run in order on one coordinator;
// a command that awaits ends a turn at each await, so the rows check the set of resources, not the
// number of flushes.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// changedResources is the sorted, de-duplicated set of resources the given turns reported.
func changedResources(changes [][]Resource) []Resource {
	seen := map[Resource]struct{}{}
	var out []Resource
	for _, turn := range changes {
		for _, resource := range turn {
			if _, ok := seen[resource]; !ok {
				seen[resource] = struct{}{}
				out = append(out, resource)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// coordRow runs one transition of the matrix, waits for the work it spawned, and checks the resources it
// reported against the expected set.
func coordRow(t *testing.T, h *harness, c *Coordinator, name string, expected []Resource, change func()) {
	t.Helper()
	h.ClearChanges()
	change()
	c.Idle()
	got := changedResources(h.Changes())
	want := sortedResources(expected...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: changed %v, want %v", name, got, want)
	}
}

// matrixCoordinator is the coordinator the matrix rows run on: no belts, the fixture models, and the
// marketplace, factory and model discovery fakes the rows reach.
func matrixCoordinator(t *testing.T) (*harness, *Coordinator) {
	t.Helper()
	h := newLibraryHarness(t)
	// The contract fixture has no built-in agents, so an import of one creates it (the shared folder seeds them).
	if err := os.RemoveAll(filepath.Join(h.root, "agents")); err != nil {
		t.Fatalf("remove the seeded agents: %v", err)
	}
	models := []core.ModelChoice{{ID: "fixture-model", Name: "Fixture model"}, {ID: "openrouter/stealth/space-bunny-alpha", Name: "Default fixture model"}}
	c := h.withServices(t, func(services *Services) {
		services.ModelDiscovery = fakeDiscovery{answer: func(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
			return models, nil
		}}
	})
	observe(h, c)
	return h, c
}

// observe records the resource changes c reports into h, so h.Changes sees them (the harness's own
// listener is on its own coordinator, not on one built with withServices).
func observe(h *harness, c *Coordinator) {
	c.SetChangeListener(func(resources []Resource) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.changes = append(h.changes, append([]Resource(nil), resources...))
	})
}

// TestNamedResourceTransitionMatrixRowsLeft: the matrix rows after the ones the projection test covers, in
// the rows, in order, on one coordinator.
func TestNamedResourceTransitionMatrixRowsLeft(t *testing.T) {
	t.Parallel()
	h, c := matrixCoordinator(t)
	ctx := context.Background()
	belt := c.NewBeltDraft()
	belt.Name = "Event matrix"
	saved, err := c.SaveBelt(belt)
	if err != nil {
		t.Fatalf("save belt: %v", err)
	}
	// 	// The paused flag is written to storage directly here, past the resume refusal of a belt with warnings.
	if err := h.storage.SetPaused(saved.ID, false); err != nil {
		t.Fatalf("unpause belt: %v", err)
	}
	locked(c, func() { c.library.reload(false) })
	c.Idle()
	catalog, err := core.NewModelCatalogSnapshot(core.BackendPi, core.BackendPi.Executable(c.env), testkit.ContractTime, []core.ModelChoice{
		{ID: "fixture-model", Name: "Fixture model"}, {ID: "openrouter/stealth/space-bunny-alpha", Name: "Default fixture model"},
	})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := c.SaveModelCatalog(catalog); err != nil {
		t.Fatalf("SaveModelCatalog: %v", err)
	}
	for _, id := range []string{"fixture-model", "openrouter/stealth/space-bunny-alpha"} {
		if err := c.SetModelAllowed(id, core.BackendPi, true); err != nil {
			t.Fatalf("allow %s: %v", id, err)
		}
	}
	agent := c.NewAgentDraft()
	agent.Name = "Event matrix agent"
	agentSaved, err := c.SaveAgent(agent)
	if err != nil {
		t.Fatalf("save agent: %v", err)
	}
	c.Idle()
	h.ClearChanges()

	beltID := saved.ID
	agentID := agentSaved.ID
	coordRow(t, h, c, "belt pause", []Resource{ResourceLibrary}, func() {
		if err := c.SetPaused(beltID, true); err != nil {
			t.Fatalf("pause: %v", err)
		}
	})
	coordRow(t, h, c, "belt save", []Resource{ResourceLibrary}, func() {
		changed := beltRecordOf(t, c, beltID).Belt
		changed.Name += " saved"
		if _, err := c.SaveBelt(changed); err != nil {
			t.Fatalf("save belt: %v", err)
		}
	})
	coordRow(t, h, c, "belt archive", []Resource{ResourceLibrary}, func() {
		c.SetBeltArchived(beltID, true)
	})
	coordRow(t, h, c, "belt reload after disk update", []Resource{ResourceLibrary}, func() {
		changed := beltRecordOf(t, c, beltID).Belt
		changed.Name += " reloaded"
		if _, err := h.storage.SaveNewVersion(changed); err != nil {
			t.Fatalf("save new version: %v", err)
		}
		locked(c, func() { c.library.reload(false) })
	})
	coordRow(t, h, c, "agent save", []Resource{ResourceLibrary}, func() {
		changed := c.LatestAgent(agentID).Agent
		changed.Name += " saved"
		if _, err := c.SaveAgent(changed); err != nil {
			t.Fatalf("save agent: %v", err)
		}
	})
	coordRow(t, h, c, "agent archive", []Resource{ResourceLibrary}, func() {
		c.SetAgentArchived(agentID, true)
	})
	coordRow(t, h, c, "agent reload after disk update", []Resource{ResourceLibrary}, func() {
		changed := c.LatestAgent(agentID).Agent
		changed.Name += " reloaded"
		if _, err := h.storage.SaveNewAgentVersion(changed); err != nil {
			t.Fatalf("save new agent version: %v", err)
		}
		locked(c, func() { c.library.reload(false) })
	})
	coordRow(t, h, c, "saved settings", []Resource{ResourceSettings, ResourceState}, func() {
		if _, err := c.ApplySettingsPatch(SettingsPatch{Login: new("event matrix")}); err != nil {
			t.Fatalf("settings: %v", err)
		}
	})
	coordRow(t, h, c, "global pause", []Resource{ResourceSettings, ResourceState}, func() {
		c.SetGloballyPaused(ctx, true)
	})
	coordRow(t, h, c, "global resume", []Resource{ResourceSettings, ResourceState}, func() {
		c.SetGloballyPaused(ctx, false)
	})
	// The belts saved above carry the test clock as their created time, while a reload reads the files'
	// 	// birth times. Reloading here makes the library match the disk, as the app does after each save, so
	// the assistant rows report only what they change.
	locked(c, func() { c.library.reload(false) })
	c.Idle()
	h.ClearChanges()
	coordRow(t, h, c, "assistant start", []Resource{ResourceAssistant, ResourceSettings}, func() {
		if _, err := c.ConfigureAssistant(AssistantConfiguration{Backend: core.BackendPi, Model: "fixture-model"}); err != nil {
			t.Fatalf("configure assistant: %v", err)
		}
		if err := c.StartAssistant(ctx, "Observe a test change"); err != nil {
			t.Fatalf("start assistant: %v", err)
		}
	})
	coordRow(t, h, c, "assistant problems", []Resource{ResourceAssistant}, func() {
		coordWrite(t, c.Paths().Root, "belts/invalid-event-fixture/v1.json", []byte("{"))
		locked(c, func() { c.assistant.noticeChanges(true) })
		locked(c, func() {
			if len(c.assistant.problems) == 0 {
				t.Errorf("the broken belt file is not a problem")
			}
		})
	})
	coordRow(t, h, c, "assistant stop", []Resource{ResourceAssistant}, func() {
		if err := c.StopAssistant(ctx); err != nil {
			t.Fatalf("stop assistant: %v", err)
		}
	})
	coordRow(t, h, c, "AFK review", []Resource{ResourceAfk}, func() {
		locked(c, func() {
			c.autopilot.add(core.AfkDecision{ID: "matrix-decision", Status: core.AfkDecisionStatusLeftForYou})
		})
		c.ReviewAfk(map[string]struct{}{"matrix-decision": {}})
	})
	coordRow(t, h, c, "model disallow", []Resource{ResourceModels, ResourceSettings}, func() {
		if err := c.SetModelAllowed("fixture-model", core.BackendPi, false); err != nil {
			t.Fatalf("disallow: %v", err)
		}
	})
	coordRow(t, h, c, "model allow", []Resource{ResourceModels, ResourceSettings}, func() {
		if err := c.SetModelAllowed("fixture-model", core.BackendPi, true); err != nil {
			t.Fatalf("allow: %v", err)
		}
	})
	coordRow(t, h, c, "model refresh success", []Resource{ResourceModels, ResourceSettings}, func() {
		// A refresh stamps the catalog with the time it ran; the test clock moves on as real time would.
		h.clock.Advance(time.Minute)
		c.RefreshModelCatalog(ctx, core.BackendPi)
	})
}

// TestFailedModelRefreshInvalidatesModelsWithoutChangingSavedSettings: a model refresh that fails reports the
// models and leaves the saved settings as they were.
func TestFailedModelRefreshInvalidatesModelsWithoutChangingSavedSettings(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	c := h.withServices(t, func(services *Services) { services.ModelDiscovery = failingDiscovery() })
	observe(h, c)
	before := settingsJSON(t, c)
	h.ClearChanges()
	c.RefreshModelCatalog(context.Background(), core.BackendPi)
	c.Idle()
	if got := changedResources(h.Changes()); !reflect.DeepEqual(got, []Resource{ResourceModels}) {
		t.Errorf("changed %v, want [models]", got)
	}
	c.Lock()
	failed := c.modelCatalogs.errors[core.BackendPi] != ""
	c.EndTurn()
	if !failed {
		t.Errorf("the failed refresh did not record its error")
	}
	if after := settingsJSON(t, c); after != before {
		t.Errorf("a failed refresh changed the saved settings")
	}
}

// TestUnchangedIdleTickEmitsNothing: a tick that changes nothing reports no resource.
func TestUnchangedIdleTickEmitsNothing(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	h.ClearChanges()
	h.Tick()
	if changes := h.Changes(); len(changes) != 0 {
		t.Errorf("an unchanged idle tick reported %v", changes)
	}
}

// TestAnExternalLibraryEditPublishesOnceAndUnchangedReadsPublishNothing: a belt or agent edited on disk is
// reported as a library change by the read that loads it, and reads of the unchanged library report nothing.
func TestAnExternalLibraryEditPublishesOnceAndUnchangedReadsPublishNothing(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t)
	c := h.Coord()
	observe(h, c)
	fresh := assistantHelper(1)
	fresh.ID = "fresh"
	fresh.Key = "fresh"
	fresh.Name = "Fresh helper"
	h.ClearChanges()
	coordWrite(t, h.root, "agents/fresh/v1.md", []byte(core.AgentFileWrite(fresh)))

	c.ReadAgentsReads("", "all")
	c.Idle()
	if got := changedResources(h.Changes()); !reflect.DeepEqual(got, sortedResources(ResourceLibrary)) {
		t.Fatalf("the read that loaded the edit reported %v, want [library]", got)
	}

	h.ClearChanges()
	c.ReadAgentsReads("", "all")
	c.ReadAgentDetail("fresh")
	c.ReadBeltsReads("", "all")
	c.ReadAgentBrowser("", "all")
	c.Idle()
	if got := changedResources(h.Changes()); len(got) != 0 {
		t.Fatalf("reads of the unchanged library reported %v, want nothing", got)
	}
}
