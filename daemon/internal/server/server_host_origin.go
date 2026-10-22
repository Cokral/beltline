package server

import (
	"net/http"
	"strconv"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// HostOriginMiddleware lets through only requests addressed to this listener by
// name: Host must be exactly `127.0.0.1:<port>`, and an Origin, when one is
// sent, exactly `http://127.0.0.1:<port>`. It runs before anything looks at
// credentials, so a page on another origin, or a DNS name rebound to loopback,
// gets 403 even with a valid token. Forwarding headers are ignored.
type HostOriginMiddleware struct {
	authority string
	origin    string
	next      http.Handler
}

// NewHostOriginMiddleware around next:
// authority is 127.0.0.1:<port>, origin is http:// plus the authority.
func NewHostOriginMiddleware(port int, next http.Handler) HostOriginMiddleware {
	authority := listenHost + ":" + strconv.Itoa(port)
	return HostOriginMiddleware{authority: authority, origin: "http://" + authority, next: next}
}

// ServeHTTP is a request that
// allows(…) passes to next; any other gets the forbidden error body with 403.
func (m HostOriginMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !m.allows(r) {
		routes.JSONError(w, api.ErrorForbidden, http.StatusForbidden)
		return
	}
	m.next.ServeHTTP(w, r)
}

// allows is the Host header is exactly the
// authority; the Origin is absent, or one value equal to the origin.
//
// net/http reads the Host header itself: it answers 400 before this handler when
// the header is missing or repeated, and r.Host holds the request-target's
// authority when the target is absolute. Those cases are refused, with 400 rather
// than the 403 the contract gives.
func (m HostOriginMiddleware) allows(r *http.Request) bool {
	if r.Host != m.authority {
		return false
	}
	origins := r.Header.Values("Origin")
	switch len(origins) {
	case 0:
		return true
	case 1:
		return origins[0] == m.origin
	default:
		return false
	}
}
