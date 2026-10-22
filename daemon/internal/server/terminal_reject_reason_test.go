// 83-95): a rejected
// terminal request reads "HTTP/1.1 422 Unprocessable Content", not net/http's reason phrase.
package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// TestTerminalRejectionStatusLineUsesReasonPhrase reads the raw status line of a 422
// terminal rejection and checks its reason phrase and the headers after it.
func TestTerminalRejectionStatusLineUsesReasonPhrase(t *testing.T) {
	t.Parallel()
	port := terminalTestServer(t)
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	headers := []string{fmt.Sprintf("Host: 127.0.0.1:%d", port), "Authorization: Bearer " + terminalTestToken}
	if _, err := io.WriteString(conn, terminalHandshake("/api/v1/terminals?session=", headers)); err != nil {
		t.Fatalf("write: %v", err)
	}
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("status line: %v", err)
	}
	if want := "HTTP/1.1 422 Unprocessable Content\r\n"; statusLine != want {
		t.Fatalf("status line %q, want %q", statusLine, want)
	}
	rest, _ := io.ReadAll(reader)
	if !strings.Contains(string(rest), "Content-Type: application/json\r\n") || !strings.Contains(string(rest), "Connection: close\r\n") {
		t.Fatalf("headers %q", rest)
	}
}
