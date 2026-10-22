// Tests of model discovery in the demo: with the fixture discovery, every runner's catalog refreshes
// without an error, lists the fixture models, and allowing its first model shows two choices.

package coordinator

import (
	"context"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// TestDemoDiscoveryAndPermissionEditsPersist: the demo catalogs refresh for every runner, carry the fixture models,
// and a permission edit on one of them persists.
func TestDemoDiscoveryAndPermissionEditsPersist(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.withServices(t, func(s *Services) { s.ModelDiscovery = runner.FixtureModelCatalog{} })
	for _, backend := range core.AllBackend {
		c.Lock()
		c.modelCatalogs.Refresh(context.Background(), backend, c, true)
		errText, failed := c.modelCatalogs.errors[backend]
		snapshot, saved := c.settings.ModelCatalogs[string(backend)]
		c.EndTurn()
		if failed {
			t.Fatalf("%s: discovery failed: %s", backend, errText)
		}
		if !saved {
			t.Fatalf("%s: no catalog was saved", backend)
		}
		if len(snapshot.Models) != len(runner.FixtureModelCatalogModels) {
			t.Fatalf("%s: models = %d, want %d", backend, len(snapshot.Models), len(runner.FixtureModelCatalogModels))
		}
		if !strings.Contains(snapshot.Models[0].Name, "demo") {
			t.Fatalf("%s: first model %q is not the demo's", backend, snapshot.Models[0].Name)
		}
		if err := c.SetModelAllowed(snapshot.Models[0].ID, backend, true); err != nil {
			t.Fatalf("%s: allow %s: %v", backend, snapshot.Models[0].ID, err)
		}
		if got := c.AllowedModels(backend, ""); len(got) != 2 {
			t.Fatalf("%s: allowed choices = %v, want two", backend, coordIDs(got))
		}
	}
	c.Lock()
	idle := len(c.modelCatalogs.loading)
	c.EndTurn()
	if idle != 0 {
		t.Fatalf("%d catalogs are still loading", idle)
	}
}
