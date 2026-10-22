// It lives in this
// package because the artifact routes sign and verify links and routes cannot
// import server (server imports routes).
package routes

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"time"
)

// ArtifactLinkLifetimeMilliseconds is the artifact link lifetime milliseconds.
const ArtifactLinkLifetimeMilliseconds int64 = 60_000

// artifactLinkTag opens every signed message.
const artifactLinkTag = "beltline-artifact-v1"

// ArtifactLinkSigner is a server-lifetime capability signer. Production servers
// create a new key for every listener start; tests can supply a fixed key and clock.
type ArtifactLinkSigner struct {
	Key []byte
	// Clock returns the current time.
	Clock func() time.Time
}

// unixMilliseconds is Int64((timeIntervalSince1970 * 1_000).rounded(.down)),
// computed with the same double operations.
func unixMilliseconds(t time.Time) int64 {
	seconds := float64(t.Unix()) + float64(t.Nanosecond())/1e9
	return int64(math.Floor(seconds * 1000))
}

// Expiration now in milliseconds since
// the epoch, rounded down, plus the lifetime.
func (s ArtifactLinkSigner) Expiration() int64 {
	return unixMilliseconds(s.Clock()) + ArtifactLinkLifetimeMilliseconds
}

// Signature is the signature:
// the lower-case hex HMAC-SHA256 of the signed message.
func (s ArtifactLinkSigner) Signature(runID, attemptID, name string, expires int64) string {
	mac := hmac.New(sha256.New, s.Key)
	mac.Write(artifactLinkMessage(runID, attemptID, name, expires))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verifies is the verifies:
// a constant-time comparison of a 64-character lower-case hex signature.
func (s ArtifactLinkSigner) Verifies(supplied, runID, attemptID, name string, expires int64) bool {
	if len(supplied) != 64 {
		return false
	}
	for i := 0; i < len(supplied); i++ {
		c := supplied[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	expected := s.Signature(runID, attemptID, name, expires)
	return hmac.Equal([]byte(expected), []byte(supplied))
}

// IsCurrent is the link has not expired.
func (s ArtifactLinkSigner) IsCurrent(expires int64) bool {
	return unixMilliseconds(s.Clock()) < expires
}

// artifactLinkMessage is the artifact link message:
// the "beltline-artifact-v1" tag, then each value with a big-endian 64-bit length
// prefix, then the expiry as a big-endian 64-bit integer.
func artifactLinkMessage(runID, attemptID, name string, expires int64) []byte {
	out := []byte(artifactLinkTag)
	for _, value := range []string{runID, attemptID, name} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		out = append(out, length[:]...)
		out = append(out, value...)
	}
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(expires))
	return append(out, expiry[:]...)
}
