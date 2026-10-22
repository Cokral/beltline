// Tests of model permissions and catalog metadata: they survive a restart, a failed write keeps the
// previous policy, and context permissions survive a restart independently. A read during discovery
// reports loading and then the completed resource, and permission edits persist and only admitted
// choices appear. Restarted coordinators are h.Restart.

package coordinator

import (
	"context"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// coordSnapshot is a catalog snapshot of runner at executable, refreshed at at, with models.
func coordSnapshot(t *testing.T, runner core.Backend, executable string, at time.Time, models []core.ModelChoice) core.ModelCatalogSnapshot {
	t.Helper()
	snapshot, err := core.NewModelCatalogSnapshot(runner, executable, at, models)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snapshot
}

// sortedAllowed returns the allowed identifiers of a runner, sorted (nil when there are none).
func sortedAllowed(settings store.Settings, runner core.Backend) []string {
	ids := slices.Sorted(maps.Keys(settings.AllowedModels[string(runner)]))
	return ids
}

// coordIDs returns the ids of the choices, in order.
func coordIDs(choices []core.ModelChoice) []string {
	var ids []string
	for _, choice := range choices {
		ids = append(ids, choice.ID)
	}
	return ids
}

// TestPermissionsAndCatalogMetadataSurviveRestart: a saved catalog and an allowed model survive a restart; a new
// catalog without the model leaves the permission but no choice; the permission can then be removed.
func TestPermissionsAndCatalogMetadataSurviveRestart(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) {
		s.AllowedModels = map[string]map[string]struct{}{}
		s.ModelCatalogs = map[string]core.ModelCatalogSnapshot{}
	})
	c.EndTurn()
	snapshot := coordSnapshot(t, core.BackendPi, core.BackendPi.Executable(func(string) string { return "" }),
		time.Unix(100, 0), []core.ModelChoice{{ID: "vendor/model", Name: "Display"}})
	if err := c.SaveModelCatalog(snapshot); err != nil {
		t.Fatalf("save catalog: %v", err)
	}
	if err := c.SetModelAllowed("vendor/model", core.BackendPi, true); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if got := coordIDs(c.AllowedModels(core.BackendPi, "")); !reflect.DeepEqual(got, []string{"vendor/model"}) {
		t.Fatalf("allowed = %v", got)
	}
	restarted := h.Restart()
	// Dates compare by instant; the loaded value may carry another location.
	if got := restarted.ReadSettings().ModelCatalogs[string(core.BackendPi)]; !got.RefreshedAt.Equal(snapshot.RefreshedAt.Time) ||
		!reflect.DeepEqual(got.Models, snapshot.Models) || got.Runner != snapshot.Runner || got.Executable != snapshot.Executable {
		t.Fatalf("restarted catalog = %+v, want %+v", got, snapshot)
	}
	if got := sortedAllowed(restarted.ReadSettings(), core.BackendPi); !reflect.DeepEqual(got, []string{"vendor/model"}) {
		t.Fatalf("restarted permissions = %v", got)
	}
	names := restarted.AllowedModels(core.BackendPi, "")
	if len(names) != 1 || names[0].Name != "Display" {
		t.Fatalf("restarted choices = %+v, want Display", names)
	}
	empty, err := core.NewModelCatalogSnapshot(core.BackendPi, core.BackendPi.Executable(func(string) string { return "" }), time.Now(), nil)
	if err != nil {
		t.Fatalf("empty snapshot: %v", err)
	}
	if err := restarted.SaveModelCatalog(empty); err != nil {
		t.Fatalf("save empty catalog: %v", err)
	}
	if got := restarted.AllowedModels(core.BackendPi, ""); len(got) != 0 {
		t.Fatalf("choices with an empty catalog = %+v, want none", got)
	}
	if got := sortedAllowed(restarted.ReadSettings(), core.BackendPi); !reflect.DeepEqual(got, []string{"vendor/model"}) {
		t.Fatalf("an empty catalog removed the permission: %v", got)
	}
	if err := restarted.SetModelAllowed("vendor/model", core.BackendPi, false); err != nil {
		t.Fatalf("disallow: %v", err)
	}
	if got := sortedAllowed(restarted.ReadSettings(), core.BackendPi); len(got) != 0 {
		t.Fatalf("permissions after disallow = %v, want none", got)
	}
}

// TestFailedWriteKeepsPreviousEffectivePolicy: when the settings file cannot be written, the permission change is
// refused and the settings and the effective policy stay as they were.
func TestFailedWriteKeepsPreviousEffectivePolicy(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) {
		s.AllowedModels = map[string]map[string]struct{}{}
		s.ModelCatalogs = map[string]core.ModelCatalogSnapshot{}
	})
	c.EndTurn()
	if err := c.SaveModelCatalog(coordSnapshot(t, core.BackendPi, core.BackendPi.Executable(func(string) string { return "" }),
		time.Now(), []core.ModelChoice{{ID: "model", Name: "Model"}})); err != nil {
		t.Fatalf("save catalog: %v", err)
	}
	before := c.ReadSettings()
	settingsPath := h.storage.Paths.Settings()
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("remove settings: %v", err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatalf("make settings a folder: %v", err)
	}
	if err := c.SetModelAllowed("model", core.BackendPi, true); err == nil {
		t.Fatalf("a permission written to a folder was accepted")
	}
	if got := c.ReadSettings(); !reflect.DeepEqual(got, before) {
		t.Fatalf("a failed write changed the settings")
	}
	if got := c.AllowedModels(core.BackendPi, ""); len(got) != 0 {
		t.Fatalf("a failed write granted %v", coordIDs(got))
	}
}

