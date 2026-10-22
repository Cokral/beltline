package testkit

import (
	"sync"
	"time"
)

// DemoTime is the fixed instant tests start from: the demo's "now". Goldens
// show times like 2026-10-07T06:36:40Z, all before this instant.
var DemoTime = time.Date(2026, time.October, 7, 6, 40, 0, 0, time.UTC)

// Clock is a fake clock. Its zero value is not usable; use NewClock. It is
// safe for concurrent use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a clock that reads start until it is advanced or set.
func NewClock(start time.Time) *Clock {
	return &Clock{now: start}
}

// Now returns the clock's current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set moves the clock to t, forward or backward.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// ContractTime is the "now" of the server contract fixture
// (1791356400, 2026-10-07T07:00:00Z): the view and run goldens were recorded
// with it, so tests that compare with goldens seed the demo at this time.
var ContractTime = time.Date(2026, time.October, 7, 7, 0, 0, 0, time.UTC)
