package store

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestPendingErasureSavesLoadsAndDeletesOnce checks an intent is listed and
// found while it exists, and a second delete is already finished.
func TestPendingErasureSavesLoadsAndDeletesOnce(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	pending := PendingErasure{ID: "acme/web:7", Generation: 2, Folder: "issues/acme/web/7", Repos: []string{"acme/web"}, Sessions: []string{"session-a"}}
	if err := s.SavePendingErasure(pending); err != nil {
		t.Fatal(err)
	}
	if got := s.LoadPendingErasures(); !reflect.DeepEqual(got, []PendingErasure{pending}) {
		t.Fatalf("listed %+v", got)
	}
	if got, ok := s.PendingErasure(pending.ID); !ok || !reflect.DeepEqual(got, pending) {
		t.Fatalf("found %+v, %v", got, ok)
	}
	if err := s.DeletePendingErasure(pending.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.PendingErasure(pending.ID); ok {
		t.Fatal("a deleted intent is still found")
	}
	if err := s.DeletePendingErasure(pending.ID); err != nil {
		t.Fatalf("a second delete: %v", err)
	}
}

// TestPendingErasureFileNameIsItsIDInBase64 checks the intent's file is named
// by its id, with "/" and "=" replaced so it is one path element.
func TestPendingErasureFileNameIsItsIDInBase64(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	id := "a/b?"
	name := base64.StdEncoding.EncodeToString([]byte(id))
	want := filepath.Join(s.Paths.Erasures(), "YS9iPw--.json")
	if name != "YS9iPw==" || s.erasureFile(id) != want {
		t.Fatalf("file %s, base64 %s", s.erasureFile(id), name)
	}
}

// TestGenerationsKeepTheHighestErasedGeneration checks the next generation
// follows the highest erased one, and a lower record does not lower it.
func TestGenerationsKeepTheHighestErasedGeneration(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	if got := s.NextGeneration("run"); got != 0 {
		t.Fatalf("first generation %d", got)
	}
	if err := s.RecordErased("run", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordErased("run", 3); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordErased("run", 1); err != nil {
		t.Fatal(err)
	}
	if got := s.NextGeneration("run"); got != 4 {
		t.Fatalf("next generation %d", got)
	}
	if got := s.NextGeneration("other"); got != 0 {
		t.Fatalf("another run's generation %d", got)
	}
}

// TestDeleteRunFolderIsIdempotent checks a folder already gone is success, and
// a present one is removed with its contents.
func TestDeleteRunFolderIsIdempotent(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	if err := s.DeleteRunFolder("missing/run"); err != nil {
		t.Fatalf("a missing folder: %v", err)
	}
	writeText(t, filepath.Join(s.Paths.Runs(), "acme", "web", "1", "run.json"), "{}")
	if err := s.DeleteRunFolder("acme/web/1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Paths.Runs(), "acme", "web", "1")); err == nil {
		t.Fatal("the run folder is still there")
	}
}
