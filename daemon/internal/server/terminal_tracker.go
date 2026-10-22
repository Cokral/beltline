// Ends the attached terminal relays when the listener shuts down. The upgraded connections are not in
// http.Server's set, so they are tracked here.

package server

import (
	"context"
	"maps"
	"slices"
	"sync"
)

// terminalTracker counts the terminal handlers running on one listener and
// cancels their contexts when the listener shuts down. Each handler's context
// is derived from its request, so cancelling it closes the socket and ends the relay.
type terminalTracker struct {
	mu      sync.Mutex
	closed  bool
	nextID  uint64
	cancels map[uint64]context.CancelFunc
	active  sync.WaitGroup
}

// newTerminalTracker makes a tracker that accepts handlers until closeAll.
func newTerminalTracker() *terminalTracker {
	return &terminalTracker{cancels: map[uint64]context.CancelFunc{}}
}

// begin registers one terminal handler whose context is parent's, and cancelled
// when the tracker closes. done must be called when the handler returns. ok is
// false once the tracker is closed, and then the handler must not run.
func (t *terminalTracker) begin(parent context.Context) (ctx context.Context, done func(), ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	id := t.nextID
	t.nextID++
	t.cancels[id] = cancel
	t.active.Add(1)
	var once sync.Once
	done = func() {
		once.Do(func() {
			t.mu.Lock()
			delete(t.cancels, id)
			t.mu.Unlock()
			cancel()
			t.active.Done()
		})
	}
	return ctx, done, true
}

// closeAll refuses new handlers and cancels the contexts of the running ones.
func (t *terminalTracker) closeAll() {
	t.mu.Lock()
	t.closed = true
	cancels := slices.Collect(maps.Values(t.cancels))
	t.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// wait blocks until every handler that began has returned.
func (t *terminalTracker) wait() {
	t.active.Wait()
}
