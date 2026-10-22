// at the handler level. The
// cases whose targets a request line cannot carry (spaces, invalid escapes) are
// in server_listener_integration_test.go, where the bytes go over a socket.
//
// Most cases run twice: once with the files in a directory, and once with the same
// files in an in-memory tree (testing/fstest), through the same static serving path.
package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// webFixture is StaticWebTests.WebFixture: a temporary web root with files in it.
// Every file written is also recorded in mem, the same tree in memory.
type webFixture struct {
	parent string
	root   string
	mem    fstest.MapFS
}

// newWebFixture makes an empty web root under a temporary parent.
func newWebFixture(t *testing.T) *webFixture {
	t.Helper()
	parent := t.TempDir()
	root := filepath.Join(parent, "beltline-web")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return &webFixture{parent: parent, root: root, mem: fstest.MapFS{}}
}

// write creates a file under the root with the given contents.
func (f *webFixture) write(t *testing.T, rel, contents string) {
	t.Helper()
	file := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	f.mem[rel] = &fstest.MapFile{Data: []byte(contents)}
}

// symlink makes link under the root point at target. Links exist on disk only.
func (f *webFixture) symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(f.root, link)); err != nil {
		t.Fatal(err)
	}
}

// webSourceCase is one way a fixture's files are served.
type webSourceCase struct {
	name   string
	router http.Handler
}

// sources serves the fixture's files from its directory and from its in-memory
// tree, so each behaviour is checked against both sources.
func (f *webFixture) sources() []webSourceCase {
	return []webSourceCase{
		{name: "disk", router: testRouter(testToken, "dev", f.root, nil)},
		{name: "memory", router: testRouterSource(testToken, "dev", staticWebSource{files: f.mem}, nil)},
	}
}

// TestServesFilesAndSpaFallbackWithMethodAndCacheParity
func TestServesFilesAndSpaFallbackWithMethodAndCacheParity(t *testing.T) {
	t.Parallel()
	f := newWebFixture(t)
	f.write(t, "index.html", "<!doctype html><title>fixture shell</title>")
	f.write(t, "assets/index-A1b2C3d4.js", "export const fixture = true;")
	f.write(t, "assets/index-AbCdEf09.css", "body { color: red }")
	f.write(t, "fonts/inter.woff2", "font-fixture")
	for _, source := range f.sources() {
		t.Run(source.name, func(t *testing.T) {
			checkServesFilesAndSpaFallback(t, source.router)
		})
	}
}

