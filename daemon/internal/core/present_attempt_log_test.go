package core

import (
	"strings"
	"testing"
)

// TestSessionLogTextJoinsTheOriginalTranscriptAndTheContinuation pins what the Sessions sidebar's log
// shows for each saved transcript, and the copy for a session with none: still starting, never saved,
// or simulated in demo mode.
func TestSessionLogTextJoinsTheOriginalTranscriptAndTheContinuation(t *testing.T) {
	t.Parallel()
	one, two := "one", "two"
	cases := []struct {
		name    string
		session *string
		cont    *string
		open    bool
		want    string
	}{
		{"both", &one, &two, false, "one\n─────\ntwo"},
		{"original", &one, nil, false, "one"},
		{"continuation", nil, &two, false, "two"},
		{"neither, closed", nil, nil, false, "No log was saved for this session."},
		{"neither, open", nil, nil, true, "Connecting to the session…"},
	}
	for _, tc := range cases {
		if got := SessionLogText(tc.session, tc.cont, tc.open, false); got != tc.want {
			t.Errorf("%s: log = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := SessionLogText(nil, nil, true, true); !strings.HasPrefix(got, "Demo mode:") {
		t.Errorf("simulated log = %q, want the demo copy", got)
	}
}
