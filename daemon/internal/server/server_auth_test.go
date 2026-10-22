// Handler-level tests of credentials, cookie authorization, and the Host and Origin checks that
// run before them. The raw request-line cases are in server_listener_integration_test.go.

package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// authTestToken has characters that must be percent-encoded in a query.
const authTestToken = "tok-en_0123456789!$&'()*+:=@~"

// authPaths are the /api spellings the credential check must guard.
var authPaths = []string{"/api", "/api/v1/version", "/api/v1/missing", "/api/", "//api/v1/version", "/%61pi/v1/version", "/API/v1/version"}

// securityPaths are the paths the Host and Origin checks guard.
var securityPaths = []string{"/api/v1/version", "/api/v1/missing", "/", "/auth?token=" + testToken}

// percentEncodeAll percent-encodes every byte outside the unreserved set.
func percentEncodeAll(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// setCookieParts splits a Set-Cookie value into its trimmed attributes.
func setCookieParts(value string) []string {
	var parts []string
	for _, p := range strings.Split(value, ";") {
		parts = append(parts, strings.TrimSpace(p))
	}
	return parts
}

// headerOf is a header with one value.
func headerOf(name, value string) http.Header {
	h := http.Header{}
	h.Set(name, value)
	return h
}

// headerPairs is a header with the given name and value pairs.
func headerPairs(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

// securityHosts are hostile Host values, as a function of the bound port.
func securityHosts(port int) []string {
	return []string{
		"127.0.0.1",
		fmt.Sprintf("127.0.0.1:%d", port+1),
		fmt.Sprintf("localhost:%d", port),
		fmt.Sprintf("[::1]:%d", port),
		fmt.Sprintf("127.0.0.1.evil.example:%d", port),
		fmt.Sprintf("evil.example:%d", port),
		fmt.Sprintf("127.0.0.2:%d", port),
		fmt.Sprintf("LOCALHOST:%d", port),
	}
}

// securityOrigins are hostile Origin values, as a function of the bound port.
func securityOrigins(port int) []string {
	return []string{
		fmt.Sprintf("http://127.0.0.1:%d", port+1),
		fmt.Sprintf("https://127.0.0.1:%d", port),
		fmt.Sprintf("http://localhost:%d", port),
		fmt.Sprintf("http://[::1]:%d", port),
		fmt.Sprintf("http://127.0.0.1.evil.example:%d", port),
		"http://evil.example",
		"null",
		fmt.Sprintf("http://127.0.0.1:%d/", port),
		"not a url",
	}
}

// TestSetsTheSessionCookieAndRedirectsHome checks the auth redirect: its Location, the no-store headers,
// and a session cookie scoped to /api with no domain, expiry or secure attributes.
func TestSetsTheSessionCookieAndRedirectsHome(t *testing.T) {
	t.Parallel()
	h := testRouter(authTestToken, "dev", testWebRoot(t), nil)
	reply := send(h, http.MethodGet, "/auth?token="+percentEncodeAll(authTestToken), nil)
	if reply.status != 302 || reply.header("Location") != "/" || len(reply.body) != 0 {
		t.Fatalf("status %d, Location %q, body %q", reply.status, reply.header("Location"), reply.body)
	}
	if reply.header("Cache-Control") != "no-store" || reply.header("Referrer-Policy") != "no-referrer" {
		t.Fatalf("cache headers: %v", reply.headers)
	}
	parts := setCookieParts(reply.header("Set-Cookie"))
	if parts[0] != "beltline_auth="+authTestToken {
		t.Fatalf("cookie %q", parts[0])
	}
	if !slices.Equal(parts[1:], []string{"Path=/api", "HttpOnly", "SameSite=Strict"}) {
		t.Fatalf("attributes %v", parts[1:])
	}
	if strings.Contains(reply.header("Location"), authTestToken) {
		t.Fatal("Location carries the token")
	}
	// The cookie that the redirect sets authorizes the API, and only that.
	if got := send(h, http.MethodGet, "/api/v1/version", headerOf("Cookie", parts[0])).status; got != 200 {
		t.Fatalf("API with the cookie: status %d", got)
	}
}

// TestFollowingTheRedirectShowsThePlainNotBuiltPageWhenTheShellIsMissing checks the 503 page of a missing web app.
func TestFollowingTheRedirectShowsThePlainNotBuiltPageWhenTheShellIsMissing(t *testing.T) {
	t.Parallel()
	h := testRouter(authTestToken, "dev", testWebRoot(t), nil)
	redirect := send(h, http.MethodGet, "/auth?token="+percentEncodeAll(authTestToken), nil)
	page := send(h, http.MethodGet, redirect.header("Location"), nil)
	if page.status != 503 || !strings.Contains(page.text(), "<h1>Web app not built</h1>") ||
		!strings.Contains(page.text(), "Check BELTLINE_WEB_DIR points to a readable web build containing index.html, then restart beltlined.") {
		t.Fatalf("status %d, page: %s", page.status, page.text())
	}
	if page.header("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control %q", page.header("Cache-Control"))
	}
}

// TestRefusedAuthRedirectsSetNothingAndCheckHostAndOriginFirst checks every refused token and every hostile
// Host or Origin on the redirect: 401 or 403, no cookie, no Location, and no echo of the token.
func TestRefusedAuthRedirectsSetNothingAndCheckHostAndOriginFirst(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	refused := []struct {
		name    string
		query   string
		headers http.Header
		status  int
	}{
		{"no token", "", nil, 401},
		{"empty token", "?token=", nil, 401},
		{"wrong token", "?token=wrong", nil, 401},
		{"repeated token", "?token=" + testToken + "&token=" + testToken, nil, 401},
		{"token with suffix", "?token=" + testToken + "x", nil, 401},
		{"wrong parameter", "?tok=" + testToken, nil, 401},
		{"foreign Host", "?token=" + testToken, headerOf("Host", "localhost:"+fmt.Sprint(testPort)), 403},
		{"foreign Origin", "?token=" + testToken, headerOf("Origin", "http://evil.example"), 403},
		{"null Origin", "?token=" + testToken, headerOf("Origin", "null"), 403},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			reply := send(h, http.MethodGet, "/auth"+c.query, c.headers)
			if reply.status != c.status {
				t.Fatalf("status %d, want %d", reply.status, c.status)
			}
			if reply.header("Set-Cookie") != "" || reply.header("Location") != "" || strings.Contains(reply.text(), testToken) {
				t.Fatalf("refusal sets a cookie or location, or echoes the token: %v", reply.headers)
			}
		})
	}
	if same := send(h, http.MethodGet, "/auth?token="+testToken, headerOf("Origin", fmt.Sprintf("http://127.0.0.1:%d", testPort))); same.status != 302 {
		t.Fatalf("same origin: status %d", same.status)
	}
}

