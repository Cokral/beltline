package core

import "time"

// DateInterval is a span of time between two instants, as Foundation's DateInterval.
type DateInterval struct {
	Start time.Time
	End   time.Time
}

// Duration is the length of the interval in seconds.
func (d DateInterval) Duration() float64 {
	return d.End.Sub(d.Start).Seconds()
}

// Intersection is the part of d that lies inside other, or nil when they do not
// overlap.
func (d DateInterval) Intersection(other DateInterval) *DateInterval {
	start := d.Start
	if other.Start.After(start) {
		start = other.Start
	}
	end := d.End
	if other.End.Before(end) {
		end = other.End
	}
	if start.After(end) {
		return nil
	}
	return &DateInterval{Start: start, End: end}
}

// Pause is the app's one global pause. Closing the laptop should not cost a night of
// work: one switch suspends every agent session the app owns, starts nothing
// new and leaves every run exactly where it was, so pressing it again picks
// the same sessions up where they stopped.
//
// Nothing here is a run state or an engine event. A paused run is an
// ordinary `running` run whose sessions are frozen, which is what makes
// continuing exact.
type Pause struct {
	// IsPaused is whether the app is paused now.
	IsPaused bool
	// PausedAt is when this pause began; nil while the app is live.
	PausedAt *time.Time
	// Pauses are the pauses that have ended, so a run's timer only takes out the
	// paused time that fell while it was going.
	Pauses []DateInterval
}

// PauseLive is the app as it is while nothing is paused.
var PauseLive = Pause{}

// PausedFor is all the time spent paused in the pauses that have ended, in
// seconds.
func (p Pause) PausedFor() float64 {
	total := 0.0
	for _, interval := range p.Pauses {
		total += interval.Duration()
	}
	return total
}

// PauseLabelFor is the one control's label: what you press to pause, and to
// continue.
func PauseLabelFor(isPaused bool) string {
	if isPaused {
		return "Paused · Continue"
	}
	return "Pause everything"
}

// Label is what the control reads on this instance.
func (p Pause) Label() string {
	return PauseLabelFor(p.IsPaused)
}

// PauseClockLabel is the sidebar's lamp, which tells you once that everything is
// stopped.
func PauseClockLabel(isPaused bool, time string) string {
	word := "LIVE"
	if isPaused {
		word = "PAUSED"
	}
	return word + " · " + time
}

// ClockLabel is the sidebar's lamp, with this pause's time already taken out.
func (p Pause) ClockLabel(time string) string {
	return PauseClockLabel(p.IsPaused, time)
}

// PauseElapsed is how long something has really been going since `since`, with the
// paused time that fell after it taken back out. While paused the number stands
// still, and carries on from there when you continue.
func PauseElapsed(since time.Time, pauses []DateInterval, pausedAt *time.Time, now time.Time) float64 {
	if !now.After(since) {
		return 0
	}
	window := DateInterval{Start: since, End: now}
	all := append([]DateInterval(nil), pauses...)
	if pausedAt != nil && !pausedAt.After(now) {
		all = append(all, DateInterval{Start: *pausedAt, End: now})
	}
	paused := 0.0
	for _, interval := range all {
		if part := window.Intersection(interval); part != nil {
			paused += part.Duration()
		}
	}
	return max(0, window.Duration()-paused)
}

// Elapsed is the same clock, from this pause's own numbers.
func (p Pause) Elapsed(since, now time.Time) float64 {
	return PauseElapsed(since, p.Pauses, p.PausedAt, now)
}
