// The loop that ticks the coordinator every two seconds.

package daemon

import (
	"errors"
	"time"
)

// DefaultTickInterval is the wait between one tick's completion and the next.
const DefaultTickInterval = 2 * time.Second

// errTickSleepEnded is what the default sleep returns once the loop is cancelled.
var errTickSleepEnded = errors.New("tick loop cancelled")

// TickSleep waits d, or returns an error when done closes first.
type TickSleep func(done <-chan struct{}, d time.Duration) error

// TickLoop ticks the coordinator forever: one tick, then the interval after it
// completes. A slow tick never overlaps the next one. Cancellation waits for the
// tick in progress.
type TickLoop struct {
	Interval time.Duration
	Tick     func()
	Sleep    TickSleep
}

// Run ticks until done closes. A tick that is running when done closes finishes first.
// Lock: none; the tick takes the coordinator's lock itself.
func (l TickLoop) Run(done <-chan struct{}) {
	sleep := l.Sleep
	if sleep == nil {
		sleep = defaultTickSleep
	}
	for !closed(done) {
		l.Tick()
		if closed(done) {
			return
		}
		if err := sleep(done, l.Interval); err != nil {
			return
		}
	}
}

// defaultTickSleep is Task.sleep: it returns early, with an error, when done closes.
// Lock: none.
func defaultTickSleep(done <-chan struct{}, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-done:
		return errTickSleepEnded
	}
}

// closed reports whether done has been closed, without blocking.
// Lock: none.
func closed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
