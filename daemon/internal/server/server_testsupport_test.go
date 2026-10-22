// Helpers the handler tests use: the token, the reply shape, the request helpers and the golden
// reader. Tests here call the production router through httptest, without a listener; the listener
// tests are in server_listener_integration_test.go.

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/kit/launch"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// testToken is the bearer and cookie token the tests use.
const testToken = "test-token-0123456789abcdef"

// testPort is the port the tests put in Host and Origin.
const testPort = 40123

// testSigningKey is the artifact signing key handler tests use.
var testSigningKey = []byte("handler-test-artifact-signing-key-32-bytes")

// testReply is a response as the tests read it.
type testReply struct {
	status  int
	headers http.Header
	body    []byte
}

// text is the body as a string.
func (r testReply) text() string { return string(r.body) }

// header is the first value of a response header, "" when absent.
func (r testReply) header(name string) string { return r.headers.Get(name) }

// errorBody decodes the shared error shape from the body.
func (r testReply) errorBody(t *testing.T) api.ErrorResponse {
	t.Helper()
	var e api.ErrorResponse
	if err := json.Unmarshal(r.body, &e); err != nil {
		t.Fatalf("error body %q: %v", r.body, err)
	}
	return e
}

// testRouter builds the production router for testPort: the same composition
// the listener serves, without the listener.
func testRouter(token, version, webRoot string, configure func(mux *http.ServeMux)) http.Handler {
	signer := routes.ArtifactLinkSigner{Key: testSigningKey, Clock: func() time.Time { return testNow }}
	return ServerApplicationRouter(testPort, token, version, webRoot, nil, signer, routes.Dependencies{Platform: launch.PlatformMacOS}, configure)
}

// testRouterSource is testRouter with the web source chosen by the test: an
// in-memory tree, or a bundled stand-in beside a directory override.
func testRouterSource(token, version string, web staticWebSource, configure func(mux *http.ServeMux)) http.Handler {
	signer := routes.ArtifactLinkSigner{Key: testSigningKey, Clock: func() time.Time { return testNow }}
	return serverApplicationRouter(testPort, token, version, web, nil, signer, routes.Dependencies{Platform: launch.PlatformMacOS}, configure)
}

// testNow is the fixed clock the signer and the tests share.
var testNow = time.Date(2026, time.October, 7, 0, 0, 0, 0, time.UTC)

// testWebRoot is an empty directory standing for a web root that has no build.
func testWebRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "missing-web")
}

// bearerHeader is the Authorization header for token.
func bearerHeader(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h
}

// withHeaders returns a copy of base with the given header values set.
func withHeaders(base http.Header, extra map[string]string) http.Header {
	out := base.Clone()
	if out == nil {
		out = http.Header{}
	}
	for name, value := range extra {
		out.Set(name, value)
	}
	return out
}

// send serves one request through h, as a client on testPort would send it. The
// target is parsed the way net/http parses a request line. A Host entry in
// headers sets the request's Host, as net/http reads it.
func send(h http.Handler, method, target string, headers http.Header) testReply {
	req := httptest.NewRequest(method, target, nil)
	req.Host = "127.0.0.1:" + strconv.Itoa(testPort)
	for name, values := range headers {
		if name == "Host" {
			req.Host = values[0]
			continue
		}
		req.Header[name] = append([]string(nil), values...)
	}
	return serve(h, req)
}

// serve runs req through h and reads the recorded response.
func serve(h http.Handler, req *http.Request) testReply {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer func() { _ = res.Body.Close() }() // the response is read; a close error has nothing left to report
	body := rec.Body.Bytes()
	return testReply{status: res.StatusCode, headers: res.Header, body: append([]byte(nil), body...)}
}

// golden reads a recorded response from daemon/testdata/golden.
func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", name))
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	return data
}
