// Tests of BeltEditorWarning, the belt editor's note.

package core

import "testing"

func TestAWarnedEditSaysSavingWillPauseTheBelt(t *testing.T) {
	t.Parallel()
	note := BeltEditorWarningNote([]string{"Add a stage."}, nil, true, false)
	if note == nil || *note != "Saving will pause this belt until its warnings are resolved." {
		t.Errorf("note = %v, want the saving-pauses line", note)
	}
}

func TestACleanEditHasNoNoteEvenWhenTheSavedVersionIsWarned(t *testing.T) {
	t.Parallel()
	if note := BeltEditorWarningNote(nil, []string{"Add a stage."}, true, false); note != nil {
		t.Errorf("note = %q, want none", *note)
	}
}

func TestAPausedSavedBeltWithWarningsExplainsResume(t *testing.T) {
	t.Parallel()
	note := BeltEditorWarningNote(nil, []string{"Add a stage."}, false, true)
	if note == nil || *note != "This belt is paused. Resolve its warnings, save, then resume it." {
		t.Errorf("note = %v, want the paused line", note)
	}

}

func TestACleanRunningBeltHasNoNote(t *testing.T) {
	t.Parallel()
	if note := BeltEditorWarningNote(nil, nil, false, false); note != nil {
		t.Errorf("note = %q, want none", *note)
	}
}
