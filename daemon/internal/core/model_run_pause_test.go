package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// testPauseAt parses an RFC 3339 time for the pause fixtures.
func testPauseAt(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// TestCumulativeSpendReplacesAndADeltaAddsOnceAndAMissingReadingStaysUnpriced
// a cumulative reading replaces the spend, a delta
// adds once, and a missing reading stays unpriced.
func TestCumulativeSpendReplacesAndADeltaAddsOnceAndAMissingReadingStaysUnpriced(t *testing.T) {
	t.Parallel()
	first := NewUsage(UsageArgs{CostNanos: new(10), Input: new(1), TotalTokens: new(1)})
	second := NewUsage(UsageArgs{CostNanos: new(25), Input: new(3), TotalTokens: new(4)})
	if got := PauseSpendMerge(&first, &second, PauseSpendKindCumulative); testDiffEmpty(&second, got) != "" {
		t.Errorf("cumulative = %+v, want the second reading", got)
	}
	if got := PauseSpendMerge(&first, &second, PauseSpendKindDelta); testDiffEmpty(func() *Usage { s := UsageAdd(first, second); return &s }(), got) != "" {
		t.Errorf("delta = %+v, want first + second", got)
	}
	if got := PauseSpendMerge(&first, nil, PauseSpendKindCumulative); testDiffEmpty(&first, got) != "" {
		t.Errorf("missing reading = %+v, want the first", got)
	}
	if got := PauseSpendMerge(nil, nil, PauseSpendKindDelta); got != nil {
		t.Errorf("nothing = %+v, want nil", got)
	}
	if got := PauseSpendMerge(nil, &second, PauseSpendKindDelta); testDiffEmpty(&second, got) != "" {
		t.Errorf("delta onto nothing = %+v, want the reading", got)
	}
}

// TestDeferredMergeReadsOnlyTheZeroPayload checks that a payload is read at
// its _0 key, and that any other key in it is ignored.
func TestDeferredMergeReadsOnlyTheZeroPayload(t *testing.T) {
	t.Parallel()
	var d RunPauseDeferred
	testDecode(t, `{"conflict":{"_0":["beltline/a"],"x":[1,null]}}`, &d)
	if d.Kind != RunPauseDeferredConflict || len(d.Values) != 1 || d.Values[0] != "beltline/a" {
		t.Fatalf("decoded %+v", d)
	}
}

// TestBlocksProgressInEveryPhase checks that every pause phase blocks progress.
func TestBlocksProgressInEveryPhase(t *testing.T) {
	t.Parallel()
	for _, phase := range AllRunPausePhase {
		if !(RunPause{Phase: phase}).BlocksProgress() {
			t.Errorf("%s does not block progress", phase)
		}
	}
}

// TestRunCanPauseByStateAndPhase checks that an active run can pause unless it
// is already paused, and that only an incomplete pause can be tried again.
func TestRunCanPauseByStateAndPhase(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateQueued, RunStateRunning, RunStateWaiting, RunStateHeld, RunStateParked} {
		if !RunCanPause(Run{State: state}) {
			t.Errorf("%s cannot pause", state)
		}
	}
	for _, state := range []RunState{RunStateShipped, RunStateDropped} {
		if RunCanPause(Run{State: state}) {
			t.Errorf("%s can pause", state)
		}
	}
	paused := Run{State: RunStateRunning, Pause: &RunPause{Phase: RunPausePhasePaused}}
	if RunCanPause(paused) {
		t.Error("a paused run can pause again")
	}
	paused.Pause.Phase = RunPausePhaseIncomplete
	if !RunCanPause(paused) {
		t.Error("an incomplete pause cannot be tried again")
	}
}

// TestRunPauseExcludedCountsClosedAndOpenPauses checks the pause time between
// two instants: closed intervals clipped to the window, and an open pause.
func TestRunPauseExcludedCountsClosedAndOpenPauses(t *testing.T) {
	t.Parallel()
	since := testPauseAt(t, "2026-07-22T02:00:00Z")
	now := testPauseAt(t, "2026-07-22T02:30:00Z")
	closed := Run{PauseTime: []RunPauseInterval{
		{Start: jsonx.Date{Time: testPauseAt(t, "2026-07-22T01:50:00Z")}, End: jsonx.Date{Time: testPauseAt(t, "2026-07-22T02:10:00Z")}},
		{Start: jsonx.Date{Time: testPauseAt(t, "2026-07-22T02:20:00Z")}, End: jsonx.Date{Time: testPauseAt(t, "2026-07-22T02:21:00Z")}},
	}}
	if got := RunPauseExcluded(closed, since, now); got != 11*60 {
		t.Errorf("closed pauses = %v seconds, want 660", got)
	}
	open := Run{Pause: &RunPause{Phase: RunPausePhasePaused, RequestedAt: jsonx.Date{Time: testPauseAt(t, "2026-07-22T02:25:00Z")}}}
	if got := RunPauseExcluded(open, since, now); got != 5*60 {
		t.Errorf("open pause = %v seconds, want 300", got)
	}
	resuming := open
	resuming.Pause = &RunPause{Phase: RunPausePhaseResuming, RequestedAt: jsonx.Date{Time: testPauseAt(t, "2026-07-22T02:25:00Z")}}
	if got := RunPauseExcluded(resuming, since, now); got != 0 {
		t.Errorf("a resuming pause counts %v seconds, want 0: it is closed into PauseTime", got)
	}
}

// TestOlderRunJSONLoadsWithoutPauseRecords checks that a run saved before pausing
// and before interruptions reads with no pause, no pause time and no interruption.
// The model-only half of RunPauseStorageTests.pauseRecordsRoundTripAndOlderJsonStillLoads.
func TestOlderRunJSONLoadsWithoutPauseRecords(t *testing.T) {
	t.Parallel()
	obj := testRunObject()
	obj["attempts"] = []any{map[string]any{
		"id": "build-1", "stageId": "build", "number": 1, "startedAt": "2026-07-22T02:18:00Z", "folder": "02-build-1",
	}}
	run := testRunFrom(t, obj)
	if run.Pause != nil || len(run.PauseTime) != 0 || run.Attempts[0].Interruption != nil || run.Attempts[0].ReadOnlyBaseline != nil {
		t.Errorf("older run = pause %+v pauseTime %v attempt %+v", run.Pause, run.PauseTime, run.Attempts[0])
	}
}
