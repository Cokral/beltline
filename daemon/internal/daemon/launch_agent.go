// Registers the existing daemon executable as a per-user LaunchAgent. The runner and the file system
// are injected, so registration tests never touch the user's LaunchAgents folder or call launchctl.

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// LaunchAgentLabel is the job's label.
const LaunchAgentLabel = "com.beltline.daemon"

// DefaultLaunchctlPath is the launchctl the registration calls.
const DefaultLaunchctlPath = "/bin/launchctl"

// LaunchAgentAction is what an install or uninstall did.
type LaunchAgentAction string

const (
	LaunchAgentInstalled          LaunchAgentAction = "installed"
	LaunchAgentUpdated            LaunchAgentAction = "updated"
	LaunchAgentAlreadyInstalled   LaunchAgentAction = "alreadyInstalled"
	LaunchAgentUninstalled        LaunchAgentAction = "uninstalled"
	LaunchAgentAlreadyUninstalled LaunchAgentAction = "alreadyUninstalled"
	LaunchAgentRestarted          LaunchAgentAction = "restarted"
)

// Title is the word the command prints for the action.
// Lock: none (pure).
func (a LaunchAgentAction) Title() string {
	switch a {
	case LaunchAgentInstalled:
		return "Installed"
	case LaunchAgentUpdated:
		return "Updated"
	case LaunchAgentAlreadyInstalled:
		return "Already installed"
	case LaunchAgentUninstalled:
		return "Uninstalled"
	case LaunchAgentAlreadyUninstalled:
		return "Already uninstalled"
	case LaunchAgentRestarted:
		return "Restarted"
	}
	return string(a)
}

// LaunchAgentResult is the outcome of an install or uninstall.
type LaunchAgentResult struct {
	Action    LaunchAgentAction
	Label     string
	Domain    string
	PlistPath string
}

// Output is what the command prints: the action, the label and the domain, then the plist's path.
// Lock: none (pure).
func (r LaunchAgentResult) Output() string {
	return fmt.Sprintf("%s %s in %s\nPlist: %s\n", r.Action.Title(), r.Label, r.Domain, r.PlistPath)
}

// LaunchAgentFileSystem is the file access the registration makes.
type LaunchAgentFileSystem interface {
	FileExists(path string) bool
	CreateDirectory(path string) error
	ReadFile(path string) ([]byte, error)
	WriteAtomically(data []byte, path string) error
	RemoveItem(path string) error
}

// LocalLaunchAgentFileSystem is the real file system.
type LocalLaunchAgentFileSystem struct{}

// FileExists reports whether path exists.
// Lock: none.
func (LocalLaunchAgentFileSystem) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CreateDirectory makes path and its parents.
// Lock: none.
func (LocalLaunchAgentFileSystem) CreateDirectory(path string) error {
	return os.MkdirAll(path, 0o755)
}

