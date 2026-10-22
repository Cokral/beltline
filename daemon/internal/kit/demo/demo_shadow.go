// The demo's shadow test: the issues belt tries Build on another model, and runs
// since the test started are split between the two versions.

package demo

import (
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// demoShadow is the running shadow test and the shadow version its runs start on.
type demoShadow struct {
	test core.ShadowTest
	belt core.Belt
}

// demoShadowArms are the seeded runs of the issues belt that started during the test, with the
// version each took.
var demoShadowArms = map[string]core.ShadowArm{
	"issues#139": core.ShadowArmLive, "issues#142": core.ShadowArmShadow, "issues#145": core.ShadowArmShadow,
	"issues#147": core.ShadowArmLive, "issues#151": core.ShadowArmLive, "issues#152": core.ShadowArmLive,
}

// seedShadow starts a shadow test of record's belt two weeks ago: Build on Pi instead of Copilot, and
// blockers no longer waited for. Nil when the demo has no such belt.
func seedShadow(storage store.Storage, record store.BeltRecord, now time.Time) (*demoShadow, error) {
	if record.Belt.ID == "" {
		return nil, nil
	}
	draft := record.Belt.Clone()
	for i := range draft.Stages {
		if draft.Stages[i].ID == "build" {
			draft.Stages[i].Runner = new(core.BackendPi)
			draft.Stages[i].Model = "openrouter/stealth/space-bunny-alpha"
		}
	}
	draft.Policies.WaitForBlockers = false
	record.ShadowTests = []core.ShadowTest{{ID: "t1", Live: record.Belt.Version, StartedAt: jsonx.Date{Time: now.AddDate(0, 0, -14)}}}
	saved, record, err := storage.SaveShadowVersion(draft, record)
	if err != nil {
		return nil, err
	}
	return &demoShadow{test: *record.ShadowTest(), belt: core.ShadowDefinition(saved, record.Belt)}, nil
}
