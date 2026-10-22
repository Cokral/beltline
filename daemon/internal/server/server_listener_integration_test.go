//go:build integration

// Tests of the listener: the lifecycle cases, the raw request-line cases, and the raw-target cases of
// the static web. They bind loopback ports, so they run only with the integration tag.

package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"maps"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/server/routes"
)

// portBlock holds consecutive loopback ports so a test controls which are free.
type portBlock struct {
	first     int
	listeners map[int]net.Listener
}

// reservePortBlock holds count consecutive free ports from a random place in the
// range below the ephemeral one, so parallel tests rarely meet.
func reservePortBlock(t *testing.T, count int) *portBlock {
	t.Helper()
	for attempt := 0; attempt < 200; attempt++ {
		first := 20000 + rand.IntN(28000-count)
		held := map[int]net.Listener{}
		for port := first; port < first+count; port++ {
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				break
			}
			held[port] = ln
		}
		if len(held) == count {
			return &portBlock{first: first, listeners: held}
		}
		for _, ln := range held {
			_ = ln.Close()
		}
	}
	t.Fatalf("no free block of %d ports", count)
	return nil
}

// holdPort holds exactly port, or returns nil when something else has it.
func holdPort(port int) *portBlock {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil
	}
	return &portBlock{first: port, listeners: map[int]net.Listener{port: ln}}
}

// release frees one port of the block.
func (b *portBlock) release(port int) {
	if ln, ok := b.listeners[port]; ok {
		_ = ln.Close()
		delete(b.listeners, port)
	}
}

// releasing frees every port and returns the first.
func (b *portBlock) releasing() int {
	for port := range b.listeners {
		b.release(port)
	}
	return b.first
}

// liveClient is an HTTP client that never follows redirects and keeps no state.
func liveClient(keepAlives bool) *http.Client {
	return &http.Client{
		Timeout:   lifecycleTimeout,
		Transport: &http.Transport{DisableKeepAlives: !keepAlives},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// getLive sends one GET with the given headers and returns the status and body.
func getLive(t *testing.T, client *http.Client, url string, headers http.Header) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(req.Header, headers)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }() // the response is read; a close error has nothing left to report
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

// loopbackURL is the base URL of a port on this listener.
func loopbackURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// nonLoopbackIPv4 is the machine's first non-loopback IPv4 address, if it has one.
func nonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
			continue
		}
		return ipnet.IP.String()
	}
	return ""
}

// startLive starts a server on start with the production router.
func startLive(t *testing.T, start int, token string, bindHost string) *BeltlineServer {
	t.Helper()
	return newBeltlineServerForTest(token, "dev", start, bindHost, testWebRoot(t), nil,
		routes.EventWriteConfigurationProduction, nil, nil, routes.Dependencies{}, nil)
}

// rawExchange writes request as it is to a fresh loopback connection and reads
// the response until the server closes it: the status line's code and the body.
func rawExchange(t *testing.T, port int, request string) (int, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), lifecycleTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }() // the test is done with this connection; a close error has nothing to report
	_ = conn.SetDeadline(time.Now().Add(lifecycleTimeout))
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(bufio.NewReader(conn))
	text := string(data)
	head, body, found := strings.Cut(text, "\r\n\r\n")
	if !found {
		t.Fatalf("no response: %q", text)
	}
	fields := strings.Fields(strings.SplitN(head, "\r\n", 2)[0])
	if len(fields) < 2 {
		t.Fatalf("no status line: %q", head)
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("status line: %q", head)
	}
	return status, body
}

// TestServesOnLoopbackAsSoonAsStartReturns
func TestServesOnLoopbackAsSoonAsStartReturns(t *testing.T) {
	t.Parallel()
	block := reservePortBlock(t, 1)
	start := block.releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken))
	if status != 200 {
		t.Fatalf("version status %d", status)
	}
	if got, ok := server.Port(); !ok || got != port {
		t.Fatalf("Port() = %d, %v", got, ok)
	}
	if addr, ok := server.BoundAddress(); !ok || addr != "127.0.0.1" {
		t.Fatalf("BoundAddress() = %q, %v", addr, ok)
	}
}

