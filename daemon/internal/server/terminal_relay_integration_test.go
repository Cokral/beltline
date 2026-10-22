//go:build integration

// Covers the terminal relay against a real socket and PTY: an oversized inbound
// message ends the relay with 1011, and a listener's Stop ends an attached
// terminal before it returns. The stop test needs tmux and runs on the private
// tmux server of this test binary, as terminal_integration_test.go does.

package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

func TestOversizedTerminalMessageClosesTheRelayWith1011(t *testing.T) {
	t.Parallel()
	relayEnded := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			relayEnded <- err
			return
		}
		conn.SetReadLimit(-1)
		bridge, err := PTYBridgeLaunch([]string{"sleep", "30"}, t.TempDir())
		if err != nil {
			relayEnded <- err
			return
		}
		bridge.Relay(r.Context(), terminalLimitedSocket{Conn: conn}, false, false, nil)
		relayEnded <- nil
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.CloseNow() }() // the test is done with this connection; a close error has nothing to report
	client.SetReadLimit(-1)

	oversized := make([]byte, terminalMaxMessageSize+1)
	go func() { _ = client.Write(ctx, websocket.MessageBinary, oversized) }()
	_, _, readErr := client.Read(ctx)
	if got := websocket.CloseStatus(readErr); got != websocket.StatusInternalError {
		t.Fatalf("close status = %d (%v), want 1011 from the relay failure", got, readErr)
	}
	select {
	case <-relayEnded:
	case <-ctx.Done():
		t.Fatal("the relay did not end")
	}
}

func TestStopEndsAnAttachedTerminalBeforeItReturns(t *testing.T) {
	terminalTmuxServerCleanup(t)
	root := t.TempDir()
	storage := store.NewStorage(store.Paths{Root: root})
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	session := "bl-terminal-stop-" + suffix
	if err := storage.SaveRun(terminalIntegrationRun("stop-"+suffix, session, core.BackendPi)); err != nil {
		t.Fatalf("save run: %v", err)
	}
	c, err := coordinator.New(coordinator.Services{
		Storage:    storage,
		GitHub:     github.NewFakeGitHub(nil, nil, nil, nil),
		Jira:       jira.NewFakeJira(nil, nil, nil),
		Runner:     runner.NewTmuxRunner(nil),
		Workspaces: runner.FolderWorkspaces{},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	t.Cleanup(c.Close)
	terminalCreateShellSession(t, session)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe port: %v", err)
	}
	start := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	server := newBeltlineServerForTest(terminalTestToken, "dev", start, "127.0.0.1", t.TempDir(), c,
		routes.EventWriteConfigurationProduction, nil, nil, routes.Dependencies{}, nil)
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	client := terminalDial(t, port, session, false)
	client.waitForOutput([]byte("terminal-ready"))

	stopped := make(chan struct{})
	go func() {
		server.Stop(context.Background())
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop did not return")
	}
	// Stop returned, so the relay has ended: the socket is closed, and the tmux
	// client that was attached to the session has detached.
	readCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Output the relay sent before it closed may still be queued: read past it to the close.
	for {
		_, _, err := client.conn.Read(readCtx)
		if err == nil {
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("the attached terminal did not close after Stop")
		}
		break
	}
	terminalWaitForNoTmuxClients(t, session)
}
