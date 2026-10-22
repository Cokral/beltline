// Covers the request walk: enum strings and undeclared keys are refused at every
// level of a request body, and the bodies the web app sends are accepted.

package routes

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// notFormatMessage is the 422 a value that is not a case of its enum gets.
const notFormatMessage = "Invalid request body. The data couldn't be read because it isn't in the correct format."

// requestMessage is the message of the Failure err is, or "" when it is not one.
func requestMessage(err error) string {
	var failure core.Failure
	if errors.As(err, &failure) {
		return failure.Message
	}
	return ""
}

// beltDraftJSON is a belt with one issues trigger and the given stage objects.
func beltDraftJSON(stages string) string {
	return `{"id":"belt-a","name":"Belt","version":1,` +
		`"triggers":[{"id":"t1","trigger":{"kind":"issues"},"repos":["owner/repo"]}],` +
		`"stages":[` + stages + `]}`
}

// TestDecodeBodyRejectsUnknownEnumAs422 checks an enum string that is not a case,
// at the top level of a body, and that a case is accepted.
func TestDecodeBodyRejectsUnknownEnumAs422(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[api.BeltAfkRequest](bodyRequest(`{"afk":"bogus"}`), DefaultBodyLimit)
	if got := requestMessage(err); got != notFormatMessage {
		t.Fatalf("unknown afk mode: message %q, error %v", got, err)
	}
	got, err := DecodeBody[api.BeltAfkRequest](bodyRequest(`{"afk":"always"}`), DefaultBodyLimit)
	if err != nil || got.Afk != core.AfkModeAlways {
		t.Fatalf("known afk mode: %+v %v", got, err)
	}
}

// TestDecodeBodyRejectsUnknownEnumInAList checks an enum string inside a list
// reached through a custom decoder: a stage's workspace, in a belt's stages.
func TestDecodeBodyRejectsUnknownEnumInAList(t *testing.T) {
	t.Parallel()
	stage := `{"id":"s1","type":"agent","name":"Build","outcomes":[],"workspace":"bogus"}`
	_, err := DecodeBody[core.Belt](bodyRequest(beltDraftJSON(stage)), DefaultBodyLimit)
	if got := requestMessage(err); got != notFormatMessage {
		t.Fatalf("unknown workspace in a list: message %q, error %v", got, err)
	}
	stage = `{"id":"s1","type":"agent","name":"Build","outcomes":[],"workspace":"worktree"}`
	if _, err := DecodeBody[core.Belt](bodyRequest(beltDraftJSON(stage)), DefaultBodyLimit); err != nil {
		t.Fatalf("known workspace in a list: %v", err)
	}
}

// TestDecodeBodyRejectsUnknownEnumInTrigger checks the enum of a trigger's kind
// settings, which the trigger's decoder reads through its own shape.
func TestDecodeBodyRejectsUnknownEnumInTrigger(t *testing.T) {
	t.Parallel()
	body := `{"id":"belt-a","name":"Belt","version":1,` +
		`"triggers":[{"id":"t1","trigger":{"kind":"issues","assignee":"bogus"},"repos":[]}],"stages":[]}`
	_, err := DecodeBody[core.Belt](bodyRequest(body), DefaultBodyLimit)
	if got := requestMessage(err); got != notFormatMessage {
		t.Fatalf("unknown assignee: message %q, error %v", got, err)
	}
}

// testColor is a string enum the walk's own tests use.
type testColor string

const (
	testColorRed  testColor = "red"
	testColorBlue testColor = "blue"
)

// Known reports whether c is one of the cases of testColor.
func (c testColor) Known() bool { return c == testColorRed || c == testColorBlue }

// colorBody holds an enum in a list, and as a map key and a map value's key.
type colorBody struct {
	Palette []testColor       `json:"palette"`
	Counts  map[testColor]int `json:"counts"`
}

