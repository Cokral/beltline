// Coordinator tests of the assistant's admission: a model revoked while the assistant is starting,
// and the assistant's resource offers while it stops, starts and is busy. The conversations are
// recording fakes, with their gates held open at the points that await.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestPermissionChangeBeforeCaptureIsObservedDuringStartupProbe: a model revoked while the startup probe
// runs is observed when the start resumes, so nothing starts and nothing is admitted.
func TestPermissionChangeBeforeCaptureIsObservedDuringStartupProbe(t *testing.T) {
	t.Parallel()
	storage := assistantStorage(t, true)
	root := storage.Paths.Root
	conversations := newH5m6Conversations()
	conversations.probe = make(chan struct{})
	conversations.probeEntered = make(chan struct{})
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})
	<-conversations.probeEntered

	done := make(chan error, 1)
	go func() { done <- c.StartAssistant(context.Background(), "Denied before capture") }()
	if err := c.SetModelAllowed("gpt-test", core.BackendPi, false); err != nil {
		t.Fatalf("SetModelAllowed: %v", err)
	}
	close(conversations.probe)

	err := <-done
	if err == nil {
		t.Fatal("the admission ignored the revocation made during preparation")
	}
	hasText(t, "the refusal", err.Error(), "not allowed")
	if n := len(conversations.Specs()); n != 0 {
		t.Errorf("conversations started = %d", n)
	}
	if _, statErr := os.Stat(filepath.Join(root, "assistant", "admission.json")); !os.IsNotExist(statErr) {
		t.Errorf("an admission was captured: %v", statErr)
	}
}

// TestPermissionChangeAfterDurableCaptureDoesNotRevokePendingLaunch: once the admission is durable, a revocation
// does not stop the launch already under way, and it does not authorize a second one.
func TestPermissionChangeAfterDurableCaptureDoesNotRevokePendingLaunch(t *testing.T) {
	t.Parallel()
	storage := assistantStorage(t, true)
	root := storage.Paths.Root
	conversations := newH5m6Conversations()
	conversations.launch = make(chan struct{})
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})

	done := make(chan error, 1)
	go func() { done <- c.StartAssistant(context.Background(), "Captured before revocation") }()
	spec := <-conversations.launched

	record := assistantAdmission(t, root)
	if record.Identity != spec.SessionID || record.Session != spec.Name {
		t.Errorf("admission identity %q session %q, want %q and %q", record.Identity, record.Session, spec.SessionID, spec.Name)
	}
	locked(c, func() {
		if !c.assistant.busy {
			t.Errorf("the assistant is not busy while its launch is pending")
		}
	})
	if err := c.SetModelAllowed("gpt-test", core.BackendPi, false); err != nil {
		t.Fatalf("SetModelAllowed: %v", err)
	}
	close(conversations.launch)

	if err := <-done; err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	locked(c, func() {
		if !c.assistant.running {
			t.Errorf("the captured launch did not run")
		}
	})
	if !conversations.ConversationAlive(context.Background(), spec.Name) {
		t.Errorf("the conversation %q is not alive", spec.Name)
	}
	stopAssistant(c)
	if err := c.StartAssistant(context.Background(), "New denied"); err == nil {
		t.Errorf("a revoked model started a new conversation")
	}
}

// assistantAction finds the action with id in an assistant resource.
func assistantAction(t *testing.T, resource AssistantResource, id string) AssistantAction {
	t.Helper()
	for _, action := range resource.Actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("the assistant offers no %q action", id)
	return AssistantAction{}
}

// TestOffersFollowStoppedRunningAndBusyCoordinatorState: the assistant's offers follow whether it is stopped,
// starting, running or stopping.
func TestOffersFollowStoppedRunningAndBusyCoordinatorState(t *testing.T) {
	t.Parallel()
	storage := assistantStorage(t, false)
	conversations := newH5m6Conversations()
	conversations.launch = make(chan struct{})
	c := assistantCoordinator(t, storage, conversations, &SequenceIDs{})
	ctx := context.Background()

	stopped := c.ReadAssistant()
	expectOffer(t, "stopped", stopped, "configuration", true)
	expectOffer(t, "stopped", stopped, "start", true)
	expectOffer(t, "stopped", stopped, "stop", false)

	done := make(chan error, 1)
	go func() { done <- c.StartAssistant(ctx, "Add a belt") }()
	<-conversations.launched
	busy := c.ReadAssistant()
	if !busy.Busy {
		t.Errorf("the assistant is not busy while it starts")
	}
	expectOffer(t, "starting", busy, "configuration", false)
	expectOffer(t, "starting", busy, "start", false)
	expectOffer(t, "starting", busy, "stop", false)

	close(conversations.launch)
	if err := <-done; err != nil {
		t.Fatalf("StartAssistant: %v", err)
	}
	running := c.ReadAssistant()
	if !running.Running || running.Busy {
		t.Errorf("running %v, busy %v", running.Running, running.Busy)
	}
	expectOffer(t, "running", running, "configuration", false)
	expectOffer(t, "running", running, "start", false)
	expectOffer(t, "running", running, "stop", true)

	if err := c.StopAssistant(ctx); err != nil {
		t.Fatalf("StopAssistant: %v", err)
	}
	stoppedAgain := c.ReadAssistant()
	if stoppedAgain.Running {
		t.Errorf("the assistant still runs after stop")
	}
	expectOffer(t, "stopped again", stoppedAgain, "configuration", true)
	expectOffer(t, "stopped again", stoppedAgain, "start", true)
	expectOffer(t, "stopped again", stoppedAgain, "stop", false)
}

// expectOffer checks one action's enabled state in an assistant resource.
func expectOffer(t *testing.T, state string, resource AssistantResource, id string, enabled bool) {
	t.Helper()
	if action := assistantAction(t, resource, id); action.Enabled != enabled {
		t.Errorf("%s: %q enabled = %v, want %v", state, id, action.Enabled, enabled)
	}
}
