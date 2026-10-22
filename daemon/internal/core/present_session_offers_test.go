// Pins the Ghostty offer rules of present_session_offers.go: an unselected session, a
// missing terminal, a paused run and the enabled case.

package core

import "testing"

// TestSessionGhosttyNoSessionOfferIsUnavailable: with no session selected, Ghostty is
// unavailable with the no-session reason.
func TestSessionGhosttyNoSessionOfferIsUnavailable(t *testing.T) {
	offer := SessionGhosttyNoSessionOffer()
	if offer.Enabled || offer.Reason == nil || *offer.Reason != "This run has no session to open in Ghostty." {
		t.Fatalf("no-session offer = %+v", offer)
	}
}

// TestSessionGhosttyOfferRules: the terminal check comes first, then the pause, and
// an attachable session on a run with no pause is enabled with no reason.
func TestSessionGhosttyOfferRules(t *testing.T) {
	running := Run{ID: "run", StageID: "build", State: RunStateRunning}
	paused := running
	paused.Pause = &RunPause{Phase: RunPausePhasePaused, Operation: "p", Snapshots: []RunPauseSnapshot{}, Deferred: []RunPauseDeferred{}}

	noTerminal := SessionGhosttyOffer(paused, false)
	if noTerminal.Enabled || noTerminal.Reason == nil || *noTerminal.Reason != "No terminal is running for this session." {
		t.Fatalf("no terminal, paused run: %+v; want the no-terminal reason first", noTerminal)
	}
	pausedOffer := SessionGhosttyOffer(paused, true)
	if pausedOffer.Enabled || pausedOffer.Reason == nil || *pausedOffer.Reason != DesktopActionsPausedReason {
		t.Fatalf("paused run with a terminal: %+v; want %q", pausedOffer, DesktopActionsPausedReason)
	}
	enabled := SessionGhosttyOffer(running, true)
	if !enabled.Enabled || enabled.Reason != nil {
		t.Fatalf("running run with a terminal: %+v; want enabled with no reason", enabled)
	}
}
