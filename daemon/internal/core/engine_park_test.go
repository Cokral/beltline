package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// parkTestNow is the instant the park tests use; parkTestLater is a moment after it.
var (
	parkTestNow   = time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC)
	parkTestLater = parkTestNow.Add(10 * time.Second)
)

// parkTestRun is a run with the given parking, and no legs.
func parkTestRun(parking *Parking) Run {
	return Run{ID: "run", StageID: "triage", State: RunStateQueued, Parking: parking}
}

// TestEngineBlocksWhileParkedLetsOnlyParkEventsThrough: while the Run page button
// has the run parked, only the events listed in the park rules may still apply.
func TestEngineBlocksWhileParkedLetsOnlyParkEventsThrough(t *testing.T) {
	t.Parallel()
	allowed := []EventKind{
		EventKindParkRun, EventKindUnparkRun, EventKindParkStopped, EventKindDrop,
		EventKindSubjectGone, EventKindSubjectChanged, EventKindResume,
		EventKindMergedLegs, EventKindMergeConflict,
	}
	blocked := []EventKind{
		EventKindSessionFinished, EventKindSessionStarted, EventKindDecided, EventKindWatched,
		EventKindAnswered, EventKindAbort, EventKindOverrule, EventKindPause,
	}
	for _, kind := range allowed {
		if engineBlocksWhileParked(Event{Kind: kind}) {
			t.Errorf("%s blocked while parked, want it to apply", kind)
		}
	}
	for _, kind := range blocked {
		if !engineBlocksWhileParked(Event{Kind: kind}) {
			t.Errorf("%s allowed while parked, want it ignored", kind)
		}
	}
}

// TestEngineUnparkRunStopsEveryParkedSessionAfterAFailedStop: a stop that failed is
// retried on Unpark, one stop per session the parked routes had, and nothing restarts
// until those stops succeed.
func TestEngineUnparkRunStopsEveryParkedSessionAfterAFailedStop(t *testing.T) {
	t.Parallel()
	run := parkTestRun(&Parking{
		Routes: []ParkingRoute{
			{Leg: "r:quick", Session: new("bl-q")},
			{Leg: "r:thorough"},
			{Leg: "r:build", Session: new("bl-b")},
		},
		Stopping: []string{}, StopError: new("tmux failed"), At: jsonx.Date{Time: parkTestNow},
	})
	effects := engineUnparkRun(&run, parkTestLater)
	if len(effects) != 2 || effects[0].Session != "bl-q" || effects[1].Session != "bl-b" {
		t.Fatalf("effects = %+v, want stopForPark for bl-q and bl-b", effects)
	}
	for _, effect := range effects {
		if effect.Kind != EffectKindStopForPark {
			t.Fatalf("effect kind = %s, want stopForPark", effect.Kind)
		}
	}
	if run.Parking == nil || run.Parking.StopError != nil || !run.Parking.UnparkPending {
		t.Fatalf("parking = %+v, want the stop error cleared and unpark pending", run.Parking)
	}
	if got := run.Parking.Stopping; len(got) != 2 || got[0] != "bl-q" || got[1] != "bl-b" {
		t.Fatalf("stopping = %v, want both sessions", got)
	}
}

// TestEngineUnparkRunWaitsForAStopStillFinishing: Unpark while a stop is unfinished
// records the request and starts nothing; asking again changes nothing.
func TestEngineUnparkRunWaitsForAStopStillFinishing(t *testing.T) {
	t.Parallel()
	run := parkTestRun(&Parking{Routes: []ParkingRoute{{Leg: "r:quick"}}, Stopping: []string{"bl-q"}, At: jsonx.Date{Time: parkTestNow}})
	if effects := engineUnparkRun(&run, parkTestLater); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none while the stop finishes", effects)
	}
	if run.Parking == nil || !run.Parking.UnparkPending {
		t.Fatalf("parking = %+v, want unpark pending", run.Parking)
	}
	if effects := engineUnparkRun(&run, parkTestLater); len(effects) != 0 || run.Parking == nil {
		t.Fatalf("second unpark changed the wait: effects %+v, parking %+v", effects, run.Parking)
	}
}

