// Registers the daemon executable as a systemd user service on Linux, the counterpart of the
// macOS LaunchAgent. The runner and the file system are injected, so the tests never touch the
// user's units folder or call systemctl.

package daemon

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// SystemdUnitName is the user service's unit name.
const SystemdUnitName = "beltline.service"

// DefaultSystemctl is the systemctl the registration calls, found on PATH.
const DefaultSystemctl = "systemctl"

// SystemdUnit registers the daemon executable as a systemd user service.
type SystemdUnit struct {
	// UnitsFolder is the user's units folder, ~/.config/systemd/user.
	UnitsFolder    string
	ExecutablePath string
	Environment    map[string]string
	SupportFolder  string
	SystemctlPath  string
	Runner         shell.Runner
	FileSystem     LaunchAgentFileSystem
}

// SystemdResult is the outcome of an install, uninstall or restart.
type SystemdResult struct {
	Action   LaunchAgentAction
	UnitPath string
}

// Output is what the command prints: the action and the unit, the unit file, and after an install
// how to keep the service running while the user is logged out.
// Lock: none (pure).
func (r SystemdResult) Output() string {
	text := fmt.Sprintf("%s %s in the systemd user manager\nUnit: %s\n", r.Action.Title(), SystemdUnitName, r.UnitPath)
	if r.Action == LaunchAgentInstalled || r.Action == LaunchAgentUpdated {
		text += "It starts when you log in. To keep it running after you log out: loginctl enable-linger \"$USER\"\n"
	}
	return text
}

// UnitPath is the unit file the service is written to.
// Lock: none (pure).
func (u SystemdUnit) UnitPath() string {
	folder := u.UnitsFolder
	if abs, err := filepath.Abs(folder); err == nil {
		folder = abs
	}
	return filepath.Join(folder, SystemdUnitName)
}

// SystemdUnitsFolder is the user's systemd units folder: $XDG_CONFIG_HOME/systemd/user, or
// ~/.config/systemd/user when XDG_CONFIG_HOME is unset or relative.
// Lock: none (pure).
func SystemdUnitsFolder(home string, env func(string) string) string {
	config := env("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) {
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "systemd", "user")
}

// Install writes the unit, enables it and starts it, restarting a running service whose unit
// changed; with the same unit enabled and running it reports that it is already installed.
// Lock: none. Suspends: each systemctl call, run without any lock.
func (u SystemdUnit) Install(ctx context.Context) (SystemdResult, error) {
	unit, dataFolder, err := u.desiredUnit()
	if err != nil {
		return SystemdResult{}, err
	}
	files := u.fileSystem()
	present := files.FileExists(u.UnitPath())
	same := false
	if present {
		existing, err := files.ReadFile(u.UnitPath())
		if err != nil {
			return SystemdResult{}, launchAgentFailure("Could not read the systemd unit at %s: %s", u.UnitPath(), err.Error())
		}
		same = bytes.Equal(existing, unit)
	}
	active := u.systemctl(ctx, "is-active", "--quiet", SystemdUnitName).OK()
	enabled := u.systemctl(ctx, "is-enabled", "--quiet", SystemdUnitName).OK()
	if same && active && enabled {
		return u.result(LaunchAgentAlreadyInstalled), nil
	}
	if err := files.CreateDirectory(dataFolder); err != nil {
		return SystemdResult{}, launchAgentFailure("Could not create the data folder %s: %s", dataFolder, err.Error())
	}
	if err := files.CreateDirectory(filepath.Dir(u.UnitPath())); err != nil {
		return SystemdResult{}, launchAgentFailure("Could not write the systemd unit at %s: %s", u.UnitPath(), err.Error())
	}
	if !same {
		if err := files.WriteAtomically(unit, u.UnitPath()); err != nil {
			return SystemdResult{}, launchAgentFailure("Could not write the systemd unit at %s: %s", u.UnitPath(), err.Error())
		}
	}
	if err := u.require(ctx, "reload the systemd user manager", "daemon-reload"); err != nil {
		return SystemdResult{}, err
	}
	if err := u.require(ctx, "enable "+SystemdUnitName, "enable", SystemdUnitName); err != nil {
		return SystemdResult{}, err
	}
	start := "start"
	if active && !same {
		start = "restart"
	}
	if err := u.require(ctx, start+" "+SystemdUnitName, start, SystemdUnitName); err != nil {
		return SystemdResult{}, err
	}
	if present && !same {
		return u.result(LaunchAgentUpdated), nil
	}
	return u.result(LaunchAgentInstalled), nil
}

