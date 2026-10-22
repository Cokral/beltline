//go:build integration

package runner

import (
	"context"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// TestLiveSessionNamesListsOnlyRunningSessions checks the one-query liveness list
// against a real tmux server: a session that runs is listed, a stopped one is not,
// and a server with no sessions (or none running at all) lists nothing.
func TestLiveSessionNamesListsOnlyRunningSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runner := integrationRunner(t)
	if names := runner.LiveSessionNames(ctx); len(names) != 0 {
		t.Fatalf("no server yet: names = %v, want none", names)
	}

	running := "bl-live-" + core.NewUUID()
	stopped := "bl-live-" + core.NewUUID()
	start := func(name string) {
		t.Helper()
		result := shell.ProcessRunner{}.Run(ctx, "tmux", []string{"-L", runner.socket, "new-session", "-d", "-s", name, "sleep 60"}, "", nil)
		if !result.OK() {
			t.Fatalf("new-session %s: %s", name, result.Stderr)
		}
	}
	start(running)
	start(stopped)
	runner.Stop(ctx, stopped)

	names := runner.LiveSessionNames(ctx)
	if !names[running] {
		t.Fatalf("running session %s not listed in %v", running, names)
	}
	if names[stopped] {
		t.Fatalf("stopped session %s still listed in %v", stopped, names)
	}
	if len(names) != 1 {
		t.Fatalf("names = %v, want only %s", names, running)
	}
}
