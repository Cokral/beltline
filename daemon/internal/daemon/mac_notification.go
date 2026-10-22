// A bare executable has no bundle
// for UNUserNotificationCenter, so it asks osascript to display one instead.

package daemon

import (
	"context"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// MacNotification posts a macOS notification through osascript.
type MacNotification struct {
	Runner shell.Runner
}

// Post displays title and body as a notification.
// Lock: none. Suspends: the osascript command.
func (m MacNotification) Post(ctx context.Context, title, body string) {
	runner := m.Runner
	if runner == nil {
		runner = shell.ProcessRunner{}
	}
	_ = runner.Run(ctx, "/usr/bin/osascript", []string{"-e", MacNotificationScript(title, body)}, "", nil)
}

// MacNotificationScript is the AppleScript that displays the notification.
// Lock: none (pure).
func MacNotificationScript(title, body string) string {
	return "display notification " + MacNotificationLiteral(body) + " with title " + MacNotificationLiteral(title)
}

// MacNotificationLiteral is an AppleScript string literal. Quotes, backslashes and
// line breaks are escaped so no text from a run can end the literal or add a statement.
// Lock: none (pure).
func MacNotificationLiteral(text string) string {
	var escaped strings.Builder
	// "\r
	// " is one character that matches none of the
	// cases, so a CR LF pair is written as it is. Other clusters are not regrouped.
	runes := []rune(text)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		if character == '\r' && index+1 < len(runes) && runes[index+1] == '\n' {
			escaped.WriteString("\r\n")
			index++
			continue
		}
		switch character {
		case '\\':
			escaped.WriteString(`\\`)
		case '"':
			escaped.WriteString(`\"`)
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		default:
			escaped.WriteRune(character)
		}
	}
	return `"` + escaped.String() + `"`
}