// checkServesFilesAndSpaFallback is the body of TestServesFilesAndSpaFallbackWithMethodAndCacheParity for one source.
func checkServesFilesAndSpaFallback(t *testing.T, h http.Handler) {
	t.Helper()
	csp := fmt.Sprintf("default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ws://127.0.0.1:%d; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'", testPort)
	shell := send(h, http.MethodGet, "/", nil)
	if shell.status != 200 {
		t.Fatalf("shell status %d", shell.status)
	}
	if shell.text() != "<!doctype html><title>fixture shell</title>" {
		t.Fatalf("shell body %q", shell.text())
	}
	expect := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: %q, want %q", name, got, want)
		}
	}
	expect("Content-Type", shell.header("Content-Type"), "text/html; charset=utf-8")
	expect("Cache-Control", shell.header("Cache-Control"), "no-store")
	expect("Content-Security-Policy", shell.header("Content-Security-Policy"), csp)
	expect("X-Content-Type-Options", shell.header("X-Content-Type-Options"), "nosniff")
	expect("Referrer-Policy", shell.header("Referrer-Policy"), "no-referrer")
	expect("/index.html Cache-Control", send(h, http.MethodGet, "/index.html", nil).header("Cache-Control"), "no-store")

	for _, route := range []string{"/overview", "/work", "/runs/example"} {
		fallback := send(h, http.MethodGet, route, nil)
		if fallback.status != 200 {
			t.Fatalf("%s: status %d", route, fallback.status)
		}
		if string(fallback.body) != string(shell.body) {
			t.Fatalf("%s: body differs from the shell", route)
		}
		expect(route+" Cache-Control", fallback.header("Cache-Control"), "no-store")
		expect(route+" Content-Security-Policy", fallback.header("Content-Security-Policy"), csp)
		expect(route+" nosniff", fallback.header("X-Content-Type-Options"), "nosniff")
		expect(route+" Referrer-Policy", fallback.header("Referrer-Policy"), "no-referrer")
	}

	assets := []struct{ path, bytes, contentType string }{
		{"/assets/index-A1b2C3d4.js", "export const fixture = true;", "text/javascript; charset=utf-8"},
		{"/assets/index-AbCdEf09.css", "body { color: red }", "text/css; charset=utf-8"},
		{"/fonts/inter.woff2", "font-fixture", "font/woff2"},
	}
	for _, a := range assets {
		reply := send(h, http.MethodGet, a.path, nil)
		if reply.status != 200 || reply.text() != a.bytes {
			t.Fatalf("%s: status %d body %q", a.path, reply.status, reply.text())
		}
		expect(a.path+" Content-Type", reply.header("Content-Type"), a.contentType)
		expect(a.path+" nosniff", reply.header("X-Content-Type-Options"), "nosniff")
		expect(a.path+" Referrer-Policy", reply.header("Referrer-Policy"), "no-referrer")
		expect(a.path+" CSP", reply.header("Content-Security-Policy"), csp)
		head := send(h, http.MethodHead, a.path, nil)
		if head.status != reply.status {
			t.Fatalf("%s: HEAD status %d", a.path, head.status)
		}
		if fmt.Sprint(head.headers) != fmt.Sprint(reply.headers) {
			t.Fatalf("%s: HEAD headers %v, GET %v", a.path, head.headers, reply.headers)
		}
		if len(head.body) != 0 {
			t.Fatalf("%s: HEAD carried a body", a.path)
		}
	}
	expect("hashed js cache", send(h, http.MethodGet, assets[0].path, nil).header("Cache-Control"), "public, max-age=31536000, immutable")
	expect("hashed css cache", send(h, http.MethodGet, assets[1].path, nil).header("Cache-Control"), "public, max-age=31536000, immutable")
	expect("plain asset cache", send(h, http.MethodGet, assets[2].path, nil).header("Cache-Control"), "no-cache")
	if got := send(h, http.MethodPost, "/overview", nil).status; got != 404 {
		t.Fatalf("POST /overview: status %d", got)
	}
	if got := send(h, http.MethodGet, "/assets/missing.js", nil).status; got != 404 {
		t.Fatalf("missing asset: status %d", got)
	}
}

