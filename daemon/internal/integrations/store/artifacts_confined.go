// The confined
// reads and the path checks below the trusted data root).
//
// Every component below the trusted root is opened relative to
// its parent's descriptor with openat and O_NOFOLLOW (O_DIRECTORY for the
// directories, and the final file is checked with fstat or fstatat on that
// descriptor. Nothing below the root is looked up by path again, so a parent
// swapped for a link after it was opened cannot lead outside the root.

package store

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// ArtifactRead is what reading one artifact found.
type ArtifactRead struct {
	Kind  ArtifactReadKind
	Bytes []byte
	// Size is the file's size when Kind is ArtifactReadTooLarge.
	Size int64
}

// ArtifactReadKind says which outcome an ArtifactRead holds.
type ArtifactReadKind int

const (
	// ArtifactReadMissing: the artifact is absent, not a regular file, or unreadable.
	ArtifactReadMissing ArtifactReadKind = iota
	// ArtifactReadBytes: the bytes were read.
	ArtifactReadBytes
	// ArtifactReadTooLarge: the file is over the cap and was not read.
	ArtifactReadTooLarge
)

// ArtifactsListConfined lists the eligible immediate HTML children of folder,
// when folder is a real directory below trustedRoot reached without links.
// Entries that are not regular files, or are links, are left out.
func ArtifactsListConfined(folder, trustedRoot string) []ArtifactFile {
	directory, ok := confinedFolder(folder, trustedRoot)
	if !ok {
		return []ArtifactFile{}
	}
	defer func() { _ = unix.Close(directory) }()
	duplicate, err := unix.Dup(directory)
	if err != nil {
		return []ArtifactFile{}
	}
	stream := os.NewFile(uintptr(duplicate), "artifacts")
	defer func() { _ = stream.Close() }()
	entries, err := stream.ReadDir(-1)
	if err != nil {
		return []ArtifactFile{}
	}
	found := []ArtifactFile{}
	for _, entry := range entries {
		name := entry.Name()
		if !validConfinedName(name) {
			continue
		}
		var info unix.Stat_t
		if unix.Fstatat(directory, name, &info, unix.AT_SYMLINK_NOFOLLOW) != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Size < 0 {
			continue
		}
		// A final descriptor-relative check omits children removed while
		// enumeration was in progress.
		var final unix.Stat_t
		if unix.Fstatat(directory, name, &final, unix.AT_SYMLINK_NOFOLLOW) != nil || final.Mode&unix.S_IFMT != unix.S_IFREG {
			continue
		}
		found = append(found, ArtifactFile{Name: name, SizeBytes: final.Size})
	}
	sort.Slice(found, func(i, j int) bool { return artifactPrecedes(found[i].Name, found[j].Name) })
	return found
}

// ArtifactsReadConfinedResult reads one immediate artifact child with the size
// outcome kept, so a caller can answer "too large" apart from "missing". Files
// over maxBytes are not read at all, and a file that grows past the cap while
// it is read stops there. Blocking I/O.
func ArtifactsReadConfinedResult(name, folder, trustedRoot string, maxBytes int) ArtifactRead {
	if !validConfinedName(name) {
		return ArtifactRead{Kind: ArtifactReadMissing}
	}
	directory, ok := confinedFolder(folder, trustedRoot)
	if !ok {
		return ArtifactRead{Kind: ArtifactReadMissing}
	}
	descriptor, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	_ = unix.Close(directory)
	if err != nil {
		return ArtifactRead{Kind: ArtifactReadMissing}
	}
	file := os.NewFile(uintptr(descriptor), name)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ArtifactRead{Kind: ArtifactReadMissing}
	}
	if info.Size() > int64(maxBytes) {
		return ArtifactRead{Kind: ArtifactReadTooLarge, Size: info.Size()}
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(file, int64(maxBytes)+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return ArtifactRead{Kind: ArtifactReadMissing}
	}
	if buf.Len() > maxBytes {
		return ArtifactRead{Kind: ArtifactReadTooLarge, Size: int64(buf.Len())}
	}
	return ArtifactRead{Kind: ArtifactReadBytes, Bytes: buf.Bytes()}
}

