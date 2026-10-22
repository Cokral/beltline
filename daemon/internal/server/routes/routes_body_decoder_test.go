// Covers the request body decoders on the messages the handlers answer with.

package routes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// bodyRequest is a POST whose body is the given text.
func bodyRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
}

// strictBody is a request type with one required and one optional member.
type strictBody struct {
	Name string  `json:"name" required:"true"`
	Note *string `json:"note,omitzero"`
}

// TestDecodeBodyRejectsUndeclaredFieldAs422 checks the rejectUnknownKeys text.
func TestDecodeBodyRejectsUndeclaredFieldAs422(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[strictBody](bodyRequest(`{"name":"a","alpha":2,"zeta":1}`), DefaultBodyLimit)
	var failure core.Failure
	if !errors.As(err, &failure) || failure.Message != `Unknown field "alpha"` || len(failure.Problems) != 0 {
		t.Fatalf("error %v", err)
	}
}

// TestDecodeBodyMessages checks the empty-body and invalid-body messages.
func TestDecodeBodyMessages(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[strictBody](bodyRequest(""), DefaultBodyLimit)
	var failure core.Failure
	if !errors.As(err, &failure) || failure.Message != "A JSON body is required." {
		t.Fatalf("empty body: %v", err)
	}
	_, err = DecodeBody[strictBody](bodyRequest(`{"note":"x"}`), DefaultBodyLimit)
	if !errors.As(err, &failure) || !strings.HasPrefix(failure.Message, "Invalid request body. The data couldn't be read because it is missing.") {
		t.Fatalf("missing field: %v", err)
	}
	_, err = DecodeBody[strictBody](bodyRequest(`{"name":`), DefaultBodyLimit)
	if !errors.As(err, &failure) || !strings.HasPrefix(failure.Message, "Invalid request body. ") {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestDecodeBodyLimit checks that a body over maxBytes is a 413, not a 422.
func TestDecodeBodyLimit(t *testing.T) {
	t.Parallel()
	_, err := DecodeBody[strictBody](bodyRequest(`{"name":"`+strings.Repeat("x", 100)+`"}`), 16)
	var httpErr HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 413 {
		t.Fatalf("oversized body: %v", err)
	}
}

// TestDecodeOptionalBodyFallsBackOnEmpty checks the optional decoder.
func TestDecodeOptionalBodyFallsBackOnEmpty(t *testing.T) {
	t.Parallel()
	got, err := DecodeOptionalBody(bodyRequest(""), strictBody{Name: "fallback"}, DefaultBodyLimit)
	if err != nil || got.Name != "fallback" {
		t.Fatalf("empty: %+v %v", got, err)
	}
	got, err = DecodeOptionalBody(bodyRequest(`{"name":"sent"}`), strictBody{Name: "fallback"}, DefaultBodyLimit)
	if err != nil || got.Name != "sent" {
		t.Fatalf("sent: %+v %v", got, err)
	}
}

// TestDecodeEmptyObjectOrNoBody checks the bodyless command rule.
func TestDecodeEmptyObjectOrNoBody(t *testing.T) {
	t.Parallel()
	if err := DecodeEmptyObjectOrNoBody(bodyRequest(""), DefaultBodyLimit); err != nil {
		t.Fatalf("no body: %v", err)
	}
	if err := DecodeEmptyObjectOrNoBody(bodyRequest("{}"), DefaultBodyLimit); err != nil {
		t.Fatalf("empty object: %v", err)
	}
	var failure core.Failure
	err := DecodeEmptyObjectOrNoBody(bodyRequest(`{"x":1}`), DefaultBodyLimit)
	if !errors.As(err, &failure) || failure.Message != "Body must be an empty JSON object." {
		t.Fatalf("members: %v", err)
	}
	err = DecodeEmptyObjectOrNoBody(bodyRequest(`[]`), DefaultBodyLimit)
	if !errors.As(err, &failure) || failure.Message != "Expected an empty JSON object." {
		t.Fatalf("array: %v", err)
	}
}

// TestReadRouteSupportQueryValues checks decoding and + handling of query values.
func TestReadRouteSupportQueryValues(t *testing.T) {
	t.Parallel()
	got := ReadRouteSupportQueryValues("kind", "kind=a+b&x=1&kind=c%2Fd&kin=no&kind=%ZZ")
	want := []string{"a b", "c/d", "%ZZ"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("values %q, want %q", got, want)
	}
	if v := ReadRouteSupportQuery("missing", "a=1"); v != nil {
		t.Fatalf("missing parameter: %v", *v)
	}
}

// TestReadRouteSupportCommandBody checks the command body rules.
func TestReadRouteSupportCommandBody(t *testing.T) {
	t.Parallel()
	if body, err := ReadRouteSupportCommandBody(bodyRequest(""), false); err != nil || body != nil {
		t.Fatalf("optional empty: %q %v", body, err)
	}
	var httpErr HTTPError
	if _, err := ReadRouteSupportCommandBody(bodyRequest(""), true); !errors.As(err, &httpErr) || httpErr.Status != 400 {
		t.Fatalf("required empty: %v", err)
	}
	if _, err := ReadRouteSupportCommandBody(bodyRequest("{"), false); !errors.As(err, &httpErr) || httpErr.Message != "The request body is not valid JSON." {
		t.Fatalf("malformed: %v", err)
	}
	if body, err := ReadRouteSupportCommandBody(bodyRequest("1"), false); err != nil || string(body) != "1" {
		t.Fatalf("fragment: %q %v", body, err)
	}
}

// BeltPauseRequest stands in for api.BeltPauseRequest: the 422 names the type,
// which is the Go type's name.
type BeltPauseRequest struct {
	Paused bool `json:"paused"`
}

// TestReadRouteSupportJSONNamesTheType checks that a malformed body names the
// type (R2 #1), not the Dictionary a struct describes as to jsonx.
func TestReadRouteSupportJSONNamesTheType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("["))
	_, err := ReadRouteSupportJSON[BeltPauseRequest](r, false)
	want := "The request body is not valid JSON for BeltPauseRequest."
	if f, ok := err.(core.ValidationFailure); !ok || f.Message != want {
		t.Fatalf("error %#v, want %q", err, want)
	}
}