// TestDecodeBodyRejectsUnknownEnumAsMapKey checks an enum that is a map key, and
// one in a list, on a type of the test's own.
func TestDecodeBodyRejectsUnknownEnumAsMapKey(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[colorBody](bodyRequest(`{"palette":["red"],"counts":{"green":1}}`), DefaultBodyLimit)
	if got := requestMessage(err); got != notFormatMessage {
		t.Fatalf("unknown map key: message %q, error %v", got, err)
	}
	_, err = DecodeBody[colorBody](bodyRequest(`{"palette":["red","green"]}`), DefaultBodyLimit)
	if got := requestMessage(err); got != notFormatMessage {
		t.Fatalf("unknown list item: message %q, error %v", got, err)
	}
	got, err := DecodeBody[colorBody](bodyRequest(`{"palette":["red"],"counts":{"blue":2}}`), DefaultBodyLimit)
	if err != nil || got.Counts[testColorBlue] != 2 {
		t.Fatalf("known enums: %+v %v", got, err)
	}
}

// TestDecodeBodyRejectsUnknownNestedKey checks undeclared keys at the top of a
// belt draft and inside its stage, trigger and policies objects.
func TestDecodeBodyRejectsUnknownNestedKey(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[core.Belt](bodyRequest(`{"id":"x","name":"x","version":1,"bogusKey":1}`), DefaultBodyLimit)
	if got := requestMessage(err); got != `Unknown field "bogusKey"` {
		t.Fatalf("top-level key: message %q, error %v", got, err)
	}
	stage := `{"id":"s1","type":"agent","name":"Build","outcomes":[],"bogusKey":1}`
	if _, err := DecodeBody[core.Belt](bodyRequest(beltDraftJSON(stage)), DefaultBodyLimit); requestMessage(err) != `Unknown field "bogusKey"` {
		t.Fatalf("stage key: %v", err)
	}
	trigger := `{"id":"belt-a","name":"Belt","version":1,"triggers":[{"id":"t1","trigger":{"kind":"issues","jql":"x"},"repos":[]}],"stages":[]}`
	if _, err := DecodeBody[core.Belt](bodyRequest(trigger), DefaultBodyLimit); requestMessage(err) != `Unknown field "jql"` {
		t.Fatalf("trigger key of another kind: %v", err)
	}
	policies := `{"id":"belt-a","name":"Belt","version":1,"triggers":[],"policies":{"stopOnClose":true,"extra":true},"stages":[]}`
	if _, err := DecodeBody[core.Belt](bodyRequest(policies), DefaultBodyLimit); requestMessage(err) != `Unknown field "extra"` {
		t.Fatalf("policies key: %v", err)
	}
	draft := `{"draft":` + beltDraftJSON(`{"id":"s1","type":"agent","name":"Build","outcomes":[],"bogusKey":1}`) + `}`
	if _, err := DecodeBody[api.BeltDraftOnlyRequest](bodyRequest(draft), DefaultBodyLimit); requestMessage(err) != `Unknown field "bogusKey"` {
		t.Fatalf("draft wrapper: %v", err)
	}
}

// TestDecodeBodyAcceptsStoredBeltDraft checks that the draft a golden records, which
// the web app sends back, decodes as a belt draft.
func TestDecodeBodyAcceptsStoredBeltDraft(t *testing.T) {
	t.Parallel()
	payload, err := os.ReadFile("../../../testdata/golden/belt-detail.json")
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Draft json.RawMessage `json:"draft"`
	}
	if err := json.Unmarshal(payload, &detail); err != nil || len(detail.Draft) == 0 {
		t.Fatalf("golden draft: %v", err)
	}
	if _, err := DecodeBody[api.BeltDraftOnlyRequest](bodyRequest(`{"draft":`+string(detail.Draft)+`}`), DefaultBodyLimit); err != nil {
		t.Fatalf("stored draft: %v", err)
	}
}

// TestDecodeBodyAcceptsEncodedTriggers checks that every kind of trigger, as the
// daemon encodes it, decodes back: the walk's shape of a trigger is its encoding.
func TestDecodeBodyAcceptsEncodedTriggers(t *testing.T) {
	t.Parallel()
	for _, kind := range core.AllTriggerKind {
		payload, err := json.Marshal(kind.Fresh())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeBody[core.Trigger](bodyRequest(string(payload)), DefaultBodyLimit); err != nil {
			t.Fatalf("%s trigger %s: %v", kind, payload, err)
		}
		belt := core.Belt{
			ID: "belt-a", Name: "Belt", Version: 1, Afk: core.AfkModeGlobal,
			Triggers: []core.TriggerEntry{{ID: "t1", Trigger: kind.Fresh(), Repos: []string{"owner/repo"}}},
		}
		payload, err = json.Marshal(belt)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeBody[core.Belt](bodyRequest(string(payload)), DefaultBodyLimit); err != nil {
			t.Fatalf("%s belt %s: %v", kind, payload, err)
		}
	}
}