// ArtifactsResolveConfined is the path of a regular or directory child named
// name in folder, checked without following links below trustedRoot. Used
// before a path-based desktop launch.
func ArtifactsResolveConfined(name, folder, trustedRoot string) (string, bool) {
	if !validConfinedName(name) {
		return "", false
	}
	return ArtifactsConfinedURL(filepath.Join(folder, name), trustedRoot, nil)
}

// ArtifactsConfinedURL revalidates a path below the data root without
// following links. directory, when set, requires a directory (true) or a
// regular file (false); nil accepts either. Used immediately before passing a
// target to Finder.
func ArtifactsConfinedURL(path, trustedRoot string, directory *bool) (string, bool) {
	components, ok := relativeComponents(path, trustedRoot)
	if !ok || len(components) == 0 {
		return "", false
	}
	parent, final, ok := openParent(trustedRoot, components)
	if !ok {
		return "", false
	}
	defer func() { _ = unix.Close(parent) }()
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	if directory != nil && *directory {
		flags |= unix.O_DIRECTORY
	}
	descriptor, err := unix.Openat(parent, final, flags, 0)
	if err != nil {
		return "", false
	}
	defer func() { _ = unix.Close(descriptor) }()
	var info unix.Stat_t
	if unix.Fstat(descriptor, &info) != nil {
		return "", false
	}
	// Stat_t.Mode is 16 bits on macOS and 32 on Linux.
	kind := uint32(info.Mode) & unix.S_IFMT
	if directory != nil {
		want := uint32(unix.S_IFREG)
		if *directory {
			want = unix.S_IFDIR
		}
		if kind != want {
			return "", false
		}
	} else if kind != unix.S_IFREG && kind != unix.S_IFDIR {
		return "", false
	}
	return filepath.Clean(path), true
}

// validConfinedName is Artifacts.validName: a single HTML file name.
func validConfinedName(name string) bool {
	return name != "" && !strings.Contains(name, "/") && !strings.Contains(name, "\\") &&
		!strings.Contains(name, "..") && isHTMLName(name)
}

// confinedFolder opens folder as a directory descriptor, when it is a real
// directory reached from trustedRoot without any link on the way. The caller
// closes it.
func confinedFolder(folder, trustedRoot string) (int, bool) {
	components, ok := relativeComponents(folder, trustedRoot)
	if !ok {
		return -1, false
	}
	parent, final, ok := openParent(trustedRoot, components)
	if !ok {
		return -1, false
	}
	descriptor, err := unix.Openat(parent, final, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	_ = unix.Close(parent)
	if err != nil {
		return -1, false
	}
	return descriptor, true
}

// relativeComponents are the path parts of folder below trustedRoot. The root
// may be named as given or with its links resolved, since enumeration can hand
// back the resolved form of a child of the given root.
func relativeComponents(folder, trustedRoot string) ([]string, bool) {
	path := filepath.Clean(folder)
	resolved, err := filepath.EvalSymlinks(trustedRoot)
	if err != nil {
		resolved = filepath.Clean(trustedRoot)
	}
	var suffix string
	found := false
	for _, base := range []string{filepath.Clean(trustedRoot), resolved} {
		if rest, ok := strings.CutPrefix(path, base+"/"); ok {
			suffix, found = rest, true
			break
		}
	}
	if !found {
		return nil, false
	}
	components := strings.Split(suffix, "/")
	for _, c := range components {
		if c == "" || c == "." || c == ".." {
			return nil, false
		}
	}
	return components, true
}

// confinedWalkOpened, when set by a test, runs after openParent opens the
// component at depth, so a test can swap a parent while the walk holds it.
var confinedWalkOpened func(depth int)

// openParent opens the parent of the last component, every component below the
// canonical root opened relative to the one before it without following links,
// and returns that descriptor (the caller closes it) with the last component.
func openParent(trustedRoot string, components []string) (int, string, bool) {
	if len(components) == 0 {
		return -1, "", false
	}
	canonical, err := filepath.EvalSymlinks(trustedRoot)
	if err != nil {
		return -1, "", false
	}
	current, err := unix.Open(canonical, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", false
	}
	for depth, component := range components[:len(components)-1] {
		next, err := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(current)
		if err != nil {
			return -1, "", false
		}
		current = next
		if confinedWalkOpened != nil {
			confinedWalkOpened(depth)
		}
	}
	return current, components[len(components)-1], true
}
