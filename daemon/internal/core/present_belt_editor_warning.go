package core

// Copy used by BeltEditorWarning. Literal text is exact.
const (
	// BeltEditorWarningSavePauses is shown when a warned draft will be saved paused.
	BeltEditorWarningSavePauses = "Saving will pause this belt until its warnings are resolved."
	// BeltEditorWarningPausedNote is shown on a paused belt with saved warnings.
	BeltEditorWarningPausedNote = "This belt is paused. Resolve its warnings, save, then resume it."
)

// BeltEditorWarningNote is the note for an editor, or nil. editing is true for an unsaved
// draft, or a saved belt with changes; draftWarnings are the warnings for what the
// editor shows, savedWarnings those of the saved version.
func BeltEditorWarningNote(draftWarnings, savedWarnings []string, editing, paused bool) *string {
	if len(draftWarnings) > 0 && editing {
		return new(BeltEditorWarningSavePauses)
	}
	if !editing && paused && len(savedWarnings) > 0 {
		return new(BeltEditorWarningPausedNote)
	}
	return nil
}
