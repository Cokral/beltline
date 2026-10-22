// ModelAdmission and the
// ModelPolicy namespace.
//
// ModelAdmission stores legacyRepairs as a set, which is saved as an array.

package core

import (
	"maps"
	"slices"
	"strings"
)

// ModelAdmission is the local immutable authorization captured at instance
// creation, independent of later catalogs, permissions or executable identities.
type ModelAdmission struct {
	// SchemaVersion is 1 for every admission Beltline writes.
	SchemaVersion int `json:"schemaVersion"`
	// Models are the admitted choices, by runner raw value.
	Models map[string][]ModelChoice `json:"models"`
	// LegacyRepairs are the marked pre-feature blank stages that may extend this snapshot once.
	LegacyRepairs setOf[string] `json:"legacyRepairs"`
	// LegacyAttemptSettings are the saved-stage fallbacks for pre-feature attempts, without rewriting history.
	LegacyAttemptSettings map[string]SessionSettings `json:"legacyAttemptSettings,omitzero"`
}

// NewModelAdmission creates an admission with SchemaVersion 1.
func NewModelAdmission(models map[string][]ModelChoice, legacyRepairs map[string]struct{}, legacyAttemptSettings map[string]SessionSettings) ModelAdmission {
	return ModelAdmission{SchemaVersion: 1, Models: models, LegacyRepairs: legacyRepairs, LegacyAttemptSettings: legacyAttemptSettings}.Clone()
}

// Valid reports whether the admission is version 1, every key is a known
// runner, and every choice has an id.
func (m ModelAdmission) Valid() bool {
	if m.SchemaVersion != 1 {
		return false
	}
	for runner, choices := range m.Models {
		if !modelRunnerKnown(runner) {
			return false
		}
		for _, choice := range choices {
			if strings.TrimSpace(choice.ID) == "" {
				return false
			}
		}
	}
	return true
}

// Choices are the choices admitted for runner, or none when the admission is not valid.
func (m ModelAdmission) Choices(runner Backend) []ModelChoice {
	if !m.Valid() {
		return []ModelChoice{}
	}
	return append([]ModelChoice{}, m.Models[string(runner)]...)
}

// Allows reports whether model is admitted for runner.
func (m ModelAdmission) Allows(model string, runner Backend) bool {
	return slices.ContainsFunc(m.Choices(runner), func(choice ModelChoice) bool { return choice.ID == model })
}

// RunnerModel is the identifier a session passes for an admitted model at
// thinking, or model itself when it is not admitted.
func (m ModelAdmission) RunnerModel(model string, thinking *Thinking, runner Backend) string {
	for _, choice := range m.Choices(runner) {
		if choice.ID == model {
			return choice.Identifier(thinking)
		}
	}
	return model
}

// RecordLegacyAttemptSettings records fallbacks for attempts that did not save
// them. An id already stored is left as it was.
func (m *ModelAdmission) RecordLegacyAttemptSettings(settings map[string]SessionSettings) {
	if !m.Valid() || len(settings) == 0 {
		return
	}
	updated := make(map[string]SessionSettings, len(m.LegacyAttemptSettings)+len(settings))
	maps.Copy(updated, m.LegacyAttemptSettings)
	changed := false
	for id, value := range settings {
		if _, ok := updated[id]; !ok {
			updated[id] = value
			changed = true
		}
	}
	if changed {
		m.LegacyAttemptSettings = updated
	}
}

// Repair extends the snapshot with choice for runner, once, for the marked
// pre-feature blank stage. It reports whether it did.
func (m *ModelAdmission) Repair(stage string, runner Backend, choice ModelChoice) bool {
	if !m.Valid() {
		return false
	}
	if _, ok := m.LegacyRepairs[stage]; !ok {
		return false
	}
	delete(m.LegacyRepairs, stage)
	if !m.Allows(choice.ID, runner) {
		if m.Models == nil {
			m.Models = map[string][]ModelChoice{}
		}
		m.Models[string(runner)] = append(m.Models[string(runner)], choice)
	}
	return true
}

// ModelPolicyWarnings are one warning for each agent stage's settings, its own
// and its alternates', whose model is not allowed. label, when set, gives the
// model's catalog name for a model and runner.
func ModelPolicyWarnings(stages []Stage, admission ModelAdmission, label func(model string, runner Backend) *string) []string {
	out := []string{}
	for _, stage := range stages {
		if stage.Kind() != StageKindAgent {
			continue
		}
		out = append(out, ModelPolicyWarningsForPool(stage.Pool(), stage.Name, admission, label)...)
	}
	return out
}

// ModelPolicyWarningsForPool is one warning for each of the settings in pool,
// named name, whose model is not allowed.
func ModelPolicyWarningsForPool(pool []SessionSettings, name string, admission ModelAdmission, label func(model string, runner Backend) *string) []string {
	out := []string{}
	for _, settings := range pool {
		runner := ValueOr(settings.Runner, BackendPi)
		var shown *string
		if label != nil {
			shown = label(settings.Model, runner)
		}
		if warning := ModelPolicyWarning(settings.Model, runner, name, admission, shown); warning != nil {
			out = append(out, *warning)
		}
	}
	return out
}

// ModelPolicyWarning is the warning for one model that is not allowed, or nil
// when it is. label, when set, is shown in place of the identifier.
func ModelPolicyWarning(model string, runner Backend, name string, admission ModelAdmission, label *string) *string {
	if admission.Allows(model, runner) {
		return nil
	}
	shown := model
	if strings.TrimSpace(model) == "" {
		shown = "The runner's default"
	} else if label != nil {
		shown = *label
	}
	message := name + ": " + shown + " is not allowed for " + runner.Label() + ". Choose an allowed model or configure Settings → Allowed models."
	return &message
}

// Clone is a deep copy of the admission: its model lists, legacy repairs and
// legacy attempt settings.
func (m ModelAdmission) Clone() ModelAdmission {
	return cloneValue(m)
}
