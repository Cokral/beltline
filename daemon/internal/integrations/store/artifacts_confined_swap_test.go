package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAParentSwappedForALinkMidWalkStaysInsideTheRoot swaps an already opened
// parent for a link to a folder outside the root while the walk holds it. The
// walk goes on from the descriptor it opened, so it reads the file that was
// inside, never the one the link points at. Not parallel: it sets the walk hook.
func TestAParentSwappedForALinkMidWalkStaysInsideTheRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(root, "runs", "attempt", "artifacts"), filepath.Join(outside, "attempt", "artifacts")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "runs", "attempt", "artifacts", "page.html"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "attempt", "artifacts", "page.html"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "runs", "attempt", "artifacts")

	swap := func(depth int) {
		if depth != 0 {
			return
		}
		runs := filepath.Join(root, "runs")
		if err := os.Rename(runs, filepath.Join(root, "runs-moved")); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(outside, runs); err != nil {
			t.Error(err)
		}
	}
	restore := func() {
		_ = os.Remove(filepath.Join(root, "runs"))
		_ = os.Rename(filepath.Join(root, "runs-moved"), filepath.Join(root, "runs"))
	}
	t.Cleanup(func() { confinedWalkOpened = nil })

	confinedWalkOpened = swap
	read := ArtifactsReadConfinedResult("page.html", folder, root, ArtifactMaxBytes)
	if read.Kind != ArtifactReadBytes || string(read.Bytes) != "inside" {
		t.Fatalf("read %v %q, want the file inside the root", read.Kind, read.Bytes)
	}
	restore()

	confinedWalkOpened = swap
	listed := ArtifactsListConfined(folder, root)
	if len(listed) != 1 || listed[0].SizeBytes != int64(len("inside")) {
		t.Fatalf("listed %v, want the one file inside the root", listed)
	}
	restore()

	// With the link in place before the walk, nothing below it is reached.
	confinedWalkOpened = nil
	swap(0)
	t.Cleanup(restore)
	if read := ArtifactsReadConfinedResult("page.html", folder, root, ArtifactMaxBytes); read.Kind != ArtifactReadMissing {
		t.Fatalf("read through a linked parent: %v %q", read.Kind, read.Bytes)
	}
	if _, ok := ArtifactsResolveConfined("page.html", folder, root); ok {
		t.Fatal("resolved through a linked parent")
	}
}
