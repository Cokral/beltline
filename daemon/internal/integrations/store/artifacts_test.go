package store

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// artifactNames is the base names of resolved artifact paths, in order.
func artifactNames(paths []string) []string {
	names := []string{}
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	return names
}

// writeArtifact writes a small file in folder, creating the folder.
func writeArtifact(t *testing.T, folder, name string) {
	t.Helper()
	writeText(t, filepath.Join(folder, name), "test")
}

// TestMissingEmptyAndNonDirectoryAreEmpty
func TestMissingEmptyAndNonDirectoryAreEmpty(t *testing.T) {
	t.Parallel()
	folder := filepath.Join(t.TempDir(), "artifacts")
	if got := ArtifactsList(folder); len(got) != 0 {
		t.Fatalf("missing folder lists %v", got)
	}
	if err := os.MkdirAll(folder, 0o777); err != nil {
		t.Fatal(err)
	}
	if got := ArtifactsList(folder); len(got) != 0 {
		t.Fatalf("empty folder lists %v", got)
	}
	writeArtifact(t, folder, "file")
	if got := ArtifactsList(filepath.Join(folder, "file")); len(got) != 0 {
		t.Fatalf("a file lists %v", got)
	}
}

// TestFiltersSortsAndRevalidates
func TestFiltersSortsAndRevalidates(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	folder := filepath.Join(base, "folder")
	outside := filepath.Join(base, "outside")
	for _, name := range []string{"z.html", "a.html", "B.HTML", "notes.txt"} {
		writeArtifact(t, folder, name)
	}
	writeArtifact(t, outside, "outside.html")
	writeArtifact(t, filepath.Join(folder, "sub"), "inner.html")
	mustMkdir(t, filepath.Join(folder, "directory.html"))
	mustSymlink(t, filepath.Join(outside, "outside.html"), filepath.Join(folder, "link.html"))
	mustSymlink(t, filepath.Join(folder, "z.html"), filepath.Join(folder, "local.html"))
	if got := artifactNames(ArtifactsList(folder)); !reflect.DeepEqual(got, []string{"a.html", "B.HTML", "z.html"}) {
		t.Fatalf("list %v", got)
	}
	names := []string{"a.html", "A.HTML", "z.html"}
	sort.Slice(names, func(i, j int) bool { return artifactPrecedes(names[i], names[j]) })
	if !reflect.DeepEqual(names, []string{"A.HTML", "a.html", "z.html"}) {
		t.Fatalf("tie order %v", names)
	}
	resolved, ok := ArtifactsResolve("z.html", folder)
	wantResolved, _ := filepath.EvalSymlinks(filepath.Join(folder, "z.html"))
	if !ok || resolved != wantResolved {
		t.Fatalf("resolve z.html: %q, %v", resolved, ok)
	}
	for _, name := range []string{"", "../outside.html", filepath.Join(outside, "outside.html"), "sub/inner.html", "notes.txt", "directory.html", "link.html", "local.html"} {
		if _, ok := ArtifactsResolve(name, folder); ok {
			t.Fatalf("resolved %q", name)
		}
	}
	writeArtifact(t, folder, "unreadable.html")
	if err := os.Chmod(filepath.Join(folder, "unreadable.html"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(folder, "unreadable.html"), 0o644) })
	if _, ok := ArtifactsResolve("unreadable.html", folder); ok {
		t.Fatal("an unreadable artifact resolved")
	}
	if strings.Contains(strings.Join(artifactNames(ArtifactsList(folder)), ","), "unreadable.html") {
		t.Fatal("an unreadable artifact is listed")
	}
	mustRemove(t, filepath.Join(folder, "z.html"))
	if _, ok := ArtifactsResolve("z.html", folder); ok {
		t.Fatal("a removed artifact resolved")
	}
	mustRemove(t, filepath.Join(folder, "a.html"))
	mustSymlink(t, filepath.Join(outside, "outside.html"), filepath.Join(folder, "a.html"))
	if _, ok := ArtifactsResolve("a.html", folder); ok {
		t.Fatal("a link resolved")
	}
	redirected := filepath.Join(base, "redirected")
	mustSymlink(t, outside, redirected)
	if got := ArtifactsList(redirected); len(got) != 0 {
		t.Fatalf("a redirected folder lists %v", got)
	}
	if _, ok := ArtifactsResolve("outside.html", redirected); ok {
		t.Fatal("a file in a redirected folder resolved")
	}
}

