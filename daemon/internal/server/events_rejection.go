// The EventRejectionRegistry
// and EventRejectionHandler types). A rejected upgrade keeps its HTTP status and
// body, so the response is not replaced by an empty 400 when the WebSocket
// upgrade declines the request. It implements routes.EventRejectionPort.
//
// A rejection is recorded on the channel, and a later outbound handler swaps
// the upgrader's empty failure response for it. net/http has no such failure
// response to replace, so Record writes the rejection on the request's
// ResponseWriter at once: same status, the same headers (Content-Type,
// Cache-Control, Content-Length and the same body.
package server

import (
	"net/http"
	"strconv"

	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// EventRejectionRegistry answers rejected upgrades and carries the write
// configuration (a test may change it).
type EventRejectionRegistry struct {
	writeConfiguration routes.EventWriteConfiguration
}

// NewEventRejectionRegistry builds the event rejection registry from its arguments.
func NewEventRejectionRegistry(cfg routes.EventWriteConfiguration) *EventRejectionRegistry {
	return &EventRejectionRegistry{writeConfiguration: cfg}
}

// Record and the
// rejection's head and body: it answers the request with status and body (nil
// body is an empty one).
func (r *EventRejectionRegistry) Record(w http.ResponseWriter, status int, body []byte) {
	header := w.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// WriteConfiguration is the write configuration.
func (r *EventRejectionRegistry) WriteConfiguration() routes.EventWriteConfiguration {
	return r.writeConfiguration
}
