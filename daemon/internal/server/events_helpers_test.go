// Shared helpers for the event tests: the coordinator fixture, message assertions, and the fakes the
// event tests declare at file scope.

package server

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/kit/tools"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestMain removes the demo template the event tests copy (testkit.Template).
func TestMain(m *testing.M) {
	code := m.Run()
	testkit.Cleanup()
	os.Exit(code)
}

// eventsDemoTemplate is the seeded demo folder every event test copies: seeding
// it costs about a second, and under -race about ten, so it is built once per
// test binary.
func eventsDemoTemplate(t *testing.T) string {
	return testkit.Template(t, "server-events-demo", func(dir string) error {
		_, err := demo.Make(dir, testkit.ContractTime, os.Getenv)
		return err
	})
}

// newEventsCoordinator builds a demo coordinator in a folder of its own (a copy of
// the seeded template), with the fixture services of LaunchSetupDemo and the given
// model discovery (nil: the fixture catalog). It stands in for the
// withContractFixture: the same demo the contract tests seed, at the contract's time.
func newEventsCoordinator(t *testing.T, discovery runner.ModelCatalogDiscovery) *coordinator.Coordinator {
	t.Helper()
	setup, err := demo.Prepare(eventsDemoTemplate(t), testkit.ContractTime, os.Getenv)
	if err != nil {
		t.Fatalf("demo: %v", err)
	}
	if discovery == nil {
		discovery = runner.FixtureModelCatalog{}
	}
	c, err := coordinator.New(coordinator.Services{
		Storage:        setup.Storage,
		GitHub:         setup.Services.GitHub(),
		Jira:           setup.Services.Jira(),
		Runner:         setup.Runner,
		Workspaces:     setup.Workspaces,
		Clock:          testkit.NewClock(testkit.ContractTime),
		ModelDiscovery: discovery,
		ToolCheck: func(context.Context) []tools.Result {
			return tools.Fixtures()
		},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// takeEventMessage returns the next queued message without waiting; the test
// fails when none is queued, so a missing message cannot hang it.
func takeEventMessage(t *testing.T, client *EventClient) string {
	t.Helper()
	data, ok := client.TakePendingForTesting()
	if !ok {
		t.Fatalf("no queued event message")
	}
	return string(data)
}

// requireEventMessages checks that the queued messages of client are exactly want, in order.
func requireEventMessages(t *testing.T, client *EventClient, want ...string) {
	t.Helper()
	var got []string
	for range want {
		got = append(got, takeEventMessage(t, client))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %v\nwant      %v", got, want)
	}
	if extra, ok := client.TakePendingForTesting(); ok {
		t.Fatalf("unexpected extra message %s", extra)
	}
}

// helloMessage is the hello every connection gets first (apiVersion 1).
const helloMessage = `{"apiVersion":1,"type":"hello"}`

// changedFrame is the changed message for resource at revision, in wire form.
func changedFrame(resource string, revision int) string {
	return `{"resource":"` + resource + `","revision":` + strconv.Itoa(revision) + `,"type":"changed"}`
}

// failingDiscovery is the model discovery that always fails.
type failingDiscovery struct{}

// Discover fails with the fixture catalog failure, as the rejected model catalog does.
func (failingDiscovery) Discover(context.Context, core.Backend, string) ([]core.ModelChoice, error) {
	return nil, errors.New("fixture catalog failure")
}

// readEventGolden reads one of the event goldens (testdata/golden/event-<name>.json).
func readEventGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(testkit.Testdata(t, "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(data)
}
