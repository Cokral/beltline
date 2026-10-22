package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestWriteSavesStorageJSONAtomically checks the saved bytes are the storage
// form, and that no temporary file is left beside the result.
func TestWriteSavesStorageJSONAtomically(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	path := filepath.Join(s.Paths.Belts(), belt.ID, "v1.json")
	if err := s.Write(belt, path); err != nil {
		t.Fatalf("write: %v", err)
	}
	want, err := jsonx.MarshalStorage(belt)
	if err != nil {
		t.Fatalf("MarshalStorage: %v", err)
	}
	if got := fileText(t, path); got != string(want) {
		t.Fatalf("saved bytes differ from MarshalStorage: %s", testkit.Diff(string(want), got))
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "v1.json" {
		t.Fatalf("unexpected files beside the version: %v", entries)
	}
}

// TestWriteFileAtomicReplacesTheFileAndLeavesNoTemporary checks a second write
// replaces the first in full and leaves only the named file beside it.
func TestWriteFileAtomicReplacesTheFileAndLeavesNoTemporary(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	path := filepath.Join(folder, "agent.md")
	if err := WriteFileAtomic(path, []byte("first, longer text")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteFileAtomic(path, []byte("second")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if got := fileText(t, path); got != "second" {
		t.Fatalf("file text = %q, want %q", got, "second")
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "agent.md" {
		t.Fatalf("unexpected files beside the file: %v", entries)
	}
}

// TestWriteFaultFailsTheNamedFileOnce checks an armed fault fails one write of
// that file name, then clears itself.
func TestWriteFaultFailsTheNamedFileOnce(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	s.WriteFault.Arm("state.json")
	path := filepath.Join(s.Paths.Belts(), "tasks", "state.json")
	if err := s.Write(map[string]int{"a": 1}, path); err == nil {
		t.Fatal("the armed write succeeded")
	}
	if err := s.Write(map[string]int{"a": 1}, path); err != nil {
		t.Fatalf("the write after the fault failed: %v", err)
	}
}

// TestReadDecodesAStoredFile checks Read decodes what Write saved.
func TestReadDecodesAStoredFile(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	path := filepath.Join(t.TempDir(), "numbers.json")
	if err := s.Write(map[string]int{"b": 2, "a": 1}, path); err != nil {
		t.Fatal(err)
	}
	var got map[string]int
	if err := s.Read(path, &got); err != nil || got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("read %v, %v", got, err)
	}
}

// TestRunRoundTripsThroughItsFolder checks a run saved and loaded again is
// equal, and its file holds the storage bytes of the run.
func TestRunRoundTripsThroughItsFolder(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	run := goldenRun(t)
	if err := s.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	want, err := jsonx.MarshalStorage(run)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, filepath.Join(s.Paths.RunFolder(run), "run.json")); got != string(want) {
		t.Fatalf("run.json differs from MarshalStorage: %s", testkit.Diff(string(want), got))
	}
	loaded := s.LoadRuns()
	if len(loaded) != 1 {
		t.Fatalf("loaded %d runs", len(loaded))
	}
	if diff := testkit.Diff(run, loaded[0]); diff != "" {
		t.Fatalf("loaded run differs:\n%s", diff)
	}
}

// TestSaveRunRefusesARunThatIsBeingErased checks a pending erasure of the run
// or of an older generation stops the save.
func TestSaveRunRefusesARunThatIsBeingErased(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	run := goldenRun(t)
	if err := s.SavePendingErasure(PendingErasure{ID: run.ID, Generation: run.Generation, Folder: run.Folder()}); err != nil {
		t.Fatal(err)
	}
	err := s.SaveRun(run)
	if f, ok := err.(Failure); !ok || f.Message != "This run is being erased and was not saved. Try again once erasure finishes." {
		t.Fatalf("SaveRun error %v", err)
	}
}

// TestSaveRunRefusesAnErasedGeneration checks a run at or below an erased
// generation is refused, and a newer generation saves.
func TestSaveRunRefusesAnErasedGeneration(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	run := goldenRun(t)
	if err := s.RecordErased(run.ID, run.Generation); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRun(run); err == nil {
		t.Fatal("an erased generation was saved")
	}
	run.Generation++
	if err := s.SaveRun(run); err != nil {
		t.Fatalf("a newer generation was refused: %v", err)
	}
}

// TestLoadRunsSkipsFilesThatDoNotDecode checks a broken run.json is left out
// without failing the others.
func TestLoadRunsSkipsFilesThatDoNotDecode(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	writeText(t, filepath.Join(s.Paths.Runs(), "broken", "run.json"), "{not json")
	if runs := s.LoadRuns(); len(runs) != 0 {
		t.Fatalf("loaded %d runs from a broken file", len(runs))
	}
}

// TestAfkDecisionsAreEmptyWhenMissingOrBroken checks an absent or unreadable
// history loads as empty.
func TestAfkDecisionsAreEmptyWhenMissingOrBroken(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	if got := s.LoadAfkDecisions(); got == nil || len(got) != 0 {
		t.Fatalf("missing history loaded as %v", got)
	}
	writeText(t, s.Paths.AfkDecisions(), "[")
	if got := s.LoadAfkDecisions(); got == nil || len(got) != 0 {
		t.Fatalf("broken history loaded as %v", got)
	}
}
