package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// testLaunchctl is the launchctl path the fixtures register with.
const testLaunchctl = "/fake/launchctl"

// launchctlCall is one launchctl command the recording runner saw.
type launchctlCall struct {
	executable string
	arguments  []string
}

// recordingLaunchctl is the shell runner the registration tests use: it answers
// print, bootstrap, bootout and kickstart the way launchd does, and records each call.
type recordingLaunchctl struct {
	mu                       sync.Mutex
	recorded                 []launchctlCall
	loaded                   bool
	bootstrapFailure         string
	bootoutFailure           string
	printFailure             string
	observeFolder            string
	folderExistedAtBootstrap *bool
}

// Run answers one launchctl command.
func (r *recordingLaunchctl) Run(_ context.Context, executable string, arguments []string, _ string, _ *string) shell.Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, launchctlCall{executable: executable, arguments: append([]string(nil), arguments...)})
	switch {
	case len(arguments) > 0 && arguments[0] == "print":
		if r.printFailure != "" {
			return shell.Result{Status: 1, Stderr: r.printFailure}
		}
		if r.loaded {
			return shell.Result{Status: 0, Stdout: "service state = running"}
		}
		return shell.Result{Status: 113, Stderr: fmt.Sprintf(`Could not find service "%s" in domain`, LaunchAgentLabel)}
	case len(arguments) > 0 && arguments[0] == "bootstrap":
		if r.bootstrapFailure != "" {
			return shell.Result{Status: 1, Stderr: r.bootstrapFailure}
		}
		if r.observeFolder != "" {
			_, err := os.Stat(r.observeFolder)
			existed := err == nil
			r.folderExistedAtBootstrap = &existed
		}
		r.loaded = true
		return shell.Result{Status: 0}
	case len(arguments) > 0 && arguments[0] == "bootout":
		if r.bootoutFailure != "" {
			return shell.Result{Status: 1, Stderr: r.bootoutFailure}
		}
		r.loaded = false
		return shell.Result{Status: 0}
	case len(arguments) > 0 && arguments[0] == "kickstart":
		if !r.loaded {
			return shell.Result{Status: 113, Stderr: fmt.Sprintf(`Could not find service "%s" in domain`, LaunchAgentLabel)}
		}
		return shell.Result{Status: 0}
	}
	return shell.Result{Status: 64, Stderr: "unexpected launchctl command"}
}

// calls returns the commands recorded so far.
func (r *recordingLaunchctl) calls() []launchctlCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]launchctlCall(nil), r.recorded...)
}

// isLoaded reports whether the recorded job is loaded.
func (r *recordingLaunchctl) isLoaded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loaded
}

// setLoaded sets whether the job is loaded.
func (r *recordingLaunchctl) setLoaded(loaded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loaded = loaded
}

// callsNamed returns the recorded commands whose first argument is name.
func (r *recordingLaunchctl) callsNamed(name string) []launchctlCall {
	var matching []launchctlCall
	for _, call := range r.calls() {
		if len(call.arguments) > 0 && call.arguments[0] == name {
			matching = append(matching, call)
		}
	}
	return matching
}

// controlledFileSystem is the local file system with injectable write and removal failures.
type controlledFileSystem struct {
	base         LocalLaunchAgentFileSystem
	failWrites   bool
	failRemovals bool
}

// FileExists reports whether path exists.
func (c *controlledFileSystem) FileExists(path string) bool { return c.base.FileExists(path) }

// CreateDirectory makes path.
func (c *controlledFileSystem) CreateDirectory(path string) error {
	return c.base.CreateDirectory(path)
}

// ReadFile reads path.
func (c *controlledFileSystem) ReadFile(path string) ([]byte, error) { return c.base.ReadFile(path) }

// WriteAtomically writes data to path, or fails when writes are injected to fail.
func (c *controlledFileSystem) WriteAtomically(data []byte, path string) error {
	if c.failWrites {
		return errors.New("injected write failure")
	}
	return c.base.WriteAtomically(data, path)
}

// RemoveItem removes path, or fails when removals are injected to fail.
func (c *controlledFileSystem) RemoveItem(path string) error {
	if c.failRemovals {
		return errors.New("injected removal failure")
	}
	return c.base.RemoveItem(path)
}

