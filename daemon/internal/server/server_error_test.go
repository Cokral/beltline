// Tests of error mapping: each error is answered with the shared 422 shape, checked with the contract
// harness's 422 check.

package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// errorMappingRoutes are the handlers that exist only for these tests.
func errorMappingRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/test/failure", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return core.NewFailure("The branch is gone")
	}))
	mux.Handle("GET /api/v1/test/problems", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return core.NewFailure("belt a has no stages\nagent b is missing", "belt a has no stages", "agent b is missing")
	}))
	mux.Handle("GET /api/v1/test/missing", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return api.MissingResource{}
	}))
	mux.Handle("GET /api/v1/test/conflict", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return routes.HTTPError{Status: http.StatusConflict, Message: "Already running"}
	}))
	mux.Handle("GET /api/v1/test/teapot", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return routes.HTTPError{Status: 418}
	}))
	mux.Handle("GET /api/v1/test/opaque", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("opaque")
	}))
	mux.Handle("GET /api/v1/test/cancelled", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return context.Canceled
	}))
	mux.Handle("GET /api/v1/test/agent-file", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return core.AgentFileProblem{Message: "agent.md: the outcomes list is empty"}
	}))
	mux.Handle("GET /api/v1/test/contradiction", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return core.ModelCatalogContradiction{Message: "the catalog lists a model twice"}
	}))
	mux.Handle("GET /api/v1/test/tmux", routes.ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		return core.NewFailure("tmux: duplicate session: bl-continue-1")
	}))
}

// TestMapsLocalizedErrorsTo422 checks the ErrorDescription branch of ErrorMiddleware.map:
// an error with an errorDescription is a 422 with that message and no problems. The
// types are the ones a route can return (AgentFile.Problem and
// ModelCatalogContradiction); a tmux launch failure is a Failure.
func TestMapsLocalizedErrorsTo422(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	for _, c := range []struct{ path, message string }{
		{"/api/v1/test/agent-file", "agent.md: the outcomes list is empty"},
		{"/api/v1/test/contradiction", "the catalog lists a model twice"},
		{"/api/v1/test/tmux", "tmux: duplicate session: bl-continue-1"},
	} {
		reply := send(h, http.MethodGet, c.path, bearerHeader(testToken))
		checkError(t, reply, http.StatusUnprocessableEntity, api.ErrorResponse{Message: c.message, Problems: []string{}})
	}
}

// TestLocalizedErrorWithoutDescriptionIsLeftToTheServer: a LocalizedError whose
// errorDescription is nil is not answered by the middleware.
func TestLocalizedErrorWithoutDescriptionIsLeftToTheServer(t *testing.T) {
	t.Parallel()
	if status, _, ok := ErrorMiddlewareMap(silentLocalizedError{}); ok || status != 0 {
		t.Fatalf("nil errorDescription was answered with status %d", status)
	}
}

// silentLocalizedError has no errorDescription.
type silentLocalizedError struct{}

func (silentLocalizedError) Error() string                    { return "silent" }
func (silentLocalizedError) ErrorDescription() (string, bool) { return "", false }

// errorMappingRouter is the production router with the error-mapping handlers.
func errorMappingRouter(t *testing.T) http.Handler {
	return testRouter(testToken, "dev", testWebRoot(t), errorMappingRoutes)
}

// checkError asserts a reply's status, JSON content type, decoded error and exact body.
func checkError(t *testing.T, reply testReply, status int, want api.ErrorResponse) {
	t.Helper()
	if reply.status != status {
		t.Fatalf("status %d, want %d", reply.status, status)
	}
	if got := reply.header("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type %q", got)
	}
	got := reply.errorBody(t)
	if got.Message != want.Message || !slices.Equal(got.Problems, want.Problems) {
		t.Fatalf("error %+v, want %+v", got, want)
	}
	body, err := routes.JSONEncode(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(reply.body) != string(body) {
		t.Fatalf("body %s, want %s", reply.body, body)
	}
}

