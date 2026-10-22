// The rules a task you start on a manual trigger must pass: which entry it
// starts on, what its title and repository must be, and its number. The
// coordinator's Intake.startTask keeps the state checks (pauses) and the save.

package core

import (
	"slices"
	"strconv"
	"strings"
)

// TaskNotAcceptedMessage is the refusal for a belt that takes no tasks you start,
// or an entry id that is not one of its manual triggers.
const TaskNotAcceptedMessage = "This belt doesn't take tasks you start."

// HasManualTrigger reports whether the belt has a manual trigger, the kind a task
// you start runs on.
func (b Belt) HasManualTrigger() bool {
	return slices.ContainsFunc(b.Triggers, func(entry TriggerEntry) bool {
		return entry.Trigger.Kind == TriggerKindManual
	})
}

// ManualTrigger is the manual entry a task you start runs on: the one entryID
// names, or the only manual entry when entryID is nil. A belt with several and
// no entryID, or an entryID that is not a manual entry, is refused.
func (b Belt) ManualTrigger(entryID *string) (TriggerEntry, error) {
	var manuals []TriggerEntry
	for _, entry := range b.Triggers {
		if entry.Trigger.Kind == TriggerKindManual {
			manuals = append(manuals, entry)
		}
	}
	switch {
	case len(manuals) == 0:
		return TriggerEntry{}, NewFailure(TaskNotAcceptedMessage)
	case entryID != nil:
		for _, manual := range manuals {
			if manual.ID == *entryID {
				return manual, nil
			}
		}
		return TriggerEntry{}, NewFailure(TaskNotAcceptedMessage)
	case len(manuals) == 1:
		return manuals[0], nil
	default:
		return TriggerEntry{}, NewFailure("Pick a trigger.")
	}
}

// CheckTask checks the title and repository of a task started on this entry, and
// returns the title trimmed. The repository must be one of the entry's.
func (e TriggerEntry) CheckTask(repo, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", NewFailure("Give the task a title.")
	}
	for _, r := range e.Repos {
		if r == repo {
			return title, nil
		}
	}
	return "", NewFailure("Pick one of the belt's repositories.")
}

// NextManualTaskNumber is the key of the next task on a belt: one more than the
// highest number among its manual tasks, or 1 when it has none. The
// maximum starts from the first number, so negative keys still count.
func NextManualTaskNumber(runs []Run, beltID string) int {
	highest, found := 0, false
	for _, run := range runs {
		if run.BeltID != beltID || run.Subject.Kind != SubjectKindManual {
			continue
		}
		number, err := strconv.Atoi(run.Subject.Key)
		if err != nil {
			continue
		}
		if !found || number > highest {
			highest, found = number, true
		}
	}
	return highest + 1
}