// launchFixture is one registration's folders, executable, environment and runner.
type launchFixture struct {
	t               *testing.T
	root            string
	launchAgentsDir string
	supportFolder   string
	executable      string
	runner          *recordingLaunchctl
	environment     map[string]string
	fileSystem      LaunchAgentFileSystem
	agent           LaunchAgent
}

// newLaunchFixture makes a fixture with environment and file system fs.
func newLaunchFixture(t *testing.T, environment map[string]string, fs LaunchAgentFileSystem) *launchFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "launch-agent < & spaces")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if fs == nil {
		fs = LocalLaunchAgentFileSystem{}
	}
	f := &launchFixture{
		t:               t,
		root:            root,
		launchAgentsDir: filepath.Join(root, "LaunchAgents < & spaces"),
		supportFolder:   filepath.Join(root, "Application Support"),
		executable:      filepath.Join(root, "< & executable>", "beltlined"),
		runner:          &recordingLaunchctl{},
		environment:     environment,
		fileSystem:      fs,
	}
	f.agent = f.makeAgent(environment, "")
	t.Cleanup(func() {
		if custom := environment["BELTLINE_DATA_DIR"]; custom != "" {
			_ = os.RemoveAll(f.selectedDataFolder())
		}
	})
	return f
}

// selectedDataFolder is the data folder the registration logs to (BELTLINE_DATA_DIR, else support/Beltline).
func (f *launchFixture) selectedDataFolder() string {
	if custom := f.environment["BELTLINE_DATA_DIR"]; custom != "" {
		abs, err := filepath.Abs(custom)
		if err != nil {
			return filepath.Clean(custom)
		}
		return abs
	}
	return filepath.Join(f.supportFolder, "Beltline")
}

// makeAgent builds an agent with environment and executable (the fixture's when empty).
func (f *launchFixture) makeAgent(environment map[string]string, executable string) LaunchAgent {
	if executable == "" {
		executable = f.executable
	}
	return LaunchAgent{
		LaunchAgentsFolder: f.launchAgentsDir,
		UID:                501,
		ExecutablePath:     executable,
		Environment:        environment,
		SupportFolder:      f.supportFolder,
		LaunchctlPath:      testLaunchctl,
		Runner:             f.runner,
		FileSystem:         f.fileSystem,
	}
}

// plist decodes the written LaunchAgent plist.
func (f *launchFixture) plist() map[string]any {
	f.t.Helper()
	data, err := os.ReadFile(f.agent.PlistPath())
	if err != nil {
		f.t.Fatal(err)
	}
	value, err := plistDecode(data)
	if err != nil {
		f.t.Fatal(err)
	}
	dict, ok := value.(map[string]any)
	if !ok {
		f.t.Fatalf("plist is %T, not a dictionary", value)
	}
	return dict
}

// plistStrings returns a plist array of strings.
func plistStrings(value any) []string {
	items, _ := value.([]any)
	var result []string
	for _, item := range items {
		text, _ := item.(string)
		result = append(result, text)
	}
	return result
}

// plistEnvironment returns the plist's EnvironmentVariables as strings, nil when absent.
func plistEnvironment(plist map[string]any) map[string]string {
	dict, ok := plist["EnvironmentVariables"].(map[string]any)
	if !ok {
		return nil
	}
	result := map[string]string{}
	for key, value := range dict {
		result[key], _ = value.(string)
	}
	return result
}

// hasCall reports whether calls includes one with arguments.
func hasCall(calls []launchctlCall, arguments ...string) bool {
	return slices.ContainsFunc(calls, func(call launchctlCall) bool {
		return call.executable == testLaunchctl && reflect.DeepEqual(call.arguments, arguments)
	})
}