// TestCredentialsAreRefusedOnEveryAPIPath checks every refused credential on every /api spelling: 401 with
// the shared error, and a body that echoes no credential.
func TestCredentialsAreRefusedOnEveryAPIPath(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	duplicate := http.Header{}
	duplicate.Add("Authorization", "Bearer "+testToken)
	duplicate.Add("Authorization", "Bearer wrong")
	conflicting := http.Header{}
	conflicting.Add("Cookie", "beltline_auth="+testToken)
	conflicting.Add("Cookie", "beltline_auth=wrong")
	refused := []struct {
		name    string
		headers http.Header
		query   string
	}{
		{"absent", http.Header{}, ""},
		{"empty bearer", headerOf("Authorization", "Bearer "), ""},
		{"wrong bearer", headerOf("Authorization", "Bearer wrong"), "?"},
		{"bearer prefix", headerOf("Authorization", "Bearer "+testToken[:len(testToken)-1]), ""},
		{"bearer longer", headerOf("Authorization", "Bearer "+testToken+"x"), ""},
		{"basic", headerOf("Authorization", "Basic "+testToken), ""},
		{"no scheme", headerOf("Authorization", testToken), ""},
		{"extra part", headerOf("Authorization", "Bearer "+testToken+" x"), ""},
		{"empty cookie", headerOf("Cookie", "beltline_auth="), ""},
		{"wrong cookie", headerOf("Cookie", "beltline_auth=wrong"), ""},
		{"other cookie", headerOf("Cookie", "session="+testToken), ""},
		{"conflicting cookies", headerOf("Cookie", "beltline_auth="+testToken+"; beltline_auth=wrong"), ""},
		{"duplicate Authorization", duplicate, ""},
		{"conflicting cookie headers", conflicting, ""},
		{"wrong bearer, valid cookie", headerPairs("Authorization", "Bearer wrong", "Cookie", "beltline_auth="+testToken), ""},
		{"malformed authorization, valid cookie", headerPairs("Authorization", "nonsense", "Cookie", "beltline_auth="+testToken), ""},
		{"query only", http.Header{}, "?token=" + testToken},
	}
	for _, path := range authPaths {
		for _, c := range refused {
			reply := send(h, http.MethodGet, path+c.query, c.headers)
			if reply.status != 401 || reply.errorBody(t).Message != "Unauthorized" || reply.header("Content-Type") != "application/json" {
				t.Fatalf("%s %s: status %d", c.name, path, reply.status)
			}
			if strings.Contains(reply.text(), testToken) || strings.Contains(reply.text(), "wrong") {
				t.Fatalf("%s %s: body echoes a credential: %s", c.name, path, reply.body)
			}
		}
	}
}

