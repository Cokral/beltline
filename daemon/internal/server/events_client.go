// One connection's queue
// of encoded messages, bounded by count and bytes, with a termination action.
package server

import "sync"

// Event client bounds (EventClient's static limits).
const (
	eventMaximumPendingMessages = 64
	eventMaximumPendingBytes    = 256 * 1024
	eventMaximumMessageBytes    = 256 * 1024
)

// EventClient is one connection's queue. A mutex guards it; Go uses
// mu. The waiter continuation becomes wake: a one-slot signal that the
// queue has a message or was closed. The in-flight frame is the one Next returns,
// so it is not counted against the pending limits.
//
// The termination action runs outside mu, and it must not block: the hub calls
// it while the coordinator's turn is in progress.
type EventClient struct {
	mu sync.Mutex
	// pending holds the queued encoded messages, oldest first.
	pending      [][]byte
	pendingBytes int
	closed       bool
	termination  func()
	wake         chan struct{}
}

// newEventClient builds the event client from its arguments.
func newEventClient() *EventClient {
	return &EventClient{wake: make(chan struct{}, 1)}
}

// signal wakes a blocked Next. It never blocks: a pending signal already covers
// the change.
func (c *EventClient) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Enqueue queues one encoded message, returning
// false (and dropping the client) when the message or the queue is over its bound
// or the client is closed.
func (c *EventClient) Enqueue(data []byte) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	if len(data) > eventMaximumMessageBytes || len(c.pending)+1 > eventMaximumPendingMessages ||
		c.pendingBytes+len(data) > eventMaximumPendingBytes {
		terminate := c.closeLocked()
		c.mu.Unlock()
		if terminate != nil {
			terminate()
		}
		return false
	}
	c.pending = append(c.pending, data)
	c.pendingBytes += len(data)
	c.signal()
	c.mu.Unlock()
	return true
}

// Next blocks for the next queued message, returning
// false once the client is closed and drained.
func (c *EventClient) Next() ([]byte, bool) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, false
		}
		if len(c.pending) > 0 {
			data := c.pending[0]
			c.pending = c.pending[1:]
			c.pendingBytes -= len(data)
			c.mu.Unlock()
			return data, true
		}
		c.mu.Unlock()
		<-c.wake
	}
}

// TakePendingForTesting is the next
// queued message without waiting, for tests.
func (c *EventClient) TakePendingForTesting() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || len(c.pending) == 0 {
		return nil, false
	}
	data := c.pending[0]
	c.pending = c.pending[1:]
	c.pendingBytes -= len(data)
	return data, true
}

// SetTermination is the action that runs when
// the client is dropped or closed by the hub or a failed write. On a client that
// is already closed it runs now.
func (c *EventClient) SetTermination(action func()) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		action()
		return
	}
	c.termination = action
	c.mu.Unlock()
}

// Close marks the client closed, wakes its writer and
// runs the termination action once.
func (c *EventClient) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	terminate := c.closeLocked()
	c.mu.Unlock()
	if terminate != nil {
		terminate()
	}
}

// closeLocked marks the client closed, drops its queue, wakes its writer and
// returns the termination action to run once mu is released (nil when none).
// Lock: runs with c.mu held.
func (c *EventClient) closeLocked() func() {
	c.closed = true
	c.pending = nil
	c.pendingBytes = 0
	c.signal()
	terminate := c.termination
	c.termination = nil
	return terminate
}
