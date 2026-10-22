package coordinator

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// attemptSettingsUnrecoverableBackend is the message for a historical attempt whose
// backend cannot be recovered.
const attemptSettingsUnrecoverableBackend = "This historical attempt's original backend cannot safely be recovered."

// CurrentModelAdmission returns the models allowed now, from the saved settings
// and their catalogs.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) CurrentModelAdmission() core.ModelAdmission {
	c.Lock()
	defer c.EndTurn()
	return c.currentModelAdmission()
}

// ModelWarnings returns the warnings for an agent's models, one per model that
// its runner does not allow or the catalog does not list.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ModelWarnings(agent core.Agent) []string {
	c.Lock()
	defer c.EndTurn()
	return c.modelWarnings(agent)
}

// modelWarnings is ModelWarnings with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) modelWarnings(agent core.Agent) []string {
	warnings := []string{}
	for _, settings := range agent.Pool() {
		backend := core.ValueOr(settings.Runner, core.BackendPi)
		if warning := c.modelWarning(settings.Model, backend, agent.Name); warning != nil {
			warnings = append(warnings, *warning)
		}
	}
	return warnings
}

// modelWarning is the warning for one model on one runner, or nil.
// Lock: runs with c.mu held.
func (c *Coordinator) modelWarning(model string, runner core.Backend, name string) *string {
	return c.settings.ModelWarning(model, runner, name, c.env)
}

// AdjustmentModels lists the models a run may use on a stage's runner, for the
// Adjust sheet.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AdjustmentModels(run core.Run, stage core.Stage, runner *core.Backend) []core.ModelChoice {
	c.Lock()
	defer c.EndTurn()
	return c.adjustmentModels(run, stage, runner)
}

// attemptSettingsBackendAdjusted reports whether the stage's backend was changed
// after the attempt, so the attempt's own backend must be kept.
// Lock: none (pure).
func attemptSettingsBackendAdjusted(run core.Run, attempt core.Attempt, stageID string) bool {
	return slices.ContainsFunc(run.Adjustments, func(adjustment core.Adjustment) bool {
		return adjustment.StageID == stageID && slices.Contains(adjustment.Fields, core.AdjustmentFieldBackend) && !adjustment.Date.Before(attempt.StartedAt.Time)
	})
}

// attemptSettingsModelAdjusted reports whether the stage's model was changed after the attempt.
// Lock: none (pure).
func attemptSettingsModelAdjusted(run core.Run, attempt core.Attempt, stageID string) bool {
	return slices.ContainsFunc(run.Adjustments, func(adjustment core.Adjustment) bool {
		return adjustment.StageID == stageID && slices.Contains(adjustment.Fields, core.AdjustmentFieldModel) && !adjustment.Date.Before(attempt.StartedAt.Time)
	})
}

// attemptSettingsHistorical returns the settings a past attempt must keep. Nil
// means the original backend cannot be recovered and must not be taken from the stage.
// Lock: none (pure).
func attemptSettingsHistorical(run core.Run, attempt core.Attempt, stage core.Stage) *core.SessionSettings {
	var fallback *core.SessionSettings
	if run.ModelAdmission != nil {
		if settings, ok := run.ModelAdmission.LegacyAttemptSettings[attempt.ID]; ok {
			fallback = &settings
		}
	}
	backendChanged := attemptSettingsBackendAdjusted(run, attempt, stage.ID)
	if attempt.Settings != nil {
		settings := *attempt.Settings
		if settings.Runner == nil {
			if fallback != nil && fallback.Runner != nil {
				runner := *fallback.Runner
				settings.Runner = &runner
			} else if !backendChanged {
				runner := core.StageAdjustBackend(stage)
				settings.Runner = &runner
			} else {
				return nil
			}
		}
		return &settings
	}
	if fallback != nil {
		settings := *fallback
		if settings.Runner == nil {
			if backendChanged {
				return nil
			}
			runner := core.StageAdjustBackend(stage)
			settings.Runner = &runner
		}
		return &settings
	}
	if backendChanged || attemptSettingsModelAdjusted(run, attempt, stage.ID) {
		return nil
	}
	runner := core.StageAdjustBackend(stage)
	return &core.SessionSettings{Model: stage.Model, Thinking: stage.Thinking, Runner: &runner}
}

