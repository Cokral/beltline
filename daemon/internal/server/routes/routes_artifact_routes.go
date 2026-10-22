package routes

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// ArtifactLinkResponse is the body of POST /api/v1/artifacts/link: the signed
// artifact URL and when it expires.
type ArtifactLinkResponse struct {
	URL       string     `json:"url"`
	ExpiresAt jsonx.Date `json:"expiresAt"`
}

// artifactContentSecurityPolicy is the sandbox policy an artifact is served under.
const artifactContentSecurityPolicy = "sandbox allow-scripts"

// AddArtifacts POST
// api/v1/artifacts/link signs a link for an existing artifact, and GET
// artifacts/{run}/{attempt}/{name} serves it when the signature is current and
// verifies (text/html, no-store, no-referrer, the sandbox CSP and nosniff).
// Lock: the handlers call coordinator methods, which take the lock.
func AddArtifacts(mux *http.ServeMux, c *coordinator.Coordinator, signer ArtifactLinkSigner) {
	mux.Handle("POST /api/v1/artifacts/link", JSONRoute(c, func(r *http.Request) (any, error) {
		values, err := artifactLinkBody(r, []string{"runId", "attemptId", "name"})
		if err != nil {
			return nil, err
		}
		runID, hasRun := values["runId"]
		attemptID, hasAttempt := values["attemptId"]
		name, hasName := values["name"]
		if !hasRun || !hasAttempt || !hasName {
			return nil, core.NewFailure("Expected runId, attemptId and name.")
		}
		if !store.ServableArtifactName(name) || !c.ArtifactExists(r.Context(), runID, attemptID, name) {
			return nil, api.MissingResource{}
		}
		return ArtifactSignedLink(runID, attemptID, name, signer), nil
	}))

	mux.Handle("GET /artifacts/{run}/{attempt}/{name}", ErrorRoute(func(w http.ResponseWriter, r *http.Request) error {
		// The wildcards arrive decoded once (see ReadRouteSupportParameter).
		runID := r.PathValue("run")
		attemptID := r.PathValue("attempt")
		name := r.PathValue("name")
		if c == nil {
			return api.MissingResource{}
		}
		if !store.ServableArtifactName(name) {
			return api.MissingResource{}
		}
		expires, signature, ok := artifactSignatureParameters(r.URL.RawQuery)
		if !ok || !signer.IsCurrent(expires) || !signer.Verifies(signature, runID, attemptID, name, expires) {
			JSONError(w, api.ErrorUnauthorized, http.StatusUnauthorized)
			return nil
		}
		file := c.ConfinedArtifact(runID, attemptID, name)
		if file == nil {
			return api.MissingResource{}
		}
		read := file.Read(r.Context())
		switch read.Kind {
		case store.ArtifactReadMissing:
			return api.MissingResource{}
		case store.ArtifactReadTooLarge:
			JSONError(w, api.ErrorResponse{
				Message:  fmt.Sprintf("This artifact is too large to open: %d bytes, the limit is %d.", read.Size, store.ArtifactMaxBytes),
				Problems: []string{},
			}, http.StatusRequestEntityTooLarge)
			return nil
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", artifactContentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(read.Bytes)
		return nil
	}))
}

// artifactLinkBody is a strict body of exactly
// the given string keys, else a 422 "Malformed request body.".
func artifactLinkBody(r *http.Request, keys []string) (map[string]string, error) {
	payload, err := readBodyLimited(r, 8_192)
	if err != nil {
		return nil, err
	}
	malformed := ReadRouteSupportFail("Malformed request body.")
	members, err := jsonx.Members(payload)
	if err != nil {
		return nil, malformed
	}
	// Exactly the given keys, each once: the same count and every key present.
	if len(members) != len(keys) {
		return nil, malformed
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		raw, ok := members[key]
		if !ok {
			return nil, malformed
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, malformed
		}
		out[key] = value
	}
	return out, nil
}

// artifactEncodeSegment percent-encodes
// everything outside the unreserved set, upper-case hex, byte by byte.
func artifactEncodeSegment(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// ArtifactSignedLink is the artifact signed link:
// the `/artifacts/{run}/{attempt}/{name}?expires=…&signature=…` URL and its expiry.
func ArtifactSignedLink(runID, attemptID, name string, signer ArtifactLinkSigner) ArtifactLinkResponse {
	expires := signer.Expiration()
	expiry := time.UnixMilli(expires)
	signature := signer.Signature(runID, attemptID, name, expires)
	path := strings.Join([]string{artifactEncodeSegment(runID), artifactEncodeSegment(attemptID), artifactEncodeSegment(name)}, "/")
	return ArtifactLinkResponse{
		URL:       fmt.Sprintf("/artifacts/%s?expires=%d&signature=%s", path, expires, signature),
		ExpiresAt: jsonx.Date{Time: expiry},
	}
}

// artifactSignatureParameters the
// query's `expires` (digits only) and `signature`, exactly those two keys, each once.
func artifactSignatureParameters(query string) (int64, string, bool) {
	if query == "" {
		return 0, "", false
	}
	values := map[string]string{}
	for _, field := range strings.Split(query, "&") {
		rawKey, rawValue, found := strings.Cut(field, "=")
		if !found {
			return 0, "", false
		}
		key, ok := RemovingPercentEncoding(rawKey)
		if !ok {
			return 0, "", false
		}
		value, ok := RemovingPercentEncoding(rawValue)
		if !ok {
			return 0, "", false
		}
		if _, dup := values[key]; dup {
			return 0, "", false
		}
		values[key] = value
	}
	if len(values) != 2 {
		return 0, "", false
	}
	expiryText, hasExpiry := values["expires"]
	signature, hasSignature := values["signature"]
	if !hasExpiry || !hasSignature || expiryText == "" {
		return 0, "", false
	}
	for i := 0; i < len(expiryText); i++ {
		if expiryText[i] < '0' || expiryText[i] > '9' {
			return 0, "", false
		}
	}
	expiry, err := strconv.ParseInt(expiryText, 10, 64)
	if err != nil {
		return 0, "", false
	}
	return expiry, signature, true
}
