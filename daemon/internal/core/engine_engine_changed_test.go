package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestEngineChangedTreatsAnUnsavableRunAsChanged: a run that cannot be saved
// (a date past year 9999) must not look like a no-op on either side.
func TestEngineChangedTreatsAnUnsavableRunAsChanged(t *testing.T) {
	t.Parallel()
	bad := Run{UpdatedAt: jsonx.Date{Time: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}
	before, beforeErr := engineSaved(bad)
	if beforeErr == nil {
		t.Fatal("a run dated past year 9999 saved without an error")
	}
	if !engineChanged(before, beforeErr, &bad) {
		t.Fatal("an unsavable run before the event was taken as unchanged")
	}
	good := Run{}
	saved, err := engineSaved(good)
	if err != nil {
		t.Fatal(err)
	}
	if !engineChanged(saved, nil, &bad) {
		t.Fatal("a run that became unsavable was taken as unchanged")
	}
}

// TestEngineChangedComparesTheSavedJSON: the same saved run is unchanged, a
// changed field is changed, and a change below one second is invisible, as it
// is in the saved file.
func TestEngineChangedComparesTheSavedJSON(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	run := Run{UpdatedAt: jsonx.Date{Time: at}}
	before, err := engineSaved(run)
	if err != nil {
		t.Fatal(err)
	}
	if engineChanged(before, nil, &run) {
		t.Fatal("an unchanged run was taken as changed")
	}
	run.UpdatedAt = jsonx.Date{Time: at.Add(500 * time.Millisecond)}
	if engineChanged(before, nil, &run) {
		t.Fatal("a sub-second change was taken as changed, but the saved file cannot hold it")
	}
	run.UpdatedAt = jsonx.Date{Time: at.Add(2 * time.Second)}
	if !engineChanged(before, nil, &run) {
		t.Fatal("a change of a whole second was taken as unchanged")
	}
}
