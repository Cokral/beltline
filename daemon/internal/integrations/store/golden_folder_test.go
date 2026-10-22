package store

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// TestAGoldenDataFolderLoads builds a data folder from the server goldens'
// settings, belt and run, saves it, and loads every part again.
func TestAGoldenDataFolderLoads(t *testing.T) {
	t.Parallel()
	root := testkit.DataDir(t)
	s := NewStorage(Paths{Root: root})
	settings := goldenSettings(t)
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	belt := goldenBelt(t)
	if _, err := s.SaveNewVersion(belt); err != nil {
		t.Fatal(err)
	}
	run := goldenRun(t)
	if err := s.SaveRun(run); err != nil {
		t.Fatal(err)
	}

	reopened := NewStorage(Paths{Root: root})
	if got := reopened.LoadSettings(); len(got.AllowedModels["pi"]) != 4 || len(got.Pauses) != 1 || len(got.ModelCatalogs) != 5 {
		t.Fatalf("settings did not load their lists: %+v", got)
	}
	belts := reopened.LoadBelts()
	if len(belts) != 1 || belts[0].Belt.ID != belt.ID || belts[0].Belt.Name != belt.Name {
		t.Fatalf("belts %+v", belts)
	}
	runs := reopened.LoadRuns()
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs %d", len(runs))
	}
}
