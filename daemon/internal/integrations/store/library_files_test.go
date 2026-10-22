package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestLibraryFilesListOnlyTheLibrary checks the library listing takes the
// state files and version files of belts and agents, and nothing hidden or stray.
func TestLibraryFilesListOnlyTheLibrary(t *testing.T) {
	t.Parallel()
	paths := Paths{Root: t.TempDir()}
	writeText(t, filepath.Join(paths.Belts(), "a", "v1.json"), `{}`)
	writeText(t, filepath.Join(paths.Belts(), "a", "state.json"), `{}`)
	writeText(t, filepath.Join(paths.Belts(), "a", "notes.txt"), `x`)
	writeText(t, filepath.Join(paths.Belts(), ".hidden", "v1.json"), `{}`)
	writeText(t, filepath.Join(paths.Agents(), "b", "v1.md"), `x`)
	writeText(t, filepath.Join(paths.Agents(), "b", "v2.json"), `x`)
	writeText(t, filepath.Join(paths.Agents(), "b", "state.json"), `{}`)
	want := []string{"agents/b/state.json", "agents/b/v1.md", "belts/a/state.json", "belts/a/v1.json"}
	if got := libraryFilesList(paths); !reflect.DeepEqual(got, want) {
		t.Fatalf("list %v, want %v", got, want)
	}
	files := LibraryFilesRead(paths)
	if len(files) != len(want) || string(files["belts/a/v1.json"]) != "{}" {
		t.Fatalf("read %d files", len(files))
	}
}

// TestLibraryFilesChangedReportsNewAndChangedPaths checks a stamp that moved, or
// a path that is new, is reported, sorted, and an unchanged one is not.
func TestLibraryFilesChangedReportsNewAndChangedPaths(t *testing.T) {
	t.Parallel()
	paths := Paths{Root: t.TempDir()}
	writeText(t, filepath.Join(paths.Belts(), "a", "v1.json"), `{}`)
	writeText(t, filepath.Join(paths.Belts(), "b", "v1.json"), `{}`)
	before := libraryFullStamps(paths)
	later := time.Unix(1_800_000_000, 0)
	if err := os.Chtimes(filepath.Join(paths.Belts(), "b", "v1.json"), later, later); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(paths.Belts(), "c", "v1.json"), `{}`)
	after := libraryFullStamps(paths)
	want := []string{"belts/b/v1.json", "belts/c/v1.json"}
	if got := LibraryFilesChanged(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed %v, want %v", got, want)
	}
}
