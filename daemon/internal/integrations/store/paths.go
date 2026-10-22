package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Paths is where the app keeps its data.
type Paths struct {
	Root string
}

// StandardPaths is BELTLINE_DATA_DIR when it is set and not empty, otherwise
// Beltline in support. env is the process environment, passed in so that
// nothing reads it here.
func StandardPaths(env func(string) string, support string) Paths {
	if custom := env("BELTLINE_DATA_DIR"); custom != "" {
		// A relative path is made absolute against the working
		// directory, so every spelling of the folder names the same root.
		if abs, err := filepath.Abs(custom); err == nil {
			custom = abs
		}
		return Paths{Root: custom}
	}
	return Paths{Root: filepath.Join(support, "Beltline")}
}

// SupportFolder is the folder that holds the Beltline data folder: ~/Library/Application Support
// on macOS, and on Linux $XDG_DATA_HOME, or ~/.local/share when that is unset.
func SupportFolder() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return SupportFolderFor(runtime.GOOS, home, os.Getenv), nil
}

// SupportFolderFor is SupportFolder for the system goos, a home folder and an environment.
func SupportFolderFor(goos, home string, env func(string) string) string {
	if goos == "darwin" {
		return filepath.Join(home, "Library", "Application Support")
	}
	if data := env("XDG_DATA_HOME"); filepath.IsAbs(data) {
		return data
	}
	return filepath.Join(home, ".local", "share")
}

// LibraryRepos holds a clone of each library repository, and backup-state.json.
func (p Paths) LibraryRepos() string { return filepath.Join(p.Root, "library-repos") }

// LibraryBackups is where a restore keeps the belt and agent files it replaces.
func (p Paths) LibraryBackups() string { return filepath.Join(p.Root, "library-backups") }

// Belts is the belt library: belts/<id>/v<n>.json and state.json.
func (p Paths) Belts() string { return filepath.Join(p.Root, "belts") }

// Agents is the agent library: agents/<id>/v<n>.md.
func (p Paths) Agents() string { return filepath.Join(p.Root, "agents") }

// Runs is the folder of saved runs.
func (p Paths) Runs() string { return filepath.Join(p.Root, "runs") }

// Repos holds the app's own clones, one per repository.
func (p Paths) Repos() string { return filepath.Join(p.Root, "repos") }

// Work holds checkouts and worktrees, one folder per run.
func (p Paths) Work() string { return filepath.Join(p.Root, "work") }

// Format is format.json, the data folder format the upgrade last wrote.
func (p Paths) Format() string { return filepath.Join(p.Root, "format.json") }

// UpgradeBackups is where the upgrade keeps the files it rewrote, as they were.
func (p Paths) UpgradeBackups() string { return filepath.Join(p.Root, "upgrade-backups") }

// Settings is settings.json.
func (p Paths) Settings() string { return filepath.Join(p.Root, "settings.json") }

// Erasures holds erasure intents and the generation each erased run reached.
// Outside runs/, so loading runs never treats them as a run.
func (p Paths) Erasures() string { return filepath.Join(p.Root, "erasures") }

// AfkDecisions is the AFK assistant's decisions, newest last.
func (p Paths) AfkDecisions() string { return filepath.Join(p.Root, "afk", "decisions.json") }

// RunFolder is a run's folder under runs/.
func (p Paths) RunFolder(run core.Run) string {
	return filepath.Join(p.Runs(), run.Folder())
}

// AttemptFolder is one attempt's folder inside its run's folder.
func (p Paths) AttemptFolder(run core.Run, attempt core.Attempt) string {
	return filepath.Join(p.RunFolder(run), attempt.Folder)
}

// ArtifactsFolder is the optional artifacts/ folder inside each attempt, not
// its checkout. Shared with Brief.
func (p Paths) ArtifactsFolder(run core.Run, attempt core.Attempt) string {
	return filepath.Join(p.AttemptFolder(run, attempt), "artifacts")
}

// Clone is the app's own clone of one repository.
func (p Paths) Clone(repo string) string { return filepath.Join(p.Repos(), repo) }

// WorkFolder is a run's work folder.
func (p Paths) WorkFolder(run core.Run) string {
	return filepath.Join(p.Work(), run.Folder())
}

// WorkFolderLeg is a run's work folder for one leg of its routes, each in its
// own folder so two legs never share a checkout. A nil leg, or "main", is the
// run's own folder.
func (p Paths) WorkFolderLeg(run core.Run, leg *string) string {
	if leg == nil || *leg == "main" {
		return p.WorkFolder(run)
	}
	return filepath.Join(p.WorkFolder(run), strings.ReplaceAll(*leg, "r:", ""))
}
