// The stage adjustment: the
// models a stage may use now, and the labels of what an adjustment changed.

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// AdjustStage saves only this stage's backend, model, effort and brief on the run
// . runner nil keeps the stage's backend. It returns nil
// when nothing changed. No engine event and no session kick.
// Lock: exported entry point; takes c.mu. Suspends: none.
func (c *Coordinator) AdjustStage(id, stageID string, model *string, thinking *core.Thinking, brief *string, runner *core.Backend) (*core.Adjustment, error) {
	c.Lock()
	defer c.EndTurn()
	return c.adjustStage(id, stageID, model, thinking, brief, runner)
}

// currentModelAdmission is the allowed models now, read without the lock.
// Lock: runs with c.mu held.
func (c *Coordinator) currentModelAdmission() core.ModelAdmission {
	return c.settings.ModelAdmission(c.env)
}

// adjustmentModels is AdjustmentModels with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) adjustmentModels(run core.Run, stage core.Stage, runner *core.Backend) []core.ModelChoice {
	admission := run.ModelAdmission
	if admission == nil || !admission.Valid() {
		return []core.ModelChoice{}
	}
	backend := core.ValueOr(runner, core.StageAdjustBackend(stage))
	if _, ok := admission.LegacyRepairs[stage.ID]; ok {
		choices := c.settings.AllowedChoices(backend, "", c.env)
		if choices == nil {
			return []core.ModelChoice{}
		}
		return choices
	}
	return admission.Choices(backend)
}

// legacyRepairPending reports whether a model admission marks stage as a pre-feature blank stage that may extend it once.
func legacyRepairPending(admission *core.ModelAdmission, stage string) bool {
	if admission == nil {
		return false
	}
	_, ok := admission.LegacyRepairs[stage]
	return ok
}