// TestContextPermissionsSurviveRestartIndependently: the same model at two context sizes is two choices; allowing
// the small one survives a restart, the large one stays out, the search by size works, and disallowing clears it
// while the catalog keeps both.
func TestContextPermissionsSurviveRestartIndependently(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) { s.AllowedModels = map[string]map[string]struct{}{} })
	c.EndTurn()
	small := core.ModelChoice{ID: "claude-opus-5-5", Name: "Opus 5.5", ContextTokens: new(250_000)}
	large := core.ModelChoice{ID: "claude-opus-5-5[1m]", Name: "Opus 5.5", ContextTokens: new(1_000_000)}
	claudeExecutable := core.BackendClaude.Executable(func(string) string { return "" })
	if err := c.SaveModelCatalog(coordSnapshot(t, core.BackendClaude, claudeExecutable, time.Now(), []core.ModelChoice{small, large})); err != nil {
		t.Fatalf("save catalog: %v", err)
	}
	if err := c.SetModelAllowed(small.ID, core.BackendClaude, true); err != nil {
		t.Fatalf("allow small: %v", err)
	}
	restarted := h.Restart()
	if got := coordIDs(restarted.AllowedModels(core.BackendClaude, "")); !reflect.DeepEqual(got, []string{small.ID}) {
		t.Fatalf("allowed after restart = %v", got)
	}
	if got := restarted.AllowedModels(core.BackendClaude, ""); len(got) != 1 || got[0].ContextTokens == nil || *got[0].ContextTokens != 250_000 {
		t.Fatalf("allowed context sizes = %+v, want 250000", got)
	}
	if got := coordIDs(restarted.AllowedModels(core.BackendClaude, "250k")); !reflect.DeepEqual(got, []string{small.ID}) {
		t.Fatalf("search 250k = %v", got)
	}
	if got := restarted.AllowedModels(core.BackendClaude, "1M"); len(got) != 0 {
		t.Fatalf("search 1M = %v, want none", coordIDs(got))
	}
	if slices.Contains(sortedAllowed(restarted.ReadSettings(), core.BackendClaude), large.ID) {
		t.Fatalf("the large context was allowed")
	}
	if err := restarted.SetModelAllowed(small.ID, core.BackendClaude, false); err != nil {
		t.Fatalf("disallow small: %v", err)
	}
	cleared := h.Restart()
	if got := sortedAllowed(cleared.ReadSettings(), core.BackendClaude); len(got) != 0 {
		t.Fatalf("permissions after disallow = %v", got)
	}
	if got := cleared.AllowedModels(core.BackendClaude, ""); len(got) != 0 {
		t.Fatalf("choices after disallow = %v", coordIDs(got))
	}
	catalog := cleared.ReadSettings().ModelCatalogs[string(core.BackendClaude)]
	if !reflect.DeepEqual(coordIDs(catalog.Models), []string{small.ID, large.ID}) {
		t.Fatalf("the catalog lost a model: %v", coordIDs(catalog.Models))
	}
}

// TestReadingDuringDiscoveryReportsLoadingAndReturnsTheCompletedResource: while discovery runs the runner reads as
// loading, and the refresh returns the completed resource with its models.
func TestReadingDuringDiscoveryReportsLoadingAndReturnsTheCompletedResource(t *testing.T) {
	t.Parallel()
	discovery := newH5mControlledDiscovery()
	h := newHarness(t, harnessOptions{})
	c := h.withServices(t, func(s *Services) { s.ModelDiscovery = discovery })
	done := make(chan ModelRunnerResource, 1)
	go func() { done <- c.RefreshModelCatalog(context.Background(), core.BackendPi) }()
	call := discovery.next(t)
	if !c.ReadModelRunner(core.BackendPi, "").Loading {
		t.Fatalf("the runner does not read as loading during discovery")
	}
	discovery.answer(call.number, coordAnswer{models: []core.ModelChoice{{ID: "slow", Name: "Slow"}}})
	resource := <-done
	if resource.Loading {
		t.Fatalf("the refresh returned a loading resource")
	}
	if resource.Catalog == nil || !reflect.DeepEqual(coordIDs(resource.Catalog.Models), []string{"slow"}) {
		t.Fatalf("the completed catalog = %+v, want slow", resource.Catalog)
	}
}

// TestPermissionEditsPersistAndOnlyAdmittedChoicesAppear: a permission for a model the catalog lacks is reported
// as unavailable, allowing a listed model shows it, and the edits are saved.
func TestPermissionEditsPersistAndOnlyAdmittedChoicesAppear(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) {
		s.ModelCatalogs[string(core.BackendPi)] = coordSnapshot(t, core.BackendPi, core.BackendPi.Executable(func(string) string { return "" }),
			h.clock.Now(), []core.ModelChoice{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}})
		s.AllowedModels[string(core.BackendPi)] = map[string]struct{}{"one": {}, "gone": {}}
	})
	c.EndTurn()
	resource := c.ReadModelRunner(core.BackendPi, "")
	if got := coordIDs(resource.Choices); !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatalf("choices = %v, want [one]", got)
	}
	if !reflect.DeepEqual(resource.UnavailableModels, []string{"gone"}) {
		t.Fatalf("unavailable = %v, want [gone]", resource.UnavailableModels)
	}
	if err := c.SetModelAllowed("two", core.BackendPi, true); err != nil {
		t.Fatalf("allow two: %v", err)
	}
	if got := coordIDs(c.ReadModelRunner(core.BackendPi, "").Choices); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("choices after allowing two = %v", got)
	}
	if got := sortedAllowed(h.storage.LoadSettings(), core.BackendPi); !reflect.DeepEqual(got, []string{"gone", "one", "two"}) {
		t.Fatalf("saved permissions = %v, want [gone one two]", got)
	}
}