// TestOtherAddressesCannotReachIt
func TestOtherAddressesCannotReachIt(t *testing.T) {
	t.Parallel()
	block := reservePortBlock(t, 1)
	start := block.releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	hosts := []string{"127.0.0.2:" + strconv.Itoa(port), "[::1]:" + strconv.Itoa(port)}
	if address := nonLoopbackIPv4(); address != "" {
		hosts = append(hosts, address+":"+strconv.Itoa(port))
	}
	for _, host := range hosts {
		req, _ := http.NewRequest(http.MethodGet, "http://"+host+"/api/v1/version", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		res, err := client.Do(req)
		if err == nil {
			_ = res.Body.Close() // the response is read; a close error has nothing left to report
			t.Fatalf("%s reached the listener", host)
		}
	}
}

// TestMovesToTheNextPortWhenTheFirstIsTaken
func TestMovesToTheNextPortWhenTheFirstIsTaken(t *testing.T) {
	t.Parallel()
	block := reservePortBlock(t, 3)
	first := block.first
	block.release(first + 1)
	block.release(first + 2)
	server := startLive(t, first, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if port != first+1 {
		t.Fatalf("port %d, want %d", port, first+1)
	}
	if got, _ := server.Port(); got != first+1 {
		t.Fatalf("Port() = %d", got)
	}
	if status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("version status %d", status)
	}
	server.Stop(context.Background())
	block.releasing()
}

// TestSkipsEveryTakenPortToTheFirstFreeOne
func TestSkipsEveryTakenPortToTheFirstFreeOne(t *testing.T) {
	t.Parallel()
	block := reservePortBlock(t, 3)
	first := block.first
	block.release(first + 2)
	server := startLive(t, first, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if port != first+2 {
		t.Fatalf("port %d, want %d", port, first+2)
	}
	if status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("version status %d", status)
	}
	server.Stop(context.Background())
	block.releasing()
}

// TestFailsWhenEveryPortThroughTheLastIsTaken
func TestFailsWhenEveryPortThroughTheLastIsTaken(t *testing.T) {
	t.Parallel()
	blocker := holdPort(65535)
	server := startLive(t, 65535, testToken, "127.0.0.1")
	_, err := server.Start(context.Background())
	var got StartError
	if !errors.As(err, &got) || got != (StartError{Kind: StartErrorNoFreePort, Port: 65535}) {
		t.Fatalf("Start error %v", err)
	}
	if port, running := server.Port(); running {
		t.Fatalf("port %d published", port)
	}
	server.Stop(context.Background())
	if blocker != nil {
		blocker.releasing()
	}
}

// TestOtherBindFailuresDoNotMoveOn
func TestOtherBindFailuresDoNotMoveOn(t *testing.T) {
	t.Parallel()
	block := reservePortBlock(t, 2)
	first := block.releasing()
	// 192.0.2.1 is a documentation address no interface has: EADDRNOTAVAIL.
	server := startLive(t, first, testToken, "192.0.2.1")
	_, err := server.Start(context.Background())
	var startErr StartError
	if err == nil || errors.As(err, &startErr) {
		t.Fatalf("Start error %v, want a bind failure that is not a StartError", err)
	}
	if port, running := server.Port(); running {
		t.Fatalf("port %d published", port)
	}
	server.Stop(context.Background())
	server.Stop(context.Background())
	if holdPort(first) == nil || holdPort(first+1) == nil {
		t.Fatal("the ports were not left free")
	}
}

// TestStopsReleasesThePortAndStartsAgainOnIt
func TestStopsReleasesThePortAndStartsAgainOnIt(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("version status %d", status)
	}
	_, err = server.Start(context.Background())
	if !errors.As(err, new(StartError)) || err.(StartError).Kind != StartErrorAlreadyRunning {
		t.Fatalf("second start: %v", err)
	}
	if got, _ := server.Port(); got != port {
		t.Fatalf("refused start moved the listener to %d", got)
	}
	server.Stop(context.Background())
	if _, running := server.Port(); running {
		t.Fatal("still running after stop")
	}
	if _, err := liveClient(false).Get(loopbackURL(port) + "/api/v1/version"); err == nil {
		t.Fatal("stopped server still answers")
	}
	server.Stop(context.Background())

	fresh := startLive(t, port, testToken, "127.0.0.1")
	if got, err := fresh.Start(context.Background()); err != nil || got != port {
		t.Fatalf("fresh start: %d, %v", got, err)
	}
	if status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("fresh version status %d", status)
	}
	fresh.Stop(context.Background())
	again := startLive(t, port, testToken, "127.0.0.1")
	if got, err := again.Start(context.Background()); err != nil || got != port {
		t.Fatalf("restart on the same port: %d, %v", got, err)
	}
	again.Stop(context.Background())
}

// TestStopEndsIdleKeepAliveConnections
func TestStopEndsIdleKeepAliveConnections(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := liveClient(true)
	if status, _ := getLive(t, client, loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("version status %d", status)
	}
	server.Stop(context.Background())
	req, _ := http.NewRequest(http.MethodGet, loopbackURL(port)+"/api/v1/version", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if res, err := client.Do(req); err == nil {
		_ = res.Body.Close() // the response is read; a close error has nothing left to report
		t.Fatal("idle keep-alive connection survived stop")
	}
}

// TestCancellingStartLeavesNothingBehind
func TestCancellingStartLeavesNothingBehind(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = server.Start(ctx)
	server.Stop(context.Background())
	if _, running := server.Port(); running {
		t.Fatal("running after a cancelled start")
	}
	if holdPort(start) == nil {
		t.Fatal("the port was not left free")
	}
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatalf("start after cancel: %v", err)
	}
	if status, _ := getLive(t, liveClient(false), loopbackURL(port)+"/api/v1/version", bearerHeader(testToken)); status != 200 {
		t.Fatalf("version status %d", status)
	}
	server.Stop(context.Background())
}

