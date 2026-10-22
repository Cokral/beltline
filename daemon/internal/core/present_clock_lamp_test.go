package core

import (
	"strings"
	"testing"
	"time"
)

// TestPausedLampSaysPausedAndExplainsTheStop
func TestPausedLampSaysPausedAndExplainsTheStop(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	if got := ClockLampLabel(true, false, now); !strings.HasPrefix(got, "PAUSED · ") {
		t.Fatalf("paused label = %q", got)
	}
	if got := ClockLampTooltip(true, &now, now); got != "Everything is paused. Nothing runs until you press Continue." {
		t.Fatalf("paused tooltip = %q", got)
	}
}

// TestRunningLampSaysCheckingOrLiveAndReportsTheLastCheck
func TestRunningLampSaysCheckingOrLiveAndReportsTheLastCheck(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	if got := ClockLampLabel(false, true, now); !strings.HasPrefix(got, "CHECKING · ") {
		t.Fatalf("checking label = %q", got)
	}
	if got := ClockLampLabel(false, false, now); !strings.HasPrefix(got, "LIVE · ") {
		t.Fatalf("live label = %q", got)
	}
	if got := ClockLampTooltip(false, nil, now); got != "" {
		t.Fatalf("tooltip without a check = %q", got)
	}
	last := now.Add(-600 * time.Second)
	if got := ClockLampTooltip(false, &last, now); got != "Checked GitHub 10 min ago" {
		t.Fatalf("tooltip = %q", got)
	}
}

// TestClockLampLabelPinsTheExactLabelForAFixedTimeAndZone: the clock is the local 24-hour
// time, so the zone is set for the test (it is restored after; the test is not parallel).
func TestClockLampLabelPinsTheExactLabelForAFixedTimeAndZone(t *testing.T) {
	saved := time.Local
	time.Local = time.FixedZone("CEST", 2*3600)
	defer func() { time.Local = saved }()
	now := time.Date(2026, 10, 12, 14, 5, 0, 0, time.UTC)
	if got, want := ClockLampLabel(false, false, now), "LIVE · 16:05"; got != want {
		t.Errorf("live label = %q, want %q", got, want)
	}
	if got, want := ClockLampLabel(false, true, now), "CHECKING · 16:05"; got != want {
		t.Errorf("checking label = %q, want %q", got, want)
	}
	if got, want := ClockLampLabel(true, false, now), "PAUSED · 16:05"; got != want {
		t.Errorf("paused label = %q, want %q", got, want)
	}
	last := time.Date(2026, 10, 12, 13, 50, 0, 0, time.UTC)
	if got, want := ClockLampTooltip(false, &last, now), "Checked GitHub 15 min ago"; got != want {
		t.Errorf("tooltip = %q, want %q", got, want)
	}
}
