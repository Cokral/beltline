// The library's own files, belts/<id>/… and agents/<id>/…, keyed by that path.

package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// libraryKind is one library folder and the extension of its version files.
type libraryKind struct {
	kind   string
	folder string
	ext    string
}

// libraryKinds are the two library folders the configuration assistant reads.
func libraryKinds(paths Paths) []libraryKind {
	return []libraryKind{
		{kind: "belts", folder: paths.Belts(), ext: "json"},
		{kind: "agents", folder: paths.Agents(), ext: "md"},
	}
}

// LibraryFilesRead reads every library file under the data root, keyed by its
// path relative to the root. Unreadable files are absent.
func LibraryFilesRead(paths Paths) map[string][]byte {
	files := map[string][]byte{}
	for _, path := range libraryFilesList(paths) {
		if data, err := os.ReadFile(filepath.Join(paths.Root, path)); err == nil {
			files[path] = data
		}
	}
	return files
}

// LibraryFilesChanged lists, sorted, the paths that are new or changed in
// after. The returned set is sorted.
func LibraryFilesChanged(before, after map[string]time.Time) []string {
	changed := []string{}
	for path, stamp := range after {
		previous, ok := before[path]
		if !ok || !previous.Equal(stamp) {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// libraryFilesList is the library's paths, sorted: belts/<id>/state.json and
// belts/<id>/v<n>.json, agents/<id>/state.json and agents/<id>/v<n>.md. Hidden
// folders are skipped.
func libraryFilesList(paths Paths) []string {
	result := []string{}
	for _, lib := range libraryKinds(paths) {
		for _, id := range subfolderNames(lib.folder) {
			if strings.HasPrefix(id, ".") {
				continue
			}
			for _, name := range subfolderNames(filepath.Join(lib.folder, id)) {
				if name == "state.json" || (strings.HasPrefix(name, "v") && strings.HasSuffix(name, "."+lib.ext)) {
					result = append(result, lib.kind+"/"+id+"/"+name)
				}
			}
		}
	}
	sort.Strings(result)
	return result
}

// subfolderNames lists the entry names of dir, sorted. A missing or unreadable directory is empty.
func subfolderNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}
