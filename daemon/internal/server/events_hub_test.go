// Tests of the hub's flushes. The coordinator reports each turn's changes at its end, so a turn is one
// listener call: the tests that count flushes check the batch the hub sends for a turn, and the
// coordinator-driven tests check the messages each turn produces.

package server

import (
	"os"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

func TestClientAcceptsExactly64PendingMessages(t *testing.T) {
	t.Parallel()
	client := newEventClient()
	for i := 0; i < eventMaximumPendingMessages; i++ {
		if !client.Enqueue([]byte{1}) {
			t.Fatalf("message %d refused", i)
		}
	}
	if client.Enqueue([]byte{2}) {
		t.Fatalf("65th message accepted")
	}
	if data, ok := client.Next(); ok || data != nil {
		t.Fatalf("Next after overflow = %v, %v; want nil, false", data, ok)
	}
}

func TestClientAcceptsExactly256KiBAndDropsOverflowOrOversizedMessages(t *testing.T) {
	t.Parallel()
	exact := newEventClient()
	if !exact.Enqueue(make([]byte, eventMaximumPendingBytes)) {
		t.Fatalf("exact 256 KiB refused")
	}
	if data, ok := exact.Next(); !ok || len(data) != eventMaximumPendingBytes {
		t.Fatalf("Next = %d bytes, %v; want the exact message", len(data), ok)
	}

	overflow := newEventClient()
	terminations := 0
	overflow.SetTermination(func() { terminations++ })
	if !overflow.Enqueue(make([]byte, eventMaximumPendingBytes-1)) {
		t.Fatalf("256 KiB - 1 refused")
	}
	if overflow.Enqueue([]byte{1, 2}) {
		t.Fatalf("overflowing message accepted")
	}
	if data, ok := overflow.Next(); ok || data != nil {
		t.Fatalf("Next after overflow = %v, %v; want nil, false", data, ok)
	}
	if terminations != 1 {
		t.Fatalf("terminations = %d, want 1", terminations)
	}

	oversized := newEventClient()
	if oversized.Enqueue(make([]byte, eventMaximumMessageBytes+1)) {
		t.Fatalf("oversized message accepted")
	}
}

func TestStopClearsAllObservationSubscriptionsAndWriterWork(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	hub := NewEventHub(c)
	id, client, ok := hub.connect()
	if !ok {
		t.Fatalf("connect refused")
	}
	requireEventMessages(t, client, helloMessage)

	active := hub.DebugState()
	if active.Clients != 1 || active.Writers != 0 {
		t.Fatalf("active = %+v", active)
	}
	if active.Observations != len(AllEventResources) {
		t.Fatalf("observations = %d, want %d", active.Observations, len(AllEventResources))
	}

	hub.Stop()
	hub.Stop()
	stopped := hub.DebugState()
	if !stopped.Stopped || stopped.Clients != 0 || stopped.Writers != 0 || stopped.Observations != 0 {
		t.Fatalf("stopped = %+v", stopped)
	}
	hub.Disconnect(id)
}

func TestRevisionsAdvanceWithoutClientsAndRevisionsResetForANewHub(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	first := NewEventHub(c)
	mustSetLogin(t, c, "before connect")
	c.Idle()
	id, client, ok := first.connect()
	if !ok {
		t.Fatalf("connect refused")
	}
	requireEventMessages(t, client, helloMessage)

	mustSetLogin(t, c, "after connect")
	c.Idle()
	requireEventMessages(t, client, changedFrame("settings", 3), changedFrame("state", 4))
	first.Disconnect(id)
	first.Stop()

	second := NewEventHub(c)
	id2, client2, ok := second.connect()
	if !ok {
		t.Fatalf("second connect refused")
	}
	requireEventMessages(t, client2, helloMessage)
	mustSetLogin(t, c, "new lifetime")
	c.Idle()
	requireEventMessages(t, client2, changedFrame("settings", 1), changedFrame("state", 2))
	second.Disconnect(id2)
	second.Stop()
}

func TestHelloPrecedesMutationAndClientsShareCoalescedLexicalRevisions(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	mustSaveModelCatalog(t, c, "allowed-model")
	hub := NewEventHub(c)
	firstID, first, _ := hub.connect()
	secondID, second, _ := hub.connect()
	defer func() {
		hub.Disconnect(firstID)
		hub.Disconnect(secondID)
		hub.Stop()
	}()

	// The login and the allowed models change in one turn and see one
	// batch. Each command here is a turn of its own: the hello comes first, then
	// each turn's changed messages, in the same lexical order, to both clients.
	mustSetLogin(t, c, "turn-one")
	c.Idle()
	requireEventMessages(t, first, helloMessage, changedFrame("settings", 1), changedFrame("state", 2))
	requireEventMessages(t, second, helloMessage, changedFrame("settings", 1), changedFrame("state", 2))

	mustAllowModel(t, c, "allowed-model")
	c.Idle()
	requireEventMessages(t, first, changedFrame("models", 3), changedFrame("settings", 4))
	requireEventMessages(t, second, changedFrame("models", 3), changedFrame("settings", 4))

	hub.PublishNotification("title", "body")
	want := `{"body":"body","title":"title","type":"notification"}`
	requireEventMessages(t, first, want)
	requireEventMessages(t, second, want)

	mustSetLogin(t, c, "turn-two")
	c.Idle()
	requireEventMessages(t, first, changedFrame("settings", 5), changedFrame("state", 6))
	requireEventMessages(t, second, changedFrame("settings", 5), changedFrame("state", 6))
}

func TestControlledFlushCoalescesSameTurnAndKeepsMutationsMadeBeforeRearm(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	queue := &flushQueue{}
	hub := newEventHub(c, queue.schedule)
	id, client, _ := hub.connect()
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)

	// One turn that changed three resources: one batch, sorted, one revision each.
	hub.observedChanges([]EventResource{EventResourceState, EventResourceModels, EventResourceSettings})
	hub.observedChanges([]EventResource{EventResourceSettings, EventResourceState})
	if queue.pendingCount() != 1 {
		t.Fatalf("pending flushes = %d, want 1", queue.pendingCount())
	}
	queue.flushNext(t)
	requireEventMessages(t, client,
		changedFrame("models", 1), changedFrame("settings", 2), changedFrame("state", 3))
	if queue.pendingCount() != 0 {
		t.Fatalf("a flush is still pending after the batch was sent")
	}

	hub.observedChanges([]EventResource{EventResourceSettings, EventResourceState})
	queue.flushNext(t)
	requireEventMessages(t, client, changedFrame("settings", 4), changedFrame("state", 5))
}

