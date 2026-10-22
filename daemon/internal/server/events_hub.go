// The hub that keeps the
// connected event clients, queues each one's hello, and broadcasts the changed
// and notification messages. It implements routes.EventHubPort.
//
// The hub coalesces dirty resources and flushes them in a scheduled main-actor
// task after the turn. In Go the coordinator's change listener runs at the end of
// each turn with that turn's changed resources already sorted and de-duplicated
// (Turns.compare). The hub records them and schedules one flush on the coordinator
// (Turns.Spawn), which runs after the turn that scheduled it and sends everything
// recorded by then: changes made in turns before that flush join it.
// The hub's own lock never waits for the coordinator's, so the listener can call it.
package server

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// EventHub is the event hub. Its state is guarded by mu (its
// isolation). mu is never held while the coordinator's lock is taken.
type EventHub struct {
	mu sync.Mutex

	coordinator        *coordinator.Coordinator
	clients            map[string]*EventClient
	revisions          uint64
	stopped            bool
	activeWriterTasks  int
	activeSocketWrites int
	observation        *CoordinatorObservation
	// flushScheduler runs a flush after the current turn.
	// nil flushes at once, as a hub without a coordinator has no turns to wait for.
	flushScheduler func(flush func())
	// dirty holds the resources changed since the last flush, not yet sent.
	dirty map[EventResource]struct{}
	// flushScheduled is set while a flush is scheduled and not yet run.
	flushScheduled bool
}

// NewEventHub is a hub for c (nil in tests), which
// starts observing the coordinator's resources when c is set. Its flushes run as
// work spawned on c, after the turn that changed the resources.
func NewEventHub(c *coordinator.Coordinator) *EventHub {
	var schedule func(flush func())
	if c != nil {
		schedule = func(flush func()) {
			c.Spawn(func(context.Context) { flush() })
		}
	}
	return newEventHub(c, schedule)
}

// newEventHub is a hub whose
// flushes go to schedule. Tests use it to run a flush when they choose.
func newEventHub(c *coordinator.Coordinator, schedule func(flush func())) *EventHub {
	h := &EventHub{coordinator: c, clients: map[string]*EventClient{}, flushScheduler: schedule, dirty: map[EventResource]struct{}{}}
	if c != nil {
		h.observation = NewCoordinatorObservation(c, h)
		h.observation.Start()
	}
	return h
}

// Connect registers a connection and queues its
// mandatory hello (with the API version) before it can receive later events.
// It returns false when the hub is stopped or the hello cannot be queued.
func (h *EventHub) Connect() (string, routes.EventClientPort, bool) {
	id, client, ok := h.connect()
	if !ok {
		return "", nil, false
	}
	return id, client, true
}

// connect is Connect with the concrete client type.
func (h *EventHub) connect() (string, *EventClient, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return "", nil, false
	}
	id := core.NewUUID()
	client := newEventClient()
	hello, err := jsonx.Marshal(NewHelloMessage(launch.VersionCurrent))
	if err != nil || !client.Enqueue(hello) {
		return "", nil, false
	}
	h.clients[id] = client
	return id, client, true
}

// Disconnect removes a connection and closes it.
func (h *EventHub) Disconnect(id string) {
	h.mu.Lock()
	client := h.clients[id]
	delete(h.clients, id)
	h.mu.Unlock()
	if client != nil {
		client.Close()
	}
}

// WriterStarted is the writer started.
func (h *EventHub) WriterStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeWriterTasks++
}

// WriterFinished is the count never goes below zero.
func (h *EventHub) WriterFinished() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeWriterTasks = max(0, h.activeWriterTasks-1)
}

// SocketWriteStarted is the socket write started.
func (h *EventHub) SocketWriteStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeSocketWrites++
}

// SocketWriteFinished is the count never goes below zero.
func (h *EventHub) SocketWriteFinished() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeSocketWrites = max(0, h.activeSocketWrites-1)
}

