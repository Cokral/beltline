// The terminal
// socket's order of checks (Host and Origin, then credentials, then the query)
// over a real listener, with raw HTTP requests.

package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// terminalTestToken is the creator's token the security tests send.
const terminalTestToken = "terminal-test-token"

// terminalHandshakeKey is the Sec-WebSocket-Key the handshake requests use.
const terminalHandshakeKey = "dGhlIHNhbXBsZSBub25jZQ=="

// terminalTestServer serves TerminalRouteHandler on a loopback listener over a
// fresh coordinator, and returns its port.
func terminalTestServer(t *testing.T) int {
	t.Helper()
	return terminalServerFor(t, terminalTestCoordinator(t))
}

// terminalServerFor serves TerminalRouteHandler for c on a loopback listener and
// returns its port.
func terminalServerFor(t *testing.T, c *coordinator.Coordinator) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: TerminalRouteHandler(port, terminalTestToken, c)}}
	server.Start()
	t.Cleanup(server.Close)
	return port
}

// terminalTestCoordinator is a coordinator with no saved runs, over the fakes.
func terminalTestCoordinator(t *testing.T) *coordinator.Coordinator {
	t.Helper()
	root := t.TempDir()
	c, err := coordinator.New(coordinator.Services{
		Storage:    store.NewStorage(store.Paths{Root: root}),
		GitHub:     github.NewFakeGitHub(nil, nil, nil, nil),
		Jira:       jira.NewFakeJira(nil, nil, nil),
		Runner:     runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 }, time.Now, func(runner.SessionSpec) *string { return nil }),
		Workspaces: runner.FolderWorkspaces{},
		Clock:      testkit.NewClock(testkit.ContractTime),
		IDs:        &coordinator.SequenceIDs{},
	})
	if err != nil {
		t.Fatalf("coordinator: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

// terminalRawReply is one HTTP reply to a raw request: its status, headers and body.
type terminalRawReply struct {
	status int
	header http.Header
	body   []byte
}

// terminalRawRequest sends raw request bytes to the port and reads the reply. A
// 101 reply has no body to read, so its body is empty.
func terminalRawRequest(t *testing.T, port int, request string) terminalRawReply {
	t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("write request: %v", err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	reply := terminalRawReply{status: response.StatusCode, header: response.Header}
	if response.StatusCode != http.StatusSwitchingProtocols {
		reply.body, _ = io.ReadAll(response.Body)
	}
	_ = response.Body.Close()
	return reply
}

// terminalGolden reads a recorded error body from the golden folder.
func terminalGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testkit.Testdata(t, "golden"), name))
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	return data
}

// terminalHandshake is the upgrade request for path with the given header lines.
func terminalHandshake(path string, headers []string) string {
	fields := append([]string{"Upgrade: websocket", "Connection: Upgrade", "Sec-WebSocket-Version: 13", "Sec-WebSocket-Key: " + terminalHandshakeKey}, headers...)
	return "GET " + path + " HTTP/1.1\r\n" + strings.Join(fields, "\r\n") + "\r\n\r\n"
}

func TestTerminalUpgradeChecksHostAndOriginBeforeCredentials(t *testing.T) {
	t.Parallel()
	port := terminalTestServer(t)
	forbidden := terminalGolden(t, "error-forbidden.json")
	// A request net/http refuses as malformed (no Host, or two Host headers) gets
	// its own 400 before any handler runs, where the listener answered 403.
	// The refusal is the same: nothing past Host is read. Those rows check the
	// status only.
	for _, c := range []struct {
		headers  []string
		status   int
		wantBody []byte
	}{
		{headers: []string{fmt.Sprintf("Host: localhost:%d", port)}, status: http.StatusForbidden, wantBody: forbidden},
		{headers: []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), "Origin: http://localhost:" + fmt.Sprint(port)}, status: http.StatusForbidden, wantBody: forbidden},
		{headers: []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), "Origin: null"}, status: http.StatusForbidden, wantBody: forbidden},
		{headers: []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), fmt.Sprintf("Origin: http://127.0.0.1:%d", port), fmt.Sprintf("Origin: http://127.0.0.1:%d", port)}, status: http.StatusForbidden, wantBody: forbidden},
		{headers: []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), fmt.Sprintf("Host: 127.0.0.1:%d", port)}, status: http.StatusBadRequest},
		{headers: []string{fmt.Sprintf("Origin: http://127.0.0.1:%d", port)}, status: http.StatusBadRequest},
	} {
		reply := terminalRawRequest(t, port, terminalHandshake("/api/v1/terminals?session=unknown", c.headers))
		if reply.status != c.status {
			t.Errorf("headers %v: status %d, want %d", c.headers, reply.status, c.status)
		}
		if c.wantBody != nil && !bytes.Equal(reply.body, c.wantBody) {
			t.Errorf("headers %v: body %q, want %q", c.headers, reply.body, c.wantBody)
		}
	}
}

func TestTerminalUpgradeUsesBearerOrCookieAndIgnoresQueryTokens(t *testing.T) {
	t.Parallel()
	port := terminalTestServer(t)
	base := []string{fmt.Sprintf("Host: 127.0.0.1:%d", port)}
	unauthorized := terminalGolden(t, "error-unauthorized.json")
	for _, headers := range [][]string{
		base,
		append(append([]string{}, base...), "Authorization: Bearer wrong"),
		append(append([]string{}, base...), "Cookie: beltline_auth=wrong"),
		append(append([]string{}, base...), "Authorization: Bearer wrong", "Cookie: beltline_auth="+terminalTestToken),
	} {
		reply := terminalRawRequest(t, port, terminalHandshake("/api/v1/terminals?session=unknown&token="+terminalTestToken, headers))
		if reply.status != http.StatusUnauthorized {
			t.Errorf("headers %v: status %d, want 401", headers, reply.status)
		}
		if !bytes.Equal(reply.body, unauthorized) {
			t.Errorf("headers %v: body %q, want %q", headers, reply.body, unauthorized)
		}
	}
	notFound := terminalGolden(t, "error-not-found.json")
	for _, credential := range []string{"Authorization: Bearer " + terminalTestToken, "Cookie: beltline_auth=" + terminalTestToken} {
		reply := terminalRawRequest(t, port, terminalHandshake("/api/v1/terminals?session=unknown", append(append([]string{}, base...), credential)))
		if reply.status != http.StatusNotFound {
			t.Errorf("credential %q: status %d, want 404", credential, reply.status)
		}
		if !bytes.Equal(reply.body, notFound) {
			t.Errorf("credential %q: body %q, want %q", credential, reply.body, notFound)
		}
	}
}

func TestTerminalQueryShapeIsRejectedBeforeUpgrade(t *testing.T) {
	t.Parallel()
	port := terminalTestServer(t)
	for _, query := range []string{"", "?takeOver=1", "?session=", "?session=a&session=b", "?session=a&takeOver=0", "?session=a&takeOver=1&takeOver=1"} {
		headers := []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), "Authorization: Bearer " + terminalTestToken}
		reply := terminalRawRequest(t, port, terminalHandshake("/api/v1/terminals"+query, headers))
		if reply.status != http.StatusUnprocessableEntity {
			t.Errorf("query %q: status %d, want 422", query, reply.status)
		}
		if got := reply.header.Get("Content-Type"); got != "application/json" {
			t.Errorf("query %q: Content-Type %q", query, got)
		}
	}
}