// TestInstallWritesOneAbsolutePlistForEachEnvironment checks the plist a first install writes for each
// environment: the program, the run policy, the logs and the environment variables that are set.
func TestInstallWritesOneAbsolutePlistForEachEnvironment(t *testing.T) {
	t.Parallel()
	customData := filepath.Join(t.TempDir(), "beltline custom <& x")
	relativeData := "relative data <& " + strings.ReplaceAll(t.Name(), "/", "-")
	absRelative, _ := filepath.Abs(relativeData)
	cases := []struct {
		name        string
		environment map[string]string
		data        string
		socket      string
	}{
		{"no environment", map[string]string{}, "", ""},
		{"empty values", map[string]string{"BELTLINE_DATA_DIR": "", "BELTLINE_TMUX_SOCKET": ""}, "", ""},
		{"custom folder and socket", map[string]string{"BELTLINE_DATA_DIR": customData, "BELTLINE_TMUX_SOCKET": "private socket <&"}, customData, "private socket <&"},
		{"relative folder", map[string]string{"BELTLINE_DATA_DIR": relativeData}, absRelative, ""},
		{"socket only", map[string]string{"BELTLINE_TMUX_SOCKET": "private"}, "", "private"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newLaunchFixture(t, tc.environment, nil)
			f.runner.observeFolder = f.selectedDataFolder()
			result, err := f.agent.Install(context.Background())
			if err != nil || result.Action != LaunchAgentInstalled {
				t.Fatalf("install %v: %v", result.Action, err)
			}
			plist := f.plist()
			if plist["Label"] != LaunchAgentLabel || plist["RunAtLoad"] != true {
				t.Fatalf("label %v, RunAtLoad %v", plist["Label"], plist["RunAtLoad"])
			}
			if got := plistStrings(plist["ProgramArguments"]); !reflect.DeepEqual(got, []string{f.executable}) {
				t.Fatalf("ProgramArguments %q", got)
			}
			// A crash restarts the daemon; a clean quit (exit 0) stays down.
			if keepAlive, _ := plist["KeepAlive"].(map[string]any); keepAlive["SuccessfulExit"] != false {
				t.Fatalf("KeepAlive %v", plist["KeepAlive"])
			}
			data := f.selectedDataFolder()
			if plist["StandardOutPath"] != filepath.Join(data, "beltlined.stdout.log") || plist["StandardErrorPath"] != filepath.Join(data, "beltlined.stderr.log") {
				t.Fatalf("logs %v %v", plist["StandardOutPath"], plist["StandardErrorPath"])
			}
			if f.runner.folderExistedAtBootstrap == nil || !*f.runner.folderExistedAtBootstrap {
				t.Fatal("the data folder was not created before bootstrap")
			}
			if !hasCall(f.runner.calls(), "bootstrap", "gui/501", f.agent.PlistPath()) {
				t.Fatalf("no bootstrap call in %v", f.runner.calls())
			}
			variables := plistEnvironment(plist)
			if got := variables["BELTLINE_DATA_DIR"]; got != tc.data {
				t.Fatalf("BELTLINE_DATA_DIR %q, want %q", got, tc.data)
			}
			if got := variables["BELTLINE_TMUX_SOCKET"]; got != tc.socket {
				t.Fatalf("BELTLINE_TMUX_SOCKET %q, want %q", got, tc.socket)
			}
		})
	}
}

