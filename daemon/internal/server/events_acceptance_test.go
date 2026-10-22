// Acceptance tests of the event stream: the hub's messages for the coordinator's transitions, the
// failed model refresh, and the unchanged idle tick. The coordinator half (which resources each row
// changes) is in internal/kit/coordinator. The read-after-hello test needs a socket and an HTTP
// listener, so it is in the integration tests.

package server

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// transitionRow is one row of the named transition matrix: a change made
// through the coordinator, and the resources its turn must announce.
type transitionRow struct {
	name     string
	expected []string
	change   func(t *testing.T, c *coordinator.Coordinator)
}

func TestNamedResourceTransitionMatrixInvalidatesEveryAffectedKey(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	// The fixture is empty: the demo's runs are erased and all but one running
	// belt are archived.
	for _, run := range c.Runs() {
		c.Drop(run.ID, nil)
		if err := c.Erase(context.Background(), run.ID); err != nil {
			t.Fatalf("erase %s: %v", run.ID, err)
		}
	}
	// The matrix's belt is the demo's first belt that is running; the others are archived.
	beltID := ""
	for _, record := range c.Belts() {
		if beltID == "" && !record.Paused && !record.Archived {
			beltID = record.Belt.ID
			continue
		}
		c.SetBeltArchived(record.Belt.ID, true)
	}
	if beltID == "" {
		t.Fatalf("the fixture has no running belt")
	}
	mustSaveModelCatalog(t, c, "fixture-model")
	hub := NewEventHub(c)
	id, client, ok := hub.connect()
	if !ok {
		t.Fatalf("connect refused")
	}
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)
	if hub.DebugState().Observations != len(AllEventResources) {
		t.Fatalf("observations = %d", hub.DebugState().Observations)
	}

	rows := []transitionRow{
		{"saved settings", []string{"settings", "state"}, func(t *testing.T, c *coordinator.Coordinator) {
			mustSetLogin(t, c, "event matrix")
		}},
		{"global pause", []string{"settings", "state"}, func(t *testing.T, c *coordinator.Coordinator) {
			c.SetGloballyPaused(context.Background(), true)
		}},
		{"global resume", []string{"settings", "state"}, func(t *testing.T, c *coordinator.Coordinator) {
			c.SetGloballyPaused(context.Background(), false)
		}},
		// The demo has an imported factory, and the setup's archives already flipped its
		// modified flag, so library rows change the library alone (the contract
		// fixture has no origin at all).
		{"belt pause", []string{"library"}, func(t *testing.T, c *coordinator.Coordinator) {
			if err := c.SetPaused(beltID, true); err != nil {
				t.Fatalf("pause: %v", err)
			}
		}},
		{"belt archive", []string{"library"}, func(t *testing.T, c *coordinator.Coordinator) {
			c.SetBeltArchived(beltID, true)
		}},
		{"model disallow", []string{"models", "settings"}, func(t *testing.T, c *coordinator.Coordinator) {
			if err := c.SetModelAllowed("fixture-model", core.BackendPi, false); err != nil {
				t.Fatalf("disallow: %v", err)
			}
		}},
		{"model allow", []string{"models", "settings"}, func(t *testing.T, c *coordinator.Coordinator) {
			if err := c.SetModelAllowed("fixture-model", core.BackendPi, true); err != nil {
				t.Fatalf("allow: %v", err)
			}
		}},
	}
	for _, row := range rows {
		requireRowResources(t, c, client, row)
	}
}

// requireRowResources runs one row and checks that the messages its turn queued
// name exactly the expected resources, once each, in sorted order (the
// per-row `#expect(actual == expected.sorted())`).
func requireRowResources(t *testing.T, c *coordinator.Coordinator, client *EventClient, row transitionRow) {
	t.Helper()
	row.change(t, c)
	// Spawned work (effects, a pause's stops) lands in this row, as it does in its flush.
	c.Idle()
	var got []string
	for {
		data, ok := client.TakePendingForTesting()
		if !ok {
			break
		}
		got = append(got, resourceOfFrame(t, string(data)))
	}
	// A row's whole turn is flushed at once; a command that awaits ends a turn
	// at each await, so a row may span turns. The rows check the set of resources.
	if !reflect.DeepEqual(uniqueSorted(got), uniqueSorted(row.expected)) {
		t.Fatalf("%s: changed %v, want %v", row.name, uniqueSorted(got), uniqueSorted(row.expected))
	}
}

// uniqueSorted is the sorted, de-duplicated copy of names.
func uniqueSorted(names []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, name := range names {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// resourceOfFrame is the resource a changed frame names.
func resourceOfFrame(t *testing.T, frame string) string {
	t.Helper()
	var wire struct {
		Resource string `json:"resource"`
		Type     string `json:"type"`
	}
	if err := json.Unmarshal([]byte(frame), &wire); err != nil || wire.Type != "changed" {
		t.Fatalf("expected a changed frame, got %s", frame)
	}
	return wire.Resource
}

func TestFailedModelRefreshInvalidatesModelsWithoutChangingSavedSettings(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, failingDiscovery{})
	hub := NewEventHub(c)
	id, client, ok := hub.connect()
	if !ok {
		t.Fatalf("connect refused")
	}
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)

	resource := c.RefreshModelCatalog(context.Background(), core.BackendPi)
	if resource.Error == nil {
		t.Fatalf("refresh did not report the failure")
	}
	// The refresh starts loading and then fails, one turn each; only models changes.
	c.Idle()
	var resources []string
	for {
		data, ok := client.TakePendingForTesting()
		if !ok {
			break
		}
		resources = append(resources, resourceOfFrame(t, string(data)))
	}
	if !reflect.DeepEqual(uniqueSorted(resources), []string{"models"}) {
		t.Fatalf("refresh changed %v, want only models", resources)
	}
}
