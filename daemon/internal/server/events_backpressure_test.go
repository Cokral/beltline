// Tests of event backpressure that need no socket: the stalled write that Stop must not wait for, the
// stalled write whose deadline ends the session, and the queue that drops a client which never reads.
// The socket versions are in events_integration_test.go behind the integration tag.

package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// eventWait bounds how long a test waits for a signal that should come at once.
// It is a failure guard, not a sleep: the wait ends as soon as the signal arrives.
const eventWait = 10 * time.Second

// fakeEventSocket is a routes.EventSocket whose writes succeed at once and whose
// reads block until their context is cancelled, as a silent connected peer does.
// A write that a test holds (ShouldStall in the configuration) never reaches it.
type fakeEventSocket struct {
	mu      sync.Mutex
	readCtx context.Context
}

// WriteText records nothing: the tests look at the session's end, not its bytes.
func (s *fakeEventSocket) WriteText(ctx context.Context, message string) error {
	return ctx.Err()
}

// Read blocks until ctx is done.
func (s *fakeEventSocket) Read(ctx context.Context) error {
	s.mu.Lock()
	s.readCtx = ctx
	s.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

// readContext is the context the session's read ran with, nil before it ran.
func (s *fakeEventSocket) readContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readCtx
}

// stallSignal holds notification writes: ShouldStall reports the first one on
// started and holds every notification.
type stallSignal struct {
	started chan struct{}
}

func newStallSignal() *stallSignal {
	return &stallSignal{started: make(chan struct{}, 1)}
}

func (s *stallSignal) shouldStall(message string) bool {
	if !strings.Contains(message, `"type":"notification"`) {
		return false
	}
	select {
	case s.started <- struct{}{}:
	default:
	}
	return true
}

// serveFake runs EventRoutesServe for a fresh client of hub over sock, returning
// the channel that closes when it returns.
func serveFake(t *testing.T, hub *EventHub, sock *fakeEventSocket, cfg routes.EventWriteConfiguration) <-chan struct{} {
	t.Helper()
	id, client, ok := hub.Connect()
	if !ok {
		t.Fatalf("connect refused")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		routes.EventRoutesServe(context.Background(), sock, hub, client, id, cfg)
	}()
	return done
}

// requireClosed waits for done, failing the test when it does not close in time.
// The wait is a failure guard: it ends as soon as the session returns.
func requireClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(eventWait):
		t.Fatalf("%s did not finish", what)
	}
}

// requireSignal waits for ch, failing the test when it does not fire in time.
func requireSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(eventWait):
		t.Fatalf("%s did not happen", what)
	}
}

// The in-process form of the test of the same name (its socket form is in
// events_integration_test.go): the session stops without waiting for the write.
func TestStopEndsAStalledSessionWithoutWaitingForItsWrite(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	hub := NewEventHub(c)
	sock := &fakeEventSocket{}
	stall := newStallSignal()
	// The stalled write would take an hour; Stop must end it without waiting.
	cfg := routes.EventWriteConfiguration{Deadline: time.Hour, StallDuration: time.Hour, ShouldStall: stall.shouldStall}
	done := serveFake(t, hub, sock, cfg)

	hub.PublishNotification("stall", "hold this outbound write")
	requireSignal(t, stall.started, "the stalled write starts")
	if stalled := hub.DebugState(); stalled.Clients != 1 || stalled.Writers != 1 || stalled.Writes != 1 {
		t.Fatalf("stalled = %+v", stalled)
	}
	if observed := hub.DebugState().Observations; observed != len(AllEventResources) {
		t.Fatalf("observations = %d", observed)
	}

	hub.Stop()
	requireClosed(t, done, "the stopped client's session")
	if read := sock.readContext(); read == nil || read.Err() == nil {
		t.Fatalf("stop did not cancel the session's socket")
	}
	if after := hub.DebugState(); after.Clients != 0 || after.Writers != 0 || after.Writes != 0 {
		t.Fatalf("after stop = %+v", after)
	}
}

// The in-process form of the test of the same name: the deadline ends the
// session and cancels its socket context.
func TestWriteDeadlineEndsAStalledSession(t *testing.T) {
	t.Parallel()
	hub := NewEventHub(nil)
	sock := &fakeEventSocket{}
	stall := newStallSignal()
	cfg := routes.EventWriteConfiguration{Deadline: 20 * time.Millisecond, StallDuration: time.Hour, ShouldStall: stall.shouldStall}
	done := serveFake(t, hub, sock, cfg)

	hub.PublishNotification("deadline", "hold until deadline")
	// The deadline ends the held write; the client is dropped, and the session's
	// read is cancelled, so the caller closes the socket when the session returns.
	requireClosed(t, done, "the session after the write deadline")
	if read := sock.readContext(); read == nil || read.Err() == nil {
		t.Fatalf("the write deadline did not close the socket")
	}
	if after := hub.DebugState(); after.Clients != 0 || after.Writers != 0 || after.Writes != 0 {
		t.Fatalf("after deadline = %+v", after)
	}
}

func TestUnreadClientIsDroppedAtCapacityWhileHealthyClientKeepsDraining(t *testing.T) {
	t.Parallel()
	hub := NewEventHub(nil)
	unreadID, unread, ok := hub.connect()
	if !ok {
		t.Fatalf("unread connect refused")
	}
	healthyID, healthy, ok := hub.connect()
	if !ok {
		t.Fatalf("healthy connect refused")
	}
	defer hub.Disconnect(healthyID)
	defer hub.Disconnect(unreadID)
	takeEventMessage(t, healthy) // hello

	body := make([]byte, eventMaximumMessageBytes/4-128)
	for i := range body {
		body[i] = 'x'
	}
	for i := 0; i < 80; i++ {
		hub.PublishNotification("bulk", string(body))
		// The healthy client drains as a socket writer would; the unread one never does.
		if data, ok := healthy.TakePendingForTesting(); !ok || !strings.Contains(string(data), `"type":"notification"`) {
			t.Fatalf("healthy client message %d = %q, %v", i, data, ok)
		}
	}
	if state := hub.DebugState(); state.Clients != 1 {
		t.Fatalf("clients = %d, want only the healthy one", state.Clients)
	}
	if data, ok := unread.Next(); ok {
		t.Fatalf("unread client still delivers %d bytes", len(data))
	}
}