// TestInstallIsIdempotentRecoversMissingJobAndUpdatesOneLoadedJob checks a repeated install, the recovery of
// a job that is not loaded, and an update that replaces the one loaded job with bootout then bootstrap.
func TestInstallIsIdempotentRecoversMissingJobAndUpdatesOneLoadedJob(t *testing.T) {
	t.Parallel()
	firstData := filepath.Join(t.TempDir(), "beltline-first")
	f := newLaunchFixture(t, map[string]string{"BELTLINE_DATA_DIR": firstData, "BELTLINE_TMUX_SOCKET": "first"}, nil)
	first, err := f.agent.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := f.agent.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Action != LaunchAgentInstalled || repeated.Action != LaunchAgentAlreadyInstalled {
		t.Fatalf("actions %s, %s", first.Action, repeated.Action)
	}
	if !strings.Contains(first.Output(), "Installed "+LaunchAgentLabel+" in gui/501") || !strings.Contains(first.Output(), f.agent.PlistPath()) {
		t.Fatalf("first output %q", first.Output())
	}
	if !strings.Contains(repeated.Output(), "Already installed "+LaunchAgentLabel+" in gui/501") {
		t.Fatalf("repeated output %q", repeated.Output())
	}
	if n := len(f.runner.callsNamed("bootstrap")); n != 1 || len(f.runner.callsNamed("bootout")) != 0 {
		t.Fatalf("%d bootstraps, want 1, and no bootout", n)
	}

	f.runner.setLoaded(false)
	recovered, err := f.agent.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Action != LaunchAgentInstalled || !f.runner.isLoaded() {
		t.Fatalf("recovery action %s, loaded %v", recovered.Action, f.runner.isLoaded())
	}

	nextData := filepath.Join(f.root, "next folder")
	nextExecutable := filepath.Join(f.root, "new executable", "beltlined")
	changed := f.makeAgent(map[string]string{"BELTLINE_DATA_DIR": nextData, "BELTLINE_TMUX_SOCKET": "next"}, nextExecutable)
	updated, err := changed.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plist := f.plist()
	if updated.Action != LaunchAgentUpdated || !strings.Contains(updated.Output(), "Updated "+LaunchAgentLabel+" in gui/501") {
		t.Fatalf("update action %s, output %q", updated.Action, updated.Output())
	}
	if got := plistStrings(plist["ProgramArguments"]); !reflect.DeepEqual(got, []string{nextExecutable}) {
		t.Fatalf("ProgramArguments %q", got)
	}
	if want := map[string]string{"BELTLINE_DATA_DIR": nextData, "BELTLINE_TMUX_SOCKET": "next"}; !reflect.DeepEqual(plistEnvironment(plist), want) {
		t.Fatalf("EnvironmentVariables %v, want %v", plistEnvironment(plist), want)
	}
	var mutations []launchctlCall
	for _, call := range f.runner.calls() {
		if len(call.arguments) > 0 && (call.arguments[0] == "bootout" || call.arguments[0] == "bootstrap") {
			mutations = append(mutations, call)
		}
	}
	mutations = mutations[len(mutations)-2:]
	if !reflect.DeepEqual(mutations[0].arguments, []string{"bootout", "gui/501", f.agent.PlistPath()}) ||
		!reflect.DeepEqual(mutations[1].arguments, []string{"bootstrap", "gui/501", f.agent.PlistPath()}) {
		t.Fatalf("mutations %v, want bootout then bootstrap by the plist", mutations)
	}
	if !f.runner.isLoaded() {
		t.Fatal("the updated job is not loaded")
	}
}

