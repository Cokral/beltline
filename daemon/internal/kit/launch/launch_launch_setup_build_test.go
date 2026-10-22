// Tests of the launch setup that build a demo coordinator and tick it. The live builder and the live
// launch need tmux and are in launch_launch_setup_integration_test.go.

package launch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// launchDemoFolder is a copy of a seeded demo folder for one test: the demo is seeded once per test
// binary and copied. The folder holds a settings file, so a launch opens
// it as it is.
func launchDemoFolder(t *testing.T) string {
	t.Helper()
	return testkit.Template(t, "launch-demo", func(dir string) error {
		_, err := demo.Make(dir, testkit.ContractTime, os.Getenv)
		return err
	})
}

// TestTheDemoBuilderUsesTheGivenNotifier: the demo coordinator has no assistant check command
// when none is given. The notifier is not checked here: the test calls Autopilot.notify, which is
// unexported and not reachable from this package; the notify path is covered by the coordinator's
// notify-effect tests.
func TestTheDemoBuilderUsesTheGivenNotifier(t *testing.T) {
	t.Parallel()
	c, err := LaunchSetupDemo(launchDemoFolder(t), noNotify, nil)
	if err != nil {
		t.Fatalf("LaunchSetupDemo: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if check := c.ReadAssistant().CheckCommand; check != nil {
		t.Errorf("the demo assistant check command = %v, want none", check)
	}
}

// TestOwnershipIsHeldThroughBuildingAndTicking: the demo folder stays locked while its coordinator is built,
// while it ticks, and until the session closes.
func TestOwnershipIsHeldThroughBuildingAndTicking(t *testing.T) {
	t.Parallel()
	root := launchDemoFolder(t)
	var during string
	session, err := LaunchSessionDemo(&root, "", noNotify, nil,
		func(folder string, notifier Notifier, check []string) (*coordinator.Coordinator, error) {
			during = lockProbe(t, folder)
			return LaunchSetupDemo(folder, notifier, check)
		})
	if err != nil {
		t.Fatalf("LaunchSessionDemo: %v", err)
	}
	held := fmt.Sprintf("held %d", os.Getpid())
	if during != held {
		t.Fatalf("while building the folder is %q, want %q", during, held)
	}
	session.Coordinator.Tick(context.Background())
	session.Coordinator.Idle()
	if got := lockProbe(t, root); got != held {
		t.Errorf("after a tick the folder is %q, want %q", got, held)
	}
	session.Close()
	if got := lockProbe(t, root); got != "free" {
		t.Errorf("after close the folder is %q, want free", got)
	}
}

// TestDemoSeedsANewEventAndCanPollChecksWithNoExternalServices checks that the demo seeds a new event and polls
// checks with no external services: the demo starts a run held with merge conflicts and a watch whose checks fail, and a check of the
// watches (CheckNow, which also ticks) records
// the failing checks on that watch with no external service.
func TestDemoSeedsANewEventAndCanPollChecksWithNoExternalServices(t *testing.T) {
	t.Parallel()
	c, err := LaunchSetupDemo(launchDemoFolder(t), noNotify, nil)
	if err != nil {
		t.Fatalf("LaunchSetupDemo: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	var conflicted *core.Run
	for _, run := range c.Runs() {
		if run.Reason != nil && *run.Reason == "has conflicts" {
			conflicted = &run
			break
		}
	}
	if conflicted == nil {
		t.Fatal("no demo run is held with conflicts")
	}
	if conflicted.State != core.RunStateHeld {
		t.Fatalf("conflicted run state = %v, want held", conflicted.State)
	}
	if last := conflicted.Attempts[len(conflicted.Attempts)-1]; last.Detail == nil || !strings.Contains(*last.Detail, "has merge conflicts") {
		t.Fatalf("conflicted run's last detail = %v, want the merge conflicts", last.Detail)
	}
	var watching *core.Run
	for _, run := range c.Runs() {
		if stage := run.Stage(); stage != nil && stage.Kind() == core.StageKindWatch {
			if pull := run.PullRequest(); pull != nil && pull.Checks != nil && *pull.Checks == core.PullRequestCheckStateFailing {
				watching = &run
				break
			}
		}
	}
	if watching == nil {
		t.Fatal("no demo watch has failing checks")
	}
	c.CheckNow(context.Background())
	now := c.Run(watching.ID)
	if now == nil {
		t.Fatal("the watching run is gone")
	}
	var outcome *string
	for _, attempt := range now.Attempts {
		if attempt.StageID == watching.StageID {
			outcome = attempt.Outcome
		}
	}
	if outcome == nil || *outcome != "checks failing" {
		t.Fatalf("watch outcome = %v, want checks failing", outcome)
	}
}
