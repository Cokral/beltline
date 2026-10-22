// Tests the authenticated double-slash case: the router omits empty path segments, so //api/v1/version
// with a token answers as /api/v1/version does.

package server

import (
	"net/http"
	"testing"
)

// TestAuthenticatedDoubleSlashAPIPathAnswersAsTheRoute checks that //api/v1/version with
// the token gets the version route's answer, and that a traversal stays with the static
// handler.
func TestAuthenticatedDoubleSlashAPIPathAnswersAsTheRoute(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	want := send(h, http.MethodGet, "/api/v1/version", bearerHeader(testToken))
	if want.status != http.StatusOK {
		t.Fatalf("version: status %d", want.status)
	}
	got := send(h, http.MethodGet, "//api/v1/version", bearerHeader(testToken))
	if got.status != http.StatusOK || string(got.body) != string(want.body) {
		t.Fatalf("//api/v1/version: status %d body %s, want %d %s", got.status, got.body, want.status, want.body)
	}
	if traversal := send(h, http.MethodGet, "/api/../api/v1/version", bearerHeader(testToken)); traversal.status == http.StatusOK {
		t.Fatalf("traversal answered 200")
	}
}
