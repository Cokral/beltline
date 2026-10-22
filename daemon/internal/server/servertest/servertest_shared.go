// Shared contract fixtures for the read-only tests. Starting a fixture loads the coordinator's runs and
// library, which is most of a read-only test's cost, so the tests that only read one of the three
// fixture kinds below share it: one start per package, not one per test.
//
// A test may use a shared fixture only when it changes nothing in it. Writes, commands, the views
// seeding of another group, the market's browsing, files written into the data folder, the desktop
// recorder and the mutable lookups all change what the next test reads, so those tests start a fixture
// of their own with Start. The shared fixtures are closed by closeSharedFixtures, which TestMain calls
// after the tests.

package servertest

import (
	"os"
	"sync"
	"testing"
)

var (
	sharedMu sync.Mutex
	// sharedClosers are the close functions of the shared fixtures' coordinators, in start order.
	sharedClosers []func()
	// sharedDirs are the folders the shared fixtures' data and web roots were made in.
	sharedDirs []string

	readOnlyOnce, seededOnce, emptyOnce sync.Once
	readOnly, seeded, empty             *Fixture
)

// ReadOnlyFixture is the shared contract fixture for read-only tests: the contract demo as Start(t, Options{})
// starts it, with no views seeded. Read-only tests must not call SeedViews on it.
func ReadOnlyFixture(t testing.TB) *Fixture {
	t.Helper()
	readOnlyOnce.Do(func() { readOnly = startShared(t, Options{}) })
	return requireShared(t, readOnly)
}

// SeededReadOnlyFixture is the shared contract fixture for read-only tests that read the seeded views: as
// ReadOnlyFixture, with SeedViews applied once. Tests on it call SeedViews before their reads, as they did
// with Start.
func SeededReadOnlyFixture(t testing.TB) *Fixture {
	t.Helper()
	seededOnce.Do(func() {
		seeded = startShared(t, Options{})
		seeded.SeedViews()
	})
	return requireShared(t, seeded)
}

// EmptyReadOnlyFixture is the shared contract fixture for read-only tests of a first start:
// Start(t, Options{Empty: true}), an empty data folder with default settings.
func EmptyReadOnlyFixture(t testing.TB) *Fixture {
	t.Helper()
	emptyOnce.Do(func() { empty = startShared(t, Options{Empty: true}) })
	return requireShared(t, empty)
}

// startShared starts a fixture whose folders and coordinator outlive the test that started it.
func startShared(t testing.TB, opts Options) *Fixture {
	t.Helper()
	return startFixture(t, opts, fixtureScope{tempDir: sharedTempDir, cleanup: addSharedCloser})
}

// requireShared is the shared fixture, or a failure when its start failed in an earlier test.
func requireShared(t testing.TB, f *Fixture) *Fixture {
	t.Helper()
	if f == nil {
		t.Fatal("servertest: the shared fixture did not start (its first test failed)")
	}
	return f
}

// sharedTempDir is a new empty folder that closeSharedFixtures removes.
func sharedTempDir() string {
	dir, err := os.MkdirTemp("", "beltline-servertest-shared-")
	if err != nil {
		panic("servertest: shared folder: " + err.Error())
	}
	sharedMu.Lock()
	defer sharedMu.Unlock()
	sharedDirs = append(sharedDirs, dir)
	return dir
}

// addSharedCloser records close to run in closeSharedFixtures.
func addSharedCloser(close func()) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	sharedClosers = append(sharedClosers, close)
}

// closeSharedFixtures closes every shared fixture's coordinator, last started first, and removes the folders
// they were made in. TestMain calls it after m.Run.
func closeSharedFixtures() {
	sharedMu.Lock()
	closers, dirs := sharedClosers, sharedDirs
	sharedClosers, sharedDirs = nil, nil
	sharedMu.Unlock()
	for i := len(closers) - 1; i >= 0; i-- {
		closers[i]()
	}
	for _, dir := range dirs {
		_ = os.RemoveAll(dir)
	}
}
