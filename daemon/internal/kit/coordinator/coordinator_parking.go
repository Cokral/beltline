package coordinator

import (
	"context"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// Park parks every unfinished route from the Run page. Saving happens before any
// session is stopped; a failed save leaves the run as it was.
// Lock: exported entry point; takes c.mu. Spawns: the endContinuing of each
// parked session, through c.Spawn.
func (c *Coordinator) Park(id string) {
	c.Lock()
	defer c.EndTurn()
	c.park(id)
}

// park is Park with c.mu held.
// Lock: runs with c.mu held. Spawns: the endContinuing of each parked session, and kick.
func (c *Coordinator) park(id string) {
	run := c.book.Run(id)
	if run == nil || !run.CanButtonPark() {
		return
	}
	if !c.book.apply(id, core.Event{Kind: core.EventKindParkRun}, nil, nil, nil) {
		return
	}
	if after := c.book.Run(id); after == nil || !after.IsButtonParked() {
		return
	}
	c.autopilot.supersedeParked(id)
	continued := c.book.Run(id)
	c.Spawn(func(ctx context.Context) {
		// The task runs under the lock to read the specs,
		// and the stops wait without it.
		var specs []runner.SessionSpec
		c.Lock()
		if continued != nil {
			for _, attempt := range continued.Attempts {
				if spec := c.sessions.spec(*continued, attempt, ""); spec != nil {
					specs = append(specs, *spec)
				}
			}
		}
		c.EndTurn()
		for _, spec := range specs {
			c.runner.EndContinuing(ctx, spec)
		}
	})
	c.kick()
}

// Unpark restores a run parked from the Run page. Nothing starts while a stop is
// still finishing or has failed; a failed stop is retried from the engine's
// effect before a replacement can start. A belt or app pause still holds the
// restored work in the queue.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) Unpark(id string) {
	c.Lock()
	defer c.EndTurn()
	c.unpark(id)
}

// unpark is Unpark with c.mu held.
// Lock: runs with c.mu held. Spawns: kick.
func (c *Coordinator) unpark(id string) {
	run := c.book.Run(id)
	if run == nil || !run.IsButtonParked() {
		return
	}
	c.book.apply(id, core.Event{Kind: core.EventKindUnparkRun}, nil, nil, nil)
	c.kick()
}

// recoverParkStops stops sessions a parked run still names, including after a
// restart interrupted the stop. A stop that already failed is left for Unpark to
// retry.
// Lock: runs with c.mu held. Suspends: stopForPark.
func (c *Coordinator) recoverParkStops(ctx context.Context) {
	if c.recoveringParks {
		return
	}
	c.recoveringParks = true
	defer func() { c.recoveringParks = false }()
	stopping := func(run core.Run) bool { return run.Parking != nil && run.Parking.StopError == nil }
	for _, run := range runBookCopiesWhere(&c.book, stopping) {
		for _, session := range run.Parking.Stopping {
			c.stopForPark(run.ID, session)
		}
	}
}

// stopForPark stops one parked session, and records a failure for Unpark to retry.
// Lock: runs with c.mu held. Spawns: runner.ensureStopped, through c.Spawn.
func (c *Coordinator) stopForPark(id, session string) {
	key := id + "|" + session
	if _, running := c.parkStops[key]; running {
		return
	}
	c.parkStops[key] = struct{}{}
	c.trackStop(id, func(ctx context.Context) {
		var message *string
		var err error
		c.Suspend(func() { err = c.runner.EnsureStopped(ctx, session) })
		if err != nil {
			text := err.Error()
			message = &text
		}
		delete(c.parkStops, key)
		if run := c.book.Run(id); run == nil || run.Parking == nil || !parkingStopping(run.Parking, session) {
			return
		}
		ref := id
		if run := c.book.Run(id); run != nil {
			ref = run.Subject.Ref()
		}
		c.book.apply(id, core.Event{Kind: core.EventKindParkStopped, Session: session, Error: message}, nil, nil, nil)
		if message != nil {
			c.book.note("Could not stop a session on "+ref+": "+*message, &id)
		}
		c.kick()
	})
}

// parkingStopping reports whether session is still among the parking's stops.
// Lock: none (pure).
func parkingStopping(parking *core.Parking, session string) bool {
	return slices.Contains(parking.Stopping, session)
}
