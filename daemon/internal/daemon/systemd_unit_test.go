package daemon

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// recordingSystemctl answers `systemctl --user` the way the user manager does for one unit, and
// records each call.
type recordingSystemctl struct {
	mu       sync.Mutex
	recorded [][]string
	enabled  bool
	active   bool
	failing  string
}

// Run answers one systemctl command.
func (r *recordingSystemctl) Run(_ context.Context, executable string, arguments []string, _ string, _ *string) shell.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, append([]string{executable}, arguments...))
	if len(arguments) < 2 || arguments[0] != "--user" {
		return shell.Result{Status: 64, Stderr: "unexpected systemctl command"}
	}
	command := arguments[1]
	if command == r.failing {
		return shell.Result{Status: 1, Stderr: "Failed to connect to bus: No medium found"}
	}
	switch command {
	case "is-active":
		if r.active {
			return shell.Result{}
		}
		return shell.Result{Status: 3}
	case "is-enabled":
		if r.enabled {
			return shell.Result{}
		}
		return shell.Result{Status: 1}
	case "enable":
		r.enabled = true
	case "start", "restart":
		r.active = true
	case "disable":
		r.enabled, r.active = false, false
	case "daemon-reload":
	default:
		return shell.Result{Status: 64, Stderr: "unexpected systemctl command"}
	}
	return shell.Result{}
}

// commands returns the subcommands recorded so far, after `--user`.
func (r *recordingSystemctl) commands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, call := range r.recorded {
		out = append(out, strings.Join(call[2:], " "))
	}
	return out
}

// clear forgets the recorded calls.
func (r *recordingSystemctl) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = nil
}

// newSystemdFixture is a unit in a temporary home, with an executable whose path has a space and a %.
func newSystemdFixture(t *testing.T, environment map[string]string) (SystemdUnit, *recordingSystemctl, string) {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "bin 100%", "beltlined")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, executable, "#!/bin/sh\n")
	systemctl := &recordingSystemctl{}
	unit := SystemdUnit{
		UnitsFolder:    SystemdUnitsFolder(filepath.Join(root, "home"), func(string) string { return "" }),
		ExecutablePath: executable,
		Environment:    environment,
		SupportFolder:  filepath.Join(root, "share"),
		SystemctlPath:  "/fake/systemctl",
		Runner:         systemctl,
	}
	return unit, systemctl, resolveSymlinks(executable)
}

func TestSystemdInstallWritesTheUnitEnablesAndStartsItOnce(t *testing.T) {
	t.Parallel()
	unit, systemctl, executable := newSystemdFixture(t, map[string]string{"BELTLINE_TMUX_SOCKET": "work"})
	result, err := unit.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != LaunchAgentInstalled || !strings.Contains(result.Output(), "loginctl enable-linger") {
		t.Fatalf("result %+v: %s", result, result.Output())
	}
	if !strings.HasSuffix(unit.UnitPath(), filepath.Join("home", ".config", "systemd", "user", "beltline.service")) {
		t.Fatalf("unit path %s", unit.UnitPath())
	}
	data, err := os.ReadFile(unit.UnitPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	dataFolder := filepath.Join(filepath.Dir(unit.SupportFolder), "share", "Beltline")
	for _, line := range []string{
		`ExecStart="` + strings.ReplaceAll(executable, "%", "%%") + `"`,
		"Restart=on-failure",
		`Environment="BELTLINE_TMUX_SOCKET=work"`,
		"StandardOutput=append:" + filepath.Join(dataFolder, "beltlined.stdout.log"),
		"WantedBy=default.target",
	} {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("unit lacks %q:\n%s", line, text)
		}
	}
	if strings.Contains(text, "BELTLINE_DATA_DIR") {
		t.Errorf("unit names a data folder that was not set:\n%s", text)
	}
	if want := []string{"is-active --quiet beltline.service", "is-enabled --quiet beltline.service", "daemon-reload",
		"enable beltline.service", "start beltline.service"}; !reflect.DeepEqual(systemctl.commands(), want) {
		t.Fatalf("commands %q, want %q", systemctl.commands(), want)
	}

	systemctl.clear()
	again, err := unit.Install(context.Background())
	if err != nil || again.Action != LaunchAgentAlreadyInstalled {
		t.Fatalf("repeat install %+v %v", again, err)
	}
	if len(systemctl.commands()) != 2 {
		t.Fatalf("a repeat install ran %q", systemctl.commands())
	}
}