// DebugState is the debug state.
func (h *EventHub) DebugState() EventHubDebugState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return EventHubDebugState{
		Clients:      len(h.clients),
		Writers:      h.activeWriterTasks,
		Writes:       h.activeSocketWrites,
		Observations: h.observationCountLocked(),
		Stopped:      h.stopped,
	}
}

// observationCountLocked is the observed resource count (zero without a coordinator
// or once stopped). Lock: runs with h.mu held.
func (h *EventHub) observationCountLocked() int {
	if h.observation == nil {
		return 0
	}
	return h.observation.ActiveSubscriptionCount()
}

// PublishNotification a
// coordinator notification already admitted by Settings.notifications, broadcast
// without touching observation or revisions.
func (h *EventHub) PublishNotification(title, body string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	data, err := jsonx.Marshal(NewNotificationMessage(title, body))
	if err != nil {
		return
	}
	h.broadcastLocked(data)
}

// Stop stops observation, closes every client and drops
// the queued work. Harmless when already stopped. Lock: takes h.mu itself, and
// releases it before the coordinator's lock is taken to stop observing.
func (h *EventHub) Stop() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	clients := slices.Collect(maps.Values(h.clients))
	h.clients = map[string]*EventClient{}
	h.dirty = map[EventResource]struct{}{}
	observation := h.observation
	h.observation = nil
	h.mu.Unlock()

	if observation != nil {
		observation.Stop()
	}
	for _, client := range clients {
		client.Close()
	}
}

// ObservedChange records that one resource changed in the current turn and sends
// its changed message now. The coordinator's listener calls observedChanges with a
// whole turn; this is the single-resource form.
func (h *EventHub) ObservedChange(resource EventResource) {
	h.observedChanges([]EventResource{resource})
}

// observedChanges records the resources one turn changed and schedules a flush
// of them. Changes recorded before that
// flush runs are sent by it together, one changed message per resource, in sorted
// order, each with the next hub-wide revision. Without a scheduler the flush runs
// now. A stopped hub records nothing.
// Lock: takes h.mu; called from the coordinator's listener, with its lock held.
func (h *EventHub) observedChanges(resources []EventResource) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	for _, resource := range resources {
		h.dirty[resource] = struct{}{}
	}
	if h.flushScheduler == nil {
		h.flushDirtyLocked()
		return
	}
	if !h.flushScheduled && len(h.dirty) > 0 {
		h.flushScheduled = true
		h.flushScheduler(h.runScheduledFlush)
	}
}

// runScheduledFlush is the flush a scheduler runs: it sends what is dirty now.
// Lock: takes h.mu.
func (h *EventHub) runScheduledFlush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushScheduled = false
	h.flushDirtyLocked()
}

// flushDirtyLocked sends the dirty resources and clears them. Lock: runs with h.mu held.
func (h *EventHub) flushDirtyLocked() {
	resources := slices.Collect(maps.Keys(h.dirty))
	h.dirty = map[EventResource]struct{}{}
	h.flush(resources)
}

// flush for one turn's resources: sorted, each sent with
// the next revision. Lock: runs with h.mu held.
func (h *EventHub) flush(resources []EventResource) {
	if h.stopped || len(resources) == 0 {
		return
	}
	sorted := append([]EventResource(nil), resources...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Less(sorted[j]) })
	for _, resource := range sorted {
		h.revisions++
		data, err := jsonx.Marshal(NewChangedMessage(resource, h.revisions))
		if err != nil {
			continue
		}
		h.broadcastLocked(data)
	}
}

// broadcastLocked queues data on every client; a
// client whose queue rejects it is dropped. Lock: runs with h.mu held.
func (h *EventHub) broadcastLocked(data []byte) {
	for id, client := range h.clients {
		if !client.Enqueue(data) {
			delete(h.clients, id)
		}
	}
}

// EventHubDebugState is the hub's counts, for tests and debugging.
type EventHubDebugState struct {
	Clients      int
	Writers      int
	Writes       int
	Observations int
	Stopped      bool
}
