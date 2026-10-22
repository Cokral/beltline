// The intake's starts without a trigger: the schedules that fire on their time and the
// tasks you start yourself.

package coordinator

import (
	"strconv"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// fireSchedules starts each scheduled belt's task when its time comes: once,
// however many times went by while the app was closed. A time that passes while
// the belt is paused, or while its last task is still going, is skipped
// .
// Lock: runs with c.mu held. Spawns: none.
func (i *Intake) fireSchedules() {
	now := i.clock.Now()
	if i.globallyPaused {
		return
	}
	for _, record := range i.runBeltsSnapshot() {
		for _, entry := range record.Belt.Triggers {
			if entry.Trigger.Kind != core.TriggerKindSchedule || entry.Trigger.Schedule == nil {
				continue
			}
			task := *entry.Trigger.Schedule
			slot := task.Schedule.Latest(now, nil)
			if slot == nil {
				continue
			}
			live := i.library.Belt(record.Belt.ID)
			var last time.Time
			fired := false
			if live != nil {
				last, fired = live.Fired[entry.ID]
			}
			if !fired {
				// A new schedule counts from now, rather than firing for a time already past.
				i.claim(record.Belt.ID, entry.ID, now)
				continue
			}
			if !slot.After(last) {
				continue
			}
			// A slot that cannot be recorded is not consumed and does not start a run.
			if !i.claim(record.Belt.ID, entry.ID, *slot) {
				continue
			}
			fresh := record
			if current := i.library.Belt(record.Belt.ID); current != nil {
				fresh = *current
			}
			if fresh.EffectivelyPaused() || len(entry.Repos) == 0 {
				continue
			}
			repo := entry.Repos[0]
			subject := core.Subject{
				Kind: core.SubjectKindScheduled, Repo: repo, Key: entry.ScheduledKey(*slot, nil),
				Title: task.Title, Body: task.Task, IsOpen: true, // the subject's default
			}
			id := core.RunID(fresh.Belt.ID, subject)
			if i.book.isErasing(id) || i.book.Run(id) != nil {
				continue
			}
			going := i.scheduledGoing(fresh.Belt.ID, entry.ID)
			if going != nil {
				i.book.note(fresh.Belt.Name+" skipped its "+subject.Key+" run: "+going.Subject.Ref()+" is still on the belt", nil)
				continue
			}
			if _, err := i.join(fresh, subject, &entry); err != nil {
				i.book.note(fresh.Belt.Name+" skipped its "+subject.Key+" run: "+err.Error(), nil)
			}
		}
	}
}

// scheduledGoing returns a scheduled run on the belt that is still active and was admitted by entry, or nil.
// Lock: runs with c.mu held.
func (i *Intake) scheduledGoing(beltID, entryID string) *core.Run {
	for _, run := range i.runsSnapshot() {
		if run.BeltID != beltID || !run.State.IsActive() || run.Subject.Kind != core.SubjectKindScheduled {
			continue
		}
		if prov := i.provenance(run); prov != nil && prov.Entry == entryID {
			found := run
			return &found
		}
	}
	return nil
}

// claim records a schedule cursor. False when it could not be saved, leaving the
// previous cursor in place.
// Lock: runs with c.mu held.
func (i *Intake) claim(id, entry string, date time.Time) bool {
	if err := i.library.setFired(id, entry, date); err != nil {
		name := id
		if record := i.library.Belt(id); record != nil {
			name = record.Belt.Name
		}
		i.book.note("Could not save "+name+"'s schedule: "+err.Error(), nil)
		return false
	}
	return true
}

// startTask starts a task you wrote on a manual belt, in one of its repositories.
// Lock: runs with c.mu held.
func (i *Intake) startTask(beltID, repo, title, body string, entryID *string) (core.Run, error) {
	record := i.library.Belt(beltID)
	if record == nil {
		return core.Run{}, core.NewFailure(core.TaskNotAcceptedMessage)
	}
	chosen, err := record.Belt.ManualTrigger(entryID)
	if err != nil {
		return core.Run{}, err
	}
	if i.globallyPaused {
		return core.Run{}, core.NewFailure("Everything is paused.")
	}
	if record.EffectivelyPaused() {
		return core.Run{}, core.NewFailure("This belt is paused or archived.")
	}
	title, err = chosen.CheckTask(repo, title)
	if err != nil {
		return core.Run{}, err
	}
	subject := core.Subject{
		Kind: core.SubjectKindManual, Repo: repo, Key: strconv.Itoa(core.NextManualTaskNumber(i.book.runs, beltID)), Title: title,
		Body: strings.TrimSpace(body), IsOpen: true,
	}
	return i.join(*record, subject, &chosen)
}
