package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// summaryRun is RunSummaryTests' run(_:state:stage:updatedAt:): a manual run of one stage.
func summaryRun(id string, state RunState, stageType StageType, updatedAt time.Time) Run {
	stage := NewStage(StageArgs{ID: "stage", Type: stageType, Outcomes: []Outcome{}})
	run := NewRun(RunArgs{
		Belt:    testKindBelt(TriggerKindManual, stage),
		Subject: Subject{Kind: SubjectKindManual, Key: id, Title: id, IsOpen: true},
		Now:     updatedAt,
	})
	run.ID = id
	run.State = state
	run.StageID = "stage"
	run.UpdatedAt = jsonx.Date{Time: updatedAt}
	return run
}

// TestAppAndStateCountsShareRunSemanticsAndUseTheSuppliedLocalDay
func TestAppAndStateCountsShareRunSemanticsAndUseTheSuppliedLocalDay(t *testing.T) {
	t.Parallel()
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339, "2026-10-07T00:30:00+02:00")
	if err != nil {
		t.Fatal(err)
	}
	todayStart := time.Date(2026, time.October, 7, 0, 0, 0, 0, amsterdam)

	runs := []Run{
		summaryRun("watch-running", RunStateRunning, StageTypeWatch, now),
		summaryRun("watch-queued", RunStateQueued, StageTypeWatch, now),
		summaryRun("watch-waiting", RunStateWaiting, StageTypeWatch, now),
		summaryRun("watch-held", RunStateHeld, StageTypeWatch, now),
		summaryRun("ordinary-running", RunStateRunning, StageTypeAgent, now),
		summaryRun("parked", RunStateParked, StageTypeAgent, now),
		summaryRun("shipped", RunStateShipped, StageTypeAgent, now),
		summaryRun("yesterday", RunStateShipped, StageTypeAgent, todayStart.Add(-time.Second)),
		summaryRun("dropped", RunStateDropped, StageTypeAgent, now),
		summaryRun("done-today", RunStateDone, StageTypeAgent, now),
	}
	// A Done run is finished and counted in the total, but it is not shipped today.
	want := RunCounts{Total: 10, Active: 4, Watching: 2, Running: 1, NeedsYou: 2, ShippedToday: 1}
	if got := RunSummaryCounts(runs, now, amsterdam); got != want {
		t.Fatalf("counts = %+v, want %+v", got, want)
	}
}