// TestChangesFromTurnsBeforeTheFlushJoinIt checks that turns which run before a
// scheduled flush are sent by that one flush: two turns in a row give one changed
// frame per resource, with the later turn's state.
func TestChangesFromTurnsBeforeTheFlushJoinIt(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	queue := &flushQueue{}
	hub := newEventHub(c, queue.schedule)
	id, client, _ := hub.connect()
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)

	mustSetLogin(t, c, "first turn")
	mustSetLogin(t, c, "second turn")
	if queue.pendingCount() != 1 {
		t.Fatalf("pending flushes = %d, want one for both turns", queue.pendingCount())
	}
	if _, sent := client.TakePendingForTesting(); sent {
		t.Fatal("a message was sent before the flush ran")
	}
	queue.flushNext(t)
	requireEventMessages(t, client, changedFrame("settings", 1), changedFrame("state", 2))
}

func TestProductionFlushKeepsAwaitSeparatedChangesInDifferentBatches(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	hub := NewEventHub(c)
	id, client, _ := hub.connect()
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)

	// Each change is followed by the wait for its flush, so
	// the two changes are two batches.
	mustSetLogin(t, c, "same-turn-one")
	c.Idle()
	mustSetLogin(t, c, "same-turn-two")
	c.Idle()
	requireEventMessages(t, client,
		changedFrame("settings", 1), changedFrame("state", 2),
		changedFrame("settings", 3), changedFrame("state", 4))
}

func TestCheckingAndLastCheckInvalidateStateWithoutInvalidatingIntake(t *testing.T) {
	t.Parallel()
	c := newEventsCoordinator(t, nil)
	queue := &flushQueue{}
	hub := newEventHub(c, queue.schedule)
	id, client, _ := hub.connect()
	defer func() {
		hub.Disconnect(id)
		hub.Stop()
	}()
	requireEventMessages(t, client, helloMessage)

	// The state projection carries the intake's checking flag and last check,
	// while the intake projection is the waiting subjects only.
	hub.observedChanges([]EventResource{EventResourceState})
	queue.flushNext(t)
	requireEventMessages(t, client, changedFrame("state", 1))
}

// flushQueue holds the flushes a hub schedules, so a test runs each one when it
// chooses.
type flushQueue struct {
	mu      sync.Mutex
	pending []func()
}

// schedule is the hub's flush scheduler: it queues flush.
func (q *flushQueue) schedule(flush func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, flush)
}

// pendingCount is the number of flushes waiting to run.
func (q *flushQueue) pendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// flushNext runs the oldest pending flush, failing the test when none is pending.
func (q *flushQueue) flushNext(t *testing.T) {
	t.Helper()
	q.mu.Lock()
	if len(q.pending) == 0 {
		q.mu.Unlock()
		t.Fatal("no flush is pending")
	}
	flush := q.pending[0]
	q.pending = q.pending[1:]
	q.mu.Unlock()
	flush()
}

// mustSetLogin changes the saved login through the coordinator, in one turn.
func mustSetLogin(t *testing.T, c *coordinator.Coordinator, login string) {
	t.Helper()
	value := login
	if _, err := c.ApplySettingsPatch(coordinator.SettingsPatch{Login: &value}); err != nil {
		t.Fatalf("set login: %v", err)
	}
}

// mustSaveModelCatalog saves a Pi catalog holding model, as the fixture does before
// it is allowed.
func mustSaveModelCatalog(t *testing.T, c *coordinator.Coordinator, model string) {
	t.Helper()
	catalog := core.ModelCatalogSnapshot{
		Runner:      core.BackendPi,
		Executable:  core.BackendPi.Executable(os.Getenv),
		RefreshedAt: jsonx.Date{Time: testkit.ContractTime},
		Models:      []core.ModelChoice{{ID: model, Name: "Fixture model"}},
	}
	if err := c.SaveModelCatalog(catalog); err != nil {
		t.Fatalf("save catalog: %v", err)
	}
}

// mustAllowModel allows one Pi model through the coordinator, in one turn.
func mustAllowModel(t *testing.T, c *coordinator.Coordinator, model string) {
	t.Helper()
	if err := c.SetModelAllowed(model, core.BackendPi, true); err != nil {
		t.Fatalf("allow model: %v", err)
	}
}
