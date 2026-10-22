package testkit

import (
	"os"
	"path/filepath"
	"testing"
)

// RepoRoot returns the repository root (the folder holding daemon/ and Web/),
// found by walking up from the working directory until a directory containing
// both daemon/go.mod and a Web folder exists. It fails the test if none does.
func RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("testkit: working directory: %v", err)
		return ""
	}
	for {
		if isRepoRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("testkit: no repository root (daemon/go.mod and Web/) above the working directory")
			return ""
		}
		dir = parent
	}
}

// isRepoRoot reports whether dir holds daemon/go.mod and a Web folder.
func isRepoRoot(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "daemon", "go.mod")); err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "Web"))
	return err == nil && info.IsDir()
}

// DaemonRoot returns RepoRoot/daemon.
func DaemonRoot(t testing.TB) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), "daemon")
}

// Testdata returns DaemonRoot/testdata joined with the given elements.
func Testdata(t testing.TB, elem ...string) string {
	t.Helper()
	return filepath.Join(append([]string{DaemonRoot(t), "testdata"}, elem...)...)
}

// ReadFixture reads Testdata("fixtures", elem...) and fails the test on error.
func ReadFixture(t testing.TB, elem ...string) []byte {
	t.Helper()
	path := Testdata(t, append([]string{"fixtures"}, elem...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testkit: read fixture %s: %v", path, err)
		return nil
	}
	return data
}
