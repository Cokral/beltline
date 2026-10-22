//go:build integration

// Tests of the two launch setups that open a live coordinator: the live builder (tmux, gh, acli and git
// behind it) and a live launch over a saved demo folder. They run with the integration tag, since
// LaunchSetupLive builds the real TmuxRunner.

package launch

import (
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// TestTheLiveBuilderUsesTheRealIntegrationsAndTheGivenNotifier: the live coordinator opens the folder it is
// given and carries the assistant's check command. The concrete integration types and the notifier
// are not reachable from this package: the coordinator's parts are unexported.
func TestTheLiveBuilderUsesTheRealIntegrationsAndTheGivenNotifier(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	check := []string{"/x/Beltline", "--check-library"}
	c, err := LaunchSetupLive(store.Paths{Root: root}, noNotify, check)
	if err != nil {
		t.Fatalf("LaunchSetupLive: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if got := c.Paths().Root; got != root {
		t.Errorf("paths root = %q, want %q", got, root)
	}
	if got := c.ReadAssistant().CheckCommand; len(got) != 2 || got[0] != check[0] || got[1] != check[1] {
		t.Errorf("assistant check command = %v, want %v", got, check)
	}
}

// TestALiveLaunchLoadsTheSavedState: a live launch over a saved demo folder loads the same belts, runs and
// settings as the demo coordinator over that folder.
func TestALiveLaunchLoadsTheSavedState(t *testing.T) {
	t.Parallel()
	source := launchDemoFolder(t)
	session, err := LaunchSessionLive(store.Paths{Root: source}, noNotify, nil, nil)
	if err != nil {
		t.Fatalf("LaunchSessionLive: %v", err)
	}
	defer session.Close()
	reference, err := LaunchSetupDemo(launchDemoFolder(t), noNotify, nil)
	if err != nil {
		t.Fatalf("LaunchSetupDemo: %v", err)
	}
	t.Cleanup(func() { reference.Close() })

	live := session.Coordinator
	if got, want := beltIDsOf(live), beltIDsOf(reference); !slices.Equal(got, want) {
		t.Errorf("belts = %v, want %v", got, want)
	}
	if got, want := runIDsOf(live), runIDsOf(reference); !slices.Equal(got, want) {
		t.Errorf("runs = %v, want %v", got, want)
	}
	if got, want := live.ReadSettings().Login, reference.ReadSettings().Login; got != want {
		t.Errorf("login = %q, want %q", got, want)
	}
}

// beltIDsOf lists the ids of the belts a coordinator loaded.
func beltIDsOf(c *coordinator.Coordinator) []string {
	var ids []string
	for _, record := range c.Belts() {
		ids = append(ids, record.Belt.ID)
	}
	return ids
}

// runIDsOf lists the ids of the runs a coordinator loaded, in its order.
func runIDsOf(c *coordinator.Coordinator) []string {
	var ids []string
	for _, run := range c.Runs() {
		ids = append(ids, run.ID)
	}
	return ids
}
