package store

import (
	"path/filepath"
	"testing"
)

// TestLoadRunsFindsRunJSONAtAnyDepthInWalkOrder pins what LoadRuns reads: a run.json below an
// attempt folder is a run too, and the runs come in the walk's lexical order, so a run folder's
// attempt folders (their run.json files) come before the run folder's own run.json. Leaving
// attempt folders out of the walk would change this.
func TestLoadRunsFindsRunJSONAtAnyDepthInWalkOrder(t *testing.T) {
	t.Parallel()
	s := newTestStorage(t)
	run := goldenRun(t)
	if err := s.SaveRun(run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	nested := run
	nested.ID = "nested-run"
	if err := s.Write(nested, filepath.Join(s.Paths.RunFolder(run), "01-triage-1", "run.json")); err != nil {
		t.Fatalf("write nested run: %v", err)
	}
	loaded := s.LoadRuns()
	if len(loaded) != 2 {
		t.Fatalf("loaded %d runs, want 2", len(loaded))
	}
	if loaded[0].ID != "nested-run" || loaded[1].ID != run.ID {
		t.Fatalf("loaded run order %q, %q; want the attempt's run.json first, then the run's", loaded[0].ID, loaded[1].ID)
	}
}
