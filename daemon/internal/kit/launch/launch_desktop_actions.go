package launch

import (
	"net/url"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
)

// Visual Studio Code's bundle id and the messages its open action shows.
const (
	DesktopActionsVSCodeBundleID    = "com.microsoft.VSCode"
	DesktopActionsVSCodeUnavailable = "This worktree is no longer available."
	DesktopActionsVSCodeMissing     = "Visual Studio Code is not installed. Install it and try again."
	DesktopActionsVSCodeRetry       = "Try opening the worktree again."
)

// Ghostty's bundle id and where it is installed.
const (
	DesktopActionsGhosttyBundleID        = "com.mitchellh.ghostty"
	DesktopActionsGhosttyApplicationPath = tools.GhosttyApplicationPath
)

// DesktopActionsVSCodeError is why a worktree cannot be opened in Visual Studio
// Code, or nil when it can.
func DesktopActionsVSCodeError(checkoutExists, applicationInstalled bool) *string {
	var message string
	switch {
	case !checkoutExists:
		message = DesktopActionsVSCodeUnavailable
	case !applicationInstalled:
		message = DesktopActionsVSCodeMissing
	default:
		return nil
	}
	return &message
}

// DesktopActionsVSCodeLaunchError is the message for a launch that failed for
// reason, with the retry hint appended.
func DesktopActionsVSCodeLaunchError(reason *string) string {
	trimmed := ""
	if reason != nil {
		trimmed = strings.TrimSpace(*reason)
	}
	if trimmed == "" || trimmed == DesktopActionsVSCodeRetry {
		return DesktopActionsVSCodeRetry
	}
	return trimmed + " " + DesktopActionsVSCodeRetry
}

// DesktopActionsGhosttySession is the session the sidebar shows: an open attempt's
// own session, or a closed one's Continue session when that is running. Nil when
// there is no attempt.
func DesktopActionsGhosttySession(attempt *string, continueAvailable, isOpen bool) *string {
	if attempt == nil || *attempt == "" {
		return nil
	}
	session := *attempt
	if !isOpen && continueAvailable {
		session += "-continue"
	}
	return &session
}

// DesktopActionsGhosttyCommand is the shell command that attaches to a session on the given tmux socket.
func DesktopActionsGhosttyCommand(socket, session string) string {
	return "tmux -L " + DesktopActionsShellQuote(socket) + " attach-session -t " + DesktopActionsShellQuote(session)
}

// DesktopActionsValidWebURL reports whether value is an http or https URL with a host.
func DesktopActionsValidWebURL(value string) bool {
	if value == "" {
		return false
	}
	// URL parsing refuses a literal space and control characters
	// (url.Parse accepts a space), so they are refused before parsing.
	for _, r := range value {
		if r == ' ' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	return parsed.Hostname() != ""
}

// DesktopActionsShellQuote single-quotes value for the shell.
func DesktopActionsShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
