// The events WebSocket
// route (its path, its write configuration and the upgrade that selects it).
//
// The upgrade depends on the event hub and the rejection registry, which live
// in server (server imports routes), so this package sees them through the
// small interfaces below; server implements them. The Host/Origin and credential
// checks live in server too (HostOriginMiddleware, AuthenticationMiddleware), so
// server passes them in as an EventAccessCheck.
package routes

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"
)

// EventRoutesPath is EventRoutes.path: the events WebSocket.
const EventRoutesPath = "/api/v1/events"

// EventWriteConfiguration bounds one socket write: a write that does not finish
// by Deadline drops the client; ShouldStall (test only, nil in production) makes
// a write sleep for StallDuration first.
type EventWriteConfiguration struct {
	Deadline      time.Duration
	StallDuration time.Duration
	ShouldStall   func(message string) bool
}

// EventWriteConfigurationProduction is EventWriteConfiguration.production: a
// five-second deadline, no stall.
var EventWriteConfigurationProduction = EventWriteConfiguration{Deadline: 5 * time.Second}

// EventSocket is the accepted WebSocket as the events route uses it: text writes
// and reads until the peer closes.
type EventSocket interface {
	// WriteText sends one text frame, honouring ctx: a cancelled ctx aborts the write.
	WriteText(ctx context.Context, message string) error
	// Read blocks until the peer sends a frame or closes, or ctx is cancelled;
	// an error ends the session.
	Read(ctx context.Context) error
}

// EventClientPort is what the upgrade needs of one event client queue.
type EventClientPort interface {
	// Next blocks for the next queued message, or returns false once the client is closed.
	Next() ([]byte, bool)
	// Close ends the client and releases its writer.
	Close()
	// SetTermination registers what runs when the hub or a write drops the client.
	SetTermination(action func())
}

// EventHubPort is what the upgrade needs of the event hub (server's EventHub).
type EventHubPort interface {
	// Connect registers a connection and queues its hello; ok is false when the hub is stopped.
	Connect() (id string, client EventClientPort, ok bool)
	// Disconnect removes a connection.
	Disconnect(id string)
	// WriterStarted, WriterFinished, SocketWriteStarted and SocketWriteFinished count in-flight work for debugging.
	WriterStarted()
	WriterFinished()
	SocketWriteStarted()
	SocketWriteFinished()
}

// EventRejectionPort is what the upgrade needs of the rejection registry: a
// rejected upgrade answers its HTTP status and body, and the write configuration.
type EventRejectionPort interface {
	// Record answers a rejected upgrade with status and body on w.
	Record(w http.ResponseWriter, status int, body []byte)
	// WriteConfiguration is the configuration the events route writes with.
	WriteConfiguration() EventWriteConfiguration
}

// EventAccessCheck is the Host, Origin and credential check that runs before any
// upgrade. When ok is
// false, status and body are the rejection to answer with.
type EventAccessCheck func(r *http.Request) (status int, body []byte, ok bool)

// EventRoutesUpgradeResult says what the upgrade decided: accept the request as
// the events socket, reject it (the rejection was answered), or decline it (it is
// not a GET of the events path; nothing was written, so the caller answers).
type EventRoutesUpgradeResult int

// The EventRoutesUpgradeResult values.
const (
	EventRoutesDecline EventRoutesUpgradeResult = iota
	EventRoutesRejected
	EventRoutesAccept
)

// EventRoutesUpgrade is the event routes upgrade.
// It runs access first (Host and Origin, then the credentials for /api),
// recording the rejection it returns; a request that is not a GET of
// EventRoutesPath is declined without a rejection. An accepted request is served
// by EventRoutesServe once the socket is open.
func EventRoutesUpgrade(w http.ResponseWriter, r *http.Request, access EventAccessCheck,
	rejections EventRejectionPort) EventRoutesUpgradeResult {
	if status, body, ok := access(r); !ok {
		rejections.Record(w, status, body)
		return EventRoutesRejected
	}
	if r.Method != http.MethodGet || r.URL.EscapedPath() != EventRoutesPath {
		return EventRoutesDecline
	}
	return EventRoutesAccept
}

// EventRoutesServe is the handler the events upgrade returns, from the point
// the client is connected: a writer goroutine sends each queued message with a
// deadline and drops the client on a failed or late write, while the calling
// goroutine reads and discards the peer's frames until the peer closes. Then the
// writer is stopped and the client disconnected. The socket is not closed here;
// the caller closes it when this returns. Cancelling ctx, or the client's
// termination (a hub stop, a full queue), aborts an in-flight write and the read.
func EventRoutesServe(ctx context.Context, sock EventSocket, hub EventHubPort, client EventClientPort,
	id string, cfg EventWriteConfiguration) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client.SetTermination(cancel)

	hub.WriterStarted()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			payload, ok := client.Next()
			if !ok {
				break
			}
			hub.SocketWriteStarted()
			wrote := false
			if utf8.Valid(payload) {
				wrote = eventRoutesWrite(ctx, sock, string(payload), cfg)
			}
			hub.SocketWriteFinished()
			if !wrote {
				client.Close()
				break
			}
		}
		hub.WriterFinished()
	}()

	for {
		if err := sock.Read(ctx); err != nil {
			break
		}
	}
	cancel()
	hub.Disconnect(id)
	<-done
}

// eventRoutesWrite writes one
// message, returning false when the write fails or passes the deadline; a
// configured stall sleeps first (and counts as written when it ends in time).
func eventRoutesWrite(ctx context.Context, sock EventSocket, message string, cfg EventWriteConfiguration) bool {
	wctx, cancel := context.WithTimeout(ctx, cfg.Deadline)
	defer cancel()
	if cfg.ShouldStall != nil && cfg.ShouldStall(message) {
		timer := time.NewTimer(cfg.StallDuration)
		defer timer.Stop()
		select {
		case <-timer.C:
			return true
		case <-wctx.Done():
			return false
		}
	}
	return sock.WriteText(wctx, message) == nil
}
