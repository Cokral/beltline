// Tests of the launch that open a demo through LaunchSession or LaunchSetup, which the demo package's
// DemoRoot tests do not cover. The coordinator's unexported parts are not asserted here.

package launch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/kit/demo"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestPausingABeltInOneDemoLeavesTheOtherRunning: two demos are two folders, so a belt paused in one is not paused
// in the other.
func TestPausingABeltInOneDemoLeavesTheOtherRunning(t *testing.T) {
	t.Parallel()
	one, err := LaunchSetupDemo(launchDemoFolder(t), noNotify, nil)
	if err != nil {
		t.Fatalf("demo one: %v", err)
	}
	t.Cleanup(func() { one.Close() })
	two, err := LaunchSetupDemo(launchDemoFolder(t), noNotify, nil)
	if err != nil {
		t.Fatalf("demo two: %v", err)
	}
	t.Cleanup(func() { two.Close() })
	var id string
	for _, record := range one.Belts() {
		if !record.Paused {
			id = record.Belt.ID
			break
		}
	}
	if id == "" {
		t.Fatal("the demo has no running belt")
	}
	if err := one.SetPaused(id, true); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	if !pausedBelt(one, id) {
		t.Errorf("belt %q is not paused in the demo that paused it", id)
	}
	if pausedBelt(two, id) {
		t.Errorf("belt %q is paused in the other demo", id)
	}
}

// pausedBelt reports whether the coordinator has the belt paused.
func pausedBelt(c *coordinator.Coordinator, id string) bool {
	for _, record := range c.Belts() {
		if record.Belt.ID == id {
			return record.Paused
		}
	}
	return false
}

// spyBuild stands in for seeding, so allocation tests stay fast: a seeded demo's contents move in beside the
// lock file, which stays.
func spyBuild(t *testing.T) LaunchSessionBuild {
	return func(folder string, notifier Notifier, check []string) (*coordinator.Coordinator, error) {
		seeded := launchDemoFolder(t)
		entries, err := os.ReadDir(seeded)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if err := os.Rename(filepath.Join(seeded, entry.Name()), filepath.Join(folder, entry.Name())); err != nil {
				return nil, err
			}
		}
		return LaunchSetupDemo(folder, notifier, check)
	}
}

// TestTwoDefaultDemosGetTheirOwnFoldersAndLocks: two default demos under one parent get their own folders and locks,
// and closing one removes only its own folder.
func TestTwoDefaultDemosGetTheirOwnFoldersAndLocks(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), "beltline-demo")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	one, err := LaunchSessionDemo(nil, parent, noNotify, nil, spyBuild(t))
	if err != nil {
		t.Fatalf("demo one: %v", err)
	}
	two, err := LaunchSessionDemo(nil, parent, noNotify, nil, spyBuild(t))
	if err != nil {
		t.Fatalf("demo two: %v", err)
	}
	pid := os.Getpid()
	if one.Root == two.Root {
		t.Fatalf("both demos share the folder %q", one.Root)
	}
	if filepath.Dir(one.Root) != parent || filepath.Dir(two.Root) != parent {
		t.Errorf("the demos are not in %q: %q and %q", parent, one.Root, two.Root)
	}
	if prefix := fmt.Sprintf("beltline-demo-%d-", pid); !strings.HasPrefix(filepath.Base(one.Root), prefix) {
		t.Errorf("folder %q lacks the prefix %q", filepath.Base(one.Root), prefix)
	}
	if !one.Lock.Held() || !two.Lock.Held() {
		t.Error("a demo does not hold its lock")
	}
	for _, root := range []string{one.Root, two.Root} {
		if _, err := os.Stat(OwnershipLockFile(root)); err != nil {
			t.Errorf("no lock file in %q: %v", root, err)
		}
	}
	if !one.AutomaticRoot || !two.AutomaticRoot {
		t.Error("the default demos are not automatic")
	}
	one.Close()
	if _, err := os.Stat(one.Root); !os.IsNotExist(err) {
		t.Errorf("closing kept its folder: %v", err)
	}
	if _, err := os.Stat(two.Root); err != nil {
		t.Errorf("closing one removed the other: %v", err)
	}
	_, err = OwnershipLockAcquire(two.Root, int32(pid))
	wantHeld(t, err, two.Root, new(int32(pid)))
	two.Close()
}

// TestASuppliedRootIsReopenedWithoutSweepingItsSiblings: a demo folder the caller names is opened as it is, not
// seeded again, and its siblings are left alone.
func TestASuppliedRootIsReopenedWithoutSweepingItsSiblings(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	dead := filepath.Join(parent, fmt.Sprintf("beltline-demo-%d-gone", unusedProcessID()))
	if err := os.MkdirAll(dead, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	root := filepath.Join(parent, "beltline-demo-mine")
	if err := os.Rename(launchDemoFolder(t), root); err != nil {
		t.Fatalf("move demo copy: %v", err)
	}
	marker := filepath.Join(root, "belts", "marker")
	if err := os.WriteFile(marker, []byte("kept"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	session, err := LaunchSessionDemo(&root, "", noNotify, nil, nil)
	if err != nil {
		t.Fatalf("LaunchSessionDemo: %v", err)
	}
	if session.AutomaticRoot {
		t.Error("a supplied root is marked automatic")
	}
	assertPresent(t, dead)
	assertPresent(t, marker)
	session.Close()
	assertPresent(t, root)
	assertPresent(t, OwnershipLockFile(root))
}

// TestSeedingAFreshDemoKeepsItsLock: seeding empties the folder first, and the lock stays held and the same file
// throughout.
func TestSeedingAFreshDemoKeepsItsLock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := OwnershipLockFile(root)
	var probes []string
	var inodes []uint64
	session, err := LaunchSessionDemo(&root, "", noNotify, nil,
		func(folder string, notifier Notifier, check []string) (*coordinator.Coordinator, error) {
			if err := os.WriteFile(filepath.Join(folder, "old-fixture"), []byte("old"), 0o644); err != nil {
				return nil, err
			}
			inodes = append(inodes, inodeOf(file))
			probes = append(probes, lockProbe(t, folder))
			if _, err := demo.Make(folder, testkit.ContractTime, os.Getenv); err != nil {
				return nil, err
			}
			inodes = append(inodes, inodeOf(file))
			probes = append(probes, lockProbe(t, folder))
			return LaunchSetupDemo(folder, notifier, check)
		})
	if err != nil {
		t.Fatalf("LaunchSessionDemo: %v", err)
	}
	held := fmt.Sprintf("held %d", os.Getpid())
	if len(probes) != 2 || probes[0] != held || probes[1] != held {
		t.Errorf("probes = %v, want two of %q", probes, held)
	}
	if len(inodes) != 2 || inodes[0] == 0 || inodes[0] != inodes[1] {
		t.Errorf("lock inodes = %v", inodes)
	}
	if _, err := os.Stat(filepath.Join(root, "old-fixture")); !os.IsNotExist(err) {
		t.Errorf("seeding kept the old fixture: %v", err)
	}
	if got := lockProbe(t, root); got != held {
		t.Errorf("after seeding the folder is %q", got)
	}
	if inodeOf(file) != inodes[0] {
		t.Error("the lock file was replaced by seeding")
	}
	session.Close()
}

// assertPresent fails the test when path does not exist.
func assertPresent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("%s is gone: %v", path, err)
	}
}
