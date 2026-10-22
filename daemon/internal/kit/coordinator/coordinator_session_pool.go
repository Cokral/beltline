package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// pooledSettings returns the settings a fresh session at stage starts with, from
// its pool: the ones this run had there before, else the ones fewest of the
// sessions in the stage's slots use now, the same sessions Runs at once counts.
// Nil when the run allows none of them.
// Lock: runs with c.mu held.
func (s *Sessions) pooledSettings(run core.Run, stage core.Stage, leg string) *core.SessionSettings {
	pool := stage.Pool()
	if len(pool) <= 1 {
		if len(pool) == 0 {
			return nil
		}
		first := pool[0]
		return &first
	}
	admission := core.ValueOr(run.ModelAdmission, core.ModelAdmission{Models: map[string][]core.ModelChoice{}})
	var running []core.SessionSettings
	for _, other := range s.book.runs {
		if other.BeltID != run.BeltID {
			continue
		}
		for _, working := range other.WorkingLegs() {
			if working.StageID == nil || *working.StageID != stage.ID || !core.EngineOccupiesSlot(other, working) {
				continue
			}
			if other.ID == run.ID && working.ID == leg {
				continue
			}
			if settings := lastOpenSettings(other, stage.ID, working.ID); settings != nil {
				running = append(running, *settings)
			}
		}
	}
	var previous *core.SessionSettings
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		attempt := run.Attempts[index]
		if attempt.StageID == stage.ID && attempt.Settings != nil {
			settings := *attempt.Settings
			previous = &settings
			break
		}
	}
	return core.ModelPoolPick(pool, running, previous, func(settings core.SessionSettings) bool {
		backend := core.ValueOr(settings.Runner, core.BackendPi)
		return admission.Allows(settings.Model, backend)
	}, &s.random)
}

// lastOpenSettings is the settings of the last open attempt of a leg at stage, or nil.
// Lock: none (pure).
func lastOpenSettings(run core.Run, stageID, leg string) *core.SessionSettings {
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		attempt := run.Attempts[index]
		if attempt.StageID != stageID || !attempt.IsOpen() || core.AttemptKey(attempt.StageID, attempt.Leg) != core.AttemptKey(stageID, &leg) {
			continue
		}
		if attempt.Settings == nil {
			return nil
		}
		settings := *attempt.Settings
		return &settings
	}
	return nil
}