// TestStopDuringStartWaitsForTheListenerToEnd
func TestStopDuringStartWaitsForTheListenerToEnd(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	started := make(chan error, 1)
	go func() {
		_, err := server.Start(context.Background())
		started <- err
	}()
	server.Stop(context.Background())
	<-started
	server.Stop(context.Background())
	if _, running := server.Port(); running {
		t.Fatal("still running")
	}
	if holdPort(start) == nil {
		t.Fatal("the port was not left free")
	}
}

// TestRawRequestsWithBadHostsAreForbidden
// net/http answers a request whose Host header is missing, repeated or malformed
// as 400 before the handler runs; those cases are checked for
// refusal by the transport.
// An absolute request target is checked against its own authority, because
// net/http removes the Host header from the request; that case is a known deviation.
func TestRawRequestsWithBadHostsAreForbidden(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	auth := "Authorization: Bearer " + testToken + "\r\n"
	p := strconv.Itoa(port)
	forbidden := []string{
		"GET /api/v1/version HTTP/1.1\r\nHost: 127.0.0.1:" + p + "\r\nOrigin: http://127.0.0.1:" + p + "\r\nOrigin: http://127.0.0.1:" + p + "\r\n" + auth + "Connection: close\r\n\r\n",
		"GET /api/v1/version HTTP/1.1\r\nHost: \r\n" + auth + "Connection: close\r\n\r\n",
		"GET /auth?token=" + testToken + " HTTP/1.1\r\nHost: evil.example:" + p + "\r\nConnection: close\r\n\r\n",
	}
	for _, request := range forbidden {
		status, body := rawExchange(t, port, request)
		if status != 403 || !strings.Contains(body, "Forbidden") {
			t.Fatalf("%q: status %d body %q", request, status, body)
		}
	}
	refusedByTransport := []string{
		"GET /api/v1/version HTTP/1.1\r\nHost: 127.0.0.1:" + p + "\r\nHost: evil.example\r\n" + auth + "Connection: close\r\n\r\n",
		"GET /api/v1/version HTTP/1.1\r\n" + auth + "Connection: close\r\n\r\n",
		"GET /api/v1/version HTTP/1.1\r\nHost: 127.0.0.1:" + p + " x\r\n" + auth + "Connection: close\r\n\r\n",
		"GET /api/v1/version HTTP/1.1\r\nHost: 127.0.0.1:" + p + "\r\nHost: 127.0.0.1:" + p + "\r\n" + auth + "Connection: close\r\n\r\n",
	}
	for _, request := range refusedByTransport {
		if status, _ := rawExchange(t, port, request); status != 400 {
			t.Fatalf("%q: status %d, want the transport's 400", request, status)
		}
	}
	canonical, _ := rawExchange(t, port, "GET /api/v1/version HTTP/1.1\r\nHost: 127.0.0.1:"+p+"\r\n"+auth+"Connection: close\r\n\r\n")
	if canonical != 200 {
		t.Fatalf("canonical: status %d", canonical)
	}
	t.Logf("deviation: an absolute request target naming the canonical authority is served whatever its Host header")
}

// TestMalformedHTTPIsRejectedByTheTransport
func TestMalformedHTTPIsRejectedByTheTransport(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := startLive(t, start, testToken, "127.0.0.1")
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	for _, request := range []string{"NONSENSE\r\n\r\n", "GET /api/v1/version HTTP/1.1\r\nHost 127.0.0.1\r\n\r\n"} {
		if status, _ := rawExchange(t, port, request); status != 400 {
			t.Fatalf("%q: status %d", request, status)
		}
	}
}

// TestConfinesRawTargets covers the raw-target cases of StaticWebTests.confinesRawTargetsAndSymlinksWithoutDirectoryListings
// whose targets a request line cannot carry through httptest: a space and an invalid escape.
func TestConfinesRawTargets(t *testing.T) {
	t.Parallel()
	start := reservePortBlock(t, 1).releasing()
	server := newBeltlineServerForTest(testToken, "dev", start, "127.0.0.1", testWebRoot(t), nil,
		routes.EventWriteConfigurationProduction, nil, nil, routes.Dependencies{}, nil)
	port, err := server.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop(context.Background())
	for _, target := range []string{"/../web sibling-prefix sentinel.txt", "/bad%ZZpath"} {
		status, _ := rawExchange(t, port, "GET "+target+" HTTP/1.1\r\nHost: 127.0.0.1:"+strconv.Itoa(port)+"\r\nConnection: close\r\n\r\n")
		if status != 400 && status != 404 {
			t.Fatalf("%q: status %d", target, status)
		}
	}
}

// lifecycleTimeout bounds the waits these tests make.
const lifecycleTimeout = 5 * time.Second