// TestConfinedReadRejectsSymlinkedFilesAndEveryAttemptParent
func TestConfinedReadRejectsSymlinkedFilesAndEveryAttemptParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	artifacts := filepath.Join(root, "runs", "run", "attempt", "artifacts")
	writeArtifact(t, artifacts, "page.html")
	writeArtifact(t, outside, "outside.html")
	if got, ok := ArtifactsReadConfined("page.html", artifacts, root); !ok || string(got) != "test" {
		t.Fatalf("read page: %q, %v", got, ok)
	}
	if _, ok := ArtifactsResolveConfined("page.html", artifacts, root); !ok {
		t.Fatal("a plain page did not resolve")
	}

	mustRemove(t, filepath.Join(artifacts, "page.html"))
	mustSymlink(t, filepath.Join(outside, "outside.html"), filepath.Join(artifacts, "page.html"))
	if _, ok := ArtifactsReadConfined("page.html", artifacts, root); ok {
		t.Fatal("a symlinked page was read")
	}
	if _, ok := ArtifactsResolveConfined("page.html", artifacts, root); ok {
		t.Fatal("a symlinked page resolved")
	}

	mustRemoveAll(t, artifacts)
	mustSymlink(t, outside, artifacts)
	if _, ok := ArtifactsReadConfined("outside.html", artifacts, root); ok {
		t.Fatal("a file behind a symlinked folder was read")
	}

	mustRemoveAll(t, artifacts)
	mustMkdir(t, artifacts)
	attempt := filepath.Dir(artifacts)
	detached := filepath.Join(root, "detached")
	if err := os.Rename(attempt, detached); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, detached, attempt)
	if _, ok := ArtifactsReadConfined("page.html", artifacts, root); ok {
		t.Fatal("a page behind a symlinked attempt was read")
	}

	realRun := filepath.Join(root, "real-run")
	realArtifacts := filepath.Join(realRun, "attempt", "artifacts")
	writeArtifact(t, realArtifacts, "page.html")
	runFolder := filepath.Join(root, "runs", "run")
	mustRemoveAll(t, runFolder)
	mustSymlink(t, realRun, runFolder)
	if got, ok := ArtifactsReadConfined("page.html", realArtifacts, root); !ok || string(got) != "test" {
		t.Fatalf("read through the real path: %q, %v", got, ok)
	}
	throughLink := filepath.Join(runFolder, "attempt", "artifacts")
	if _, ok := ArtifactsReadConfined("page.html", throughLink, root); ok {
		t.Fatal("a page read through a symlinked run folder")
	}
	if _, ok := ArtifactsResolveConfined("page.html", throughLink, root); ok {
		t.Fatal("a page resolved through a symlinked run folder")
	}
}

// TestOversizedArtifactsAreReportedNotRead
func TestOversizedArtifactsAreReportedNotRead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	folder := filepath.Join(root, "runs", "run", "attempt", "artifacts")
	writeArtifact(t, folder, "page.html")
	// "test" is four bytes: a cap of four still reads it, a cap of three reports its size.
	if got := ArtifactsReadConfinedResult("page.html", folder, root, 4); got.Kind != ArtifactReadBytes || string(got.Bytes) != "test" {
		t.Fatalf("cap 4: %+v", got)
	}
	if got := ArtifactsReadConfinedResult("page.html", folder, root, 3); got.Kind != ArtifactReadTooLarge || got.Size != 4 {
		t.Fatalf("cap 3: %+v", got)
	}
	if got := ArtifactsReadConfinedResult("absent.html", folder, root, ArtifactMaxBytes); got.Kind != ArtifactReadMissing {
		t.Fatalf("absent: %+v", got)
	}
}

// TestConfinedListingSortsAndLeavesOutLinks checks the confined listing reports
// sizes of regular HTML files only.
func TestConfinedListingSortsAndLeavesOutLinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	folder := filepath.Join(root, "attempt", "artifacts")
	writeArtifact(t, folder, "b.html")
	writeArtifact(t, folder, "A.html")
	writeArtifact(t, folder, "notes.txt")
	mustSymlink(t, filepath.Join(folder, "b.html"), filepath.Join(folder, "c.html"))
	got := ArtifactsListConfined(folder, root)
	if len(got) != 2 || got[0].Name != "A.html" || got[1].Name != "b.html" || got[0].SizeBytes != 4 {
		t.Fatalf("listing %+v", got)
	}
}

// mustMkdir creates a directory, failing the test on error.
func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o777); err != nil {
		t.Fatal(err)
	}
}

// mustRemove deletes a file, failing the test on error.
func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// mustRemoveAll deletes a tree, failing the test on error.
func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

// mustSymlink links name to target, failing the test on error.
func mustSymlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

// ArtifactsReadConfined reads one immediate artifact child, or reports false.
func ArtifactsReadConfined(name, folder, trustedRoot string) ([]byte, bool) {
	result := ArtifactsReadConfinedResult(name, folder, trustedRoot, ArtifactMaxBytes)
	if result.Kind == ArtifactReadBytes {
		return result.Bytes, true
	}
	return nil, false
}
