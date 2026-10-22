// The relay that delivers the daemon's notifications to the desktop.

package daemon

import (
	"sync"

	"github.com/robbertmanders/beltline/daemon/internal/server"
)

// NotificationObserver receives each relayed notification.
type NotificationObserver func(title, body string)

// NotificationRelay fans a coordinator notification out to the log, the observer,
// the desktop notifier and, once the listener is ready, the current event hub.
// Notifications raised before the listener connects are kept and delivered in
// order when it does.
type NotificationRelay struct {
	mu      sync.Mutex
	log     *DaemonLog
	observe NotificationObserver
	desktop NotificationObserver
	server  *server.BeltlineServer
	pending []notice
}

// notice is one queued notification.
type notice struct{ title, body string }

// newNotificationRelay makes an unconnected relay.
// Lock: none.
func newNotificationRelay() *NotificationRelay { return &NotificationRelay{} }

// SetLog sets the log notifications are written to.
// Lock: takes r.mu.
func (r *NotificationRelay) SetLog(log *DaemonLog) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = log
}

// SetObserver sets the observer hook (tests); nil for none.
// Lock: takes r.mu.
func (r *NotificationRelay) SetObserver(observe NotificationObserver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observe = observe
}

// SetDesktop sets the desktop notifier; nil for none.
// Lock: takes r.mu.
func (r *NotificationRelay) SetDesktop(desktop NotificationObserver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.desktop = desktop
}

// Connect hands the relay the listener and delivers everything pending to it.
// Lock: takes r.mu; the listener's publish runs with it held.
func (r *NotificationRelay) Connect(s *server.BeltlineServer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.server = s
	r.deliverPendingLocked()
}

// Disconnect drops the listener and anything still pending.
// Lock: takes r.mu.
func (r *NotificationRelay) Disconnect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.server = nil
	r.pending = nil
}

// Publish logs a notification, reports it to the observer and the desktop notifier,
// and sends it to the listener's clients when one is connected.
// Lock: takes r.mu. The desktop notifier runs in its own goroutine.
func (r *NotificationRelay) Publish(title, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log.Notification(title, body)
	if r.observe != nil {
		r.observe(title, body)
	}
	if r.desktop != nil {
		go r.desktop(title, body)
	}
	if r.server == nil {
		return
	}
	r.pending = append(r.pending, notice{title, body})
	r.deliverPendingLocked()
}

// deliverPendingLocked sends every queued notification to the listener, in order.
// Lock: runs with r.mu held.
func (r *NotificationRelay) deliverPendingLocked() {
	for r.server != nil && len(r.pending) > 0 {
		next := r.pending[0]
		r.pending = r.pending[1:]
		r.server.PublishNotification(next.title, next.body)
	}
}
