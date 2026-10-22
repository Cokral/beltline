package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// restoreTestWrite writes text to a path under root, making its folders.
func restoreTestWrite(t *testing.T, root, path, text string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

// restoreTestRead is the text at path under root, or "<missing>" when there is no file there.
func restoreTestRead(root, path string) string {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "<missing>"
	}
	return string(data)
}

func TestLibraryRestoreReplacesTheFilesAndKeepsTheOldOnes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	restoreTestWrite(t, root, "belts/a/v1.json", "old a")
	restoreTestWrite(t, root, "settings.json", "kept")
	s := NewStorage(Paths{Root: root})
	saved, err := s.LibraryRestore("stamp", []core.LibraryFile{{Path: "belts/a/v1.json", Data: []byte("new a")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := restoreTestRead(root, "belts/a/v1.json"); got != "new a" {
		t.Fatalf("restored file holds %q", got)
	}
	if got := restoreTestRead(root, filepath.Join(saved, "belts/a/v1.json")); got != "old a" {
		t.Fatalf("moved-aside file holds %q", got)
	}
	if got := restoreTestRead(root, "settings.json"); got != "kept" {
		t.Fatalf("a state file moved: %q", got)
	}
}

func TestLibraryRestoreAFailedWriteLeavesTheLibraryAsItWas(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	restoreTestWrite(t, root, "belts/a/v1.json", "old a")
	restoreTestWrite(t, root, "agents/x/v1.md", "old x")
	// A file where the restore needs a folder: writing belts/b/v1.json fails after belts/a is written.
	restoreTestWrite(t, root, "belts/b", "not a folder")
	s := NewStorage(Paths{Root: root})
	files := []core.LibraryFile{
		{Path: "belts/a/v1.json", Data: []byte("new a")},
		{Path: "belts/b/v1.json", Data: []byte("new b")},
	}
	if _, err := s.LibraryRestore("stamp", files); err == nil {
		t.Fatal("a restore whose write fails reported no error")
	}
	if got := restoreTestRead(root, "belts/a/v1.json"); got != "old a" {
		t.Fatalf("belts/a/v1.json holds %q after the failed restore, want the old file back", got)
	}
	if got := restoreTestRead(root, "agents/x/v1.md"); got != "old x" {
		t.Fatalf("agents/x/v1.md holds %q after the failed restore, want the old file back", got)
	}
	if got := restoreTestRead(root, "belts/b"); got != "not a folder" {
		t.Fatalf("belts/b changed: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "library-backups", "stamp")); !os.IsNotExist(err) {
		t.Fatalf("the backup folder of the failed restore is still there: %v", err)
	}
}