// TestUninstallCoversLoadedAndAbsentStatesAndPreservesOtherFiles checks uninstall of a loaded job, of an
// unloaded one, of a job whose plist is gone, and of nothing; the log, the binary and other agents stay.
func TestUninstallCoversLoadedAndAbsentStatesAndPreservesOtherFiles(t *testing.T) {
	t.Parallel()
	keptData := filepath.Join(t.TempDir(), "beltline-kept")
	f := newLaunchFixture(t, map[string]string{"BELTLINE_DATA_DIR": keptData}, nil)
	for _, folder := range []string{f.selectedDataFolder(), f.launchAgentsDir, filepath.Dir(f.executable)} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(f.selectedDataFolder(), "beltlined.log")
	otherPath := filepath.Join(f.launchAgentsDir, "other.plist")
	writeFixtureFile(t, logPath, "keep log")
	writeFixtureFile(t, f.executable, "keep binary")
	writeFixtureFile(t, otherPath, "unrelated agent")
	ctx := context.Background()
	uninstall := func() LaunchAgentResult {
		t.Helper()
		result, err := f.agent.Uninstall(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	if _, err := f.agent.Install(ctx); err != nil {
		t.Fatal(err)
	}
	loaded := uninstall()
	if loaded.Action != LaunchAgentUninstalled || !strings.Contains(loaded.Output(), "Uninstalled "+LaunchAgentLabel+" in gui/501") {
		t.Fatalf("loaded uninstall %s %q", loaded.Action, loaded.Output())
	}
	if _, err := os.Stat(f.agent.PlistPath()); err == nil {
		t.Fatal("the plist survived uninstall")
	}
	if !hasCall(f.runner.calls(), "bootout", "gui/501", f.agent.PlistPath()) {
		t.Fatal("no bootout by plist")
	}

	if _, err := f.agent.Install(ctx); err != nil {
		t.Fatal(err)
	}
	f.runner.setLoaded(false)
	if result := uninstall(); result.Action != LaunchAgentUninstalled || len(f.runner.callsNamed("bootout")) != 1 {
		t.Fatalf("unloaded uninstall %s", result.Action)
	}

	if _, err := f.agent.Install(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(f.agent.PlistPath()); err != nil {
		t.Fatal(err)
	}
	if result := uninstall(); result.Action != LaunchAgentUninstalled {
		t.Fatalf("missing plist uninstall %s", result.Action)
	}
	if !hasCall(f.runner.calls(), "bootout", "gui/501/"+LaunchAgentLabel) {
		t.Fatal("no bootout by service target")
	}

	absent := uninstall()
	if absent.Action != LaunchAgentAlreadyUninstalled || uninstall().Action != LaunchAgentAlreadyUninstalled ||
		!strings.Contains(absent.Output(), "Already uninstalled "+LaunchAgentLabel+" in gui/501") {
		t.Fatalf("absent uninstall %s %q", absent.Action, absent.Output())
	}
	assertFixtureFile(t, logPath, "keep log")
	assertFixtureFile(t, f.executable, "keep binary")
	assertFixtureFile(t, otherPath, "unrelated agent")
}

// writeFixtureFile writes text to path.
func writeFixtureFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertFixtureFile checks that path holds text.
func assertFixtureFile(t *testing.T, path, text string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != text {
		t.Fatalf("%s holds %q, want %q", path, data, text)
	}
}

// TestFailuresLeaveARetryableState checks that each failure is reported with its cause and leaves a state a
// retry can finish: a failed bootstrap keeps the plist, a failed bootout keeps the old plist, a failed
// status check or removal keeps the job or the plist, and a retry then succeeds.
func TestFailuresLeaveARetryableState(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t, map[string]string{}, nil)
	ctx := context.Background()
	f.runner.bootstrapFailure = "bootstrap denied"
	if _, err := f.agent.Install(ctx); err == nil || !strings.Contains(err.Error(), "Could not bootstrap") || !strings.Contains(err.Error(), "bootstrap denied") {
		t.Fatalf("bootstrap failure %v", err)
	}
	if _, statErr := os.Stat(f.agent.PlistPath()); statErr != nil {
		t.Fatal("the plist was not left for a retry")
	}
	f.runner.bootstrapFailure = ""
	if result, err := f.agent.Install(ctx); err != nil || result.Action != LaunchAgentInstalled {
		t.Fatalf("retry %v %v", result.Action, err)
	}

	changed := f.makeAgent(map[string]string{"BELTLINE_TMUX_SOCKET": "changed"}, "")
	f.runner.bootoutFailure = "Operation not permitted"
	if _, err := changed.Install(ctx); err == nil || !strings.Contains(err.Error(), "Could not unload") || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("bootout failure %v", err)
	}
	if env := plistEnvironment(f.plist()); env != nil {
		t.Fatalf("the plist changed after a failed bootout: %v", env)
	}

	f.runner.printFailure = "Operation not permitted"
	if _, err := f.makeAgent(map[string]string{}, "").Uninstall(ctx); err == nil || !strings.Contains(err.Error(), "Could not check") {
		t.Fatalf("print failure %v", err)
	}

	f.runner.printFailure = ""
	if _, err := f.agent.Uninstall(ctx); err == nil || !strings.Contains(err.Error(), "Could not unload") || !strings.Contains(err.Error(), "Operation not permitted") {
		t.Fatalf("uninstall bootout failure %v", err)
	}
	if _, statErr := os.Stat(f.agent.PlistPath()); statErr != nil {
		t.Fatal("the plist was removed although unloading failed")
	}
	f.runner.bootoutFailure = ""
	if result, err := f.agent.Uninstall(ctx); err != nil || result.Action != LaunchAgentUninstalled {
		t.Fatalf("retry uninstall %v %v", result.Action, err)
	}
}

// TestWriteAndRemovalFailuresRemainVisibleAndRetryable checks that a failed plist write starts no job, and
// that a failed plist removal keeps the plist and retries cleanly.
func TestWriteAndRemovalFailuresRemainVisibleAndRetryable(t *testing.T) {
	t.Parallel()
	files := &controlledFileSystem{}
	f := newLaunchFixture(t, map[string]string{}, files)
	ctx := context.Background()
	files.failWrites = true
	if _, err := f.agent.Install(ctx); err == nil || !strings.Contains(err.Error(), "Could not write the LaunchAgent plist") || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("write failure %v", err)
	}
	if _, statErr := os.Stat(f.agent.PlistPath()); statErr == nil {
		t.Fatal("a plist was written despite the failure")
	}
	if n := len(f.runner.callsNamed("bootstrap")); n != 0 {
		t.Fatalf("%d bootstraps after a failed write", n)
	}

	files.failWrites = false
	if _, err := f.agent.Install(ctx); err != nil {
		t.Fatal(err)
	}
	files.failRemovals = true
	if _, err := f.agent.Uninstall(ctx); err == nil || !strings.Contains(err.Error(), "Could not remove the LaunchAgent plist") || !strings.Contains(err.Error(), "injected removal failure") {
		t.Fatalf("removal failure %v", err)
	}
	if _, statErr := os.Stat(f.agent.PlistPath()); statErr != nil {
		t.Fatal("the plist vanished although removal failed")
	}
	if f.runner.isLoaded() {
		t.Fatal("the job is still loaded after the bootout")
	}
	files.failRemovals = false
	if result, err := f.agent.Uninstall(ctx); err != nil || result.Action != LaunchAgentUninstalled {
		t.Fatalf("retry %v %v", result.Action, err)
	}
	if _, statErr := os.Stat(f.agent.PlistPath()); statErr == nil {
		t.Fatal("the plist survived the retry")
	}
}

