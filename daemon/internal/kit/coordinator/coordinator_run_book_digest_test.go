package coordinator

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestRunsProjectionDigestsABookInParallelAsInOrder checks the runs projection of a book large
// enough for digestStale to spread its encoding against a fresh digest of every run, on the first
// projection and after one run changes.
func TestRunsProjectionDigestsABookInParallelAsInOrder(t *testing.T) {
	t.Parallel()
	var runs []core.Run
	for i := range 3 * parallelDigests {
		run := sessionSampleRun()
		run.ID = "run-" + strconv.Itoa(i)
		run.Subject.Title = "Run " + strconv.Itoa(i)
		runs = append(runs, run)
	}
	b := &RunBook{runs: runs}
	if got, want := b.encodeRuns(), runsDigest(b.runs, nil); !bytes.Equal(got, want) {
		t.Fatal("the first projection differs from a fresh digest of every run")
	}
	b.runs[7].Subject.Title = "Changed"
	b.touched(b.runs[7].ID)
	if got, want := b.encodeRuns(), runsDigest(b.runs, nil); !bytes.Equal(got, want) {
		t.Fatal("the projection after a change differs from a fresh digest of every run")
	}
}
