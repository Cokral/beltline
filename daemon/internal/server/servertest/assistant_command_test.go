// Contract tests of the assistant commands.

package servertest

import (
	"reflect"
	"testing"
)

// TestAssistantReadsConfigurationStartAndStop is CommandContractTests.assistantReadsConfigurationStartAndStop.
func TestAssistantReadsConfigurationStartAndStop(t *testing.T) {
	g := Start(t, Options{})
	configured := postJSON(g, "/api/v1/assistant/configuration", []byte(assistantConfigurationBody))
	requireStatus(t, configured, 200)
	CheckGolden(t, "assistantConfiguration", contractEndpoint(t, "assistantConfiguration").GoldenName, configured, g.Root)
	configuration := contractMember(t, contractTree(t, configured.Body), "configuration")
	if contractMember(t, configuration, "backend") != "pi" || contractMember(t, configuration, "model") != "fixture-model" ||
		contractMember(t, configuration, "thinking") != "high" {
		t.Fatalf("configuration %v", configuration)
	}
	settings := g.Coordinator.ReadSettings()
	if settings.ConfigurationModel != "fixture-model" {
		t.Fatalf("persisted model %q", settings.ConfigurationModel)
	}

	started := postJSON(g, "/api/v1/assistant/start", []byte(assistantStartBody))
	requireStatus(t, started, 200)
	CheckGolden(t, "assistantStart", contractEndpoint(t, "assistantStart").GoldenName, started, g.Root)
	running := contractTree(t, started.Body)
	if contractMember(t, running, "running") != true || contractMember(t, running, "busy") != false ||
		contractMember(t, contractMember(t, running, "configuration"), "model") != "fixture-model" {
		t.Fatal("the started assistant is not running on the configured model")
	}
	session := g.Coordinator.ReadAssistant().Session
	if len(session) < len("bl-assistant-") || session[:len("bl-assistant-")] != "bl-assistant-" {
		t.Fatalf("assistant session %q", session)
	}

	// A second start while running must not create another conversation.
	requireStatus(t, postJSON(g, "/api/v1/assistant/start", []byte(`{"request":"Again"}`)), 422)
	if g.Coordinator.ReadAssistant().Session != session {
		t.Fatal("a second start replaced the conversation")
	}
	// Configuration is refused while the conversation is running.
	requireStatus(t, postJSON(g, "/api/v1/assistant/configuration",
		[]byte(`{"backend":"pi","model":"fixture-model","thinking":null}`)), 422)

	stopped := postJSON(g, "/api/v1/assistant/stop", nil)
	requireStatus(t, stopped, 200)
	CheckGolden(t, "assistantStop", contractEndpoint(t, "assistantStop").GoldenName, stopped, g.Root)
	if contractMember(t, contractTree(t, stopped.Body), "running") != false {
		t.Fatal("the stopped assistant is still running")
	}
	// Already stopped is a no-op that still returns reconciled state.
	again := postJSON(g, "/api/v1/assistant/stop", []byte("{}"))
	requireStatus(t, again, 200)
	if !reflect.DeepEqual(contractTree(t, again.Body), contractTree(t, stopped.Body)) {
		t.Fatal("a second stop changed the reply")
	}
}

// TestAssistantConfigurationRejectsBlankDisallowedAndInvalidThinking is
// CommandContractTests.assistantConfigurationRejectsBlankDisallowedAndInvalidThinking.
func TestAssistantConfigurationRejectsBlankDisallowedAndInvalidThinking(t *testing.T) {
	g := Start(t, Options{})
	prior := g.Coordinator.ReadSettings()
	for _, body := range []string{
		`{"backend":"pi","model":"","thinking":null}`,
		`{"backend":"pi","model":"not-admitted","thinking":null}`,
		`{"backend":"pi","model":"fixture-model","thinking":"sideways"}`,
		`{"backend":"pi","model":"fixture-model"}`,
		`{"backend":"pi","model":"fixture-model","thinking":null,"extra":0}`,
	} {
		requireStatus(t, postJSON(g, "/api/v1/assistant/configuration", []byte(body)), 422)
		if !reflect.DeepEqual(g.Coordinator.ReadSettings(), prior) {
			t.Fatalf("a rejected configuration %s changed the settings", body)
		}
	}
	requireStatus(t, postJSON(g, "/api/v1/assistant/start", []byte(`{"request":"   "}`)), 422)
	if g.Coordinator.ReadAssistant().Running {
		t.Fatal("a blank request started the assistant")
	}
}

// TestCommandTransportRejectsWrongMethodsUnknownRunnersBodiesAndAuth checks the assistant routes of
// CommandContractTests.commandTransportRejectsWrongMethodsUnknownRunnersBodiesAndAuth: the bearer
// check, the body rules and the nonempty-body rule of stop.
func TestCommandTransportRejectsAssistantBodiesAndAuth(t *testing.T) {
	g := Start(t, Options{})
	for _, path := range []string{"/api/v1/assistant/configuration", "/api/v1/assistant/start"} {
		if reply := g.Do(Request{Method: "POST", Path: path}); reply.Status != 401 {
			t.Fatalf("%s without a token answered %d, want 401", path, reply.Status)
		}
		if reply := g.Do(Request{Method: "POST", Path: path, Body: []byte("{}"), Token: "wrong"}); reply.Status != 401 {
			t.Fatalf("%s with a wrong token answered %d, want 401", path, reply.Status)
		}
	}
	const start = "/api/v1/assistant/start"
	requireStatus(t, postJSON(g, start, nil), 422)
	requireStatus(t, postJSON(g, start, []byte("not json")), 422)
	requireStatus(t, postJSON(g, start, []byte("{}")), 422)
	requireStatus(t, postJSON(g, start, []byte(`{"unknownField":true}`)), 422)
	requireStatus(t, postJSON(g, "/api/v1/assistant/stop", nil), 200)
	for _, body := range []string{"not json", `{"extra":true}`} {
		requireStatus(t, postJSON(g, "/api/v1/assistant/stop", []byte(body)), 422)
	}
}
