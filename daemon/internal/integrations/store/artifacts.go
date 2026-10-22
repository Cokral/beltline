// Discovery and
// name checks. Confined reads are in artifacts_confined.go.
//
// Disk-only discovery. It never reads or executes generated HTML.

package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ArtifactMaxBytes is the largest artifact the daemon serves. Larger files answer "too large".
const ArtifactMaxBytes = 8 * 1024 * 1024

// ArtifactFile is an artifact child's name and size.
type ArtifactFile struct {
	Name      string
	SizeBytes int64
}

// ArtifactsList lists the eligible HTML children of folder as resolved paths,
// sorted by name. A folder that is missing, empty or not a real directory has
// none.
func ArtifactsList(folder string) []string {
	if !artifactFolderIsReal(folder) {
		return []string{}
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return []string{}
	}
	type found struct{ name, path string }
	var files []found
	for _, entry := range entries {
		if path, ok := ArtifactsResolve(entry.Name(), folder); ok {
			files = append(files, found{name: entry.Name(), path: path})
		}
	}
	sort.Slice(files, func(i, j int) bool { return artifactPrecedes(files[i].name, files[j].name) })
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.path)
	}
	return paths
}

// artifactPrecedes orders names case-insensitively, with the case-sensitive
// order breaking ties, so the result does not depend on the host's volume.
func artifactPrecedes(left, right string) bool {
	// NSString's compare treats canonically equivalent text alike and orders by the composed
	// characters, so names are composed (NFC) before they are compared.
	l, r := norm.NFC.String(strings.ToLower(left)), norm.NFC.String(strings.ToLower(right))
	if l != r {
		return l < r
	}
	return left < right
}

// ArtifactsResolve is the resolved path of one artifact name in folder, when
// it is an HTML regular file that is readable, not a link, and lies directly
// in folder. The name is re-checked on every action; the file may have changed
// since discovery. This is not a sandbox against a malicious agent.
func ArtifactsResolve(name, folder string) (string, bool) {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") ||
		!isHTMLName(name) || !artifactFolderIsReal(folder) {
		return "", false
	}
	file := filepath.Join(folder, name)
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	if !isReadable(file) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", false
	}
	directory, err := filepath.EvalSymlinks(folder)
	if err != nil || filepath.Dir(resolved) != directory {
		return "", false
	}
	return resolved, true
}

// isHTMLName reports whether a name's extension is html, in any case.
func isHTMLName(name string) bool {
	return strings.EqualFold(strings.TrimPrefix(filepath.Ext(name), "."), "html")
}

// isReadable reports whether the file opens for reading.
func isReadable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

// artifactFolderIsReal reports whether folder is an existing directory that is
// not a symbolic link.
func artifactFolderIsReal(folder string) bool {
	info, err := os.Lstat(folder)
	return err == nil && info.IsDir()
}
