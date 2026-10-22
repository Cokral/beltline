package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder is a testing.TB that records failures instead of stopping the
// test, so the helpers' failure paths can be checked. Only the methods the
// helpers call are overridden; the rest would panic on the nil embedded TB.
type recorder struct {
	testing.TB
	mu     sync.Mutex
	errors []string
	fatals []string
	logs   []string
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(format string, args ...any) {
	r.add(&r.errors, fmt.Sprintf(format, args...))
}

func (r *recorder) Fatalf(format string, args ...any) {
	r.add(&r.fatals, fmt.Sprintf(format, args...))
}

func (r *recorder) Logf(format string, args ...any) {
	r.add(&r.logs, fmt.Sprintf(format, args...))
}

func (r *recorder) add(list *[]string, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*list = append(*list, msg)
}

// keySeq gives each template test its own key, so -count=N runs never share
// a built template.
var keySeq atomic.Int64

func nextKey(name string) string {
	return fmt.Sprintf("%s-%d", name, keySeq.Add(1))
}

func TestClockAdvanceAndSet(t *testing.T) {
	t.Parallel()
	if got := DemoTime.Format(time.RFC3339); got != "2026-10-07T06:40:00Z" {
		t.Fatalf("DemoTime = %s, want 2026-10-07T06:40:00Z", got)
	}
	c := NewClock(DemoTime)
	if got := c.Now(); !got.Equal(DemoTime) {
		t.Fatalf("Now() = %v, want %v", got, DemoTime)
	}
	c.Advance(90 * time.Second)
	if got, want := c.Now(), DemoTime.Add(90*time.Second); !got.Equal(want) {
		t.Fatalf("after Advance, Now() = %v, want %v", got, want)
	}
	earlier := DemoTime.Add(-time.Hour)
	c.Set(earlier)
	if got := c.Now(); !got.Equal(earlier) {
		t.Fatalf("after Set, Now() = %v, want %v", got, earlier)
	}
}

func TestClockConcurrentUse(t *testing.T) {
	t.Parallel()
	c := NewClock(DemoTime)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				c.Advance(time.Second)
				_ = c.Now()
			}
		}()
	}
	wg.Wait()
	if got, want := c.Now(), DemoTime.Add(3200*time.Second); !got.Equal(want) {
		t.Fatalf("after concurrent Advance, Now() = %v, want %v", got, want)
	}
}

func TestRepoRootFindsRepository(t *testing.T) {
	t.Parallel()
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "daemon", "go.mod")); err != nil {
		t.Fatalf("RepoRoot %s has no daemon/go.mod: %v", root, err)
	}
	if info, err := os.Stat(filepath.Join(root, "Web")); err != nil || !info.IsDir() {
		t.Fatalf("RepoRoot %s has no Web folder: %v", root, err)
	}
	if got, want := DaemonRoot(t), filepath.Join(root, "daemon"); got != want {
		t.Fatalf("DaemonRoot() = %s, want %s", got, want)
	}
	version := filepath.Join(root, "daemon", "testdata", "golden", "version.json")
	if _, err := os.Stat(version); err != nil {
		t.Fatalf("golden version.json not under RepoRoot: %v", err)
	}
}

func TestTestdataJoinsElements(t *testing.T) {
	t.Parallel()
	if got, want := Testdata(t), filepath.Join(DaemonRoot(t), "testdata"); got != want {
		t.Fatalf("Testdata() = %s, want %s", got, want)
	}
	if got, want := Testdata(t, "golden", "version.json"), filepath.Join(DaemonRoot(t), "testdata", "golden", "version.json"); got != want {
		t.Fatalf("Testdata(golden, version.json) = %s, want %s", got, want)
	}
}

func TestDataDirIsEmptyAndDistinct(t *testing.T) {
	t.Parallel()
	first, second := DataDir(t), DataDir(t)
	if first == second {
		t.Fatalf("two DataDir calls returned the same folder %s", first)
	}
	for _, dir := range []string{first, second} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read data folder %s: %v", dir, err)
		}
		if len(entries) != 0 {
			t.Fatalf("data folder %s has %d entries, want none", dir, len(entries))
		}
	}
}

func TestCopyTreeCopiesNestedFilesAndModes(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	mustWrite := func(path, content string, perm os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, perm); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(src, "a.txt"), "alpha", 0o600)
	mustWrite(filepath.Join(src, "bin", "run.sh"), "#!/bin/sh\n", 0o755)
	mustWrite(filepath.Join(src, "deep", "er", "est", "ro.txt"), "read only", 0o444)
	mustWrite(filepath.Join(src, "locked", "inside.txt"), "inside", 0o644)
	if err := os.Chmod(filepath.Join(src, "locked"), 0o555); err != nil {
		t.Fatal(err)
	}
	// Restore the mode before t.TempDir's cleanup removes the source.
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(src, "locked"), 0o755); err != nil {
			t.Errorf("restore mode: %v", err)
		}
	})
	if err := os.Symlink("a.txt", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	CopyTree(t, src, dst)
	// Let t.TempDir's cleanup remove the read-only copy.
	t.Cleanup(func() {
		if err := os.Chmod(filepath.Join(dst, "locked"), 0o755); err != nil {
			t.Errorf("restore mode: %v", err)
		}
	})

	files := map[string]string{
		"a.txt":              "alpha",
		"bin/run.sh":         "#!/bin/sh\n",
		"deep/er/est/ro.txt": "read only",
		"locked/inside.txt":  "inside",
	}
	modes := map[string]os.FileMode{
		"a.txt":              0o600,
		"bin/run.sh":         0o755,
		"deep/er/est/ro.txt": 0o444,
		"locked/inside.txt":  0o644,
		"locked":             0o555,
		".":                  0o755,
	}
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil || string(got) != want {
			t.Fatalf("copied %s = %q, %v; want %q", rel, got, err, want)
		}
	}
	for rel, want := range modes {
		info, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("stat copied %s: %v", rel, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("copied %s mode = %v, want %v", rel, got, want)
		}
	}
	target, err := os.Readlink(filepath.Join(dst, "link"))
	if err != nil || target != "a.txt" {
		t.Fatalf("copied link = %q, %v; want a.txt", target, err)
	}
}

