// LaunchServices is not
// reachable without AppKit, so the bundle identifier is read from the
// Info.plist of each application in the standard folders.

package daemon

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
)

// applicationFolders are the folders an installed application is looked up in, in order.
var applicationFolders = []string{"/Applications", "/System/Applications"}

// DesktopApplicationInstalled reports whether the application with bundleID is installed.
// Ghostty is installed when the Settings tools check says so, so the two never disagree.
// Lock: none.
func DesktopApplicationInstalled(bundleID string) bool {
	if bundleID == launch.DesktopActionsGhosttyBundleID {
		return tools.GhosttyInstalled()
	}
	if runtime.GOOS != "darwin" {
		// Linux has no bundles: VS Code is installed when its command is.
		return bundleID == launch.DesktopActionsVSCodeBundleID && shell.LookPath("code")
	}
	folders := append([]string{}, applicationFolders...)
	if home, err := os.UserHomeDir(); err == nil {
		folders = append(folders, filepath.Join(home, "Applications"))
	}
	for _, folder := range folders {
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".app" {
				continue
			}
			if bundleIdentifierOf(filepath.Join(folder, entry.Name())) == bundleID {
				return true
			}
		}
	}
	return false
}

// bundleIdentifierOf is the CFBundleIdentifier in an application's Info.plist, or "".
// Lock: none.
func bundleIdentifierOf(app string) string {
	data, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	value, err := plistDecode(data)
	if err != nil {
		return ""
	}
	dict, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	identifier, _ := dict["CFBundleIdentifier"].(string)
	return identifier
}
