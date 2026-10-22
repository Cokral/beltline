// Covers the required members a request body must give (the request walk's
// required:"true" check): a belt, stage or then missing one is refused, and the
// bodies the web app and the goldens send are still accepted.

package routes

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// missingMessage is the 422 a body without a required member gets.
const missingMessage = "Invalid request body. The data couldn't be read because it is missing."

// requiredBelt is a belt body with every member a request must give.
const requiredBelt = `{"id":"belt-a","name":"Belt","version":1,` +
	`"triggers":[{"id":"t1","trigger":{"kind":"issues"},"repos":["owner/repo"]}],"stages":[]}`

// withMember returns the JSON object payload with member set to value (raw JSON),
// or without it when value is nil.
func withMember(t *testing.T, payload string, member string, value json.RawMessage) string {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &members); err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(members, member)
	} else {
		members[member] = value
	}
	out, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestDecodeBodyRejectsMissingRequiredBeltMember checks that a belt body missing id,
// name, version, triggers or stages, or giving one null, is a missing-data 422.
func TestDecodeBodyRejectsMissingRequiredBeltMember(t *testing.T) {
	t.Parallel()
	for _, member := range []string{"id", "name", "version", "triggers", "stages"} {
		absent := withMember(t, requiredBelt, member, nil)
		if _, err := DecodeBody[core.Belt](bodyRequest(absent), DefaultBodyLimit); requestMessage(err) != missingMessage {
			t.Fatalf("belt without %s: message %q, error %v", member, requestMessage(err), err)
		}
		null := withMember(t, requiredBelt, member, json.RawMessage("null"))
		if _, err := DecodeBody[core.Belt](bodyRequest(null), DefaultBodyLimit); requestMessage(err) != missingMessage {
			t.Fatalf("belt with %s null: message %q, error %v", member, requestMessage(err), err)
		}
	}
}

// TestDecodeBodyRejectsMissingRequiredNestedMember checks the stage and then members
// a body must give, at the level they sit in a belt.
func TestDecodeBodyRejectsMissingRequiredNestedMember(t *testing.T) {
	t.Parallel()
	stages := map[string]string{
		"id":       `{"type":"agent","name":"Build","outcomes":[]}`,
		"type":     `{"id":"s1","name":"Build","outcomes":[]}`,
		"name":     `{"id":"s1","type":"agent","outcomes":[]}`,
		"outcomes": `{"id":"s1","type":"agent","name":"Build"}`,
	}
	for member, stage := range stages {
		if _, err := DecodeBody[core.Belt](bodyRequest(beltDraftJSON(stage)), DefaultBodyLimit); requestMessage(err) != missingMessage {
			t.Fatalf("stage without %s: message %q, error %v", member, requestMessage(err), err)
		}
	}
	stage := `{"id":"s1","type":"agent","name":"Build","outcomes":[{"id":"o1","name":"Done","when":"","tone":"good","then":{}}]}`
	if _, err := DecodeBody[core.Belt](bodyRequest(beltDraftJSON(stage)), DefaultBodyLimit); requestMessage(err) != missingMessage {
		t.Fatalf("then without do: message %q, error %v", requestMessage(err), err)
	}
}

// TestDecodeBodyAcceptsRequiredMembersGiven checks that a belt with every required
// member, even an empty stage list, is accepted.
func TestDecodeBodyAcceptsRequiredMembersGiven(t *testing.T) {
	t.Parallel()
	if _, err := DecodeBody[core.Belt](bodyRequest(requiredBelt), DefaultBodyLimit); err != nil {
		t.Fatalf("belt with required members: %v", err)
	}
}

// TestDataFilesDoNotRequireMembers checks that the data-file path (encoding/json,
// as the stores read belts) still loads a belt without its id, so the requirement
// is for request bodies only.
func TestDataFilesDoNotRequireMembers(t *testing.T) {
	t.Parallel()
	var belt core.Belt
	if err := json.Unmarshal([]byte(`{"name":"Belt","version":1,"triggers":[],"stages":[]}`), &belt); err != nil {
		t.Fatalf("data file without id: %v", err)
	}
}

// TestDraftEndpointBodiesAcceptStoredDrafts checks the belt draft endpoints' body,
// read by ReadRouteSupportJSON, for the draft a golden records, and refuses a draft
// without its id as the strict decoder does.
func TestDraftEndpointBodiesAcceptStoredDrafts(t *testing.T) {
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
	body := `{"draft":` + string(detail.Draft) + `}`
	if _, err := ReadRouteSupportJSON[api.BeltDraftOnlyRequest](bodyRequest(body), false); err != nil {
		t.Fatalf("draft endpoint, stored draft: %v", err)
	}
	noID := withMember(t, string(detail.Draft), "id", nil)
	if _, err := ReadRouteSupportJSON[api.BeltDraftOnlyRequest](bodyRequest(`{"draft":`+noID+`}`), false); err == nil {
		t.Fatal("draft endpoint accepted a draft without an id")
	}
}
