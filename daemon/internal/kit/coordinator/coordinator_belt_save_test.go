// Coordinator tests of saving, pausing and archiving a belt, which each kick a tick.

package coordinator

import (
	"errors"
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestSaveArchivePauseAndAfkChangeTheSavedRecord: each command changes the saved record it names.
func TestSaveArchivePauseAndAfkChangeTheSavedRecord(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	draft := beltRecordOf(t, c, "command").Belt
	draft.Name = "Saved once"
	saved, err := c.SaveBeltCommand(draft)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Belt.Version != 2 || saved.Belt.Name != "Saved once" {
		t.Errorf("saved version %d named %q", saved.Belt.Version, saved.Belt.Name)
	}

	archived, err := c.ArchiveBeltCommand("command", true)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.Archived {
		t.Error("the belt is not archived")
	}
	if restored, err := c.ArchiveBeltCommand("command", false); err != nil || restored.Archived {
		t.Errorf("unarchive: archived %v, err %v", restored.Archived, err)
	}

	if _, err := c.PauseBeltCommand("command", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !beltRecordOf(t, c, "command").Paused {
		t.Error("the belt is not paused")
	}
	if _, err := c.PauseBeltCommand("command", false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if beltRecordOf(t, c, "command").Paused {
		t.Error("the belt is still paused")
	}

	afk, err := c.AfkBeltCommand("command", core.AfkModeAlways)
	if err != nil {
		t.Fatalf("afk: %v", err)
	}
	if afk.Belt.Afk != core.AfkModeAlways {
		t.Errorf("afk record = %v", afk.Belt.Afk)
	}
	if got := beltRecordOf(t, c, "command").Belt.Afk; got != core.AfkModeAlways {
		t.Errorf("saved afk = %v", got)
	}
}

// TestAWarnedSavePausesAndAResumeIsRefused: a save with warnings is written and paused, and resuming it is refused
// until its warnings are gone.
func TestAWarnedSavePausesAndAResumeIsRefused(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	warned := beltRecordOf(t, c, "command").Belt
	warned.Stages = []core.Stage{}
	saved, err := c.SaveBeltCommand(warned)
	if err != nil {
		t.Fatalf("save warned: %v", err)
	}
	if !saved.Paused {
		t.Error("a warned save is not paused")
	}
	if !slices.Contains(c.Warnings(saved.Belt), "Add at least one stage.") {
		t.Errorf("warnings = %v", c.Warnings(saved.Belt))
	}
	if v := beltRecordOf(t, c, "command").Belt.Version; v != 2 {
		t.Errorf("saved version = %d, want 2", v)
	}
	_, err = c.PauseBeltCommand("command", false)
	var failure core.ValidationFailure
	if !errors.As(err, &failure) {
		t.Errorf("resume error = %v, want a validation failure", err)
	}
	if !beltRecordOf(t, c, "command").Paused {
		t.Error("the refused resume unpaused the belt")
	}
}

// TestTheContextNotesAWarnedBeltAndResumeStaysExplicitOnceItIsClean: the editor's notes follow the warnings of the
// draft and of the saved version, and resume is offered only once the saved belt is clean.
func TestTheContextNotesAWarnedBeltAndResumeStaysExplicitOnceItIsClean(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	clean := beltRecordOf(t, c, "command").Belt
	warned := clean.Clone()
	warned.Stages = []core.Stage{}
	whileEditing := core.BeltEditorWarningSavePauses
	if got := c.BeltContext(warned, nil).Notes; got == nil || !notesEqual(got, EditorNotes{WhileEditing: &whileEditing}) {
		t.Errorf("notes for a warned draft = %+v", got)
	}

	saved, err := c.SaveBeltCommand(warned)
	if err != nil {
		t.Fatalf("save warned: %v", err)
	}
	if !saved.Paused {
		t.Fatal("a warned save is not paused")
	}
	pausedNote := core.BeltEditorWarningPausedNote
	savedContext := c.BeltContext(saved.Belt, nil)
	if savedContext.Notes == nil || savedContext.Notes.WhenSaved == nil || *savedContext.Notes.WhenSaved != pausedNote {
		t.Errorf("the saved warned belt's notes = %+v", savedContext.Notes)
	}
	if actionByID(t, savedContext.Actions, "resume").Enabled {
		t.Error("resume is offered while the saved belt has warnings")
	}

	fixed := saved.Belt.Clone()
	fixed.Stages = clean.Stages
	resaved, err := c.SaveBeltCommand(fixed)
	if err != nil {
		t.Fatalf("save fix: %v", err)
	}
	// Saving the fix leaves the belt paused; resuming it is its own command.
	if !resaved.Paused {
		t.Error("saving the fix unpaused the belt")
	}
	fixedContext := c.BeltContext(resaved.Belt, nil)
	if fixedContext.Notes != nil && (fixedContext.Notes.WhileEditing != nil || fixedContext.Notes.WhenSaved != nil) {
		t.Errorf("notes after the fix = %+v", fixedContext.Notes)
	}
	if !actionByID(t, fixedContext.Actions, "resume").Enabled {
		t.Error("resume is not offered once the saved belt is clean")
	}
	if _, err := c.PauseBeltCommand("command", false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if beltRecordOf(t, c, "command").Paused {
		t.Error("the belt is still paused after resume")
	}
}

// notesEqual compares two editor notes by value.
func notesEqual(a *EditorNotes, b EditorNotes) bool {
	return core.SamePointee(a.WhileEditing, b.WhileEditing) && core.SamePointee(a.WhenSaved, b.WhenSaved)
}
