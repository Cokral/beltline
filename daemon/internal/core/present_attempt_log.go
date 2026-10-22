package core

// attemptLogSeparator sits between the original transcript and the continuation. Copy from AttemptLog.text.
const attemptLogSeparator = "\n─────\n"

// AttemptLogText is the original agent transcript followed by the interactive
// continuation. Nil when both are missing.
func AttemptLogText(session *string, continuing *string) *string {
	switch {
	case session != nil && continuing != nil:
		text := *session + attemptLogSeparator + *continuing
		return &text
	case session != nil:
		text := *session
		return &text
	case continuing != nil:
		text := *continuing
		return &text
	}
	return nil
}

// SessionLogText is the log the Sessions sidebar shows: the saved transcript followed by its
// continuation. Without either, the copy says whether the session is still starting or never saved
// a log. A simulated runner (demo mode) says so.
func SessionLogText(session *string, continuing *string, isOpen bool, simulated bool) string {
	if text := AttemptLogText(session, continuing); text != nil {
		return *text
	}
	if !isOpen {
		return "No log was saved for this session."
	}
	if simulated {
		return "Demo mode: this agent is simulated, so there is no terminal to attach.\nIt finishes on its own in a few seconds."
	}
	return "Connecting to the session…"
}
