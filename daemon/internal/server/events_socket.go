// The
// access check the upgrade runs (Host and Origin, then credentials), the accepted
// socket as the events route uses it, and the handler that connects a client and
// serves it. This file is the only place the events route imports the WebSocket
// package.
package server

import (
	"context"
	"net/http"

	"github.com/coder/websocket"

	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// eventSocket is the accepted events WebSocket (routes.EventSocket).
type eventSocket struct {
	conn *websocket.Conn
}

// WriteText sends one text frame. A cancelled ctx aborts the write and closes the
// connection, which is what closing the socket does.
func (s eventSocket) WriteText(ctx context.Context, message string) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(message))
}

// Read reads and discards one frame from the peer. A cancelled ctx or a closed
// connection returns an error, which ends the session.
func (s eventSocket) Read(ctx context.Context) error {
	_, _, err := s.conn.Read(ctx)
	return err
}

// eventsAccess is the Host, Origin and credential check the events upgrade runs
// first, in order: the Host and Origin middleware (403 forbidden), then the
// credentials for /api (401 unauthorized).
func eventsAccess(port int, token string) routes.EventAccessCheck {
	origin := NewHostOriginMiddleware(port, nil)
	return func(r *http.Request) (int, []byte, bool) {
		if !origin.allows(r) {
			body, _ := jsonx.Marshal(api.ErrorForbidden)
			return http.StatusForbidden, body, false
		}
		if !authIsAPI(r.URL.Path) || !authAuthenticates(r.Header, token) {
			body, _ := jsonx.Marshal(api.ErrorUnauthorized)
			return http.StatusUnauthorized, body, false
		}
		return 0, nil, true
	}
}

// serveEvents answers one request to the events path: a rejection is answered
// with its status and body, a request that is not a GET of the path gets 404 (the
// router's unknown-route answer), and an accepted one becomes a client of hub and
// is served until its peer closes.
func serveEvents(port int, token string, hub *EventHub, rejections *EventRejectionRegistry,
	w http.ResponseWriter, r *http.Request) {
	switch routes.EventRoutesUpgrade(w, r, eventsAccess(port, token), rejections) {
	case routes.EventRoutesRejected:
		return
	case routes.EventRoutesDecline:
		body, _ := jsonx.Marshal(api.ErrorNotFound)
		rejections.Record(w, http.StatusNotFound, body)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		// Accept has written its own failure response.
		return
	}
	defer func() { _ = conn.CloseNow() }() // the session is over; nothing is left to report to the client
	id, client, ok := hub.Connect()
	if !ok {
		return
	}
	routes.EventRoutesServe(r.Context(), eventSocket{conn: conn}, hub, client, id, rejections.WriteConfiguration())
}