// require422 is ErrorContract.require422: a 422, JSON, no-store, the shared bytes.
func require422(t *testing.T, reply testReply, message string, problems []string) {
	t.Helper()
	if reply.status != 422 {
		t.Fatalf("expected HTTP 422, got %d", reply.status)
	}
	if reply.header("Content-Type") != "application/json" || reply.header("Cache-Control") != "no-store" {
		t.Fatal("422 responses must be JSON and no-store")
	}
	want, err := routes.JSONEncode(api.ErrorResponse{Message: message, Problems: problems})
	if err != nil {
		t.Fatal(err)
	}
	if string(reply.body) != string(want) {
		t.Fatalf("422 response %s did not match %s", reply.body, want)
	}
}

// TestMapsDomainErrorsTo422
func TestMapsDomainErrorsTo422(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	failure := send(h, http.MethodGet, "/api/v1/test/failure", bearerHeader(testToken))
	checkError(t, failure, 422, api.ErrorResponse{Message: "The branch is gone", Problems: []string{}})
	require422(t, failure, "The branch is gone", []string{})
	if string(failure.body) != string(golden(t, "error-failure.json")) {
		t.Fatalf("golden mismatch: %s", failure.body)
	}

	problems := send(h, http.MethodGet, "/api/v1/test/problems", bearerHeader(testToken))
	checkError(t, problems, 422, api.ErrorResponse{
		Message:  "belt a has no stages\nagent b is missing",
		Problems: []string{"belt a has no stages", "agent b is missing"},
	})
	require422(t, problems, "belt a has no stages\nagent b is missing", []string{"belt a has no stages", "agent b is missing"})
	if string(problems.body) != string(golden(t, "error-problems.json")) {
		t.Fatalf("golden mismatch: %s", problems.body)
	}
}

// TestMapsMissingResourcesAndUnknownRoutesTo404
func TestMapsMissingResourcesAndUnknownRoutesTo404(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	for _, path := range []string{"/api/v1/test/missing", "/api/v1/nothing-here", "/api/v2/version"} {
		reply := send(h, http.MethodGet, path, bearerHeader(testToken))
		checkError(t, reply, 404, api.ErrorNotFound)
		if string(reply.body) != string(golden(t, "error-not-found.json")) {
			t.Fatalf("%s: golden mismatch: %s", path, reply.body)
		}
	}
}

// TestKeepsExplicitStatuses
func TestKeepsExplicitStatuses(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	checkError(t, send(h, http.MethodGet, "/api/v1/test/conflict", bearerHeader(testToken)), 409,
		api.ErrorResponse{Message: "Already running", Problems: []string{}})
	teapot := send(h, http.MethodGet, "/api/v1/test/teapot", bearerHeader(testToken))
	if teapot.status != 418 {
		t.Fatalf("teapot status %d", teapot.status)
	}
	if got := teapot.errorBody(t); len(got.Problems) != 0 {
		t.Fatalf("teapot problems %v", got.Problems)
	}
}

// TestLeavesOtherErrorsToTheServer
func TestLeavesOtherErrorsToTheServer(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	if got := send(h, http.MethodGet, "/api/v1/test/opaque", bearerHeader(testToken)).status; got != 500 {
		t.Fatalf("opaque: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/test/cancelled", bearerHeader(testToken)).status; got != 500 {
		t.Fatalf("cancelled: status %d", got)
	}
}

// TestSecurityRejectionsComeFirst
func TestSecurityRejectionsComeFirst(t *testing.T) {
	t.Parallel()
	h := errorMappingRouter(t)
	for _, path := range []string{"/api/v1/test/failure", "/api/v1/test/missing", "/api/v1/nothing-here"} {
		unauthorized := send(h, http.MethodGet, path, nil)
		checkError(t, unauthorized, 401, api.ErrorUnauthorized)
		if string(unauthorized.body) != string(golden(t, "error-unauthorized.json")) {
			t.Fatalf("%s: golden mismatch: %s", path, unauthorized.body)
		}
		forbidden := send(h, http.MethodGet, path, withHeaders(bearerHeader(testToken), map[string]string{"Origin": "http://evil.example"}))
		checkError(t, forbidden, 403, api.ErrorForbidden)
		if string(forbidden.body) != string(golden(t, "error-forbidden.json")) {
			t.Fatalf("%s: golden mismatch: %s", path, forbidden.body)
		}
	}
}
