// The adapter between the coordinator and the event hub. The coordinator compares every resource's
// projection at the end of each turn (Turns) and reports the changed ones to one listener
// (SetChangeListener). This type sets that listener to the hub, and clears it when the hub stops.

package server

import (
	"sync"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// CoordinatorObservation forwards each turn's changed resources to its hub.
type CoordinatorObservation struct {
	coordinator *coordinator.Coordinator
	hub         *EventHub
	// mu guards active. It is never held while the coordinator's lock is taken.
	mu     sync.Mutex
	active bool
}

// NewCoordinatorObservation builds the coordinator observation from its arguments.
func NewCoordinatorObservation(c *coordinator.Coordinator, hub *EventHub) *CoordinatorObservation {
	return &CoordinatorObservation{coordinator: c, hub: hub}
}

// Start every resource is observed from
// now on, so the hub's observation count is every resource. Lock: takes the
// coordinator's lock through SetChangeListener; the caller holds none.
func (o *CoordinatorObservation) Start() {
	o.mu.Lock()
	if o.active {
		o.mu.Unlock()
		return
	}
	o.active = true
	o.mu.Unlock()
	o.coordinator.SetChangeListener(o.turnEnded)
}

// Stop drops the subscription, so the
// coordinator reports nothing more to this hub. Lock: takes the coordinator's
// lock through SetChangeListener; the caller holds none.
func (o *CoordinatorObservation) Stop() {
	o.mu.Lock()
	if !o.active {
		o.mu.Unlock()
		return
	}
	o.active = false
	o.mu.Unlock()
	o.coordinator.SetChangeListener(nil)
}

// ActiveSubscriptionCount is the active subscription count:
// the number of resources observed, which is all of them while started.
func (o *CoordinatorObservation) ActiveSubscriptionCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.active {
		return 0
	}
	return len(AllEventResources)
}

// turnEnded is the coordinator's change listener: it receives the resources whose
// projection changed in one turn, sorted, and hands them to the hub.
// Lock: runs with the coordinator's lock held, so it must not block.
func (o *CoordinatorObservation) turnEnded(resources []coordinator.Resource) {
	o.mu.Lock()
	active := o.active
	o.mu.Unlock()
	if !active {
		return
	}
	changed := make([]EventResource, 0, len(resources))
	for _, r := range resources {
		changed = append(changed, EventResource(r))
	}
	o.hub.observedChanges(changed)
}
