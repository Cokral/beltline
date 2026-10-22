// Tests of the schedule's calendar behaviour: the next and latest firing.

package core

import (
	"testing"
	"time"
)

// newYork loads America/New_York for the DST tests.
func newYork(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("no zoneinfo for America/New_York: %v", err)
	}
	return loc
}

// TestScheduleSpringForwardGapFiresAtTheJump checks that a wall time inside the
// spring-forward gap (02:30 on 2026-03-08 in New York) fires at the instant the
// clock jumps, 03:00 EDT.
func TestScheduleSpringForwardGapFiresAtTheJump(t *testing.T) {
	t.Parallel()
	loc := newYork(t)
	schedule := Schedule{Days: ScheduleDaysEveryDay, Hour: 2, Minute: 30}
	after := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	btAssertTime(t, "next gap slot", schedule.Next(after, loc), time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC))
}

// TestScheduleFallBackFiresAtFirstOccurrence checks that a repeated wall time
// (01:30 on 2026-11-01 in New York) fires at its first occurrence, EDT.
func TestScheduleFallBackFiresAtFirstOccurrence(t *testing.T) {
	t.Parallel()
	loc := newYork(t)
	schedule := Schedule{Days: ScheduleDaysEveryDay, Hour: 1, Minute: 30}
	after := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	btAssertTime(t, "next repeated slot", schedule.Next(after, loc), time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
}

// TestScheduleOutOfRangeTimeSkipsTheDay checks that an hour outside 0-23 or a
// minute outside 0-59 has no slot on any day, so nothing fires.
func TestScheduleOutOfRangeTimeSkipsTheDay(t *testing.T) {
	t.Parallel()
	loc := newYork(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, loc)
	for _, schedule := range []Schedule{
		{Days: ScheduleDaysEveryDay, Hour: 25, Minute: 0},
		{Days: ScheduleDaysEveryDay, Hour: -1, Minute: 0},
		{Days: ScheduleDaysEveryDay, Hour: 9, Minute: 60},
	} {
		if got := schedule.Next(now, loc); got != nil {
			t.Errorf("Next(%d:%d) = %v, want nil", schedule.Hour, schedule.Minute, *got)
		}
		if got := schedule.Latest(now, loc); got != nil {
			t.Errorf("Latest(%d:%d) = %v, want nil", schedule.Hour, schedule.Minute, *got)
		}
	}
}
