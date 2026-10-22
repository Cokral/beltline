// Tests of the demo folders. The launch and coordinator tests are in the launch package.

package demo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

// TestDemoRootSweepsOnlyItsParent: allocating a root removes the dead demos of
// its parent and nothing else.
func TestDemoRootSweepsOnlyItsParent(t *testing.T) {
	t.Parallel()
	temporary := t.TempDir()
	parent := filepath.Join(temporary, "beltline-demo")
	deadID := unusedProcessID(t)
	dead := filepath.Join(parent, "beltline-demo-"+strconv.Itoa(deadID)+"-gone")
	live := filepath.Join(parent, "beltline-demo-"+strconv.Itoa(os.Getpid())+"-live")
	legacy := filepath.Join(parent, "beltline-demo-old")
	malformed := filepath.Join(parent, "beltline-demo-")
	unrelated := filepath.Join(parent, "notes")
	beside := filepath.Join(temporary, "beltline-demo-"+strconv.Itoa(deadID)+"-beside")
	for _, folder := range []string{dead, live, legacy, malformed, unrelated, beside} {
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root := DemoRootAllocate(parent, os.Getpid(), syscall.Kill)
	if filepath.Dir(root) != parent {
		t.Errorf("allocated %s, want a folder in %s", root, parent)
	}
	assertGone(t, dead)
	assertGone(t, legacy)
	assertGone(t, malformed)
	assertPresent(t, live)
	assertPresent(t, unrelated)
	assertPresent(t, beside)
}

// TestADemoAnotherUserOwnsIsKept: a demo whose process another user owns (EPERM)
// is kept, and one whose process is gone (ESRCH) is removed.
func TestADemoAnotherUserOwnsIsKept(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	other := filepath.Join(parent, "beltline-demo-"+strconv.Itoa(unusedProcessID(t))+"-other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	DemoRootSweep(parent, func(int, syscall.Signal) error { return syscall.EPERM })
	assertPresent(t, other)
	DemoRootSweep(parent, func(int, syscall.Signal) error { return syscall.ESRCH })
	assertGone(t, other)
}

// TestAKilledDemosFolderIsSweptNextTime: a demo folder is kept while its process
// lives and swept once the process is killed. A plain sleeping child stands in for
// the process that holds the folder's lock, since the lock belongs to the launch package.
func TestAKilledDemosFolderIsSweptNextTime(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	root := filepath.Join(parent, "beltline-demo-"+strconv.Itoa(child.Process.Pid)+"-killed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	DemoRootSweep(parent, syscall.Kill)
	assertPresent(t, root)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	DemoRootSweep(parent, syscall.Kill)
	assertGone(t, root)
}

// TestDemoRootParentHonoursTMPDIR: the parent is $TMPDIR/beltline-demo when
// TMPDIR is set and not empty, and the given temporary folder otherwise.
func TestDemoRootParentHonoursTMPDIR(t *testing.T) {
	t.Parallel()
	set := func(key string) string {
		if key == "TMPDIR" {
			return "/work/tmp"
		}
		return ""
	}
	if got := DemoRootParent(set, "/sys/tmp"); got != "/work/tmp/beltline-demo" {
		t.Errorf("parent with TMPDIR = %s", got)
	}
	if got := DemoRootParent(noEnv, "/sys/tmp"); got != "/sys/tmp/beltline-demo" {
		t.Errorf("parent without TMPDIR = %s", got)
	}
}

// assertGone fails the test when path still exists.
func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s should have been removed", path)
	}
}

// assertPresent fails the test when path is missing.
func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s should still exist: %v", path, err)
	}
}
