package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// TestOverrideServesDirectoryOverBundledSentinel checks that a nonempty
// BELTLINE_WEB_DIR serves its directory, paths with spaces included, in place of
// the bundled app, and that the bundled app serves only when no override is set.
// The bundled app is a stand-in in memory, so the check runs in every build.
func TestOverrideServesDirectoryOverBundledSentinel(t *testing.T) {
	t.Parallel()
	bundled := staticWebSource{bundled: true, files: fstest.MapFS{
		"index.html":             {Data: []byte("bundled-sentinel")},
		"assets/app-1a2b3c4d.js": {Data: []byte("bundled-asset")},
	}}
	f := newWebFixture(t)
	spaced := filepath.Join(f.parent, "web build with spaces")
	if err := os.MkdirAll(filepath.Join(spaced, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaced, "index.html"), []byte("override-sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spaced, "assets", "app-1a2b3c4d.js"), []byte("override-asset"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	overrideRouter := testRouterSource(testToken, "dev", staticWebSelect(StaticWebDefaultRoot(env(map[string]string{"BELTLINE_WEB_DIR": spaced})), bundled), nil)
	if shell := send(overrideRouter, http.MethodGet, "/overview", nil); shell.status != 200 || shell.text() != "override-sentinel" {
		t.Fatalf("override with spaces: shell status %d body %q", shell.status, shell.text())
	}
	if asset := send(overrideRouter, http.MethodGet, "/assets/app-1a2b3c4d.js", nil); asset.status != 200 || asset.text() != "override-asset" {
		t.Fatalf("override with spaces: asset status %d body %q", asset.status, asset.text())
	}

	bundledRouter := testRouterSource(testToken, "dev", staticWebSelect(StaticWebDefaultRoot(env(nil)), bundled), nil)
	if shell := send(bundledRouter, http.MethodGet, "/", nil); shell.status != 200 || shell.text() != "bundled-sentinel" {
		t.Fatalf("no override: status %d body %q", shell.status, shell.text())
	}

	missing := filepath.Join(f.parent, "no such build")
	missingRouter := testRouterSource(testToken, "dev", staticWebSelect(StaticWebDefaultRoot(env(map[string]string{"BELTLINE_WEB_DIR": missing})), bundled), nil)
	shell := send(missingRouter, http.MethodGet, "/", nil)
	if shell.status != 503 || strings.Contains(shell.text(), "bundled-sentinel") {
		t.Fatalf("missing override: status %d body %q; a missing override must not serve the bundle", shell.status, shell.text())
	}
	if !strings.Contains(shell.text(), "Check BELTLINE_WEB_DIR points to a readable web build containing index.html, then restart beltlined.") {
		t.Fatalf("missing override: page %q", shell.text())
	}
	if got := send(missingRouter, http.MethodGet, "/api/v1/version", bearerHeader(testToken)).status; got != 200 {
		t.Fatalf("missing override: API with bearer: status %d", got)
	}
}

// TestGoOnlyWebShowsBuildInstruction checks a build without the embedweb tag: the
// web app answers GET and HEAD with the build instruction, the API keeps working,
// and a BELTLINE_WEB_DIR build is served. It runs only where nothing is embedded.
func TestGoOnlyWebShowsBuildInstruction(t *testing.T) {
	t.Parallel()
	if _, ok := bundledWebFS(); ok {
		t.Skip("this build embeds the web app; the Go-only instruction applies to builds without the embedweb tag")
	}
	h := testRouter(testToken, "dev", "", nil)
	for _, path := range []string{"/", "/overview", "/runs/example"} {
		get := send(h, http.MethodGet, path, nil)
		head := send(h, http.MethodHead, path, nil)
		for _, reply := range []testReply{get, head} {
			if reply.status != 503 {
				t.Fatalf("%s: status %d", path, reply.status)
			}
		}
		if !strings.Contains(get.text(), "<h1>Web app not built</h1>") ||
			!strings.Contains(get.text(), "<p>Run make build, then restart beltlined; or set BELTLINE_WEB_DIR to a built web app.</p>") {
			t.Fatalf("%s: page %q", path, get.text())
		}
		if strings.Contains(get.text(), "<script") {
			t.Fatalf("%s: page has a script", path)
		}
		if get.header("Cache-Control") != "no-store" || get.header("Content-Security-Policy") == "" ||
			get.header("X-Content-Type-Options") != "nosniff" || get.header("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: headers %v", path, get.headers)
		}
		if head.header("Content-Type") != get.header("Content-Type") || len(head.body) != 0 {
			t.Fatalf("%s: HEAD %v with %d body bytes, GET %v", path, head.headers, len(head.body), get.headers)
		}
	}
	if got := send(h, http.MethodGet, "/api/v1/version", bearerHeader(testToken)).status; got != 200 {
		t.Fatalf("API with bearer: status %d", got)
	}
	if got := send(h, http.MethodGet, "/api/v1/version", nil).status; got != 401 {
		t.Fatalf("API without credentials: status %d", got)
	}

	f := newWebFixture(t)
	f.write(t, "index.html", "override-shell")
	override := testRouter(testToken, "dev", f.root, nil)
	if reply := send(override, http.MethodGet, "/overview", nil); reply.status != 200 || reply.text() != "override-shell" {
		t.Fatalf("override: status %d body %q", reply.status, reply.text())
	}
}
