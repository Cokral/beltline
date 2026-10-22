// Covers the terminal relays a listener's shutdown must end (terminal_tracker.go) and the close frame
// a relay ends with (terminal_pty_bridge.go): a failed relay closes with 1011 "Terminal relay failed".

package server

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTerminalTrackerCancelsRunningHandlersAndRefusesNewOnes(t *testing.T) {
	t.Parallel()
	tracker := newTerminalTracker()
	ctx, done, ok := tracker.begin(context.Background())
	if !ok {
		t.Fatal("a fresh tracker refused a handler")
	}

	tracker.closeAll()
	if ctx.Err() == nil {
		t.Fatal("closeAll did not cancel the running handler's context")
	}
	if _, _, ok := tracker.begin(context.Background()); ok {
		t.Fatal("a closed tracker accepted a handler")
	}

	waited := make(chan struct{})
	go func() {
		tracker.wait()
		close(waited)
	}()
	done()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not return after the handler finished")
	}
}

func TestTerminalTrackerDoneIsIdempotent(t *testing.T) {
	t.Parallel()
	tracker := newTerminalTracker()
	_, done, _ := tracker.begin(context.Background())
	done()
	done()
	tracker.closeAll()
	tracker.wait()
}

func TestRelayCloseEndsAFailedRelayWith1011(t *testing.T) {
	t.Parallel()
	cases := []struct {
		end    relayEnd
		code   websocket.StatusCode
		reason string
	}{
		{relayNormal, websocket.StatusNormalClosure, ""},
		{relayPolicyViolation, websocket.StatusPolicyViolation, "Terminal policy violation"},
		{relayFailure, websocket.StatusInternalError, "Terminal relay failed"},
	}
	for _, c := range cases {
		code, reason := relayClose(c.end)
		if code != c.code || reason != c.reason {
			t.Errorf("relayClose(%d) = %d %q, want %d %q", c.end, code, reason, c.code, c.reason)
		}
	}
}
