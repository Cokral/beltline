package core

import "testing"

// TestEventResumeReplaysTheLatestSession: a resume built with EventResume
// replays the stage's latest closed session.
// A literal resume leaves Earlier false, which is why every caller uses this.
func TestEventResumeReplaysTheLatestSession(t *testing.T) {
	t.Parallel()
	note := "go on"
	event := EventResume(nil, &note, nil)
	if event.Kind != EventKindResume || !event.Earlier || event.Note == nil || *event.Note != note {
		t.Fatalf("EventResume = %+v, want a resume that replays the latest session", event)
	}
	literal := Event{Kind: EventKindResume}
	if literal.Earlier {
		t.Fatal("a literal resume has Earlier set")
	}
}
