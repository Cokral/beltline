// The Listener struct:
// one bound listener and the goroutine that runs it.
package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
)

// listenerShutdownGrace is how long a shutdown waits for requests in flight
// before it closes their connections.
const listenerShutdownGrace = 5 * time.Second

// listener is one bound listener and the goroutine that runs it.
// Its eventHub is the one the events route uses.
type listener struct {
	port    int
	address string
	server  *http.Server
	done    chan struct{}
	events  *EventHub
	// terminals tracks the terminal relays this listener's handler started.
	terminals *terminalTracker
	ln        net.Listener
}

// listenerBind is the listener bind:
// binds host:port (IPv4 loopback), starts serving the handler, and reports the
// bound address once the socket accepts. An address-in-use error is returned
// as-is so listen can move to the next port.
func listenerBind(ctx context.Context, handler http.Handler, host string, port int, events *EventHub) (*listener, error) {
	var config net.ListenConfig
	ln, err := config.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	l := &listener{
		port:    port,
		address: addr.IP.String(),
		server:  &http.Server{Handler: handler},
		done:    make(chan struct{}),
		events:  events,
		ln:      ln,
	}
	ready := newReady()
	go func() {
		defer close(l.done)
		ready.finish(l.address, nil)
		_ = l.server.Serve(ln)
	}()
	if _, err := ready.wait(); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return l, nil
}

// shutdown stops the event hub, ends the terminal
// relays, shuts the server down gracefully and waits for its goroutine to end,
// then for the terminal handlers, which http.Server does not wait for once they
// have upgraded. Requests still in flight after listenerShutdownGrace have their
// connections closed.
func (l *listener) shutdown(ctx context.Context) {
	if l.events != nil {
		l.events.Stop()
	}
	if l.terminals != nil {
		l.terminals.closeAll()
	}
	graceCtx, cancel := context.WithTimeout(ctx, listenerShutdownGrace)
	defer cancel()
	if err := l.server.Shutdown(graceCtx); err != nil {
		_ = l.server.Close()
	}
	<-l.done
	if l.terminals != nil {
		l.terminals.wait()
	}
}
