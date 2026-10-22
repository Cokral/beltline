// The desktop commands that differ between macOS and Linux: revealing a file, opening a file or a
// URL, VS Code, Ghostty, and the name the web app gives revealing. Each is a command line for the
// shell runner, so the routes and the daemon run them without knowing which system they are on.

package launch

import (
	"path/filepath"
	"runtime"
)

// Platform is the operating system the desktop commands are for. The zero value is the system
// this process runs on; tests name one so their expected command lines hold on every system.
type Platform struct {
	GOOS string
}

// PlatformMacOS and PlatformLinux are the two systems Beltline runs on.
var (
	PlatformMacOS = Platform{GOOS: "darwin"}
	PlatformLinux = Platform{GOOS: "linux"}
)

// DesktopCommand is one command line: the executable (a path, or a name the runner finds on PATH)
// and its arguments.
type DesktopCommand struct {
	Executable string
	Arguments  []string
}

// goos is the system, this process's when unset.
// Lock: none (pure).
func (p Platform) goos() string {
	if p.GOOS == "" {
		return runtime.GOOS
	}
	return p.GOOS
}

// IsMacOS reports whether the commands are macOS's.
// Lock: none (pure).
func (p Platform) IsMacOS() bool { return p.goos() == "darwin" }

// Name is the system as the version record reports it: "macos" or "linux".
// Lock: none (pure).
func (p Platform) Name() string {
	if p.IsMacOS() {
		return "macos"
	}
	return p.goos()
}

// Reveal shows path in the file manager: selected in Finder on macOS; on Linux the folder itself,
// or the folder holding a file, since xdg-open has no way to select one.
// Lock: none (pure).
func (p Platform) Reveal(path string, directory bool) DesktopCommand {
	if p.IsMacOS() {
		return DesktopCommand{Executable: "/usr/bin/open", Arguments: []string{"-R", path}}
	}
	if !directory {
		path = filepath.Dir(path)
	}
	return DesktopCommand{Executable: "xdg-open", Arguments: []string{path}}
}

// Open opens a file or a URL with its default application.
// Lock: none (pure).
func (p Platform) Open(target string) DesktopCommand {
	if p.IsMacOS() {
		return DesktopCommand{Executable: "/usr/bin/open", Arguments: []string{target}}
	}
	return DesktopCommand{Executable: "xdg-open", Arguments: []string{target}}
}

// VSCode opens folder in Visual Studio Code: by its bundle on macOS, the `code` command on Linux.
// Lock: none (pure).
func (p Platform) VSCode(folder string) DesktopCommand {
	if p.IsMacOS() {
		return DesktopCommand{Executable: "/usr/bin/open", Arguments: []string{"-b", DesktopActionsVSCodeBundleID, folder}}
	}
	return DesktopCommand{Executable: "code", Arguments: []string{folder}}
}

// Ghostty opens a new Ghostty window running command (a tmux attach).
// Lock: none (pure).
func (p Platform) Ghostty(command string) DesktopCommand {
	if p.IsMacOS() {
		return DesktopCommand{Executable: "/usr/bin/open", Arguments: []string{"-na", "Ghostty", "--args", "-e", "/bin/zsh", "-lc", command}}
	}
	return DesktopCommand{Executable: "ghostty", Arguments: []string{"-e", "/bin/sh", "-c", command}}
}