// ReadFile reads path.
// Lock: none.
func (LocalLaunchAgentFileSystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteAtomically writes data to a temporary file beside path, renames it over
// path, and sets the plist's mode to 0644 whatever the umask.
// Lock: none.
func (LocalLaunchAgentFileSystem) WriteAtomically(data []byte, path string) error {
	if err := store.WriteFileAtomic(path, data); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

// RemoveItem deletes path.
// Lock: none.
func (LocalLaunchAgentFileSystem) RemoveItem(path string) error {
	return os.Remove(path)
}

// LaunchAgent registers the daemon executable as a per-user LaunchAgent.
type LaunchAgent struct {
	LaunchAgentsFolder string
	UID                int
	ExecutablePath     string
	Environment        map[string]string
	SupportFolder      string
	LaunchctlPath      string
	Runner             shell.Runner
	FileSystem         LaunchAgentFileSystem
}

// LaunchAgentFailure is a registration that could not be completed; its text is the message the command prints.
type LaunchAgentFailure struct {
	Message string
}

// Error returns the message.
// Lock: none (pure).
func (f LaunchAgentFailure) Error() string { return f.Message }

// launchAgentFailure builds a failure with a formatted message.
// Lock: none (pure).
func launchAgentFailure(format string, args ...any) error {
	return LaunchAgentFailure{Message: fmt.Sprintf(format, args...)}
}

// launchAgentDesired is the plist to write and the data folder it logs to.
type launchAgentDesired struct {
	dataFolder string
	data       []byte
}

// PlistPath is the plist the LaunchAgent is written to.
// Lock: none (pure).
func (a LaunchAgent) PlistPath() string {
	return filepath.Join(a.launchAgentsFolder(), LaunchAgentLabel+".plist")
}

// Domain is the launchd domain of the user.
// Lock: none (pure).
func (a LaunchAgent) Domain() string { return fmt.Sprintf("gui/%d", a.UID) }

// ServiceTarget is the job's service target in the domain.
// Lock: none (pure).
func (a LaunchAgent) ServiceTarget() string { return a.Domain() + "/" + LaunchAgentLabel }

// launchAgentsFolder is the LaunchAgents folder, absolute and clean.
// Lock: none (pure).
func (a LaunchAgent) launchAgentsFolder() string {
	if abs, err := filepath.Abs(a.LaunchAgentsFolder); err == nil {
		return abs
	}
	return filepath.Clean(a.LaunchAgentsFolder)
}

// launchctl is the launchctl path, the default when unset.
// Lock: none (pure).
func (a LaunchAgent) launchctl() string {
	if a.LaunchctlPath == "" {
		return DefaultLaunchctlPath
	}
	return a.LaunchctlPath
}

// fileSystem is the file system, the local one when unset.
// Lock: none (pure).
func (a LaunchAgent) fileSystem() LaunchAgentFileSystem {
	if a.FileSystem == nil {
		return LocalLaunchAgentFileSystem{}
	}
	return a.FileSystem
}

// Install registers the job and loads it, or reports that it is already registered
// with the same configuration.
// Lock: none. Suspends: each launchctl call, run without any lock.
func (a LaunchAgent) Install(ctx context.Context) (LaunchAgentResult, error) {
	desired, err := a.desiredPlist()
	if err != nil {
		return LaunchAgentResult{}, err
	}
	files := a.fileSystem()
	plistPresent := files.FileExists(a.PlistPath())
	var existing []byte
	if plistPresent {
		existing, err = files.ReadFile(a.PlistPath())
		if err != nil {
			return LaunchAgentResult{}, launchAgentFailure("Could not read the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
		}
	}
	sameConfiguration := false
	if existing != nil {
		same, err := plistsMatch(existing, desired.data)
		if err != nil {
			return LaunchAgentResult{}, launchAgentFailure("Could not read the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
		}
		sameConfiguration = same
	}

	loaded, err := a.isLoaded(ctx)
	if err != nil {
		return LaunchAgentResult{}, err
	}
	if sameConfiguration && loaded {
		return a.result(LaunchAgentAlreadyInstalled), nil
	}
	if loaded {
		if err := a.bootout(ctx, plistPresent); err != nil {
			return LaunchAgentResult{}, err
		}
	}

	if err := files.CreateDirectory(desired.dataFolder); err != nil {
		return LaunchAgentResult{}, launchAgentFailure("Could not write the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
	}
	if err := files.CreateDirectory(a.launchAgentsFolder()); err != nil {
		return LaunchAgentResult{}, launchAgentFailure("Could not write the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
	}
	if err := files.WriteAtomically(desired.data, a.PlistPath()); err != nil {
		return LaunchAgentResult{}, launchAgentFailure("Could not write the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
	}

	bootstrap := a.runner().Run(ctx, a.launchctl(), []string{"bootstrap", a.Domain(), a.PlistPath()}, "", nil)
	if !bootstrap.OK() {
		return LaunchAgentResult{}, launchAgentFailure("Could not bootstrap %s in %s from %s: %s",
			LaunchAgentLabel, a.Domain(), a.PlistPath(), bootstrap.Message())
	}
	if plistPresent && !sameConfiguration {
		return a.result(LaunchAgentUpdated), nil
	}
	return a.result(LaunchAgentInstalled), nil
}

// Restart stops the loaded job and starts it again from the registered executable, so a new
// executable at that path (after `make install`) takes over. A job that is not loaded is a failure.
// Lock: none. Suspends: each launchctl call.
func (a LaunchAgent) Restart(ctx context.Context) (LaunchAgentResult, error) {
	loaded, err := a.isLoaded(ctx)
	if err != nil {
		return LaunchAgentResult{}, err
	}
	if !loaded {
		return LaunchAgentResult{}, launchAgentFailure("%s is not loaded in %s; run `beltlined install` first", LaunchAgentLabel, a.Domain())
	}
	result := a.runner().Run(ctx, a.launchctl(), []string{"kickstart", "-k", a.ServiceTarget()}, "", nil)
	if !result.OK() {
		return LaunchAgentResult{}, launchAgentFailure("Could not restart %s in %s: %s", LaunchAgentLabel, a.Domain(), result.Message())
	}
	return a.result(LaunchAgentRestarted), nil
}

// Uninstall unloads the job and removes its plist; absent both, it reports so.
// Lock: none. Suspends: each launchctl call.
func (a LaunchAgent) Uninstall(ctx context.Context) (LaunchAgentResult, error) {
	files := a.fileSystem()
	plistPresent := files.FileExists(a.PlistPath())
	loaded, err := a.isLoaded(ctx)
	if err != nil {
		return LaunchAgentResult{}, err
	}
	if !plistPresent && !loaded {
		return a.result(LaunchAgentAlreadyUninstalled), nil
	}
	if loaded {
		if err := a.bootout(ctx, plistPresent); err != nil {
			return LaunchAgentResult{}, err
		}
	}
	if plistPresent {
		if err := files.RemoveItem(a.PlistPath()); err != nil {
			return LaunchAgentResult{}, launchAgentFailure("Could not remove the LaunchAgent plist at %s: %s", a.PlistPath(), err.Error())
		}
	}
	return a.result(LaunchAgentUninstalled), nil
}

// runner is the command runner, a process runner when unset.
// Lock: none (pure).
func (a LaunchAgent) runner() shell.Runner {
	if a.Runner == nil {
		return shell.ProcessRunner{}
	}
	return a.Runner
}

// desiredPlist builds the plist for the current executable and environment.
// Lock: none (pure apart from the executable's symlink reads).
func (a LaunchAgent) desiredPlist() (launchAgentDesired, error) {
	executable, err := a.absoluteExecutable()
	if err != nil {
		return launchAgentDesired{}, err
	}
	paths := store.StandardPaths(a.env, a.SupportFolder)
	dataFolder := paths.Root
	if abs, err := filepath.Abs(dataFolder); err == nil {
		dataFolder = abs
	}
	variables := map[string]any{}
	if value := a.env("BELTLINE_DATA_DIR"); value != "" {
		variables["BELTLINE_DATA_DIR"] = dataFolder
	}
	if value := a.env("BELTLINE_TMUX_SOCKET"); value != "" {
		variables["BELTLINE_TMUX_SOCKET"] = value
	}
	if value := a.env("BELTLINE_VERSION"); value != "" {
		variables["BELTLINE_VERSION"] = value
	}
	values := map[string]any{
		"Label":             LaunchAgentLabel,
		"ProgramArguments":  []any{executable},
		"RunAtLoad":         true,
		"KeepAlive":         map[string]any{"SuccessfulExit": false},
		"StandardOutPath":   filepath.Join(dataFolder, "beltlined.stdout.log"),
		"StandardErrorPath": filepath.Join(dataFolder, "beltlined.stderr.log"),
	}
	if len(variables) > 0 {
		values["EnvironmentVariables"] = variables
	}
	data, err := plistEncode(values)
	if err != nil {
		return launchAgentDesired{}, launchAgentFailure("Could not encode the LaunchAgent plist: %s", err.Error())
	}
	return launchAgentDesired{dataFolder: dataFolder, data: data}, nil
}

// env reads the environment the agent was built with.
// Lock: none (pure).
func (a LaunchAgent) env(key string) string {
	return a.Environment[key]
}

// isLoaded reports whether launchd has the job loaded.
// Lock: none. Suspends: the print command.
func (a LaunchAgent) isLoaded(ctx context.Context) (bool, error) {
	result := a.runner().Run(ctx, a.launchctl(), []string{"print", a.ServiceTarget()}, "", nil)
	if result.OK() {
		return true, nil
	}
	if confirmsMissingService(result) {
		return false, nil
	}
	return false, launchAgentFailure("Could not check whether %s is loaded in %s: %s", LaunchAgentLabel, a.Domain(), result.Message())
}

// bootout unloads the job, by its plist when plistPresent and by its service target otherwise.
// Lock: none. Suspends: the bootout and isLoaded calls.
func (a LaunchAgent) bootout(ctx context.Context, plistPresent bool) error {
	target := a.ServiceTarget()
	arguments := []string{"bootout", a.ServiceTarget()}
	if plistPresent {
		target = a.PlistPath()
		arguments = []string{"bootout", a.Domain(), a.PlistPath()}
	}
	result := a.runner().Run(ctx, a.launchctl(), arguments, "", nil)
	if result.OK() || confirmsMissingService(result) {
		return nil
	}
	// launchd can report a transient bootout error while another actor removes the
	// job. Only a successful print that confirms absence makes that race safe.
	loaded, err := a.isLoaded(ctx)
	if err != nil {
		return err
	}
	if !loaded {
		return nil
	}
	return launchAgentFailure("Could not unload %s from %s using %s: %s", LaunchAgentLabel, a.Domain(), target, result.Message())
}

// result is the outcome for action.
// Lock: none (pure).
func (a LaunchAgent) result(action LaunchAgentAction) LaunchAgentResult {
	return LaunchAgentResult{Action: action, Label: LaunchAgentLabel, Domain: a.Domain(), PlistPath: a.PlistPath()}
}

// confirmsMissingService reports whether launchctl said the job is not registered.
// Lock: none (pure).
func confirmsMissingService(result shell.Result) bool {
	message := strings.ToLower(result.Stderr + "\n" + result.Stdout)
	return strings.Contains(message, "could not find service") ||
		strings.Contains(message, "service not found") ||
		strings.Contains(message, "service could not be found")
}

// plistsMatch reports whether two XML plists hold equal dictionaries.
// Lock: none (pure).
func plistsMatch(current, desired []byte) (bool, error) {
	currentValue, err := plistDecode(current)
	if err != nil {
		return false, err
	}
	desiredValue, err := plistDecode(desired)
	if err != nil {
		return false, err
	}
	currentDict, ok := currentValue.(map[string]any)
	if !ok {
		return false, nil
	}
	desiredDict, ok := desiredValue.(map[string]any)
	if !ok {
		return false, nil
	}
	return reflect.DeepEqual(currentDict, desiredDict), nil
}

// absoluteExecutable resolves the executable to an absolute path with its symlinks
// resolved.
// Lock: none. Reads the file system only.
func (a LaunchAgent) absoluteExecutable() (string, error) {
	return registeredExecutable(a.ExecutablePath, a.env("PATH"))
}

// registeredExecutable is the path a service manager (launchd or systemd) should start: path made
// absolute (looked up on searchPath when it is a bare name) and its symlinks resolved.
// Lock: none. Reads the file system only.
func registeredExecutable(path, searchPath string) (string, error) {
	if path == "" {
		return "", launchAgentFailure("Could not determine the beltlined executable path")
	}
	if strings.HasPrefix(path, "/") {
		return resolveSymlinks(path), nil
	}
	if strings.Contains(path, "/") {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", launchAgentFailure("Could not determine the beltlined executable path")
		}
		return resolveSymlinks(abs), nil
	}
	for _, folder := range strings.Split(searchPath, ":") {
		if folder == "" {
			continue
		}
		candidate := filepath.Join(folder, path)
		if isExecutableFile(candidate) {
			return resolveSymlinks(candidate), nil
		}
	}
	return "", launchAgentFailure("Could not resolve beltlined on PATH to an absolute executable path")
}

// accessExecute is X_OK, the access(2) mode Foundation's isExecutableFile asks for.
const accessExecute = 0x1

// isExecutableFile reports whether the path is executable: access(2)
// with X_OK, so a directory with the execute bit counts, as it does in Foundation.
// Lock: none.
func isExecutableFile(path string) bool {
	return syscall.Access(path, accessExecute) == nil
}
