// The arguments and their exit statuses. Every effect is injected, so the argument rules run in tests
// without starting a daemon or touching the LaunchAgents folder.

package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
)

// usageText is the usage line every bad combination of arguments prints.
const usageText = "Usage: beltlined [--demo] [--open] | open [--demo-folder <folder>] | --check-library [folder] | install | uninstall | restart | --version"

// Options are the command's inputs and the effects it may cause.
type Options struct {
	Arguments          []string
	ExecutablePath     string
	Environment        map[string]string
	SupportFolder      string
	LaunchAgentsFolder string
	// SystemdUnitsFolder is the user's systemd units folder, used on Linux.
	SystemdUnitsFolder string
	// GOOS picks the service manager install, uninstall and restart use: launchd on macOS ("darwin"),
	// systemd elsewhere. Empty means this system.
	GOOS   string
	UID    int
	Runner shell.Runner
	// RunDaemon starts the daemon in mode; it returns when the daemon has stopped.
	RunDaemon func(mode Mode, openAfterStart bool, runner shell.Runner) error
	// OpenDaemon signs a browser in to the daemon in folder; nil means DaemonOpen.
	OpenDaemon func(folder string, runner shell.Runner) error
	// Stdout and Stderr receive the command's text; Stderr gets the bare message.
	Stdout func(string)
	Stderr func(string)
}

// RunCommand executes the arguments and returns the exit status.
// Lock: none. Suspends: the daemon, the open command and the LaunchAgent calls.
func RunCommand(o Options) int {
	runner := o.Runner
	if runner == nil {
		runner = shell.ProcessRunner{}
	}
	if slices.Equal(o.Arguments, []string{"--version"}) {
		o.Stdout(fmt.Sprintf("beltlined %s (API v%d)\n", BuildVersion(), launch.VersionCurrent))
		return 0
	}
	if status, handled := runLibraryCheck(o); handled {
		return status
	}
	if slices.Equal(o.Arguments, []string{"install"}) || slices.Equal(o.Arguments, []string{"uninstall"}) ||
		slices.Equal(o.Arguments, []string{"restart"}) {
		return runLaunchAgent(o, runner)
	}
	if len(o.Arguments) > 0 && o.Arguments[0] == "open" {
		return runOpen(o, runner)
	}
	return runDaemon(o, runner)
}

// runLibraryCheck runs --check-library. handled is false when the arguments do
// not name a library check the command runs, so the rest of the parse applies.
// Lock: none. Suspends: none.
func runLibraryCheck(o Options) (status int, handled bool) {
	args := o.Arguments
	if !slices.Contains(args, launch.LibraryCheckCommandFlag) {
		return 0, false
	}
	hasFolder := len(args) == 2 && !strings.HasPrefix(args[1], "-")
	if len(args) == 0 || args[0] != launch.LibraryCheckCommandFlag || len(args) > 2 || (len(args) != 1 && !hasFolder) {
		o.Stderr(usageText)
		return 2, true
	}
	standard := func() store.Paths {
		return store.StandardPaths(func(key string) string { return o.Environment[key] }, o.SupportFolder)
	}
	result := launch.LibraryCheckCommandRun(args, standard)
	if result == nil {
		return 0, false
	}
	o.Stdout(result.Output)
	return int(result.Status), true
}

// runLaunchAgent runs install, uninstall and restart: the LaunchAgent registration on macOS, the
// systemd user service elsewhere.
// Lock: none. Suspends: the LaunchAgent or systemctl calls.
func runLaunchAgent(o Options, runner shell.Runner) int {
	goos := o.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "darwin" {
		return runSystemdUnit(o, runner)
	}
	agent := LaunchAgent{
		LaunchAgentsFolder: o.LaunchAgentsFolder,
		UID:                o.UID,
		ExecutablePath:     o.ExecutablePath,
		Environment:        o.Environment,
		SupportFolder:      o.SupportFolder,
		LaunchctlPath:      DefaultLaunchctlPath,
		Runner:             runner,
	}
	var (
		result LaunchAgentResult
		err    error
	)
	switch o.Arguments[0] {
	case "install":
		result, err = agent.Install(context.Background())
	case "restart":
		result, err = agent.Restart(context.Background())
	default:
		result, err = agent.Uninstall(context.Background())
	}
	if err != nil {
		o.Stderr(err.Error())
		return 1
	}
	o.Stdout(result.Output())
	return 0
}

// runSystemdUnit runs install, uninstall and restart for the systemd user service.
// Lock: none. Suspends: the systemctl calls.
func runSystemdUnit(o Options, runner shell.Runner) int {
	unit := SystemdUnit{
		UnitsFolder:    o.SystemdUnitsFolder,
		ExecutablePath: o.ExecutablePath,
		Environment:    o.Environment,
		SupportFolder:  o.SupportFolder,
		Runner:         runner,
	}
	var (
		result SystemdResult
		err    error
	)
	switch o.Arguments[0] {
	case "install":
		result, err = unit.Install(context.Background())
	case "restart":
		result, err = unit.Restart(context.Background())
	default:
		result, err = unit.Uninstall(context.Background())
	}
	if err != nil {
		o.Stderr(err.Error())
		return 1
	}
	o.Stdout(result.Output())
	return 0
}

// runOpen runs `open`, which signs a browser in to the running daemon.
// Lock: none. Suspends: the open command.
func runOpen(o Options, runner shell.Runner) int {
	args := o.Arguments
	openDaemon := o.OpenDaemon
	if openDaemon == nil {
		openDaemon = func(folder string, runner shell.Runner) error {
			return DaemonOpen{Runner: runner, Discovery: newDaemonDiscovery()}.Run(context.Background(), folder)
		}
	}
	var folder string
	switch {
	case slices.Equal(args, []string{"open"}):
		folder = DaemonOpenLiveFolder(o.Environment, o.SupportFolder)
	case len(args) == 3 && args[1] == "--demo-folder" && args[2] != "":
		folder = absoluteFolder(args[2])
	default:
		o.Stderr(usageText)
		return 2
	}
	if err := openDaemon(folder, runner); err != nil {
		o.Stderr(err.Error())
		return 1
	}
	return 0
}

// runDaemon starts the daemon in the mode its flags select.
// Lock: none. Suspends: the daemon.
func runDaemon(o Options, runner shell.Runner) int {
	isDemo := false
	shouldOpen := false
	for _, argument := range o.Arguments {
		switch {
		case argument == "--demo" && !isDemo:
			isDemo = true
		case argument == "--open" && !shouldOpen:
			shouldOpen = true
		default:
			o.Stderr(usageText)
			return 2
		}
	}
	// BELTLINE_DEMO=1 selected demo mode in the app too.
	mode := ModeLive
	if isDemo || o.Environment["BELTLINE_DEMO"] == "1" {
		mode = ModeDemo
	}
	if err := o.RunDaemon(mode, shouldOpen, runner); err != nil {
		o.Stderr(err.Error())
		return 1
	}
	return 0
}

// absoluteFolder is a folder path made absolute and cleaned.
// Lock: none (pure).
func absoluteFolder(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