// TestBearerOrCookieAuthorizesTheAPI checks the accepted credential forms on the version route and on an
// unknown route, which answers its own 404 for an authorized caller.
func TestBearerOrCookieAuthorizesTheAPI(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	for _, headers := range []http.Header{
		bearerHeader(testToken),
		headerOf("Authorization", "bearer "+testToken),
		headerOf("Cookie", "beltline_auth="+testToken),
		headerOf("Cookie", "other=1; beltline_auth="+testToken),
		headerOf("Cookie", "beltline_auth="+testToken+"; beltline_auth="+testToken),
		withHeaders(bearerHeader(testToken), map[string]string{"Cookie": "beltline_auth=wrong"}),
	} {
		version := send(h, http.MethodGet, "/api/v1/version", headers)
		if version.status != 200 {
			t.Fatalf("%v: version status %d", headers, version.status)
		}
		if missing := send(h, http.MethodGet, "/api/v1/missing", headers); missing.status != 404 || missing.errorBody(t).Message != "Not found" {
			t.Fatalf("%v: missing status %d", headers, missing.status)
		}
	}
}

// TestTokensAreScopedToTheirServer checks that a token authorizes its own server only, bearer or cookie.
func TestTokensAreScopedToTheirServer(t *testing.T) {
	t.Parallel()
	first := testRouter("first-token", "dev", testWebRoot(t), nil)
	second := testRouter("second-token", "dev", testWebRoot(t), nil)
	for _, c := range []struct {
		name string
		h    http.Handler
		head http.Header
		want int
	}{
		{"first with first", first, bearerHeader("first-token"), 200},
		{"first with second", first, bearerHeader("second-token"), 401},
		{"second with second", second, bearerHeader("second-token"), 200},
		{"second with first cookie", second, headerOf("Cookie", "beltline_auth=first-token"), 401},
	} {
		if got := send(c.h, http.MethodGet, "/api/v1/version", c.head).status; got != c.want {
			t.Fatalf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

// TestTokensThatAreNotCookieSafeStillWork checks that a token with characters a cookie cannot carry is
// sent percent-encoded, and that the encoded cookie and the bearer both authorize.
func TestTokensThatAreNotCookieSafeStillWork(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"a b", "a;b", "quote\"back\\slash,comma", "100%", "naïve-✓"} {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			h := testRouter(token, "dev", testWebRoot(t), nil)
			reply := send(h, http.MethodGet, "/auth?token="+percentEncodeAll(token), nil)
			if reply.status != 302 {
				t.Fatalf("status %d", reply.status)
			}
			value := strings.TrimPrefix(setCookieParts(reply.header("Set-Cookie"))[0], "beltline_auth=")
			if value != credentialCookieValue(token) {
				t.Fatalf("cookie value %q", value)
			}
			for i := 0; i < len(value); i++ {
				if c := value[i]; c <= 0x20 || c >= 0x7F || strings.ContainsRune("\";\\,", rune(c)) {
					t.Fatalf("cookie value %q has byte %#x", value, c)
				}
			}
			if got := send(h, http.MethodGet, "/api/v1/version", headerOf("Cookie", "beltline_auth="+value)).status; got != 200 {
				t.Fatalf("cookie: status %d", got)
			}
			if isASCII(token) {
				if got := send(h, http.MethodGet, "/api/v1/version", bearerHeader(token)).status; got != 200 {
					t.Fatalf("bearer: status %d", got)
				}
			}
		})
	}
}

