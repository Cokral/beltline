package core

import "testing"

// TestRunAfkButtonHelpSaysWhatTheModeMeansAndWhereAClickGoes pins each mode's help and its next mode.
func TestRunAfkButtonHelpSaysWhatTheModeMeansAndWhereAClickGoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode AfkMode
		on   bool
		want string
	}{
		{AfkModeAlways, true, "This run is in AFK mode, whatever the global switch. Click for AFK: Never."},
		{AfkModeNever, false, "This run waits for you, whatever the global switch. Click for AFK: Global."},
		{AfkModeGlobal, true, "This run follows the global AFK switch, which is on. Click for AFK: Always."},
		{AfkModeGlobal, false, "This run follows the global AFK switch, which is off. Click for AFK: Always."},
	}
	for _, tc := range cases {
		if got := RunAfkButtonHelp(tc.mode, tc.on); got != tc.want {
			t.Fatalf("RunAfkButtonHelp(%q, %t) = %q, want %q", tc.mode, tc.on, got, tc.want)
		}
	}
}