// TestReadRouteSupportJSONChecksEnumsNotKeys checks the command-body path: a bad
// enum is refused, and an undeclared key is ignored as it always was there.
func TestReadRouteSupportJSONChecksEnumsNotKeys(t *testing.T) {
	t.Parallel()
	if _, err := ReadRouteSupportJSON[api.BeltAfkRequest](bodyRequest(`{"afk":"bogus"}`), false); err == nil {
		t.Fatal("unknown afk mode was accepted")
	}
	body, err := ReadRouteSupportJSON[api.BeltAfkRequest](bodyRequest(`{"afk":"always","bogus":1}`), false)
	if err != nil || body.Afk != core.AfkModeAlways {
		t.Fatalf("undeclared key: %+v %v", body, err)
	}
}

// TestRequestEnumTypesKnowTheirCases fails for a named string type a request body
// can reach that has no Known method, so a new enum cannot skip the walk.
func TestRequestEnumTypesKnowTheirCases(t *testing.T) {
	t.Parallel()
	requestTypes := []any{
		api.BeltAfkRequest{}, api.BeltDraftOnlyRequest{}, api.BeltSaveRequest{}, api.BeltUpdateStageRequest{},
		api.BeltContextRequest{}, api.BeltStageTemplateRequest{}, api.BeltPauseRequest{}, core.BeltDraftRequest{},
		core.Belt{}, core.Stage{}, core.Trigger{}, core.Fork{}, core.Then{},
		api.AgentSaveRequest{}, api.AgentDraftPresentationRequest{}, api.AgentDraftSwitchRunnerRequest{},
		api.AssistantStartRequest{}, coordinator.AssistantConfiguration{}, coordinator.SettingsPatch{},
		api.LibraryItemRequest{}, api.LibraryRepoRequest{}, api.ModelAllowRequest{},
		api.PauseRequest{}, api.AfkRequest{},
		api.RunDecideRequest{}, api.RunResumeRequest{}, api.RunAnswerRequest{}, api.RunLegRequest{},
		api.RunAttemptRequest{}, api.RunAdjustSwitchRequest{}, api.RunAdjustStageRequest{},
		api.RunAfkRequest{}, api.RunOverruleAfkRequest{}, api.RunTaskRequest{},
	}
	named := map[reflect.Type]bool{}
	seen := map[reflect.Type]bool{}
	for _, v := range requestTypes {
		collectNamedStrings(reflect.TypeOf(v), seen, named)
	}
	if len(named) == 0 {
		t.Fatal("no named string types were found")
	}
	for typ := range named {
		if _, ok := reflect.New(typ).Elem().Interface().(knownCase); !ok {
			t.Errorf("%s is a string type a request reaches, but it has no Known method", typ)
		}
	}
}

// collectNamedStrings adds to out every named string type reached from t through
// its exported fields, map keys and elements.
func collectNamedStrings(t reflect.Type, seen map[reflect.Type]bool, out map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	if t.Kind() == reflect.Map {
		collectNamedStrings(t.Key(), seen, out)
		collectNamedStrings(t.Elem(), seen, out)
		return
	}
	if seen[t] {
		return
	}
	seen[t] = true
	if t.Kind() == reflect.String && t.PkgPath() != "" {
		out[t] = true
		return
	}
	if t.Kind() == reflect.Struct {
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() || f.Anonymous {
				collectNamedStrings(f.Type, seen, out)
			}
		}
	}
}

// TestRequestWalkNamesTheFirstUnknownKey checks that the unknown key reported is the
// first in sorted order, so the same body always gets the same message.
func TestRequestWalkNamesTheFirstUnknownKey(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[strictBody](bodyRequest(`{"name":"a","zeta":1,"alpha":2}`), DefaultBodyLimit)
	if got := requestMessage(err); got != `Unknown field "alpha"` {
		t.Fatalf("message %q, error %v", got, err)
	}
}
