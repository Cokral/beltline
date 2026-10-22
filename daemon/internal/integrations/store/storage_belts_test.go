package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// manualBelt is a belt with one manual trigger and the given id, for records
// that do not need stages.
func manualBelt(id string) core.Belt {
	return core.Belt{ID: id, Name: id, Version: 1,
		Triggers: []core.TriggerEntry{{ID: core.TriggerEntryLegacyID, Trigger: core.Trigger{Kind: core.TriggerKindManual}}}}
}

// TestBeltRecordLastFiredAttachesToTheScheduleEntry checks the first schedule
// entry's cursor is the one LastFired reads and writes; a belt without one uses the legacy id.
func TestBeltRecordLastFiredAttachesToTheScheduleEntry(t *testing.T) {
	t.Parallel()
	belt := manualBelt("tasks")
	when := time.Unix(1_700_000_000, 0)
	record := NewBeltRecord(BeltRecordArgs{Belt: belt, LastFired: &when})
	if got := record.Fired[core.TriggerEntryLegacyID]; !got.Equal(when) {
		t.Fatalf("legacy cursor %v", record.Fired)
	}
	if got := record.LastFired(); got == nil || !got.Equal(when) {
		t.Fatalf("LastFired %v", got)
	}
	record.SetLastFired(nil)
	if record.LastFired() != nil || len(record.Fired) != 0 {
		t.Fatalf("cleared cursor left %v", record.Fired)
	}
}

// TestSaveNewVersionCountsFromOne checks each save takes the next number and
// earlier versions stay where they were.
func TestSaveNewVersionCountsFromOne(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	first, err := s.SaveNewVersion(belt)
	if err != nil || first.Version != 1 {
		t.Fatalf("first save %d, %v", first.Version, err)
	}
	second, err := s.SaveNewVersion(belt)
	if err != nil || second.Version != 2 {
		t.Fatalf("second save %d, %v", second.Version, err)
	}
	if got := s.Belt(belt.ID, 1); got == nil || got.Version != 1 {
		t.Fatalf("version 1 %v", got)
	}
	if got := versions(filepath.Join(s.Paths.Belts(), belt.ID), "json"); len(got) != 2 {
		t.Fatalf("versions on disk %v", got)
	}
}

// TestSaveVersionWritesThePauseBeforeTheVersion checks a failed version write
// leaves the pause on disk while the previous version stays the latest.
func TestSaveVersionWritesThePauseBeforeTheVersion(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	if _, err := s.SaveNewVersion(belt); err != nil {
		t.Fatal(err)
	}
	next := belt
	next.Name = "Edited"
	record := NewBeltRecord(BeltRecordArgs{Belt: belt, Paused: true})
	s.WriteFault.Arm("v2.json")
	if _, err := s.SaveVersion(next, record); err == nil {
		t.Fatal("the armed version write succeeded")
	}
	stored, ok := s.StoredState(belt.ID)
	if !ok || !stored.Paused {
		t.Fatalf("pause not on disk: %+v, %v", stored, ok)
	}
	loaded := s.LoadBelts()
	if len(loaded) != 1 || loaded[0].Belt.Name != belt.Name || !loaded[0].Paused {
		t.Fatalf("after the failed save: %+v", loaded)
	}
}

// TestSetPausedAndArchivedKeepEachOther checks each setter keeps the other
// flag and the cursors of the state.
func TestSetPausedAndArchivedKeepEachOther(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	when := time.Unix(1_700_000_000, 0)
	record := NewBeltRecord(BeltRecordArgs{Belt: belt, LastFired: &when, WatchOutcomes: true})
	if err := s.SaveState(record); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaused(belt.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetArchived(belt.ID, true); err != nil {
		t.Fatal(err)
	}
	stored, ok := s.StoredState(belt.ID)
	if !ok || !stored.Paused || !stored.Archived || !stored.WatchOutcomes {
		t.Fatalf("stored state %+v", stored)
	}
	if stored.LastFired == nil || !stored.LastFired.Equal(when) {
		t.Fatalf("cursor lost: %v", stored.LastFired)
	}
}

// TestSetPausedWithoutAStateFileStartsOne checks a belt with no state.json
// gets one, live but for the pause.
func TestSetPausedWithoutAStateFileStartsOne(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	if err := s.SetPaused("fresh", true); err != nil {
		t.Fatal(err)
	}
	stored, ok := s.StoredState("fresh")
	if !ok || !stored.Paused || stored.Archived || stored.WatchOutcomes || stored.ReviewOutcomes {
		t.Fatalf("new state %+v", stored)
	}
}

// TestLoadBeltsReadsStateAndLegacyLastFired checks a state file with only
// lastFired attaches it to the belt's schedule entry, and an old state without
// the watch flags reads them as off.
func TestLoadBeltsReadsStateAndLegacyLastFired(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	if _, err := s.SaveNewVersion(belt); err != nil {
		t.Fatal(err)
	}
	writeText(t, s.stateURL(belt.ID), `{"paused":true,"lastFired":"2026-10-07T06:00:00Z"}`)
	loaded := s.LoadBelts()
	if len(loaded) != 1 {
		t.Fatalf("loaded %d belts", len(loaded))
	}
	record := loaded[0]
	if !record.Paused || record.Archived || record.WatchOutcomes || record.ReviewOutcomes {
		t.Fatalf("flags %+v", record)
	}
	want := mustTime(t, "2026-10-07T06:00:00Z")
	if got := record.LastFired(); got == nil || !got.Equal(want) {
		t.Fatalf("lastFired %v", got)
	}
}

// TestLoadBeltsSkipsAFolderWithoutAReadableVersion checks a broken latest
// version leaves its belt out of the library.
func TestLoadBeltsSkipsAFolderWithoutAReadableVersion(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	writeText(t, filepath.Join(s.Paths.Belts(), "broken", "v1.json"), "{")
	if got := s.LoadBelts(); len(got) != 0 {
		t.Fatalf("loaded %d belts", len(got))
	}
}

// TestStateFileRoundTripsItsBytes checks the state file is the storage form of
// the record's state, with the cursors as ISO dates.
func TestStateFileRoundTripsItsBytes(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	belt := goldenBelt(t)
	when := time.Unix(1_700_000_000, 0).UTC()
	record := NewBeltRecord(BeltRecordArgs{Belt: belt, Paused: true, Fired: map[string]time.Time{"sched": when}, ReviewOutcomes: true})
	if err := s.SaveState(record); err != nil {
		t.Fatal(err)
	}
	want, err := jsonx.MarshalStorage(record.state())
	if err != nil {
		t.Fatal(err)
	}
	if got := fileText(t, s.stateURL(belt.ID)); got != string(want) {
		t.Fatalf("state bytes differ: %s", testkit.Diff(string(want), got))
	}
	stored, ok := s.StoredState(belt.ID)
	if !ok || !stored.Paused || !stored.ReviewOutcomes || !stored.Schedules["sched"].Equal(when) {
		t.Fatalf("stored %+v", stored)
	}
}
