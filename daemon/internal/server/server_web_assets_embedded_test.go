//go:build embedweb

package server

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestEmbeddedWebServesBundledBuild checks the app compiled into this executable,
// with no BELTLINE_WEB_DIR and no files beside it: the shell, every hashed asset the
// shell references, and a reload of an app route all come from the bundle.
func TestEmbeddedWebServesBundledBuild(t *testing.T) {
	t.Parallel()
	bundled, ok := bundledWebFS()
	if !ok {
		t.Fatal("the embedweb build has no bundled web app")
	}
	index, err := fs.ReadFile(bundled, "index.html")
	if err != nil {
		t.Fatalf("bundled index.html: %v", err)
	}
	h := testRouter(testToken, "dev", "", nil)

	shell := send(h, http.MethodGet, "/", nil)
	if shell.status != 200 || string(shell.body) != string(index) {
		t.Fatalf("shell: status %d, body differs from the bundled index.html: %v", shell.status, string(shell.body) != string(index))
	}
	refs := regexp.MustCompile(`/assets/[^"'\s]+\.(?:js|css)`).FindAllString(string(index), -1)
	if len(refs) == 0 {
		t.Fatal("bundled index.html references no hashed asset")
	}
	for _, ref := range refs {
		want, err := fs.ReadFile(bundled, strings.TrimPrefix(ref, "/"))
		if err != nil {
			t.Fatalf("bundled %s: %v", ref, err)
		}
		reply := send(h, http.MethodGet, ref, nil)
		if reply.status != 200 || string(reply.body) != string(want) {
			t.Fatalf("%s: status %d, body differs from the bundled file", ref, reply.status)
		}
	}

	reload := send(h, http.MethodGet, "/overview", nil)
	if reload.status != 200 || string(reload.body) != string(index) {
		t.Fatalf("/overview reload: status %d, body differs from the bundled index.html", reload.status)
	}
}