func TestSystemdInstallRestartsARunningServiceWhoseUnitChanged(t *testing.T) {
	t.Parallel()
	unit, systemctl, _ := newSystemdFixture(t, map[string]string{})
	if _, err := unit.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	unit.Environment = map[string]string{"BELTLINE_TMUX_SOCKET": "other"}
	systemctl.clear()
	result, err := unit.Install(context.Background())
	if err != nil || result.Action != LaunchAgentUpdated {
		t.Fatalf("update %+v %v", result, err)
	}
	if commands := systemctl.commands(); commands[len(commands)-1] != "restart beltline.service" {
		t.Fatalf("commands %q", commands)
	}
}

func TestSystemdUninstallStopsAndRemovesTheUnitAndRestartNeedsOne(t *testing.T) {
	t.Parallel()
	unit, systemctl, _ := newSystemdFixture(t, map[string]string{})
	if _, err := unit.Restart(context.Background()); err == nil || !strings.Contains(err.Error(), "beltlined install") {
		t.Fatalf("restart without a unit: %v", err)
	}
	if result, err := unit.Uninstall(context.Background()); err != nil || result.Action != LaunchAgentAlreadyUninstalled {
		t.Fatalf("uninstall without a unit %+v %v", result, err)
	}
	if _, err := unit.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, err := unit.Restart(context.Background()); err != nil || result.Action != LaunchAgentRestarted {
		t.Fatalf("restart %+v %v", result, err)
	}
	systemctl.clear()
	result, err := unit.Uninstall(context.Background())
	if err != nil || result.Action != LaunchAgentUninstalled {
		t.Fatalf("uninstall %+v %v", result, err)
	}
	if !reflect.DeepEqual(systemctl.commands()[2:], []string{"disable --now beltline.service", "daemon-reload"}) {
		t.Fatalf("commands %q", systemctl.commands())
	}
	if _, err := os.Stat(unit.UnitPath()); !os.IsNotExist(err) {
		t.Fatalf("the unit is still there: %v", err)
	}
}

func TestSystemdInstallWithoutAUserManagerSaysWhatToDoInstead(t *testing.T) {
	t.Parallel()
	unit, systemctl, _ := newSystemdFixture(t, map[string]string{})
	systemctl.failing = "daemon-reload"
	_, err := unit.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "No medium found") || !strings.Contains(err.Error(), "foreground") {
		t.Fatalf("error %v", err)
	}
}

func TestSystemdQuotingKeepsSpacesQuotesBackslashesAndSpecifiersLiteral(t *testing.T) {
	t.Parallel()
	if got, want := systemdQuote(`/a b/"c"\d%e`), `"/a b/\"c\"\\d%%e"`; got != want {
		t.Fatalf("quote = %s, want %s", got, want)
	}
	if got := SystemdUnitsFolder("/home/u", func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return "/cfg"
		}
		return ""
	}); got != "/cfg/systemd/user" {
		t.Fatalf("units folder %s", got)
	}
}

func TestCommandDispatchUsesSystemdOffMacOS(t *testing.T) {
	t.Parallel()
	unit, systemctl, _ := newSystemdFixture(t, map[string]string{})
	output := &textCapture{}
	status := RunCommand(Options{
		GOOS:               "linux",
		Arguments:          []string{"install"},
		ExecutablePath:     unit.ExecutablePath,
		Environment:        map[string]string{},
		SupportFolder:      unit.SupportFolder,
		SystemdUnitsFolder: unit.UnitsFolder,
		Runner:             systemctl,
		Stdout:             output.Append,
		Stderr:             func(text string) { t.Errorf("stderr %s", text) },
	})
	if status != 0 || !strings.HasPrefix(output.Value(), "Installed beltline.service in the systemd user manager") {
		t.Fatalf("status %d output %q", status, output.Value())
	}
}
