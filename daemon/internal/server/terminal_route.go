// The terminal
// WebSocket, GET api/v1/terminals.
//
// Every rejection is an ordinary HTTP response with the shared JSON error shape,
// sent before the socket is upgraded.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// TerminalRoutePath is TerminalRoute.path.
const TerminalRoutePath = "/api/v1/terminals"

// terminalMaxMessageSize is the largest inbound message the socket accepts: 16 MiB.
const terminalMaxMessageSize = 16 * 1024 * 1024

// terminalInternalErrorBody is the body sent when an error body cannot be encoded.
const terminalInternalErrorBody = `{"message":"Internal error","problems":[]}`

// terminalRoute is the handler TerminalRouteHandler returns: the listener's port
// and token, the coordinator the terminals belong to, and the tracker that ends
// the relays when the listener stops (nil when nothing tracks them).
type terminalRoute struct {
	port    int
	token   string
	c       *coordinator.Coordinator
	tracker *terminalTracker
}

// TerminalRouteHandler is the terminal route handler
// as an http.Handler: Host and Origin, then credentials for /api, then GET of the
// path, then the query, then the session's mode (a read-only or writable attach
// command). A session the coordinator does not list is a 404. An accepted socket
// first sends the mode frame, then relays a PTY running `tmux attach-session`
// until either side ends it.
func TerminalRouteHandler(port int, token string, c *coordinator.Coordinator) http.Handler {
	return terminalRoute{port: port, token: token, c: c}
}

// terminalRouteTracked is TerminalRouteHandler for a listener: its relays are
// registered with tracker, so the listener's shutdown ends and waits for them.
func terminalRouteTracked(port int, token string, c *coordinator.Coordinator, tracker *terminalTracker) http.Handler {
	return terminalRoute{port: port, token: token, c: c, tracker: tracker}
}

// ServeHTTP runs the checks in order, then upgrades the connection.
func (h terminalRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !NewHostOriginMiddleware(h.port, nil).allows(r) {
		terminalReject(w, terminalErrorBody(api.ErrorForbidden), http.StatusForbidden)
		return
	}
	if !authIsAPI(r.URL.EscapedPath()) || !authAuthenticates(r.Header, h.token) {
		terminalReject(w, terminalErrorBody(api.ErrorUnauthorized), http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet || r.URL.EscapedPath() != TerminalRoutePath {
		terminalReject(w, terminalErrorBody(api.ErrorNotFound), http.StatusNotFound)
		return
	}
	query, err := TerminalProtocolQuery(r.URL.RawQuery)
	if err != nil {
		terminalReject(w, terminalErrorBody(terminalMessage("Invalid terminal query.")), http.StatusUnprocessableEntity)
		return
	}
	if h.c == nil {
		terminalReject(w, terminalErrorBody(api.ErrorNotFound), http.StatusNotFound)
		return
	}
	mode, err := h.c.TerminalModeForSession(query.Session, query.TakeOver, false)
	if err != nil {
		var failure core.Failure
		if errors.As(err, &failure) {
			terminalReject(w, terminalErrorBody(terminalMessage(failure.Message)), http.StatusUnprocessableEntity)
			return
		}
		terminalReject(w, terminalErrorBody(api.ErrorNotFound), http.StatusNotFound)
		return
	}
	if mode == nil || h.c.TerminalAttachCommand(r.Context(), query.Session, mode.ReadOnly) == nil {
		terminalReject(w, terminalErrorBody(api.ErrorNotFound), http.StatusNotFound)
		return
	}

	ctx := r.Context()
	if h.tracker != nil {
		trackedCtx, done, ok := h.tracker.begin(ctx)
		if !ok {
			terminalReject(w, terminalErrorBody(terminalMessage("The server is stopping.")), http.StatusServiceUnavailable)
			return
		}
		defer done()
		ctx = trackedCtx
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept has written the handshake's error response.
		return
	}
	// The library's own limit would close with 1009 before the relay could
	// report its failure, so the relay enforces terminalMaxMessageSize itself
	// (terminalLimitedSocket).
	conn.SetReadLimit(-1)
	terminalAttach(ctx, conn, h.c, query)
}

// terminalAttach is the upgraded socket's handler: it checks the session again,
// sends the accepted mode, launches the client and relays until one side ends.
// Lock: none; it calls the coordinator's exported methods.
func terminalAttach(ctx context.Context, conn *websocket.Conn, c *coordinator.Coordinator, query TerminalQuery) {
	dataRoot := c.Paths().Root
	mode, err := c.TerminalModeForSession(query.Session, query.TakeOver, false)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "Could not start terminal client")
		return
	}
	var command []string
	if mode != nil {
		command = c.TerminalAttachCommand(ctx, query.Session, mode.ReadOnly)
	}
	if mode == nil || command == nil {
		_ = conn.Close(websocket.StatusInternalError, "Terminal session disappeared")
		return
	}
	// The accepted mode is reported before any PTY output; if it cannot be sent, the attachment closes here, before a client is launched.
	if err := conn.Write(ctx, websocket.MessageText, []byte(TerminalProtocolModeFrame(mode.ReadOnly))); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "Could not start terminal client")
		return
	}
	bridge, err := PTYBridgeLaunch(command, dataRoot)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "Could not start terminal client")
		return
	}
	bridge.Relay(ctx, terminalLimitedSocket{Conn: conn}, mode.ReadOnly, query.TakeOver, func() (*core.TerminalSessionMode, error) {
		current, err := c.TerminalModeForSession(query.Session, true, false)
		var failure core.Failure
		if errors.As(err, &failure) {
			return nil, nil
		}
		return current, err
	})
}

// terminalMessage is the error body built from a message alone.
func terminalMessage(message string) api.ErrorResponse {
	return api.ErrorResponse{Message: message, Problems: []string{}}
}

// terminalErrorBody encodes an error body with the shared codec, or the internal
// error body if it cannot be encoded.
func terminalErrorBody(body api.ErrorResponse) []byte {
	data, err := routes.JSONEncode(body)
	if err != nil {
		return []byte(terminalInternalErrorBody)
	}
	return data
}

// terminalReject is an HTTP rejection
// with the shared error body, sent before any upgrade. The headers are these:
// JSON content type, no-store cache control, the exact length, and a close.
//
// net/http writes its own reason phrase ("Unprocessable Entity"), which it does not
// let a handler change, so the status line is written on the hijacked connection
// with routes.ReasonPhrase ("Unprocessable Content", as the contract writes it). A writer
// that cannot be hijacked (a test recorder) gets net/http's status line.
func terminalReject(w http.ResponseWriter, body []byte, status int) {
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Connection", "close")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 %d %s\r\n", status, routes.ReasonPhrase(status))
	_, _ = fmt.Fprintf(rw, "Content-Type: application/json\r\nCache-Control: no-store\r\n"+
		"Content-Length: %d\r\nConnection: close\r\n\r\n", len(body))
	_, _ = rw.Write(body)
	_ = rw.Flush()
}
