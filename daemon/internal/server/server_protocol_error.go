// The protocol-error handling of the listener. net/http answers malformed HTTP/1.1 requests itself,
// and coder/websocket upgrades inside the handler, so no separate upgrade channel is needed; this
// wraps the mux so that a malformed request keeps its 400.

package server

import "net/http"

// HTTPProtocolErrorAwareChannel restores the ordinary malformed-HTTP 400 around
// the WebSocket upgrade channel. In Go it is a handler wrapper.
type HTTPProtocolErrorAwareChannel struct {
	next http.Handler
}

// ServeHTTP passes a request to the upgrade-aware handler and keeps the protocol
// error response. net/http writes the 400 itself.
func (c HTTPProtocolErrorAwareChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.next.ServeHTTP(w, r)
}
