// The global control expectations of the coordinator's state reads.

package core

import "testing"

// TestGlobalActionsListPauseAfkAndCheckNowInOrder: the controls come in pause, AFK, check-now order, and all are enabled.
func TestGlobalActionsListPauseAfkAndCheckNowInOrder(t *testing.T) {
	t.Parallel()
	actions := GlobalActionsOf(false, false, false)
	wantIDs := []string{"pause", "afk", "check-now"}
	if len(actions) != len(wantIDs) {
		t.Fatalf("actions = %+v", actions)
	}
	for i, action := range actions {
		if action.ID != wantIDs[i] || !action.Enabled || action.Reason != nil {
			t.Errorf("action %d = %+v", i, action)
		}
	}
	if actions[2].Help != nil {
		t.Errorf("check-now help = %q, want none", *actions[2].Help)
	}
}

// TestGlobalActionsLabelsFollowPauseAndAfk: the pause label, the AFK label and the AFK help
// follow the global switches, and AFK shows deciding while an AFK decision is in progress.
func TestGlobalActionsLabelsFollowPauseAndAfk(t *testing.T) {
	t.Parallel()
	off := GlobalActionsOf(false, false, false)
	if off[0].Label != "Pause everything" || off[0].Help == nil ||
		*off[0].Help != "Stop every agent session, so nothing fails while the laptop is closed" {
		t.Errorf("unpaused pause = %+v", off[0])
	}
	if off[1].Label != "GLOBAL AFK OFF" || *off[1].Help != "Global AFK is off for runs set to Global. Runs set to Always or Never keep their own." {
		t.Errorf("afk off = %+v", off[1])
	}

	paused := GlobalActionsOf(true, true, false)
	if paused[0].Label != "Paused · Continue" || *paused[0].Help != "Continue every agent session where it stopped" {
		t.Errorf("paused pause = %+v", paused[0])
	}
	if paused[1].Label != "GLOBAL AFK ON" || *paused[1].Help != "Global AFK is on for runs set to Global. Runs set to Always or Never keep their own." {
		t.Errorf("afk on = %+v", paused[1])
	}
	if !paused[2].Enabled || paused[2].Label != "Fetch" {
		t.Errorf("fetch while paused = %+v", paused[2])
	}

	deciding := GlobalActionsOf(false, true, true)
	if deciding[1].Label != "GLOBAL AFK · DECIDING" {
		t.Errorf("afk deciding label = %q", deciding[1].Label)
	}
	if off := GlobalActionsOf(false, false, true); off[1].Label != "GLOBAL AFK OFF" {
		t.Errorf("deciding without afk label = %q", off[1].Label)
	}
}