// TestConfinesRawTargetsAndSymlinksWithoutDirectoryListings
// The
// unsafe-target and directory cases run on both sources; the symlink cases are
// disk-only, since a link is a fact of the directory.
func TestConfinesRawTargetsAndSymlinksWithoutDirectoryListings(t *testing.T) {
	t.Parallel()
	f := newWebFixture(t)
	outside := filepath.Join(f.parent, "web-sibling-prefix-sentinel.txt")
	if err := os.WriteFile(outside, []byte("outside-secret-sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write(t, "index.html", "shell-sentinel")
	f.write(t, "assets/inside.js", "inside-bytes")
	f.symlink(t, filepath.Join(f.root, "assets", "inside.js"), "assets/in-root.js")
	f.symlink(t, outside, "escape.txt")
	f.symlink(t, filepath.Dir(outside), "escape-dir")

	for _, source := range f.sources() {
		t.Run(source.name, func(t *testing.T) {
			checkUnsafeTargetsStayConfined(t, source.router)
		})
	}

	h := testRouter(testToken, "dev", f.root, nil)
	if inside := send(h, http.MethodGet, "/assets/in-root.js", nil); inside.status != 200 || inside.text() != "inside-bytes" {
		t.Fatalf("in-root link: status %d body %q", inside.status, inside.text())
	}
	if err := os.Remove(filepath.Join(f.root, "index.html")); err != nil {
		t.Fatal(err)
	}
	f.symlink(t, outside, "index.html")
	escapedShell := send(h, http.MethodGet, "/overview", nil)
	if escapedShell.status != 404 || strings.Contains(escapedShell.text(), "outside-secret-sentinel") {
		t.Fatalf("escaped shell: status %d", escapedShell.status)
	}
}

// checkUnsafeTargetsStayConfined asserts that no unsafe raw target, deep traversal
// or directory answers with a file, the shell or a listing.
func checkUnsafeTargetsStayConfined(t *testing.T, h http.Handler) {
	t.Helper()
	for _, target := range []string{
		"/%2e%2e/web-sibling-prefix-sentinel.txt", "/%252e%252e/web-sibling-prefix-sentinel.txt",
		"/%2e.%2fsecret", "/%5c..%5csecret", "/escape.txt",
		"/escape-dir/web-sibling-prefix-sentinel.txt", "/assets/", "/nul%00path",
	} {
		reply := send(h, http.MethodGet, target, nil)
		if reply.status != 400 && reply.status != 404 {
			t.Fatalf("%s: status %d", target, reply.status)
		}
		for _, leak := range []string{"outside-secret-sentinel", "shell-sentinel", "Index of"} {
			if strings.Contains(reply.text(), leak) {
				t.Fatalf("%s: leaks %q", target, leak)
			}
		}
	}
	deep := send(h, http.MethodGet, "/%2e%2e/secret", nil)
	if deep.status != 400 && deep.status != 404 || strings.Contains(deep.text(), "shell-sentinel") {
		t.Fatalf("deep traversal: status %d", deep.status)
	}
	directory := send(h, http.MethodGet, "/assets", nil)
	if directory.status != 404 || strings.Contains(directory.text(), "inside.js") {
		t.Fatalf("directory: status %d", directory.status)
	}
}

// TestRunRoutesWithEncodedSlashesReloadThroughTheShell
func TestRunRoutesWithEncodedSlashesReloadThroughTheShell(t *testing.T) {
	t.Parallel()
	f := newWebFixture(t)
	f.write(t, "index.html", "shell-sentinel")
	for _, source := range f.sources() {
		t.Run(source.name, func(t *testing.T) {
			checkRunRoutesReloadThroughTheShell(t, source.router)
		})
	}
}

// checkRunRoutesReloadThroughTheShell is the body of TestRunRoutesWithEncodedSlashesReloadThroughTheShell for one source.
func checkRunRoutesReloadThroughTheShell(t *testing.T, h http.Handler) {
	t.Helper()
	run := send(h, http.MethodGet, "/runs/issues%3Aacme%2Fweb%23151", nil)
	if run.status != 200 || !strings.Contains(run.text(), "shell-sentinel") {
		t.Fatalf("run route: status %d", run.status)
	}
	for _, target := range []string{"/api%2Fv1/version", "/runs/%2E%2E%2Fsecret", "/runs/a%2F..%2Fb", "/runs/x%2Fy.js", "/runs/a%5Cb%2Fc"} {
		reply := send(h, http.MethodGet, target, nil)
		if reply.status != 400 && reply.status != 401 && reply.status != 404 {
			t.Fatalf("%s: status %d", target, reply.status)
		}
		if strings.Contains(reply.text(), "shell-sentinel") {
			t.Fatalf("%s: served the shell", target)
		}
	}
}

// TestReservedNamespacesAndHostOriginKeepTheirExistingPolicies
func TestReservedNamespacesAndHostOriginKeepTheirExistingPolicies(t *testing.T) {
	t.Parallel()
	f := newWebFixture(t)
	f.write(t, "index.html", "static-shell")
	f.write(t, "api/v1/version", "must-not-serve")
	f.write(t, "auth/index.html", "must-not-serve")
	f.write(t, "artifacts/index.html", "must-not-serve")
	for _, source := range f.sources() {
		t.Run(source.name, func(t *testing.T) {
			checkReservedNamespacesAndHostOrigin(t, source.router)
		})
	}
}

// checkReservedNamespacesAndHostOrigin is the body of TestReservedNamespacesAndHostOriginKeepTheirExistingPolicies for one source.
func checkReservedNamespacesAndHostOrigin(t *testing.T, h http.Handler) {
	t.Helper()
	if got := send(h, http.MethodGet, "/", nil).status; got != 200 {
		t.Fatalf("shell: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/version", nil).status; got != 401 {
		t.Fatalf("version unauthenticated: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/missing", bearerHeader(testToken)).status; got != 404 {
		t.Fatalf("missing with bearer: status %d", got)
	}
	if got := send(h, http.MethodGet, "/auth?token=invalid", nil).status; got != 401 {
		t.Fatalf("auth with bad token: status %d", got)
	}
	for _, path := range []string{"/api", "/api/", "//api/v1/version", "/%61pi/v1/version", "/auth/", "/%61uth/", "/artifacts", "//artifacts/anything"} {
		reply := send(h, http.MethodGet, path, bearerHeader(testToken))
		if strings.Contains(reply.text(), "static-shell") || strings.Contains(reply.text(), "must-not-serve") {
			t.Fatalf("%s: served a file", path)
		}
	}
	for _, path := range []string{"/api", "/api/", "//api/v1/version", "/%61pi/v1/version", "/%2561pi/v1/version"} {
		if got := send(h, http.MethodGet, path, nil).status; got != 401 {
			t.Fatalf("%s unauthenticated: status %d", path, got)
		}
	}
	for _, headers := range []http.Header{
		headerOf("Host", "localhost:"+fmt.Sprint(testPort)),
		headerOf("Origin", fmt.Sprintf("https://127.0.0.1:%d", testPort)),
	} {
		file := send(h, http.MethodGet, "/assets/missing-route", headers)
		if file.status != 403 || strings.Contains(file.text(), "static-shell") {
			t.Fatalf("%v: status %d", headers, file.status)
		}
	}
	if got := send(h, http.MethodGet, "/api/v1/version", bearerHeader(testToken)).status; got != 200 {
		t.Fatalf("version with bearer: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/missing", headerOf("Cookie", "beltline_auth="+testToken)).status; got != 404 {
		t.Fatalf("missing with cookie: status %d", got)
	}
}

// TestMissingOrUnreadableShellShowsPlainNotBuiltPageWithoutBreakingApi
// A directory override that is absent or has no readable index.html answers the
// override's not-built page, the same for GET and HEAD, and the API keeps working.
func TestMissingOrUnreadableShellShowsPlainNotBuiltPageWithoutBreakingApi(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	absent := filepath.Join(parent, "absent")
	empty := filepath.Join(parent, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(empty, "index.html"), []byte("private shell"), 0o000); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{absent, empty} {
		h := testRouter(testToken, "dev", root, nil)
		for _, path := range []string{"/", "/overview", "/runs/example"} {
			get := send(h, http.MethodGet, path, nil)
			head := send(h, http.MethodHead, path, nil)
			for _, reply := range []testReply{get, head} {
				if reply.status != 503 {
					t.Fatalf("%s %s: status %d", root, path, reply.status)
				}
			}
			if !strings.Contains(get.text(), "<h1>Web app not built</h1>") ||
				!strings.Contains(get.text(), "Check BELTLINE_WEB_DIR points to a readable web build containing index.html, then restart beltlined.") {
				t.Fatalf("%s %s: page %q", root, path, get.text())
			}
			if strings.Contains(get.text(), "<script") {
				t.Fatalf("%s %s: page has a script", root, path)
			}
			if get.header("Cache-Control") != "no-store" || get.header("Content-Security-Policy") == "" ||
				get.header("X-Content-Type-Options") != "nosniff" || get.header("Referrer-Policy") != "no-referrer" {
				t.Fatalf("%s %s: headers %v", root, path, get.headers)
			}
			if fmt.Sprint(head.headers) != fmt.Sprint(get.headers) {
				t.Fatalf("%s %s: HEAD headers %v, GET %v", root, path, head.headers, get.headers)
			}
			if len(head.body) != 0 {
				t.Fatalf("%s %s: HEAD carried a body", root, path)
			}
		}
		if got := send(h, http.MethodGet, "/api/v1/version", bearerHeader(testToken)).status; got != 200 {
			t.Fatalf("%s: API with bearer: status %d", root, got)
		}
		if got := send(h, http.MethodGet, "/api/v1/version", nil).status; got != 401 {
			t.Fatalf("%s: API without credentials: status %d", root, got)
		}
		if got := send(h, http.MethodGet, "/artifacts/missing", nil).status; got == 503 {
			t.Fatalf("%s: artifacts path answered as not built", root)
		}
	}
}

// TestDefaultRootIsTheOverrideOnlyWhenNonempty checks the default-root rule of
// the static web root: only a nonempty override is used. Now that nothing is looked up beside the
// executable: an unset or empty
// BELTLINE_WEB_DIR selects the bundled app (the empty root), and a nonempty one is
// the directory made absolute from the working directory.
func TestDefaultRootIsTheOverrideOnlyWhenNonempty(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if got := StaticWebDefaultRoot(env(nil)); got != "" {
		t.Fatalf("unset override: %q, want the bundled app", got)
	}
	if got := StaticWebDefaultRoot(env(map[string]string{"BELTLINE_WEB_DIR": ""})); got != "" {
		t.Fatalf("empty override: %q, want the bundled app", got)
	}
	if got := StaticWebDefaultRoot(env(map[string]string{"BELTLINE_WEB_DIR": "/custom web output"})); got != "/custom web output" {
		t.Fatalf("absolute override: %q", got)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := StaticWebDefaultRoot(env(map[string]string{"BELTLINE_WEB_DIR": "relative web build"})); got != filepath.Join(wd, "relative web build") {
		t.Fatalf("relative override: %q, want it under %q", got, wd)
	}
}
