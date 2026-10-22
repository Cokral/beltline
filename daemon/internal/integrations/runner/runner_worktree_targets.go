package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// WorktreeTargetsAction is the title of the action that opens one worktree.
const WorktreeTargetsAction = "Open in VS Code"

// WorktreeTargetsUnavailable is the reason when a run has no worktree to open.
const WorktreeTargetsUnavailable = "No worktree available"

// WorktreeTarget is one existing worktree a run can open in an editor. Detail is
// its full path, for help and VoiceOver.
type WorktreeTarget struct {
	ID     string
	Title  string
	Folder string
}

// Detail is the worktree's full path.
func (t WorktreeTarget) Detail() string { return t.Folder }

// WorktreeTargetsFor lists the worktrees under a run's work folder: the main
// `worktree` first, then each immediate route folder's `worktree` by leg id. One
// folder keeps the plain action; several are qualified, and a repeated or unknown
// route name includes its leg id so the rows stay distinct. It reads only, and
// does not create checkouts.
func WorktreeTargetsFor(run core.Run, work string) []WorktreeTarget {
	var found []worktreeLocated
	main := filepath.Join(work, "worktree")
	if WorktreeIsCheckout(main, run.Repos) {
		name := "Main"
		found = append(found, worktreeLocated{Leg: "main", Name: &name, Folder: main})
	}
	entries, _ := os.ReadDir(work)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || name == "readonly" || name == "worktree" {
			continue
		}
		if !runnerIsRealDirectory(filepath.Join(work, name)) {
			continue
		}
		worktree := filepath.Join(work, name, "worktree")
		if !WorktreeIsCheckout(worktree, run.Repos) {
			continue
		}
		leg := "r:" + name
		var routeName *string
		if route := run.Layout().Route(name); route != nil {
			label := route.Name
			routeName = &label
		}
		found = append(found, worktreeLocated{Leg: leg, Name: routeName, Folder: worktree})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Leg == "main" {
			return true
		}
		if found[j].Leg == "main" {
			return false
		}
		return found[i].Leg < found[j].Leg
	})
	seen := map[string]struct{}{}
	var unique []worktreeLocated
	for _, item := range found {
		key := worktreeCanonical(item.Folder)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, item)
	}
	return worktreeLabel(unique)
}

// WorktreeIsCheckout reports whether folder is a worktree directory that holds one
// of repos. A single repository's checkout is the directory itself. Several
// repositories share that directory, and a child counts only when it is one of
// their checkouts.
func WorktreeIsCheckout(folder string, repos []string) bool {
	if len(repos) == 0 || !runnerIsDirectory(folder) {
		return false
	}
	if len(repos) == 1 {
		return true
	}
	return slices.ContainsFunc(repos, func(repo string) bool {
		return runnerIsDirectory(WorkspaceSpecCheckout(repo, repos, folder))
	})
}

// worktreeLocated is one worktree found under a run's work folder.
type worktreeLocated struct {
	Leg    string
	Name   *string
	Folder string
}

// worktreeCanonical is folder with symbolic links resolved and standardised, for comparing checkouts.
func worktreeCanonical(folder string) string {
	resolved, err := filepath.EvalSymlinks(folder)
	if err != nil {
		resolved = folder
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return filepath.Clean(resolved)
	}
	return abs
}

// worktreeLabel gives each found worktree its title, qualified by route name and leg where needed.
func worktreeLabel(found []worktreeLocated) []WorktreeTarget {
	if len(found) == 1 {
		return []WorktreeTarget{{ID: found[0].Leg, Title: WorktreeTargetsAction, Folder: found[0].Folder}}
	}
	counts := map[string]int{}
	for _, item := range found {
		if item.Name != nil {
			counts[*item.Name]++
		}
	}
	targets := make([]WorktreeTarget, 0, len(found))
	for _, item := range found {
		var qualifier string
		switch {
		case item.Name != nil && counts[*item.Name] == 1:
			qualifier = *item.Name
		case item.Name != nil:
			qualifier = fmt.Sprintf("%s · %s", *item.Name, item.Leg)
		default:
			qualifier = item.Leg
		}
		targets = append(targets, WorktreeTarget{ID: item.Leg, Title: WorktreeTargetsAction + " · " + qualifier, Folder: item.Folder})
	}
	return targets
}
