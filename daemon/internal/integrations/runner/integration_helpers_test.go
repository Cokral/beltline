//go:build integration

package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// Integration tests run real tmux sessions with the stand-in agent in
// daemon/testdata/fixtures instead of each agent CLI. TestMain points every backend at it.

// integrationTimeout bounds one wait on a real process.
const integrationTimeout = 30 * time.Second

// integrationSocketSeq numbers the tmux servers the tests start in this process.
var integrationSocketSeq atomic.Int64

func TestMain(m *testing.M) {
	stand, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "fixtures", "stand-in-agent.sh"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "stand-in agent:", err)
		os.Exit(1)
	}
	if _, err := os.Stat(stand); err != nil {
		fmt.Fprintln(os.Stderr, "stand-in agent:", err)
		os.Exit(1)
	}
	for _, key := range []string{"BELTLINE_PI", "BELTLINE_CLAUDE", "BELTLINE_CODEX", "BELTLINE_COPILOT", "BELTLINE_CURSOR"} {
		if err := os.Setenv(key, stand); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// integrationSocket is a tmux server name of the test's own, killed when the test ends.
func integrationSocket(t *testing.T) string {
	t.Helper()
	socket := fmt.Sprintf("beltline-test-%d-%d", os.Getpid(), integrationSocketSeq.Add(1))
	t.Cleanup(func() {
		_ = shell.ProcessRunner{}.Run(context.Background(), "tmux", []string{"-L", socket, "kill-server"}, "", nil)
		// kill-server leaves the socket file; remove it.
		dir := os.Getenv("TMUX_TMPDIR")
		if dir == "" {
			dir = "/tmp"
		}
		_ = os.Remove(fmt.Sprintf("%s/tmux-%d/%s", dir, os.Getuid(), socket))
	})
	return socket
}

// integrationRunner is a TmuxRunner on a server of its own.
func integrationRunner(t *testing.T) TmuxRunner {
	t.Helper()
	return TmuxRunner{shell: shell.ProcessRunner{}, socket: integrationSocket(t)}
}

// tmuxPaneText is the visible text of a session's pane, bypassing the runner under test.
func tmuxPaneText(socket, session string) string {
	return shell.ProcessRunner{}.Run(context.Background(), "tmux", []string{"-L", socket, "capture-pane", "-p", "-t", tmuxExactPane(session)}, "", nil).Stdout
}

// readText is a file's text, or empty when it cannot be read.
func readTestText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// fixturePath is a file under daemon/testdata/fixtures.
func fixturePath(t testing.TB, elem ...string) string {
	t.Helper()
	return testkit.Testdata(t, append([]string{"fixtures"}, elem...)...)
}