// isASCII reports whether every byte of s is ASCII.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// TestEncodedCookieKeepsTokensApart checks that "a b" and "a%20b" never share a cookie value.
func TestEncodedCookieKeepsTokensApart(t *testing.T) {
	t.Parallel()
	if got := credentialCookieValue("a b"); got != "a%20b" {
		t.Fatalf("a b: %q", got)
	}
	if got := credentialCookieValue("a%20b"); got != "a%2520b" {
		t.Fatalf("a%%20b: %q", got)
	}
	h := testRouter("a%20b", "dev", testWebRoot(t), nil)
	if got := send(h, http.MethodGet, "/api/v1/version", headerOf("Cookie", "beltline_auth=a%20b")).status; got != 401 {
		t.Fatalf("other cookie: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/version", headerOf("Cookie", "beltline_auth=a%2520b")).status; got != 200 {
		t.Fatalf("right cookie: status %d", got)
	}
}

// TestWrongHostOrOriginIsForbiddenWhateverTheCredentials checks the Host and Origin checks on the API and the
// redirect: each hostile value is 403 with the shared error, for every credential and path.
func TestWrongHostOrOriginIsForbiddenWhateverTheCredentials(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	credentials := []http.Header{bearerHeader(testToken), bearerHeader("wrong"), {}, headerOf("Cookie", "beltline_auth="+testToken)}
	for _, host := range securityHosts(testPort) {
		for _, creds := range credentials {
			for _, path := range securityPaths {
				reply := send(h, http.MethodGet, path, withHeaders(creds, map[string]string{"Host": host}))
				if reply.status != 403 || reply.errorBody(t).Message != "Forbidden" || reply.header("Set-Cookie") != "" {
					t.Fatalf("host %s %s: status %d", host, path, reply.status)
				}
			}
		}
	}
	for _, origin := range securityOrigins(testPort) {
		for _, creds := range []http.Header{bearerHeader(testToken), bearerHeader("wrong"), {}} {
			for _, path := range securityPaths {
				reply := send(h, http.MethodGet, path, withHeaders(creds, map[string]string{"Origin": origin}))
				if reply.status != 403 || reply.errorBody(t).Message != "Forbidden" || strings.Contains(reply.text(), testToken) {
					t.Fatalf("origin %s %s: status %d", origin, path, reply.status)
				}
			}
		}
	}
}

// TestCanonicalHostAndOriginPassAndForwardingHeadersChangeNothing checks the accepted Host and Origin,
// that a missing Origin is accepted, and that forwarding headers never change the Host check.
func TestCanonicalHostAndOriginPassAndForwardingHeadersChangeNothing(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	canonical := withHeaders(bearerHeader(testToken), map[string]string{
		"Host":   fmt.Sprintf("127.0.0.1:%d", testPort),
		"Origin": fmt.Sprintf("http://127.0.0.1:%d", testPort),
	})
	if got := send(h, http.MethodGet, "/api/v1/version", canonical).status; got != 200 {
		t.Fatalf("canonical: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/version", bearerHeader(testToken)).status; got != 200 {
		t.Fatalf("no Origin: status %d", got)
	}
	forwarded := map[string]string{
		"X-Forwarded-Host":  fmt.Sprintf("127.0.0.1:%d", testPort),
		"X-Forwarded-Proto": "http",
		"Forwarded":         fmt.Sprintf("host=127.0.0.1:%d;proto=http", testPort),
		"X-Forwarded-For":   "127.0.0.1",
	}
	hostile := withHeaders(bearerHeader(testToken), forwarded)
	hostile.Set("Host", fmt.Sprintf("evil.example:%d", testPort))
	if got := send(h, http.MethodGet, "/api/v1/version", hostile).status; got != 403 {
		t.Fatalf("hostile host: status %d", got)
	}
	fine := withHeaders(bearerHeader(testToken), forwarded)
	fine.Set("X-Forwarded-Host", "evil.example")
	if got := send(h, http.MethodGet, "/api/v1/version", fine).status; got != 200 {
		t.Fatalf("forwarded host ignored: status %d", got)
	}
}

// TestGrantsNoCrossOriginAccess checks that no response carries a CORS header, for any Origin.
func TestGrantsNoCrossOriginAccess(t *testing.T) {
	t.Parallel()
	h := testRouter(testToken, "dev", testWebRoot(t), nil)
	for _, origin := range []string{"", fmt.Sprintf("http://127.0.0.1:%d", testPort), "http://evil.example"} {
		reply := send(h, http.MethodGet, "/api/v1/version", withHeaders(bearerHeader(testToken), map[string]string{"Origin": origin}))
		for name := range reply.headers {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				t.Fatalf("cross-origin header %s", name)
			}
		}
	}
}
