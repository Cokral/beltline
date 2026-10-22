package daemon

import (
	"context"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

func TestLinuxNotificationRunsNotifySendOnlyWhenItIsInstalled(t *testing.T) {
	t.Parallel()
	runner := &shell.Recorder{}
	LinuxNotification{Runner: runner, Available: func() bool { return false }}.Post(context.Background(), "Title", "Body")
	if len(runner.Calls()) != 0 {
		t.Fatalf("calls %v without notify-send", runner.Calls())
	}
	LinuxNotification{Runner: runner, Available: func() bool { return true }}.Post(context.Background(), "-Title", "Body\nline")
	calls := runner.Calls()
	if len(calls) != 1 || calls[0].Executable != "notify-send" {
		t.Fatalf("calls %v", calls)
	}
	if want := []string{"--app-name=Beltline", "--", "-Title", "Body\nline"}; !reflect.DeepEqual(calls[0].Args, want) {
		t.Fatalf("arguments %q, want %q", calls[0].Args, want)
	}
}
