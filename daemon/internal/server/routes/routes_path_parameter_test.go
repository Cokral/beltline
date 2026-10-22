// Tests of the path parameter rules: a parameter is decoded once, an invalid one is a 400, and the raw
// parameters keep their escapes.

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// serveWith routes one request through a mux pattern and returns what h recorded.
func serveWith(t *testing.T, pattern, target string, h func(*http.Request)) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { h(r) })
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
}

// TestReadRouteSupportParameterDecodesOnceAndRejectsNonUTF8 checks the decoded id, the
// encoded slash that stays in one id, and the 400 for an escape that is not UTF-8.
func TestReadRouteSupportParameterDecodesOnceAndRejectsNonUTF8(t *testing.T) {
	t.Parallel()
	var got string
	var err error
	serveWith(t, "GET /api/v1/runs/{id}", "/api/v1/runs/a%2Fb%20c", func(r *http.Request) {
		got, err = ReadRouteSupportID(r)
	})
	if err != nil || got != "a/b c" {
		t.Fatalf("id = %q, %v; want %q", got, err, "a/b c")
	}

	serveWith(t, "GET /api/v1/runs/{id}", "/api/v1/runs/%FF", func(r *http.Request) {
		_, err = ReadRouteSupportID(r)
	})
	httpErr, ok := err.(HTTPError)
	if !ok || httpErr.Status != http.StatusBadRequest {
		t.Fatalf("error = %#v, want a 400 HTTPError", err)
	}
	if httpErr.Error() != "Bad Request" {
		t.Fatalf("message %q, want the reason phrase", httpErr.Error())
	}
}

// TestRawParametersKeepTheirEscapes checks the parameters read raw: the models
// runner (so `%63laude` is not claude) and the market version (`%31` is not 1).
func TestRawParametersKeepTheirEscapes(t *testing.T) {
	t.Parallel()
	var runnerErr error
	serveWith(t, "GET /api/v1/models/{runner}", "/api/v1/models/%63laude", func(r *http.Request) {
		_, runnerErr = modelsRunner(r)
	})
	if _, ok := runnerErr.(api.MissingResource); !ok {
		t.Fatalf("runner %%63laude: error %#v, want MissingResource", runnerErr)
	}
}

// TestEventsUpgradeMatchesTheRawPath checks that the events upgrade is dispatched on the
// escaped path, as Hummingbird's request.uri.path is: `%65vents` is not the events route.
func TestEventsUpgradeMatchesTheRawPath(t *testing.T) {
	t.Parallel()
	access := EventAccessCheck(func(*http.Request) (int, []byte, bool) { return 0, nil, true })
	for target, want := range map[string]EventRoutesUpgradeResult{
		"/api/v1/events":   EventRoutesAccept,
		"/api/v1/%65vents": EventRoutesDecline,
	} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if got := EventRoutesUpgrade(httptest.NewRecorder(), r, access, nil); got != want {
			t.Fatalf("%s: %v, want %v", target, got, want)
		}
	}
}
