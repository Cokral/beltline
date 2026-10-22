package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenDaemonLogMovesALargeLogAsideAndKeepsASmallOne checks the rotation at start: a log past
// DaemonLogRotateBytes becomes beltlined.log.1, replacing the older one, and a smaller log is appended to.
func TestOpenDaemonLogMovesALargeLogAsideAndKeepsASmallOne(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	file := filepath.Join(folder, DaemonLogFileName)
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	write(file, "small\n")
	log, err := OpenDaemonLog(folder)
	if err != nil {
		t.Fatal(err)
	}
	log.Append("started")
	log.Close()
	if text := read(file); !strings.HasPrefix(text, "small\n") || !strings.HasSuffix(text, " started\n") {
		t.Fatalf("small log = %q", text)
	}

	large := strings.Repeat("x", DaemonLogRotateBytes+1)
	write(file, large)
	write(file+".1", "older")
	log, err = OpenDaemonLog(folder)
	if err != nil {
		t.Fatal(err)
	}
	log.Append("started again")
	log.Close()
	if read(file+".1") != large {
		t.Fatal("the large log was not moved to beltlined.log.1")
	}
	if text := read(file); !strings.HasSuffix(text, " started again\n") || strings.Contains(text, "x") {
		t.Fatalf("fresh log = %q", text)
	}
}
