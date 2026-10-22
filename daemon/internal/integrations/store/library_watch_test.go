package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// settle moves every folder under the library back past the racy window, so the watch keeps its names.
func settle(t *testing.T, paths Paths) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	for _, dir := range []string{paths.Belts(), paths.Agents()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			child := filepath.Join(dir, entry.Name())
			if err := os.Chtimes(child, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// sameStamps checks the watch against the full listing, with each folder's names read again.
func sameStamps(t *testing.T, w *LibraryWatch, paths Paths, what string) {
	t.Helper()
	want := libraryFullStamps(paths)
	got := w.Stamps(paths)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: watch has %v, the full listing has %v", what, got, want)
	}
}

func writeLibraryFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLibraryWatchMatchesTheFullListing checks the watch stamps the library exactly as libraryFullStamps
// does through the changes a library sees: a new version, a rename into place, a removed folder, and an
// in-place edit, each with the folders settled so the names are kept.
func TestLibraryWatchMatchesTheFullListing(t *testing.T) {
	t.Parallel()
	paths := Paths{Root: t.TempDir()}
	writeLibraryFile(t, filepath.Join(paths.Belts(), "build", "v1.json"), "{}")
	writeLibraryFile(t, filepath.Join(paths.Belts(), "build", "state.json"), "{}")
	writeLibraryFile(t, filepath.Join(paths.Agents(), "helper", "v1.md"), "# helper")
	writeLibraryFile(t, filepath.Join(paths.Belts(), ".hidden", "v1.json"), "{}")
	w := NewLibraryWatch()

	sameStamps(t, w, paths, "first read")
	settle(t, paths)
	sameStamps(t, w, paths, "settled read")

	writeLibraryFile(t, filepath.Join(paths.Belts(), "build", "v2.json"), "{}")
	sameStamps(t, w, paths, "a new version")
	settle(t, paths)
	sameStamps(t, w, paths, "a new version once settled")

	temp := filepath.Join(paths.Belts(), "build", "state.json.tmp")
	writeLibraryFile(t, temp, `{"paused":true}`)
	if err := os.Rename(temp, filepath.Join(paths.Belts(), "build", "state.json")); err != nil {
		t.Fatal(err)
	}
	sameStamps(t, w, paths, "a rename into place")
	settle(t, paths)

	if err := os.WriteFile(filepath.Join(paths.Agents(), "helper", "v1.md"), []byte("# edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	sameStamps(t, w, paths, "an in-place edit under a settled folder")
	settle(t, paths)

	if err := os.RemoveAll(filepath.Join(paths.Belts(), "build")); err != nil {
		t.Fatal(err)
	}
	sameStamps(t, w, paths, "a removed folder")
	writeLibraryFile(t, filepath.Join(paths.Belts(), "fresh", "v1.json"), "{}")
	sameStamps(t, w, paths, "a folder created after a removal")
}

// libraryFullStamps, the test oracle for LibraryWatch, is when each library file last changed, to notice what
// someone else wrote. A file that cannot be stat'd has the zero time.
func libraryFullStamps(paths Paths) map[string]time.Time {
	stamps := map[string]time.Time{}
	for _, path := range libraryFilesList(paths) {
		info, err := os.Stat(filepath.Join(paths.Root, path))
		if err != nil {
			stamps[path] = time.Time{}
			continue
		}
		stamps[path] = info.ModTime()
	}
	return stamps
}
