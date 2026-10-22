// The shadow test commands: save a shadow version (starting a test), accept it, discard it.

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// SaveShadowCommand saves belt as the next shadow version of the saved belt with
// its id, starting a shadow test when none runs. What cannot be written is
// refused as a save refuses it; warnings save, and keep new runs on live until
// they are resolved.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) SaveShadowCommand(belt core.Belt) (store.BeltRecord, error) {
	c.Lock()
	defer c.EndTurn()
	record := c.library.Belt(belt.ID)
	if record == nil {
		return store.BeltRecord{}, core.NewValidationFailure("No belt with id "+belt.ID+".", nil)
	}
	command := core.BeltCommandShadowSave
	if record.ShadowTest() == nil {
		command = core.BeltCommandShadowCreate
	}
	if refusal := c.beltCommandRefusalNow(command, belt.ID, false); refusal != nil {
		return store.BeltRecord{}, core.NewValidationFailure(*refusal, nil)
	}
	if problems := core.BeltStructureRefusals(belt); len(problems) > 0 {
		return store.BeltRecord{}, core.ValidationFailureLocated(problems)
	}
	if _, err := c.library.SaveShadow(belt); err != nil {
		return store.BeltRecord{}, err
	}
	c.kick()
	return c.beltRecordNow(belt.ID)
}

// AcceptShadowCommand makes the belt's shadow its live version and ends the test.
// Runs already going keep the version they started on.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) AcceptShadowCommand(id string) (store.BeltRecord, error) {
	c.Lock()
	defer c.EndTurn()
	if refusal := c.beltCommandRefusalNow(core.BeltCommandShadowAccept, id, false); refusal != nil {
		return store.BeltRecord{}, core.NewValidationFailure(*refusal, nil)
	}
	if _, err := c.library.AcceptShadow(id); err != nil {
		return store.BeltRecord{}, err
	}
	c.intake.forgetPolls(id)
	c.kick()
	return c.beltRecordNow(id)
}

// DiscardShadowCommand ends the belt's shadow test and keeps its live version.
// Runs on the shadow keep going on it.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) DiscardShadowCommand(id string) (store.BeltRecord, error) {
	c.Lock()
	defer c.EndTurn()
	if refusal := c.beltCommandRefusalNow(core.BeltCommandShadowDiscard, id, false); refusal != nil {
		return store.BeltRecord{}, core.NewValidationFailure(*refusal, nil)
	}
	if err := c.library.DiscardShadow(id); err != nil {
		return store.BeltRecord{}, err
	}
	return c.beltRecordNow(id)
}

// beltRecordNow is the belt's record after a command, or the failure that says it is gone.
// Lock: runs with c.mu held.
func (c *Coordinator) beltRecordNow(id string) (store.BeltRecord, error) {
	record := c.library.Belt(id)
	if record == nil {
		return store.BeltRecord{}, core.NewValidationFailure("No belt with id "+id+".", nil)
	}
	return *record, nil
}