func TestTemplateBuildsOnceAcrossParallelSubtests(t *testing.T) {
	t.Parallel()
	var builds atomic.Int32
	key := nextKey("build-once")
	build := func(dir string) error {
		builds.Add(1)
		return os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o644)
	}
	t.Run("group", func(t *testing.T) {
		for i := range 8 {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()
				dst := Template(t, key, build)
				got, err := os.ReadFile(filepath.Join(dst, "seed.txt"))
				if err != nil || string(got) != "seed" {
					t.Errorf("copy seed.txt = %q, %v; want seed", got, err)
				}
			})
		}
	})
	if n := builds.Load(); n != 1 {
		t.Fatalf("template built %d times, want once", n)
	}
}

func TestTemplateCopiesAreIndependent(t *testing.T) {
	t.Parallel()
	key := nextKey("independent")
	build := func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o644)
	}
	a := Template(t, key, build)
	b := Template(t, key, build)
	if a == b {
		t.Fatalf("two Template calls returned the same folder %s", a)
	}
	if err := os.WriteFile(filepath.Join(a, "seed.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, "only-a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(b, "seed.txt"))
	if err != nil || string(got) != "seed" {
		t.Fatalf("copy b seed.txt = %q, %v; want seed", got, err)
	}
	if _, err := os.Stat(filepath.Join(b, "only-a.txt")); !os.IsNotExist(err) {
		t.Fatalf("copy b sees a file written to copy a: %v", err)
	}
	c := Template(t, key, build)
	if _, err := os.Stat(filepath.Join(c, "only-a.txt")); !os.IsNotExist(err) {
		t.Fatalf("a new copy sees a file written to an earlier copy: %v", err)
	}
}

func TestTemplateReportsBuildFailure(t *testing.T) {
	t.Parallel()
	rec := &recorder{TB: t}
	key := nextKey("failing")
	dst := Template(rec, key, func(dir string) error { return fmt.Errorf("boom") })
	if dst != "" {
		t.Fatalf("Template after a failed build returned %q, want empty", dst)
	}
	if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "boom") {
		t.Fatalf("fatal messages = %q, want one mentioning boom", rec.fatals)
	}
}

// TestCleanupRemovesTemplates must not run in parallel: Cleanup forgets the
// templates of every test in the binary, and the sequential phase of the test
// run is the only time nothing else is using them.
func TestCleanupRemovesTemplates(t *testing.T) {
	key := nextKey("cleanup")
	var builds atomic.Int32
	build := func(dir string) error {
		builds.Add(1)
		return os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o644)
	}
	Template(t, key, build)
	templatesMu.Lock()
	root := templateRoot
	templatesMu.Unlock()
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("template root %q missing before Cleanup: %v", root, err)
	}
	Cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("template root %q still exists after Cleanup: %v", root, err)
	}
	Template(t, key, build)
	if n := builds.Load(); n != 2 {
		t.Fatalf("template built %d times, want 2 (once before and once after Cleanup)", n)
	}
}

func TestEventuallySucceeds(t *testing.T) {
	t.Parallel()
	rec := &recorder{TB: t}
	var calls atomic.Int32
	Eventually(rec, 2*time.Second, "reaches three", func() bool { return calls.Add(1) >= 3 })
	if len(rec.fatals) != 0 || calls.Load() < 3 {
		t.Fatalf("fatals = %q after %d calls", rec.fatals, calls.Load())
	}
}

func TestEventuallyTimesOut(t *testing.T) {
	t.Parallel()
	rec := &recorder{TB: t}
	Eventually(rec, 60*time.Millisecond, "never true", func() bool { return false })
	if len(rec.fatals) != 1 || !strings.Contains(rec.fatals[0], "never true") {
		t.Fatalf("fatal messages = %q, want one naming the condition", rec.fatals)
	}
}

func TestDiff(t *testing.T) {
	t.Parallel()
	type item struct {
		name  string
		count int
	}
	if got := Diff(item{"a", 1}, item{"a", 1}); got != "" {
		t.Fatalf("Diff of equal values = %q, want empty", got)
	}
	got := Diff(item{"a", 1}, item{"a", 2})
	if !strings.Contains(got, "-") || !strings.Contains(got, "+") {
		t.Fatalf("Diff of different values = %q, want - and + lines", got)
	}
	if got := Diff(DemoTime, DemoTime); got != "" {
		t.Fatalf("Diff of equal times = %q, want empty", got)
	}
}
