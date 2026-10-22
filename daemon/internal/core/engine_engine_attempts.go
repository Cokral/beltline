// The attempts: making one, its
// folder, and closing the open one.

package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// engineNewAttempt is the next attempt on leg at stage (nil: the leg's stage, or
// the run's), numbered after the leg's others at that stage, with the leg's note.
func engineNewAttempt(run Run, leg *string, stage *string, now time.Time, folder string) Attempt {
	var stageID string
	leggedStage := engineLegStage(run, leg)
	switch {
	case stage != nil:
		stageID = *stage
	case leggedStage != nil:
		stageID = *leggedStage
	default:
		stageID = run.StageID
	}
	key := AttemptKey(stageID, leg)
	number := 1
	for _, attempt := range run.Attempts {
		if AttemptKey(attempt.StageID, attempt.Leg) == key {
			number++
		}
	}
	// The leg's note wins when the leg has one; otherwise the run's.
	note := CopyPointer(run.Note)
	if l := run.LegByID(leg); l != nil && l.Note != nil {
		note = CopyPointer(l.Note)
	}
	attempt := NewAttempt(AttemptArgs{
		StageID: stageID, Number: number, StartedAt: jsonx.Date{Time: now}, Note: note, Folder: folder, Leg: leg,
	})
	return attempt
}

// engineLegStage is the stage the leg is at, or nil when the leg is not in the
// run or has no stage. It is `run.leg(legId)?.stageId`.
func engineLegStage(run Run, leg *string) *string {
	l := run.LegByID(leg)
	if l == nil || l.StageID == nil {
		return nil
	}
	return CopyPointer(l.StageID)
}

// EngineAttemptFolder is the folder the next attempt at stage uses, relative to
// the run's folder, for the leg that is at stage (the focus when none is).
func EngineAttemptFolder(run Run, stage Stage) string {
	var leg *string
	if l := run.LegAt(&stage.ID); l != nil {
		id := l.ID
		leg = &id
	}
	return EngineAttemptFolderLeg(run, stage, leg)
}

// EngineAttemptFolderLeg is the folder the next attempt at stage uses, relative
// to the run's folder. A leg after a fork gets its own, named after it, so two
// legs at one stage never share a folder.
func EngineAttemptFolderLeg(run Run, stage Stage, leg *string) string {
	legID := LegFromID(leg)
	index := 1
	for i := range run.Stages {
		if run.Stages[i].ID == stage.ID {
			index = i + 1
			break
		}
	}
	key := AttemptKey(stage.ID, legID)
	number := 1
	for _, attempt := range run.Attempts {
		if AttemptKey(attempt.StageID, attempt.Leg) == key {
			number++
		}
	}
	name := fmt.Sprintf("%02d-%s", index, stage.ID)
	if legID != nil {
		return name + "-" + strings.ReplaceAll(*legID, "r:", "") + "-" + strconv.Itoa(number)
	}
	return name + "-" + strconv.Itoa(number)
}

// engineCloseAttempt closes the leg's open attempt at now and applies change to
// it. Nothing happens when the leg has no attempt.
func engineCloseAttempt(run *Run, leg *string, now time.Time, change func(attempt *Attempt)) {
	stageID := run.StageID
	if stage := engineLegStage(*run, leg); stage != nil {
		stageID = *stage
	}
	key := AttemptKey(stageID, leg)
	index := -1
	for i := range run.Attempts {
		if AttemptKey(run.Attempts[i].StageID, run.Attempts[i].Leg) == key {
			index = i
		}
	}
	if index < 0 {
		return
	}
	if run.Attempts[index].EndedAt == nil {
		ended := jsonx.Date{Time: now}
		run.Attempts[index].EndedAt = &ended
	}
	change(&run.Attempts[index])
}
