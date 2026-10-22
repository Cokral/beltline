// Checks the three event messages against their goldens (event-hello, event-changed, event-
// notification): the bytes a client receives, without a socket.

package server

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

func TestEventMessagesEncodeAsTheGoldens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		message EventMessage
	}{
		{"event-hello.json", NewHelloMessage(1)},
		{"event-changed.json", NewChangedMessage(EventResourceRuns, 1)},
		{"event-notification.json", NewNotificationMessage("Needs attention", "Check the build")},
	}
	for _, tc := range cases {
		got, err := jsonx.Marshal(tc.message)
		if err != nil {
			t.Fatalf("%s: encode: %v", tc.name, err)
		}
		if want := readEventGolden(t, tc.name); string(got) != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, want)
		}
	}
}

// TestEventResourcesSortInTheirWireOrder checks Less against the raw strings:
// settings sorts before state, and runs sorts after library.
func TestEventResourcesSortInTheirWireOrder(t *testing.T) {
	t.Parallel()
	if !EventResourceSettings.Less(EventResourceState) || EventResourceState.Less(EventResourceSettings) {
		t.Fatalf("settings and state are out of byte order")
	}
	if EventResourceRuns.Less(EventResourceLibrary) || !EventResourceLibrary.Less(EventResourceRuns) {
		t.Fatalf("library and runs are out of byte order")
	}
}
