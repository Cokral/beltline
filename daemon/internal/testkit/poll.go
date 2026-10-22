package testkit

import (
	"testing"
	"time"
)

// pollInterval is how often Eventually checks its condition.
const pollInterval = 20 * time.Millisecond

// Eventually polls cond every 20 ms until it returns true or timeout passes,
// then fails the test with msg. It is for real processes (tmux, child
// processes) only: a fake clock never advances during the wait, so a condition
// on a fake clock must be driven by the test instead.
func Eventually(t testing.TB, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if cond() {
			return
		}
		select {
		case <-timer.C:
			if cond() {
				return
			}
			t.Fatalf("testkit: %s (not true within %v)", msg, timeout)
			return
		case <-ticker.C:
		}
	}
}