// TestEngineParkStoppedFailureKeepsTheSessionAndWaits: a session that failed to stop
// stays in stopping, and the pending Unpark is withdrawn until it is asked again.
func TestEngineParkStoppedFailureKeepsTheSessionAndWaits(t *testing.T) {
	t.Parallel()
	run := parkTestRun(&Parking{Routes: []ParkingRoute{{Leg: "r:quick"}}, Stopping: []string{"bl-q"}, UnparkPending: true, At: jsonx.Date{Time: parkTestNow}})
	engineParkStopped(&run, "bl-q", new("still running"), parkTestLater)
	if run.Parking == nil || run.Parking.StopError == nil || *run.Parking.StopError != "still running" {
		t.Fatalf("parking = %+v, want the stop error kept", run.Parking)
	}
	if run.Parking.UnparkPending || len(run.Parking.Stopping) != 1 {
		t.Fatalf("parking = %+v, want unpark withdrawn and bl-q still stopping", run.Parking)
	}
}

// TestEngineParkStoppedLastSuccessCompletesAWaitingUnpark: the last stop to succeed
// finishes an Unpark that was waiting, and the run is no longer parked.
func TestEngineParkStoppedLastSuccessCompletesAWaitingUnpark(t *testing.T) {
	t.Parallel()
	run := parkTestRun(&Parking{Routes: []ParkingRoute{{Leg: "r:quick"}}, Stopping: []string{"bl-q"}, UnparkPending: true, At: jsonx.Date{Time: parkTestNow}})
	engineParkStopped(&run, "bl-x", nil, parkTestLater)
	if run.Parking == nil || len(run.Parking.Stopping) != 1 {
		t.Fatalf("a session that is not stopping changed the wait: %+v", run.Parking)
	}
	if effects := engineParkStopped(&run, "bl-q", nil, parkTestLater); len(effects) != 0 {
		t.Fatalf("effects = %+v, want none", effects)
	}
	if run.Parking != nil {
		t.Fatalf("parking = %+v, want the Unpark completed", run.Parking)
	}
}

// TestEngineShiftWatchMovesTheAttemptsAndTheTicketClock: parked time is added onto an
// open watch's start, its first pull request and a ticket watch's pending quiet time.
func TestEngineShiftWatchMovesTheAttemptsAndTheTicketClock(t *testing.T) {
	t.Parallel()
	run := parkTestRun(nil)
	run.Stages = []Stage{{ID: "watch", Type: StageTypeWatch, Name: "Watch", Watch: new(WatchTargetTicket)}}
	run.Attempts = []Attempt{{ID: "watch-1", StageID: "watch", Number: 1, StartedAt: jsonx.Date{Time: parkTestNow}}}
	run.FirstPullRequestAt = &jsonx.Date{Time: parkTestNow}
	run.TicketObservations = map[string]TicketObservation{"watch": {PendingSince: &jsonx.Date{Time: parkTestNow}}}
	route := ParkingRoute{Leg: "main", StageID: new("watch"), State: RunStateRunning}
	shifted := false
	engineShiftWatch(&run, route, 10, &shifted)
	if !shifted {
		t.Fatal("the first pull request is at the attempt's start, so the anchor must move")
	}
	if got := run.Attempts[0].StartedAt.Time; !got.Equal(parkTestLater) {
		t.Fatalf("attempt start = %s, want %s", got, parkTestLater)
	}
	if got := run.FirstPullRequestAt.Time; !got.Equal(parkTestLater) {
		t.Fatalf("first pull request = %s, want %s", got, parkTestLater)
	}
	if got := run.TicketObservations["watch"].PendingSince.Time; !got.Equal(parkTestLater) {
		t.Fatalf("ticket quiet period starts at %s, want %s", got, parkTestLater)
	}

	untouched := parkTestRun(nil)
	untouched.Attempts = []Attempt{{ID: "watch-1", StageID: "watch", StartedAt: jsonx.Date{Time: parkTestNow}}}
	engineShiftWatch(&untouched, route, 0, &shifted)
	if !untouched.Attempts[0].StartedAt.Equal(parkTestNow) {
		t.Fatal("a zero interval must not move the clocks")
	}
}