// TestCommandDispatchKeepsMaintenanceOutOfDaemonSetupAndRejectsInvalidArguments
func TestCommandDispatchKeepsMaintenanceOutOfDaemonSetupAndRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	f := newLaunchFixture(t, map[string]string{}, nil)
	if err := os.MkdirAll(f.selectedDataFolder(), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, filepath.Join(f.selectedDataFolder(), "daemon.lock"), "already owned")

	run := func(arguments []string) (int, string, string) {
		output := &textCapture{}
		errs := &textCapture{}
		status := RunCommand(Options{
			GOOS:               "darwin",
			Arguments:          arguments,
			ExecutablePath:     f.executable,
			Environment:        f.environment,
			SupportFolder:      f.supportFolder,
			LaunchAgentsFolder: f.launchAgentsDir,
			UID:                501,
			Runner:             f.runner,
			RunDaemon: func(Mode, bool, shell.Runner) error {
				t.Errorf("maintenance command %v started the daemon", arguments)
				return nil
			},
			Stdout: output.Append,
			Stderr: func(text string) { errs.Append("beltlined: " + text + "\n") },
		})
		return status, output.Value(), errs.Value()
	}

	status, output, errs := run([]string{"install"})
	if status != 0 {
		t.Fatalf("install status %d", status)
	}
	if !strings.Contains(output, "Installed "+LaunchAgentLabel+" in gui/501") || !strings.Contains(output, f.agent.PlistPath()) {
		t.Fatalf("install output %q", output)
	}
	if errs != "" {
		t.Fatalf("install errors %q", errs)
	}
	if status, output, _ := run([]string{"install"}); status != 0 || !strings.Contains(output, "Already installed "+LaunchAgentLabel) {
		t.Fatalf("repeat install %d %q", status, output)
	}

	for _, arguments := range [][]string{
		{"install", "--demo"},
		{"install", "--check-library"},
		{"uninstall", "extra"},
		{"--demo", "install"},
		{"--check-library", "--demo"},
		{"--check-library", f.root, "install"},
	} {
		before := len(f.runner.calls())
		if status, _, _ := run(arguments); status != 2 {
			t.Fatalf("%v: status %d, want 2", arguments, status)
		}
		if after := len(f.runner.calls()); after != before {
			t.Fatalf("%v: %d launchctl calls for a rejected command", arguments, after-before)
		}
	}

	status, output, _ = run([]string{"uninstall"})
	if status != 0 || !strings.Contains(output, "Uninstalled "+LaunchAgentLabel) {
		t.Fatalf("uninstall %d %q", status, output)
	}
	if status, output, _ := run([]string{"uninstall"}); status != 0 || !strings.Contains(output, "Already uninstalled "+LaunchAgentLabel) {
		t.Fatalf("repeat uninstall %d %q", status, output)
	}

	f.runner.printFailure = "Operation not permitted"
	status, output, errs = run([]string{"uninstall"})
	if status == 0 {
		t.Fatal("a failed uninstall returned 0")
	}
	if !strings.HasPrefix(errs, "beltlined: Could not check") {
		t.Fatalf("errors %q", errs)
	}
	if output != "" {
		t.Fatalf("output %q for a failed uninstall", output)
	}
}
