package demo

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// sharedSeedRoot holds the one seed the read-only tests share, built on first use.
var (
	sharedSeedOnce sync.Once
	sharedSeedRoot string
	sharedSeedErr  error
)

// TestMain removes the shared seed this process built, after the tests.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedSeedRoot != "" {
		_ = os.RemoveAll(sharedSeedRoot)
	}
	os.Exit(code)
}

// noEnv is an environment with no variables set.
func noEnv(string) string { return "" }

// sharedSeed is the demo seed built with testkit.DemoTime, built once per test
// binary and read by every test that does not write to it. It is not copied
// per test: testkit.CopyTree copies file by file, which takes about half a
// second for this seed, longer than the tests it would serve. Mutating tests
// build their own small data instead.
func sharedSeed(t testing.TB) string {
	t.Helper()
	sharedSeedOnce.Do(func() {
		dir, err := os.MkdirTemp("", "beltline-demo-seed-")
		if err != nil {
			sharedSeedErr = err
			return
		}
		sharedSeedRoot = dir
		_, sharedSeedErr = Make(filepath.Join(dir, "seed"), testkit.DemoTime, noEnv)
	})
	if sharedSeedErr != nil {
		t.Fatalf("demo seed: %v", sharedSeedErr)
	}
	return filepath.Join(sharedSeedRoot, "seed")
}

// unusedProcessID is a process id no live process has, found by probing upwards.
func unusedProcessID(t *testing.T) int {
	t.Helper()
	candidate := 100_000
	for {
		err := syscall.Kill(candidate, 0)
		if err != nil && err != syscall.EPERM {
			return candidate
		}
		candidate++
	}
}
