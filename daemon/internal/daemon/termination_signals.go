// SIGINT and SIGTERM stop the daemon, and a signal that arrives before anyone waits is remembered.

package daemon

import (
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// terminationSignals records the first SIGINT or SIGTERM.
type terminationSignals struct {
	signals  chan os.Signal
	received atomic.Bool
	fired    chan struct{}
	stopOnce sync.Once
	stop     chan struct{}
}

// newTerminationSignals starts listening for SIGINT and SIGTERM.
// Lock: none.
func newTerminationSignals() *terminationSignals {
	t := &terminationSignals{
		signals: make(chan os.Signal, 2),
		fired:   make(chan struct{}),
		stop:    make(chan struct{}),
	}
	signal.Notify(t.signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-t.signals:
			t.received.Store(true)
			close(t.fired)
		case <-t.stop:
		}
	}()
	return t
}

// HasReceivedSignal reports whether a signal has arrived.
// Lock: none.
func (t *terminationSignals) HasReceivedSignal() bool { return t.received.Load() }

// Wait blocks until a signal arrives.
// Lock: none.
func (t *terminationSignals) Wait() { <-t.fired }

// Close stops listening.
// Lock: none.
func (t *terminationSignals) Close() {
	t.stopOnce.Do(func() {
		signal.Stop(t.signals)
		close(t.stop)
	})
}
