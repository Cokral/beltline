// Covers the artifact link helpers against the golden the contract fixture records.

package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// contractArtifactKey and contractArtifactNow are the signing key and clock of
// ArtifactRouteTests' contract fixture, which artifact-link.json was recorded with.
const contractArtifactKey = "contract-artifact-signing-key-32-bytes"

var contractArtifactNow = time.Date(2026, time.October, 7, 7, 0, 0, 0, time.UTC)

// TestArtifactLinkMatchesGolden reproduces the signed link the golden records,
// byte for byte, from the same key, clock and ids.
func TestArtifactLinkMatchesGolden(t *testing.T) {
	t.Parallel()
	signer := ArtifactLinkSigner{Key: []byte(contractArtifactKey), Clock: func() time.Time { return contractArtifactNow }}
	link := ArtifactSignedLink("epics:jira:PAY-40", "plan-epic-1", "contract.html", signer)
	got, err := JSONEncode(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "golden", "artifact-link.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("link bytes differ\n got: %s\nwant: %s", got, want)
	}
}

// TestArtifactLinkVerifiesWithinItsLifetime checks the signature and expiry rules.
func TestArtifactLinkVerifiesWithinItsLifetime(t *testing.T) {
	t.Parallel()
	now := contractArtifactNow
	signer := ArtifactLinkSigner{Key: []byte(contractArtifactKey), Clock: func() time.Time { return now }}
	expires := signer.Expiration()
	signature := signer.Signature("run", "attempt", "page.html", expires)
	if len(signature) != 64 || signature != strings.ToLower(signature) {
		t.Fatalf("signature %q is not 64 lower-case hex", signature)
	}
	if !signer.Verifies(signature, "run", "attempt", "page.html", expires) {
		t.Fatal("a fresh signature must verify")
	}
	if signer.Verifies(strings.ToUpper(signature), "run", "attempt", "page.html", expires) {
		t.Fatal("an upper-case signature must not verify")
	}
	if signer.Verifies(signature, "run", "attempt", "other.html", expires) {
		t.Fatal("a signature names its artifact")
	}
	if !signer.IsCurrent(expires) {
		t.Fatal("the link is current at issue")
	}
	now = now.Add(time.Duration(ArtifactLinkLifetimeMilliseconds) * time.Millisecond)
	if signer.IsCurrent(expires) {
		t.Fatal("the link expires at its lifetime")
	}
}

// TestArtifactSignatureParameters checks the query rule: exactly expires and
// signature, each once, expires digits only.
func TestArtifactSignatureParameters(t *testing.T) {
	t.Parallel()
	if e, s, ok := artifactSignatureParameters("expires=1791356460000&signature=abc"); !ok || e != 1791356460000 || s != "abc" {
		t.Fatalf("valid query: %d %q %v", e, s, ok)
	}
	for _, query := range []string{"", "expires=1", "signature=x", "expires=1&signature=x&extra=1",
		"expires=1&expires=2&signature=x", "expires=no&signature=x", "expires=&signature=x", "expires=-1&signature=x", "expires=1&signature"} {
		if _, _, ok := artifactSignatureParameters(query); ok {
			t.Fatalf("%q must be refused", query)
		}
	}
}

// TestArtifactEncodeSegment checks the unreserved-only encoding and its upper-case hex.
func TestArtifactEncodeSegment(t *testing.T) {
	t.Parallel()
	if got := artifactEncodeSegment("epics:jira PAY-40~_."); got != "epics%3Ajira%20PAY-40~_." {
		t.Fatalf("encoded %q", got)
	}
}

// TestArtifactLinkBodyIsStrict checks the exact-keys rule of the link body.
func TestArtifactLinkBodyIsStrict(t *testing.T) {
	t.Parallel()
	keys := []string{"runId", "attemptId", "name"}
	ok := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/link", strings.NewReader(`{"runId":"r","attemptId":"a","name":"n.html"}`))
	values, err := artifactLinkBody(ok, keys)
	if err != nil || values["runId"] != "r" || values["name"] != "n.html" {
		t.Fatalf("valid body: %v %v", values, err)
	}
	for _, body := range []string{`{"runId":"r","attemptId":"a"}`, `{"runId":"r","attemptId":"a","name":"n","x":"y"}`,
		`{"runId":1,"attemptId":"a","name":"n"}`, `[]`, `not json`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/link", strings.NewReader(body))
		if _, err := artifactLinkBody(req, keys); err == nil {
			t.Fatalf("%s must be malformed", body)
		}
	}
}
