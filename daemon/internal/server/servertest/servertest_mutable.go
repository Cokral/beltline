// The fixture's test doubles for the clock and the desktop lookups: tests move the clock past an
// artifact's expiry and choose which applications and sessions are available.
package servertest

import (
	"sync"
	"time"
)

// MutableClock is a clock tests set, shared by the artifact signer.
type MutableClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewMutableClock starts a clock at t.
func NewMutableClock(t time.Time) *MutableClock { return &MutableClock{now: t} }

// Now is the clock's time.
func (c *MutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to t.
func (c *MutableClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// MutableLookup answers whether a desktop application or session is available: every name when
// defaultValue is set, else only the names Set lists.
type MutableLookup struct {
	mu           sync.Mutex
	defaultValue bool
	allowed      map[string]bool
}

// NewMutableLookup starts a lookup that answers defaultValue for every name.
func NewMutableLookup(defaultValue bool) *MutableLookup {
	return &MutableLookup{defaultValue: defaultValue, allowed: map[string]bool{}}
}

// IsAllowed reports whether name is available.
func (l *MutableLookup) IsAllowed(name string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.allowed[name] || l.defaultValue
}

// Set makes exactly the names available, and no others.
func (l *MutableLookup) Set(names ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.allowed = map[string]bool{}
	for _, name := range names {
		l.allowed[name] = true
	}
	l.defaultValue = false
}

// Reset forgets the names and answers defaultValue for every name.
func (l *MutableLookup) Reset(defaultValue bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.allowed = map[string]bool{}
	l.defaultValue = defaultValue
}
