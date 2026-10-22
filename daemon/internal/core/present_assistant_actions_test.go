// The action expectations of the configuration assistant.

package core

import "testing"

// assistantActionByID finds the action with id, failing the test when it is missing.
func assistantActionByID(t *testing.T, actions []AssistantAction, id string) AssistantAction {
	t.Helper()
	for _, action := range actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("no %q action in %v", id, actions)
	return AssistantAction{}
}

// assistantWarning is a model warning callback that returns text, counting its calls in calls.
func assistantWarning(text *string, calls *int) func() *string {
	return func() *string {
		*calls++
		return text
	}
}

// TestAssistantActionsFollowBusyAndRunning: a stopped assistant can change its configuration and start,
// a running one can only stop, and a busy one offers nothing but its busy reason.
func TestAssistantActionsFollowBusyAndRunning(t *testing.T) {
	t.Parallel()
	calls := 0
	stopped := AssistantActionsOf(false, false, "gpt", assistantWarning(nil, &calls))
	if got := assistantActionByID(t, stopped, "configuration"); !got.Enabled || got.Label != "Change configuration" || got.Reason != nil {
		t.Errorf("stopped configuration action = %+v", got)
	}
	if got := assistantActionByID(t, stopped, "start"); !got.Enabled || got.Label != "Start" || got.Reason != nil {
		t.Errorf("stopped start action = %+v", got)
	}
	if got := assistantActionByID(t, stopped, "stop"); got.Enabled || got.Reason == nil || *got.Reason != "The assistant is not running." {
		t.Errorf("stopped stop action = %+v", got)
	}

	running := AssistantActionsOf(false, true, "gpt", assistantWarning(nil, &calls))
	if got := assistantActionByID(t, running, "configuration"); got.Enabled || *got.Reason != "Stop the assistant before changing its configuration." {
		t.Errorf("running configuration action = %+v", got)
	}
	if got := assistantActionByID(t, running, "start"); got.Enabled || *got.Reason != "The assistant is already running." {
		t.Errorf("running start action = %+v", got)
	}
	if got := assistantActionByID(t, running, "stop"); !got.Enabled || got.Reason != nil {
		t.Errorf("running stop action = %+v", got)
	}

	busy := AssistantActionsOf(true, false, "gpt", assistantWarning(nil, &calls))
	for _, id := range []string{"configuration", "start", "stop"} {
		if got := assistantActionByID(t, busy, id); got.Enabled || got.Reason == nil || *got.Reason != "The assistant is busy." {
			t.Errorf("busy %s action = %+v", id, got)
		}
	}
	if calls != 1 {
		t.Errorf("the model warning was asked %d times; only the stopped start action should ask it", calls)
	}
}

// TestAssistantActionsStartNeedsAnAllowedModel: a blank model is explained, a model the policy refuses carries its warning.
func TestAssistantActionsStartNeedsAnAllowedModel(t *testing.T) {
	t.Parallel()
	calls := 0
	blank := AssistantActionsOf(false, false, "  ", assistantWarning(nil, &calls))
	if got := assistantActionByID(t, blank, "start"); got.Enabled || *got.Reason != "Assistant: choose an allowed model." {
		t.Errorf("blank model start action = %+v", got)
	}
	if calls != 0 {
		t.Errorf("a blank model asked the warning %d times", calls)
	}

	warning := "Assistant model is not allowed."
	refused := AssistantActionsOf(false, false, "gpt", assistantWarning(&warning, &calls))
	if got := assistantActionByID(t, refused, "start"); got.Enabled || *got.Reason != warning {
		t.Errorf("refused model start action = %+v", got)
	}
	if calls != 1 {
		t.Errorf("a set model asked the warning %d times", calls)
	}
}
