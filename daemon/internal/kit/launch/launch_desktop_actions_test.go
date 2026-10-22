package launch

import (
	"testing"
)

func TestVSCodePolicyAndRetryWordingAreShared(t *testing.T) {
	t.Parallel()
	if got := DesktopActionsVSCodeError(false, true); got == nil || *got != DesktopActionsVSCodeUnavailable {
		t.Errorf("missing checkout = %v", got)
	}
	if got := DesktopActionsVSCodeError(true, false); got == nil || *got != DesktopActionsVSCodeMissing {
		t.Errorf("missing app = %v", got)
	}
	if got := DesktopActionsVSCodeError(true, true); got != nil {
		t.Errorf("installed = %v, want nil", *got)
	}
	if got := DesktopActionsVSCodeLaunchError(nil); got != DesktopActionsVSCodeRetry {
		t.Errorf("nil reason = %q", got)
	}
	if got := DesktopActionsVSCodeLaunchError(new("launch failed")); got != "launch failed Try opening the worktree again." {
		t.Errorf("reason = %q", got)
	}
}

func TestGhosttySelectionAndCommandQuoteEveryDynamicValue(t *testing.T) {
	t.Parallel()
	value := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	cases := []struct {
		got  *string
		want string
	}{
		{DesktopActionsGhosttySession(new("attempt"), false, false), "attempt"},
		{DesktopActionsGhosttySession(new("attempt"), true, false), "attempt-continue"},
		{DesktopActionsGhosttySession(new("attempt"), true, true), "attempt"},
		{DesktopActionsGhosttySession(nil, true, false), "<nil>"},
	}
	for _, c := range cases {
		if value(c.got) != c.want {
			t.Errorf("got %q, want %q", value(c.got), c.want)
		}
	}
	if got := DesktopActionsGhosttyCommand("sock'$(touch nope)", "name; echo bad"); got != "tmux -L 'sock'\\''$(touch nope)' attach-session -t 'name; echo bad'" {
		t.Errorf("command = %q", got)
	}
}

func TestAcceptsOnlyAbsoluteWebLinks(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"http://example.test/path", "https://example.test", "HTTP://example.test", "hTtPs://example.test:8443/a"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if !DesktopActionsValidWebURL(value) {
				t.Errorf("%q rejected", value)
			}
		})
	}
}

func TestRejectsUnsafeOrMalformedStoredLinks(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"file:///tmp/a", "javascript:alert(1)", "FILE:/tmp/a", "data:text/html,x", "https:", "/relative", "https:///path", "https://bad host", "https://example.com/a b", "https://example.com/\x01"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if DesktopActionsValidWebURL(value) {
				t.Errorf("%q accepted", value)
			}
		})
	}
}