// attemptSettingsFallbacks returns the fallbacks to freeze while the stage still
// holds the original backend, by stage id. Attempts that already have one are skipped.
// Lock: none (pure).
func attemptSettingsFallbacks(run core.Run) map[string]core.SessionSettings {
	existing := map[string]core.SessionSettings{}
	if run.ModelAdmission != nil {
		existing = run.ModelAdmission.LegacyAttemptSettings
	}
	additions := map[string]core.SessionSettings{}
	for _, attempt := range run.Attempts {
		if _, ok := existing[attempt.ID]; ok {
			continue
		}
		stage := findStage(run, attempt.StageID)
		if stage == nil || stage.Kind() != core.StageKindAgent {
			continue
		}
		backendChanged := attemptSettingsBackendAdjusted(run, attempt, stage.ID)
		if attempt.Settings != nil {
			if attempt.Settings.Runner != nil || backendChanged {
				continue
			}
			runner := core.StageAdjustBackend(*stage)
			additions[attempt.ID] = core.SessionSettings{Model: attempt.Settings.Model, Thinking: attempt.Settings.Thinking, Runner: &runner}
		} else if !backendChanged && !attemptSettingsModelAdjusted(run, attempt, stage.ID) && strings.TrimSpace(stage.Model) != "" {
			runner := core.StageAdjustBackend(*stage)
			additions[attempt.ID] = core.SessionSettings{Model: stage.Model, Thinking: stage.Thinking, Runner: &runner}
		}
	}
	return additions
}

// runPreserveLegacyRunners freezes missing legacy runners on run before a backend
// adjustment can replace them. Historical attempts are not rewritten.
// A method on core.Run cannot be added from this package, so it is a function.
// Lock: none (pure).
func runPreserveLegacyRunners(run *core.Run) {
	if run.ModelAdmission == nil || !run.ModelAdmission.Valid() {
		return
	}
	additions := attemptSettingsFallbacks(*run)
	if len(additions) == 0 {
		return
	}
	admission := run.ModelAdmission.Clone()
	admission.RecordLegacyAttemptSettings(additions)
	run.ModelAdmission = &admission
}

// admittedRun returns the run with id. A run saved before its admission is
// migrated first, with a durable write before any allocation; a present
// malformed snapshot is refused and never enters that path.
// Lock: runs with c.mu held. Writes: saves the migrated run.
func (s *Sessions) admittedRun(id string) (core.Run, error) {
	run := s.book.Run(id)
	if run == nil {
		return core.Run{}, core.NewFailure("Run not found")
	}
	if run.ModelAdmission != nil {
		return *run, nil
	}
	current := s.currentAdmission()
	models := map[string][]core.ModelChoice{}
	for runner, choices := range current.Clone().Models {
		models[runner] = append([]core.ModelChoice{}, choices...)
	}
	repairs := map[string]struct{}{}
	include := func(runner core.Backend, model string) {
		if strings.TrimSpace(model) == "" {
			return
		}
		for _, choice := range models[string(runner)] {
			if choice.ID == model {
				return
			}
		}
		models[string(runner)] = append(models[string(runner)], core.ModelChoice{ID: model, Name: model})
	}
	for _, stage := range run.Stages {
		if stage.Kind() != core.StageKindAgent {
			continue
		}
		runner := core.ValueOr(stage.Runner, core.BackendPi)
		include(runner, stage.Model)
		if strings.TrimSpace(stage.Model) == "" {
			repairs[stage.ID] = struct{}{}
		}
	}
	for index := range run.Attempts {
		attempt := run.Attempts[index]
		stage := findStage(*run, attempt.StageID)
		if stage == nil || stage.Kind() != core.StageKindAgent {
			continue
		}
		if attempt.Settings != nil {
			// A backend adjustment already replaced the stage, so a missing runner must not grandfather that backend.
			backendChanged := attemptSettingsBackendAdjusted(*run, attempt, stage.ID)
			if attempt.Settings.Runner != nil || !backendChanged {
				runner := core.ValueOr(attempt.Settings.Runner, core.StageAdjustBackend(*stage))
				include(runner, attempt.Settings.Model)
			}
		}
	}
	// Sorted, so the admitted choices keep one order whatever the map's.
	agents := s.library.AgentsOf(*run)
	refs := slices.Collect(maps.Keys(agents))
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID != refs[j].ID {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Version < refs[j].Version
	})
	for _, ref := range refs {
		agent := agents[ref]
		include(agent.Runner, agent.Model)
	}
	// Freeze saved-stage fallbacks, including a missing legacy runner, before a later adjustment can replace them.
	migrated := core.NewModelAdmission(models, repairs, attemptSettingsFallbacks(*run))
	if !s.book.update(id, nil, nil, func(r *core.Run) { r.ModelAdmission = &migrated }) {
		return core.Run{}, core.NewFailure("Could not persist legacy model admission. Check storage permissions before retrying.")
	}
	run.ModelAdmission = &migrated
	return *run, nil
}
