package daemon

import (
	"context"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// LinuxNotification posts a desktop notification with notify-send (libnotify). Without
// notify-send, or without a desktop session to show it in, the notification is only logged.
type LinuxNotification struct {
	Runner shell.Runner
	// Available reports whether notify-send is installed; nil checks the tools' PATH.
	Available func() bool
}

// Post shows title and body. A failure is dropped: beltlined.log keeps every notification.
// Lock: none. Suspends: the notify-send command.
func (n LinuxNotification) Post(ctx context.Context, title, body string) {
	available := n.Available
	if available == nil {
		available = func() bool { return shell.LookPath("notify-send") }
	}
	if !available() {
		return
	}
	runner := n.Runner
	if runner == nil {
		runner = shell.ProcessRunner{}
	}
	// The log already holds the notification, so a failed post loses nothing.
	_ = runner.Run(ctx, "notify-send", LinuxNotificationArguments(title, body), "", nil)
}

// LinuxNotificationArguments are notify-send's arguments: the app name, then the title and body
// after "--", so text that starts with a dash is never read as an option.
// Lock: none (pure).
func LinuxNotificationArguments(title, body string) []string {
	return []string{"--app-name=Beltline", "--", title, body}
}
