// Helpers the wire tests of this package share: the contract coordinators, the golden readers, and the
// comparisons of wire values with their golden subtrees. Every golden comparison of a response body
// goes through servertest's normalisation, so no test here normalises by hand.

package api_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/server/servertest"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestMain removes the template folders the contract demo is built in.
func TestMain(m *testing.M) {
	code := m.Run()
	testkit.Cleanup()
	os.Exit(code)
}

// normalizeWire is the contract normalisation of a wire value encoded on its own (no endpoint rules, no
// temporary root), as it sits at path in the golden: the same allowlist the goldens were normalised with.
func normalizeWire(t *testing.T, encoded []byte, path ...string) []byte {
	t.Helper()
	normalized, err := servertest.NormalizeAt(encoded, "", "", path...)
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	return normalized
}

// same fails the test unless got, encoded by jsonx, is byte for byte the golden subtree want.
// The session ids of got are numbered as the goldens number them first.
func same(t *testing.T, what string, got any, want []byte) {
	t.Helper()
	encoded, err := jsonx.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal: %v", what, err)
	}
	sameEncoded(t, what, encoded, want)
}

// sameEncoded is same for bytes that are already the encoded wire form.
func sameEncoded(t *testing.T, what string, encoded, want []byte) {
	t.Helper()
	sameEncodedAt(t, what, normalizeWire(t, encoded), want)
}

// sameEncodedAt compares already-normalised bytes with want.
func sameEncodedAt(t *testing.T, what string, encoded, want []byte) {
	t.Helper()
	if bytes.Equal(encoded, want) {
		return
	}
	canonicalGot, _ := jsonx.Canonical(encoded)
	canonicalWant, _ := jsonx.Canonical(want)
	if string(canonicalGot) == string(canonicalWant) {
		t.Errorf("%s differs from the golden only in its bytes (canonical form equal)\n want: %s\n  got: %s", what, want, encoded)
		return
	}
	at := 0
	for at < len(encoded) && at < len(want) && encoded[at] == want[at] {
		at++
	}
	t.Errorf("%s differs from the golden at byte %d\n want: %s\n  got: %s", what, at, window(want, at), window(encoded, at))
}

// window is the text of b around byte at, for a failure message.
func window(b []byte, at int) string {
	from, to := max(0, at-120), min(len(b), at+120)
	return string(b[from:to])
}
