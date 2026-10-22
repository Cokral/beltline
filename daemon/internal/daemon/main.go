// The daemon's entry point: the production wiring of RunCommand (the real support and LaunchAgents
// folders, the process environment, the daemon's stdout and stderr).

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// Main runs beltlined with args (the arguments after the program name) and returns its exit status.
// Lock: none.
func Main(args []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	stdout := func(text string) { _, _ = os.Stdout.WriteString(text) }
	stderr := func(text string) { _, _ = os.Stderr.WriteString("beltlined: " + text + "\n") }
	runner := shell.ProcessRunner{}
	return RunCommand(Options{
		Arguments:          args,
		ExecutablePath:     CurrentExecutable(),
		Environment:        environmentMap(os.Environ()),
		SupportFolder:      supportFolderOrTemp(),
		LaunchAgentsFolder: filepath.Join(home, "Library", "LaunchAgents"),
		SystemdUnitsFolder: SystemdUnitsFolder(home, os.Getenv),
		UID:                os.Getuid(),
		Runner:             runner,
		RunDaemon: func(mode Mode, openAfterStart bool, runner shell.Runner) error {
			return runProductionDaemon(mode, openAfterStart, runner, stdout, stderr)
		},
		Stdout: stdout,
		Stderr: stderr,
	})
}

// runProductionDaemon starts the daemon with the executable's hooks: the ready line
// and the browser on stdout and stderr, and a desktop notification for each notification
// (macOS's, or notify-send's on Linux).
// Lock: none. Suspends: the daemon's run.
func runProductionDaemon(mode Mode, openAfterStart bool, runner shell.Runner, stdout, stderr func(string)) error {
	hooks := NewHooks()
	hooks.ReadyLine = func(port int) {
		stdout(fmt.Sprintf("Beltline ready at http://127.0.0.1:%d/\n", port))
	}
	hooks.OpenDaemon = func(root string) error {
		return DaemonOpen{Runner: runner, Discovery: newDaemonDiscovery()}.Run(context.Background(), root)
	}
	hooks.OpenFailed = func(message string) { stderr("Could not open browser: " + message) }
	hooks.DesktopNotifier = func(title, body string) {
		if runtime.GOOS == "darwin" {
			go MacNotification{Runner: runner}.Post(context.Background(), title, body)
		} else {
			go LinuxNotification{Runner: runner}.Post(context.Background(), title, body)
		}
	}
	return NewDaemon(mode, nil, openAfterStart, hooks).Run()
}

// environmentMap turns KEY=VALUE pairs into a map; later pairs win.
// Lock: none (pure).
func environmentMap(pairs []string) map[string]string {
	environment := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if found {
			environment[key] = value
		}
	}
	return environment
}
