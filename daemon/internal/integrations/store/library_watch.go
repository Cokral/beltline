// The library's file stamps, read through a watch that keeps each folder's entry names between reads.
// LibraryFilesStamps lists every belt and agent folder on each call; most reads and every tick ask
// again, and nearly every folder is unchanged, so the cached names let a read skip the folders that did
// not change.

package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// libraryRacyWindow is how old a folder's modification time must be before its names are kept.
// Filesystems stamp times at a granularity, so a second change made within the same tick as the
// last one leaves the time where it was; a recent folder is listed again on every read.
const libraryRacyWindow = 2 * time.Second

// libraryFolder is a folder's entry names as they were listed while its modification time was modified.
type libraryFolder struct {
	modified time.Time
	names    []string
}

// LibraryWatch reads the library's stamps like LibraryFilesStamps, but lists a folder again only when
// its modification time has moved. A folder's time moves when an entry is added, removed or renamed in
// it, which is how every write to the library lands (atomic writes rename a file into place), so an
// unchanged folder's names are still right. A file rewritten in place keeps its folder's names and
// shows through its own stamp, which is read on every call. A LibraryWatch is used under the
// coordinator's lock, as the library is.
type LibraryWatch struct {
	folders map[string]libraryFolder
}

// NewLibraryWatch returns a watch with no folders listed yet.
func NewLibraryWatch() *LibraryWatch {
	return &LibraryWatch{folders: map[string]libraryFolder{}}
}

// Stamps is LibraryFilesStamps: when each library file last changed, keyed by its path under the root.
// A file that cannot be stat'd has the zero time. Lock: none of its own; the caller serialises use.
func (w *LibraryWatch) Stamps(paths Paths) map[string]time.Time {
	stamps := map[string]time.Time{}
	for _, path := range w.list(paths) {
		info, err := os.Stat(filepath.Join(paths.Root, path))
		if err != nil {
			stamps[path] = time.Time{}
			continue
		}
		stamps[path] = info.ModTime()
	}
	return stamps
}

// list is libraryFilesList with each folder's names taken from the watch.
func (w *LibraryWatch) list(paths Paths) []string {
	result := []string{}
	for _, lib := range libraryKinds(paths) {
		for _, id := range w.names(lib.folder) {
			if strings.HasPrefix(id, ".") {
				continue
			}
			for _, name := range w.names(filepath.Join(lib.folder, id)) {
				if name == "state.json" || (strings.HasPrefix(name, "v") && strings.HasSuffix(name, "."+lib.ext)) {
					result = append(result, lib.kind+"/"+id+"/"+name)
				}
			}
		}
	}
	sort.Strings(result)
	return result
}

// names is subfolderNames(dir), listed again unless dir's modification time is the one its names were
// listed at, and that time is older than the racy window. A folder that is gone has no names.
func (w *LibraryWatch) names(dir string) []string {
	info, err := os.Stat(dir)
	if err != nil {
		delete(w.folders, dir)
		return nil
	}
	modified := info.ModTime()
	settled := time.Since(modified) >= libraryRacyWindow
	if cached, ok := w.folders[dir]; ok && settled && cached.modified.Equal(modified) {
		return cached.names
	}
	names := subfolderNames(dir)
	if settled {
		w.folders[dir] = libraryFolder{modified: modified, names: names}
	} else {
		delete(w.folders, dir)
	}
	return names
}
