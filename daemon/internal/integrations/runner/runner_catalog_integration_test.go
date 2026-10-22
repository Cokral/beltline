//go:build integration

// Tests of the model catalog transports against real processes: the catalog protocols against the
// captured fixture server, and a CLI that exits at once. Run with -tags integration.

package runner

import (
	"context"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

func TestOfficialServerCatalogProtocolsSubmitNoWork(t *testing.T) {
	t.Parallel()
	executable := fixturePath(t, "catalog-server.py")
	for _, runner := range []core.Backend{core.BackendCopilot, core.BackendClaude, core.BackendCodex} {
		runner := runner
		t.Run(string(runner), func(t *testing.T) {
			t.Parallel()
			models, err := ServerModelCatalog{}.Discover(context.Background(), runner, executable)
			if err != nil {
				t.Fatal(err)
			}
			if len(models) == 0 || models[0].ID != string(runner)+"-exact" {
				t.Fatalf("models = %+v", models)
			}
			want := 1
			if runner == core.BackendCodex {
				want = 2
			}
			if len(models) != want {
				t.Fatalf("model count = %d, want %d", len(models), want)
			}
		})
	}
}

func TestACatalogCLIThatExitsAtOnceFailsWithoutEndingTheApp(t *testing.T) {
	t.Parallel()
	for _, runner := range []core.Backend{core.BackendCopilot, core.BackendClaude, core.BackendCodex} {
		runner := runner
		t.Run(string(runner), func(t *testing.T) {
			t.Parallel()
			if _, err := (ServerModelCatalog{}).Discover(context.Background(), runner, "true"); err == nil {
				t.Fatal("a CLI that exits at once produced a catalog")
			}
		})
	}
}
