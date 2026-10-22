// The coordinator's reads of an agent attempt: its session spec, the session it continues, its
// output and its artifacts.

package coordinator

import (
	"context"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// Spec returns the session spec of an agent attempt, rebuilt from the run.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) Spec(run core.Run, attempt core.Attempt, prompt string) *runner.SessionSpec {
	c.Lock()
	defer c.EndTurn()
	return c.sessions.spec(run, attempt, prompt)
}

// ContinueSession starts an interactive session that continues a finished agent attempt.
// Lock: exported entry point; takes c.mu. Suspends: sessions.continueSession.
func (c *Coordinator) ContinueSession(ctx context.Context, runID string, attempt core.Attempt) ([]string, error) {
	c.Lock()
	defer c.EndTurn()
	return c.sessions.continueSession(ctx, runID, attempt)
}

// LatestOutput returns the latest agent output on the run, or of the stages
// whose agent has key.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) LatestOutput(run core.Run, key *string) *string {
	c.Lock()
	defer c.EndTurn()
	return c.outputs.latest(run, key)
}

// Output returns the output of one attempt.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) Output(run core.Run, attempt core.Attempt) *string {
	c.Lock()
	defer c.EndTurn()
	return c.outputs.output(run, attempt)
}

// Artifacts lists an attempt's artifact file paths.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) Artifacts(run core.Run, attempt core.Attempt) []string {
	c.Lock()
	defer c.EndTurn()
	return c.outputs.artifacts(run, attempt)
}

// ResolveArtifact revalidates a listed name immediately before a browser or
// Finder action. It returns the path, or nil.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ResolveArtifact(name string, run core.Run, attempt core.Attempt) *string {
	c.Lock()
	defer c.EndTurn()
	return c.outputs.resolveArtifact(name, run, attempt)
}
