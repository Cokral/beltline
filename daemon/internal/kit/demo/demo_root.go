package demo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// DemoRootPrefix starts the name of every demo folder: beltline-demo-<pid>-<UUID>.
const DemoRootPrefix = "beltline-demo-"

// KillFunc sends a signal to a process, as syscall.Kill does. Sweeping asks it
// with signal 0, which only checks that the process exists; tests pass their own.
type KillFunc func(pid int, sig syscall.Signal) error

// DemoRootParent is where demo instances keep their data: $TMPDIR/beltline-demo.
// Foundation's temporary directory stays on the user temp folder when TMPDIR
// points inside it, so the variable is read first. env is the process
// environment and temporary the system's temporary folder, both passed in so
// that tests never touch the real ones.
func DemoRootParent(env func(string) string, temporary string) string {
	base := temporary
	if raw := env("TMPDIR"); raw != "" {
		base = raw
	}
	return filepath.Join(base, "beltline-demo")
}

// DemoRootAllocate is a new folder name for the process pid in parent, after
// removing the folders there whose demo process is gone. The folder itself is
// not created; the launch that locks it does that.
func DemoRootAllocate(parent string, pid int, kill KillFunc) string {
	_ = os.MkdirAll(parent, 0o755)
	DemoRootSweep(parent, kill)
	return filepath.Join(parent, fmt.Sprintf("%s%d-%s", DemoRootPrefix, pid, core.NewUUID()))
}

// DemoRootSweep removes beltline-demo-* folders in parent whose named process is
// gone, and those without a process id, which predate this naming. Running
// ones are kept, including those another user's process owns (EPERM). It lists
// only that parent, never the whole temporary directory.
func DemoRootSweep(parent string, kill KillFunc) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, DemoRootPrefix) {
			continue
		}
		if owner, ok := DemoRootOwner(name); ok && demoRootAlive(owner, kill) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(parent, name))
	}
}

// DemoRootOwner is the pid a demo folder's name records.
// Empty pieces are dropped, so the first non-empty piece after the prefix is the pid.
func DemoRootOwner(name string) (int, bool) {
	if !strings.HasPrefix(name, DemoRootPrefix) {
		return 0, false
	}
	pieces := strings.FieldsFunc(name[len(DemoRootPrefix):], func(r rune) bool { return r == '-' })
	if len(pieces) == 0 {
		return 0, false
	}
	pid, err := strconv.ParseInt(pieces[0], 10, 32)
	if err != nil {
		return 0, false
	}
	return int(pid), true
}

// demoRootAlive reports whether pid is a running process, including one another
// user owns (EPERM).
func demoRootAlive(pid int, kill KillFunc) bool {
	err := kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