// Uninstall stops and disables the service and removes its unit; with neither it reports so.
// Data and agent sessions stay.
// Lock: none. Suspends: each systemctl call.
func (u SystemdUnit) Uninstall(ctx context.Context) (SystemdResult, error) {
	files := u.fileSystem()
	present := files.FileExists(u.UnitPath())
	active := u.systemctl(ctx, "is-active", "--quiet", SystemdUnitName).OK()
	enabled := u.systemctl(ctx, "is-enabled", "--quiet", SystemdUnitName).OK()
	if !present && !active && !enabled {
		return u.result(LaunchAgentAlreadyUninstalled), nil
	}
	if active || enabled {
		if err := u.require(ctx, "disable "+SystemdUnitName, "disable", "--now", SystemdUnitName); err != nil {
			return SystemdResult{}, err
		}
	}
	if present {
		if err := files.RemoveItem(u.UnitPath()); err != nil {
			return SystemdResult{}, launchAgentFailure("Could not remove the systemd unit at %s: %s", u.UnitPath(), err.Error())
		}
	}
	if err := u.require(ctx, "reload the systemd user manager", "daemon-reload"); err != nil {
		return SystemdResult{}, err
	}
	return u.result(LaunchAgentUninstalled), nil
}

// Restart stops the service and starts it again from the registered executable, so a new
// executable at that path takes over. A service that is not installed is a failure.
// Lock: none. Suspends: each systemctl call.
func (u SystemdUnit) Restart(ctx context.Context) (SystemdResult, error) {
	if !u.fileSystem().FileExists(u.UnitPath()) {
		return SystemdResult{}, launchAgentFailure("%s is not installed; run `beltlined install` first", SystemdUnitName)
	}
	if err := u.require(ctx, "restart "+SystemdUnitName, "restart", SystemdUnitName); err != nil {
		return SystemdResult{}, err
	}
	return u.result(LaunchAgentRestarted), nil
}

// desiredUnit is the unit file for the current executable and environment, and the data folder
// its logs go to.
// Lock: none (pure apart from the executable's symlink reads).
func (u SystemdUnit) desiredUnit() ([]byte, string, error) {
	executable, err := registeredExecutable(u.ExecutablePath, u.env("PATH"))
	if err != nil {
		return nil, "", err
	}
	dataFolder := store.StandardPaths(u.env, u.SupportFolder).Root
	if abs, err := filepath.Abs(dataFolder); err == nil {
		dataFolder = abs
	}
	variables := map[string]string{}
	if u.env("BELTLINE_DATA_DIR") != "" {
		variables["BELTLINE_DATA_DIR"] = dataFolder
	}
	for _, key := range []string{"BELTLINE_TMUX_SOCKET", "BELTLINE_VERSION"} {
		if value := u.env(key); value != "" {
			variables[key] = value
		}
	}
	var b strings.Builder
	b.WriteString("# Written by `beltlined install`; `beltlined uninstall` removes it.\n")
	b.WriteString("[Unit]\nDescription=Beltline daemon\n\n")
	b.WriteString("[Service]\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", systemdQuote(executable))
	b.WriteString("Restart=on-failure\nRestartSec=2\n")
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "Environment=%s\n", systemdQuote(key+"="+variables[key]))
	}
	fmt.Fprintf(&b, "StandardOutput=append:%s\n", systemdEscape(filepath.Join(dataFolder, "beltlined.stdout.log")))
	fmt.Fprintf(&b, "StandardError=append:%s\n", systemdEscape(filepath.Join(dataFolder, "beltlined.stderr.log")))
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return []byte(b.String()), dataFolder, nil
}

// systemdEscape escapes the specifier character, so a % in a path is taken literally.
// Lock: none (pure).
func systemdEscape(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

// systemdQuote is value as one double-quoted word in a unit file: backslashes and quotes escaped,
// and % doubled.
// Lock: none (pure).
func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + systemdEscape(value) + `"`
}

// systemctl runs `systemctl --user` with arguments.
// Lock: none. Suspends: the command.
func (u SystemdUnit) systemctl(ctx context.Context, arguments ...string) shell.Result {
	executable := u.SystemctlPath
	if executable == "" {
		executable = DefaultSystemctl
	}
	runner := u.Runner
	if runner == nil {
		runner = shell.ProcessRunner{}
	}
	return runner.Run(ctx, executable, append([]string{"--user"}, arguments...), "", nil)
}

// require runs a systemctl command that must succeed, naming what it was for when it fails.
// Lock: none. Suspends: the command.
func (u SystemdUnit) require(ctx context.Context, what string, arguments ...string) error {
	result := u.systemctl(ctx, arguments...)
	if result.OK() {
		return nil
	}
	return launchAgentFailure("Could not %s: %s. `beltlined install` needs a systemd user manager; "+
		"without one, run beltlined in the foreground instead.", what, strings.TrimSpace(result.Message()))
}

// env reads the environment the unit was built with.
// Lock: none (pure).
func (u SystemdUnit) env(key string) string { return u.Environment[key] }

// fileSystem is the file system, the local one when unset.
// Lock: none (pure).
func (u SystemdUnit) fileSystem() LaunchAgentFileSystem {
	if u.FileSystem == nil {
		return LocalLaunchAgentFileSystem{}
	}
	return u.FileSystem
}

// result is the outcome for action.
// Lock: none (pure).
func (u SystemdUnit) result(action LaunchAgentAction) SystemdResult {
	return SystemdResult{Action: action, UnitPath: u.UnitPath()}
}
