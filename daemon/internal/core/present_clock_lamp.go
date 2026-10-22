package core

import (
	"fmt"
	"time"
)

// ClockLampLabel is the sidebar's live clock lamp label. Paused is told once, here;
// otherwise the lamp says whether GitHub is being checked.
//
// The time is the local 24-hour clock with two digits, "HH:mm".
// The locale's hour style is not reproduced.
func ClockLampLabel(isPaused, checking bool, now time.Time) string {
	local := now.In(time.Local)
	clock := fmt.Sprintf("%02d:%02d", local.Hour(), local.Minute())
	if isPaused {
		return PauseClockLabel(true, clock)
	}
	state := "LIVE"
	if checking {
		state = "CHECKING"
	}
	return state + " · " + clock
}

// ClockLampTooltip is what the lamp says on hover: the pause, or when GitHub was last
// checked (nil when never).
func ClockLampTooltip(isPaused bool, lastCheck *time.Time, now time.Time) string {
	if isPaused {
		return "Everything is paused. Nothing runs until you press Continue."
	}
	if lastCheck == nil {
		return ""
	}
	return "Checked GitHub " + RelativeTimeAgo(*lastCheck, now)
}
