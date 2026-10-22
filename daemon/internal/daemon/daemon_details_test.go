// Tests of daemon details: the errno texts, the executable check, the AppleScript literal's CR LF
// and the upgrade's log line.

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

func TestErrnoReasonIsStrerrorTextWithACapital(t *testing.T) {
	t.Parallel()
	// The text is the system's own: Linux's C library words EXDEV differently from macOS's.
	crossDevice := "Cross-device link"
	if runtime.GOOS == "linux" {
		crossDevice = "Invalid cross-device link"
	}
	cases := []struct {
		err  error
		want string
	}{
		{&os.PathError{Op: "open", Path: "beltlined.log", Err: syscall.ENOENT}, "No such file or directory"},
		{&os.PathError{Op: "open", Path: "beltlined.log", Err: syscall.EACCES}, "Permission denied"},
		{&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EXDEV}, crossDevice},
	}
	for _, c := range cases {
		if got := errnoReason(c.err); got != c.want {
			t.Errorf("errnoReason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestIsExecutableFileAcceptsADirectoryWithAnExecuteBit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, "beltlined")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if !isExecutableFile(directory) {
		t.Fatal("a directory with an execute bit is an executable file to Foundation")
	}
	plain := filepath.Join(root, "plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isExecutableFile(plain) {
		t.Fatal("a file without an execute bit is not executable")
	}
	script := filepath.Join(root, "script")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isExecutableFile(script) {
		t.Fatal("an executable file is not reported as executable")
	}
	if isExecutableFile(filepath.Join(root, "missing")) {
		t.Fatal("a missing path is reported as executable")
	}
}

func TestMacNotificationLiteralWritesACRLFPairAsItIs(t *testing.T) {
	t.Parallel()
	// The CR LF pair matches no escape, so it is written raw.
	if got, want := MacNotificationLiteral("a\r\nb"), "\"a\r\nb\""; got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
	// A lone CR or LF is still escaped.
	if got, want := MacNotificationLiteral("a\rb\nc"), `"a\rb\nc"`; got != want {
		t.Fatalf("literal = %q, want %q", got, want)
	}
}

func TestUpgradeMessageNamesWhatTheUpgradeDid(t *testing.T) {
	t.Parallel()
	if got := upgradeMessage(store.UpgradeReport{}, nil); got != "" {
		t.Errorf("nothing to do: %q", got)
	}
	got := upgradeMessage(store.UpgradeReport{Ran: true, Settings: true, Belts: 2, Runs: 1, Backup: "/b", Unreadable: []string{"runs/x/run.json"}}, nil)
	if got != "Upgraded the data folder: rewrote 2 belt versions, 0 belt states and 1 runs, and the settings; the files as they were are in /b; left unreadable files as they are: runs/x/run.json" {
		t.Errorf("message %q", got)
	}
	if got := upgradeMessage(store.UpgradeReport{}, errors.New("disk full")); !strings.Contains(got, "disk full") {
		t.Errorf("failure %q", got)
	}
}
