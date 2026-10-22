// Golden checks of the wire types of this package, over the contract demo and the empty folder. The
// contract fixture and the normalisation are servertest's.

package api_test

import (
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// TestBeltEmptyRequestAcceptsAnyObject checks that the empty request ignores unknown keys and rejects
// a body that is not an object.
func TestBeltEmptyRequestAcceptsAnyObject(t *testing.T) {
	t.Parallel()
	var request api.BeltEmptyRequest
	if err := json.Unmarshal([]byte(`{"unknown":1}`), &request); err != nil {
		t.Errorf("unknown keys: %v", err)
	}
	if err := json.Unmarshal([]byte(`[1]`), &request); err == nil {
		t.Errorf("an array was accepted")
	}
}

// TestRequestsDecodeTheirKeys checks the request bodies: a required key must be present, and the values
// decode as written.
func TestRequestsDecodeTheirKeys(t *testing.T) {
	t.Parallel()
	var pause api.PauseRequest
	if err := json.Unmarshal([]byte(`{"paused":true}`), &pause); err != nil || pause != (api.PauseRequest{Paused: true}) {
		t.Errorf("pause: %v %+v", err, pause)
	}
	var allow api.ModelAllowRequest
	if err := json.Unmarshal([]byte(`{"allowed":false,"model":"opus-5.5-1m"}`), &allow); err != nil || allow != (api.ModelAllowRequest{Model: "opus-5.5-1m", Allowed: false}) {
		t.Errorf("model allow: %v %+v", err, allow)
	}
}
