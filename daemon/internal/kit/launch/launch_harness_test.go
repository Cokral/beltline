// Helpers the launch tests share: the lock holder, the launch files, and the seeded demo copy.
// The second launch of the lock tests is this test binary, run again with BELTLINE_LOCK_HOLDER set
// (TestLockHolderHelper): it locks a folder the way OwnershipLock does and says so on its first line,
// so tests wait on that line rather than on a sleep.

package launch

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// lockHolderModeEnv names the lock helper's mode: "hold" or "probe".
const lockHolderModeEnv = "BELTLINE_LOCK_HOLDER"

// lockHolderFolderEnv names the folder the lock helper locks.
const lockHolderFolderEnv = "BELTLINE_LOCK_FOLDER"

// TestMain points the daemon's tmux socket at a name of this process, removes the
// demo templates this binary built, and runs the tests.
func TestMain(m *testing.M) {
	_ = os.Setenv("BELTLINE_TMUX_SOCKET", "beltline-test-"+strconv.Itoa(os.Getpid()))
	code := m.Run()
	testkit.Cleanup()
	os.Exit(code)
}

// TestLockHolderHelper is not a test. Run as a child with BELTLINE_LOCK_HOLDER set,
// it is the second launch the lock tests need, in its own process:
// - "hold": locks the folder, records its pid, prints "ready", and holds the
// lock until its standard input closes.
// - "probe": prints "free" when the lock can be taken (and lets it go), or
// "held <recorded pid>" and exits 3 when another process holds it.
func TestLockHolderHelper(t *testing.T) {
	mode := os.Getenv(lockHolderModeEnv)
	if mode == "" {
		return
	}
	folder := os.Getenv(lockHolderFolderEnv)
	// The parent reads these lines; if a write fails it sees no line and the test fails there.
	say := func(format string, args ...any) { _, _ = fmt.Fprintf(os.Stdout, format, args...) }
	switch mode {
	case "hold":
		lock, err := OwnershipLockAcquire(folder, int32(os.Getpid()))
		if err != nil {
			say("failed %v\n", err)
			os.Exit(2)
		}
		say("ready\n")
		_, _ = io.Copy(io.Discard, os.Stdin)
		lock.Release()
		os.Exit(0)
	case "probe":
		fd, err := syscall.Open(OwnershipLockFile(folder), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC, 0o644)
		if err != nil {
			say("failed %v\n", err)
			os.Exit(2)
		}
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			if owner := ownershipLockParse(fd); owner != nil {
				say("held %d\n", *owner)
			} else {
				say("held\n")
			}
			os.Exit(3)
		}
		say("free\n")
		_ = syscall.Close(fd)
		os.Exit(0)
	}
	os.Exit(2)
}

// lockHolder is another launch in its own process, holding a folder's lock until it is stopped or killed.
type lockHolder struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
	pid   int32
}

// startLockHolder starts a holder on folder. Its first line is read with line.
func startLockHolder(t *testing.T, folder string) *lockHolder {
	t.Helper()
	cmd := lockHelperCommand(t, "hold", folder)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	holder := &lockHolder{cmd: cmd, stdin: stdin, out: bufio.NewReader(stdout), pid: int32(cmd.Process.Pid)}
	t.Cleanup(holder.kill)
	return holder
}

// line is the holder's first line, read once it has been started: "ready", or what it failed with.
func (h *lockHolder) line() string {
	text, _ := h.out.ReadString('\n')
	return strings.TrimSpace(text)
}

// kill ends the holder without letting it close anything, and waits for it.
func (h *lockHolder) kill() {
	if h.cmd.ProcessState == nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
}

// lockProbe is what another process finds when it tries folder's lock: "free", or "held <pid>".
func lockProbe(t *testing.T, folder string) string {
	t.Helper()
	cmd := lockHelperCommand(t, "probe", folder)
	out, _ := cmd.Output() // exit status 3 when the lock is held
	return strings.TrimSpace(string(out))
}

// lockHelperCommand is this test binary running TestLockHolderHelper in mode on folder.
func lockHelperCommand(t *testing.T, mode, folder string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("executable: %v", err)
	}
	cmd := exec.Command(executable, "-test.run=^TestLockHolderHelper$")
	cmd.Env = append(os.Environ(), lockHolderModeEnv+"="+mode, lockHolderFolderEnv+"="+folder)
	return cmd
}

// snapshotFolder maps every path under folder to its bytes, or to nil for a folder,
// so a test can show nothing changed.
func snapshotFolder(t *testing.T, folder string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(folder, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(folder, path)
		if err != nil || rel == "." {
			return err
		}
		if entry.IsDir() {
			out[rel] = "<dir>"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", folder, err)
	}
	return out
}

// inodeOf is the file's inode, to tell the same lock file from a replacement; 0 when it is missing.
func inodeOf(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return stat.Ino
}

// readText is the file's text, or "" when it cannot be read.
func readText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// unusedProcessID is a pid no process has.
func unusedProcessID() int32 {
	candidate := int32(100000)
	for {
		err := syscall.Kill(int(candidate), 0)
		if err != nil && err != syscall.EPERM {
			return candidate
		}
		candidate++
	}
}
