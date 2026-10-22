// The checked artifact
// location: a recorded attempt's file, confined to its folder under the data root.

package coordinator

import (
	"context"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"path/filepath"
)

// ConfinedArtifact is a checked artifact location.
// Folder is the attempt's folder under the data root and TrustedRoot is that root.
type ConfinedArtifact struct {
	Name        string
	Folder      string
	TrustedRoot string
}

// Read reads the file with its no-follow, confined walk and the size cap
// .
// Lock: not taken; it reads no coordinator state. Spawns: none.
func (a ConfinedArtifact) Read(ctx context.Context) store.ArtifactRead {
	return store.ArtifactsReadConfinedResult(a.Name, a.Folder, a.TrustedRoot, store.ArtifactMaxBytes)
}

// ConfinedArtifact returns the checked location of a recorded agent attempt's
// artifact, confined to the attempt's folder under the data root, or nil
// .
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ConfinedArtifact(runID, attemptID, name string) *ConfinedArtifact {
	c.Lock()
	defer c.EndTurn()
	return c.confinedArtifact(runID, attemptID, name)
}

// ArtifactExists reports whether the artifact can be served now.
// Lock: exported entry point; takes c.mu. Suspends: ConfinedArtifact.Read runs
// without the lock.
func (c *Coordinator) ArtifactExists(ctx context.Context, runID, attemptID, name string) bool {
	c.Lock()
	defer c.EndTurn()
	file := c.confinedArtifact(runID, attemptID, name)
	if file == nil {
		return false
	}
	var read store.ArtifactRead
	c.Suspend(func() { read = file.Read(ctx) })
	return read.Kind != store.ArtifactReadMissing
}

// confinedArtifact is ConfinedArtifact with c.mu held. The run and
// attempt are looked up here before any path is built; nothing is read yet.
func (c *Coordinator) confinedArtifact(runID, attemptID, name string) *ConfinedArtifact {
	run := c.book.Run(runID)
	if run == nil {
		return nil
	}
	var attempt *core.Attempt
	for i := range run.Attempts {
		if run.Attempts[i].ID == attemptID {
			found := run.Attempts[i]
			attempt = &found
			break
		}
	}
	if attempt == nil || !run.HasAgentStage(attempt.StageID) {
		return nil
	}
	resolved := c.outputs.resolveArtifact(name, *run, *attempt)
	if resolved == nil {
		return nil
	}
	folder := c.paths.ArtifactsFolder(*run, *attempt)
	if !samePath(filepath.Dir(*resolved), folder) {
		return nil
	}
	return &ConfinedArtifact{Name: name, Folder: folder, TrustedRoot: c.paths.Root}
}

// samePath reports whether two folders are the same once symlinks are resolved (a missing one compares as written).
func samePath(a, b string) bool {
	return resolvedPath(a) == resolvedPath(b)
}

// resolvedPath is the clean path with its symlinks resolved, or the clean path when it does not resolve.
func resolvedPath(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved
	}
	return clean
}
