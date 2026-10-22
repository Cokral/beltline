// The golden comparison: a reply's body is compared with its golden file, and the status and headers
// are checked.

package servertest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// updateVariable names the environment variable that rewrites goldens; only the value "1" counts.
const updateVariable = "BELTLINE_UPDATE_GOLDENS"

// UpdateEnabled reports whether the update variable's value enables golden writes (only "1").
func UpdateEnabled(value string) bool { return value == "1" }

// Expect is what a contract response must be: its status and its Content-Type. Every contract
// response is no-store.
type Expect struct {
	Status      int
	ContentType string
}

// ExpectJSON is a successful JSON response, the shape of most contract goldens.
var ExpectJSON = Expect{Status: 200, ContentType: "application/json"}

// CheckResponse fails the test unless reply has want's status, its Content-Type and
// Cache-Control: no-store. It does not check the body.
func CheckResponse(t testing.TB, reply Reply, want Expect) {
	t.Helper()
	if reply.Status != want.Status {
		t.Fatalf("expected HTTP %d, got %d", want.Status, reply.Status)
	}
	if got := reply.Header.Get("Content-Type"); got != want.ContentType {
		t.Fatalf("expected %s, got %q", want.ContentType, got)
	}
	if got := reply.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control: no-store, got %q", got)
	}
}

// CheckGolden checks reply's status, Content-Type and Cache-Control as a JSON 200 (ExpectJSON),
// then its body against daemon/testdata/golden/goldenName (see CheckBody). endpoint names the
// endpoint in messages; temporaryRoot is the data folder the body may name.
func CheckGolden(t testing.TB, endpoint, goldenName string, reply Reply, temporaryRoot string) {
	t.Helper()
	CheckGoldenAs(t, ExpectJSON, endpoint, goldenName, reply, temporaryRoot)
}

// CheckGoldenAs is CheckGolden for a response that is not a JSON 200.
func CheckGoldenAs(t testing.TB, want Expect, endpoint, goldenName string, reply Reply, temporaryRoot string) {
	t.Helper()
	CheckResponse(t, reply, want)
	CheckBody(t, endpoint, goldenName, reply.Body, temporaryRoot)
}

// CheckBody normalises body (Normalize) and compares it byte for byte with the golden goldenName
// in daemon/testdata/golden. With BELTLINE_UPDATE_GOLDENS=1 it writes the golden instead.
func CheckBody(t testing.TB, endpoint, goldenName string, body []byte, temporaryRoot string) {
	t.Helper()
	dir := testkit.Testdata(t, "golden")
	if err := compareGolden(body, endpoint, goldenName, temporaryRoot, dir, UpdateEnabled(os.Getenv(updateVariable))); err != nil {
		t.Error(err)
	}
}

// compareGolden is CheckBody's comparison in dir, with update passed in: it writes the normalised
// body when update is set, and otherwise fails unless the golden exists and is byte-identical.
func compareGolden(body []byte, endpoint, goldenName, temporaryRoot, dir string, update bool) error {
	normalized, err := Normalize(body, endpoint, temporaryRoot)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, goldenName)
	if update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, normalized, 0o644)
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%s: missing golden at %s; set %s=1 to approve an update", endpoint, path, updateVariable)
	}
	if bytes.Equal(expected, normalized) {
		return nil
	}
	offset := 0
	for offset < len(expected) && offset < len(normalized) && expected[offset] == normalized[offset] {
		offset++
	}
	return fmt.Errorf("%s: response differs from golden at %s (byte %d, expected %s, got %s, lengths %d/%d); expected `%s`, got `%s`; set %s=1 only for an approved wire change",
		endpoint, path, offset, byteAt(expected, offset), byteAt(normalized, offset), len(expected), len(normalized),
		window(expected, offset), window(normalized, offset), updateVariable)
}

// byteAt is the hexadecimal byte at i, or EOF.
func byteAt(b []byte, i int) string {
	if i >= len(b) {
		return "EOF"
	}
	return fmt.Sprintf("%02x", b[i])
}

// window is the text of b around byte at, for a message.
func window(b []byte, at int) string {
	from, to := max(0, at-32), min(len(b), at+64)
	return string(b[from:to])
}
