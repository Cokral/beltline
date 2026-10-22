// Coordinator tests of the model catalog, model resources and model permission storage.

package coordinator

import (
	"context"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// settingsJSON is the saved settings as the store writes them, so two reads compare as the disk would.
func settingsJSON(t *testing.T, c *Coordinator) string {
	t.Helper()
	c.Lock()
	defer c.EndTurn()
	data, err := jsonx.Marshal(c.settings)
	if err != nil {
		t.Fatalf("encode settings: %v", err)
	}
	return string(data)
}

// catalogJSON is one runner's saved catalog as JSON, or "" when it has none.
func catalogJSON(t *testing.T, c *Coordinator, runner core.Backend) string {
	t.Helper()
	c.Lock()
	defer c.EndTurn()
	catalog, ok := c.settings.ModelCatalogs[string(runner)]
	if !ok {
		return ""
	}
	data, err := jsonx.Marshal(catalog)
	if err != nil {
		t.Fatalf("encode catalog: %v", err)
	}
	return string(data)
}

func TestReadingProjectsTheCatalogWithoutDiscoveryOrWrites(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	var calls atomic.Int32
	c := h.withServices(t, func(services *Services) {
		services.ModelDiscovery = fakeDiscovery{answer: func(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
			calls.Add(1)
			return nil, core.NewFailure("discovery must not run")
		}}
	})
	before := settingsJSON(t, c)
	resource := c.ReadModelRunner(core.BackendPi, "")
	if resource.Runner != core.BackendPi || resource.Executable != core.BackendPi.Executable(os.Getenv) {
		t.Fatalf("resource runner %v executable %q", resource.Runner, resource.Executable)
	}
	if !resource.Usable || resource.Catalog == nil || resource.Catalog.Runner != core.BackendPi {
		t.Fatalf("resource is not the saved usable catalog: usable %v, catalog %+v", resource.Usable, resource.Catalog)
	}
	if resource.Loading || resource.Error != nil {
		t.Fatalf("loading %v, error %v; want neither", resource.Loading, resource.Error)
	}
	if !sortedStrings(resource.AllowedModels) {
		t.Fatalf("allowed models are not sorted: %v", resource.AllowedModels)
	}
	if !sortedChoices(resource.Choices) {
		t.Fatalf("choices are not sorted by id")
	}
	if len(resource.UnavailableModels) != 0 {
		t.Fatalf("unavailable models = %v, want none", resource.UnavailableModels)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("a read started discovery %d times", n)
	}
	if after := settingsJSON(t, c); after != before {
		t.Fatalf("a read wrote settings")
	}
}

// TestExecutableMismatchMakesTheCatalogUnusableButStaleIdsStayRemovable checks that a mismatched executable makes the catalog unusable, while stale ids stay removable.
func TestExecutableMismatchMakesTheCatalogUnusableButStaleIdsStayRemovable(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.withServices(t, func(services *Services) { services.ModelDiscovery = failingDiscovery() })
	c.Lock()
	c.changeSettings(func(s *store.Settings) {
		snapshot, err := core.NewModelCatalogSnapshot(core.BackendPi, "somewhere-else", h.clock.Now(),
			[]core.ModelChoice{{ID: "only-model", Name: "Only"}})
		if err != nil {
			t.Errorf("snapshot: %v", err)
		}
		s.ModelCatalogs[string(core.BackendPi)] = snapshot
	})
	c.EndTurn()
	resource := c.ReadModelRunner(core.BackendPi, "")
	if resource.Usable || resource.Catalog == nil {
		t.Fatalf("usable %v, catalog %v; want an unusable catalog that is still reported", resource.Usable, resource.Catalog)
	}
	if len(resource.Choices) != 0 {
		t.Fatalf("an unusable catalog authorized %d choices", len(resource.Choices))
	}
	if !reflect.DeepEqual(resource.UnavailableModels, resource.AllowedModels) {
		t.Fatalf("unavailable %v, want every allowed model %v", resource.UnavailableModels, resource.AllowedModels)
	}
	if err := c.SetModelAllowed("only-model", core.BackendPi, true); err == nil {
		t.Fatalf("a model from an unusable catalog was allowed")
	}
}

// TestRefreshPersistsCompleteSuccessAndKeepsTheOldCatalogOnFailure checks that a complete refresh persists, and a failed one keeps the old catalog.
func TestRefreshPersistsCompleteSuccessAndKeepsTheOldCatalogOnFailure(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.withServices(t, func(services *Services) {
		services.ModelDiscovery = fakeDiscovery{answer: func(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
			return nil, core.NewFailure("the CLI is not signed in")
		}}
	})
	before := catalogJSON(t, c, core.BackendPi)
	resource := c.RefreshModelCatalog(context.Background(), core.BackendPi)
	if resource.Error == nil || *resource.Error != "the CLI is not signed in" {
		t.Fatalf("error = %v, want the discovery failure", resource.Error)
	}
	if resource.Loading {
		t.Fatalf("a finished refresh still reports loading")
	}
	if resource.Catalog == nil || catalogJSON(t, c, core.BackendPi) != before {
		t.Fatalf("a failed refresh changed the kept catalog")
	}
}

// fixedDiscovery answers the discoveries of one test in order: each entry is one call's answer.
type fixedDiscovery struct {
	answers []func() ([]core.ModelChoice, error)
	calls   atomic.Int32
}

// Discover answers the next scripted discovery.
func (d *fixedDiscovery) Discover(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
	n := int(d.calls.Add(1)) - 1
	return d.answers[n]()
}

func TestFailureAndMalformedResultsKeepLastSuccessfulCatalog(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	discovery := &fixedDiscovery{answers: []func() ([]core.ModelChoice, error){
		func() ([]core.ModelChoice, error) { return nil, core.NewFailure("Catalog unavailable. Retry.") },
		func() ([]core.ModelChoice, error) { return []core.ModelChoice{{ID: "", Name: "Broken"}}, nil },
	}}
	service := newModelCatalogs(discovery, 30*time.Second)
	before := catalogJSON(t, c, core.BackendPi)
	ctx := context.Background()

	c.Lock()
	service.Refresh(ctx, core.BackendPi, c, false)
	c.EndTurn()
	if catalogJSON(t, c, core.BackendPi) != before {
		t.Fatalf("a failed discovery changed the saved catalog")
	}
	if service.errors[core.BackendPi] == "" {
		t.Fatalf("the failure was not kept")
	}

	c.Lock()
	service.Refresh(ctx, core.BackendPi, c, true)
	c.EndTurn()
	if catalogJSON(t, c, core.BackendPi) != before {
		t.Fatalf("a malformed catalog changed the saved catalog")
	}
	if !strings.Contains(service.errors[core.BackendPi], "malformed") {
		t.Fatalf("error = %q, want a malformed catalog", service.errors[core.BackendPi])
	}
}

func TestTimeoutFinishesAndLateSuccessCannotOverwriteCache(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	release := make(chan struct{})
	returned := make(chan struct{})
	discovery := fakeDiscovery{answer: func(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
		<-release
		defer close(returned)
		return []core.ModelChoice{{ID: "late", Name: "Late"}}, nil
	}}
	service := newModelCatalogs(discovery, 20*time.Millisecond)
	before := catalogJSON(t, c, core.BackendPi)

	c.Lock()
	service.Refresh(context.Background(), core.BackendPi, c, false)
	c.EndTurn()
	if len(service.loading) != 0 {
		t.Fatalf("a timed-out discovery still loads")
	}
	if !strings.Contains(service.errors[core.BackendPi], "timed out") {
		t.Fatalf("error = %q, want a timeout", service.errors[core.BackendPi])
	}
	close(release)
	<-returned
	if catalogJSON(t, c, core.BackendPi) != before {
		t.Fatalf("a late success overwrote the saved catalog")
	}
}

func TestMissingCatalogAndWrongExecutableCannotGrantPermission(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	c.Lock()
	c.changeSettings(func(s *store.Settings) {
		s.AllowedModels = map[string]map[string]struct{}{}
		s.ModelCatalogs = map[string]core.ModelCatalogSnapshot{}
	})
	c.EndTurn()
	if err := c.SetModelAllowed("model", core.BackendPi, true); err == nil {
		t.Fatalf("a model with no catalog was allowed")
	}
	if err := c.SetModelAllowed("", core.BackendPi, true); err == nil {
		t.Fatalf("an empty identifier was allowed")
	}
	snapshot, err := core.NewModelCatalogSnapshot(core.BackendPi, "wrong", h.clock.Now(), []core.ModelChoice{{ID: "model", Name: "Model"}})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := c.SaveModelCatalog(snapshot); err == nil {
		t.Fatalf("a catalog for the wrong executable was saved")
	}
	c.Lock()
	defer c.EndTurn()
	if len(c.settings.AllowedModels) != 0 || len(c.settings.ModelCatalogs) != 0 {
		t.Fatalf("a refused change left settings behind: %+v / %+v", c.settings.AllowedModels, c.settings.ModelCatalogs)
	}
}

// sortedStrings reports whether the list is in ascending order.
func sortedStrings(list []string) bool {
	for i := 1; i < len(list); i++ {
		if list[i-1] > list[i] {
			return false
		}
	}
	return true
}

// sortedChoices reports whether the choices are in ascending id order.
func sortedChoices(list []core.ModelChoice) bool {
	for i := 1; i < len(list); i++ {
		if list[i-1].ID > list[i].ID {
			return false
		}
	}
	return true
}
