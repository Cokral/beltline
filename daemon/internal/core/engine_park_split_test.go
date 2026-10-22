// Engine tests of parking a split run, and of the watch and ticket clocks.

package core

import (
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
	"testing"
	"time"
)

// TestParkingDoesNotConsumeWatchGraceOrTicketQuietTime
func TestParkingDoesNotConsumeWatchGraceOrTicketQuietTime(t *testing.T) {
	t.Parallel()
	watch := nfDefaultRun()
	nfToWatch(&watch)
	started := watch.Attempts[len(watch.Attempts)-1].StartedAt.Time
	watch.FirstPullRequestAt = &jsonx.Date{Time: started.Add(20 * time.Second)}
	remainingGrace := float64(nfParkWatchGrace) - 20
	nfParked(&watch, started.Add(20*time.Second))
	EngineAdvance(&watch, Event{Kind: EventKindWatched, Watch: WatchEventMerged, Detail: new("#1")}, started.Add(time.Duration(20+nfParkWatchGrace)*time.Second))
	if !watch.IsButtonParked() {
		t.Errorf("not button parked")
	}
	if last := watch.Attempts[len(watch.Attempts)-1]; last.Outcome != nil {
		t.Errorf("outcome = %v, want none", *last.Outcome)
	}
	nfUnpark(&watch, started.Add(time.Duration(20+nfParkWatchGrace)*time.Second))
	shifted := watch.Attempts[len(watch.Attempts)-1].StartedAt.Time
	if !shifted.Equal(started.Add(time.Duration(nfParkWatchGrace) * time.Second)) {
		t.Errorf("shifted start = %v, want grace after %v", shifted, started)
	}
	if watch.FirstPullRequestAt == nil || !watch.FirstPullRequestAt.Equal(started.Add(time.Duration(20+nfParkWatchGrace)*time.Second)) {
		t.Errorf("first pull request = %v", watch.FirstPullRequestAt)
	}
	leftAfter := float64(nfParkWatchGrace) - watch.FirstPullRequestAt.Time.Sub(shifted).Seconds()
	if leftAfter != remainingGrace {
		t.Errorf("grace left = %v, want %v", leftAfter, remainingGrace)
	}

	ticket := NewRun(RunArgs{Belt: nfJiraBelt([]Stage{nfTicketWatch(), DefaultsStage(StageTypeApprove, nil, nil, nil)}), Subject: nfEpicSubject(), Now: engineT0})
	EngineStart(&ticket, engineT0)
	ticket.TicketObservations["watch"] = TicketObservation{Status: "Open", PendingSince: &jsonx.Date{Time: engineT0}}
	quietLeft := TicketWatchQuiet - 30
	nfParked(&ticket, engineT0.Add(30*time.Second))
	nfUnpark(&ticket, engineT0.Add(time.Duration(30+TicketWatchQuiet)*time.Second))
	pending := ticket.TicketObservations["watch"].PendingSince
	unparkedAt := engineT0.Add(time.Duration(30+TicketWatchQuiet) * time.Second)
	if pending == nil || !pending.Equal(engineT0.Add(time.Duration(TicketWatchQuiet)*time.Second)) {
		t.Errorf("pending since = %v, want quiet after start", pending)
	}
	if pending != nil && unparkedAt.Sub(pending.Time) != 30*time.Second {
		t.Errorf("elapsed = %v, want 30s", unparkedAt.Sub(pending.Time))
	}
	if TicketWatchQuiet-30 != quietLeft {
		t.Errorf("quiet left = %v, want %v", TicketWatchQuiet-30, quietLeft)
	}
}

// TestParkingASplitStopsEveryUnfinishedRouteAndUnparkRestoresEach
func TestParkingASplitStopsEveryUnfinishedRouteAndUnparkRestoresEach(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutes())
	nfFinish(&run, "ready to implement")
	EngineAdvance(&run, nfStarted("c", "f", new("r:complex")), engineT0)
	simple := nfLegOn(run, "simple")
	simpleState, simpleStage := simple.State, nfStageOf(simple)
	effects := nfParked(&run, engineT0.Add(time.Second))
	if nfStateOf(nfLegOn(run, "complex")) != RunStateParked {
		t.Errorf("complex state = %q, want parked", nfStateOf(nfLegOn(run, "complex")))
	}
	if nfStateOf(nfLegOn(run, "simple")) != RunStateParked {
		t.Errorf("simple state = %q, want parked", nfStateOf(nfLegOn(run, "simple")))
	}
	if run.State != RunStateParked {
		t.Errorf("state = %q, want parked", run.State)
	}
	if !nfContainsEffect(effects, Effect{Kind: EffectKindStopForPark, Session: "c"}) {
		t.Errorf("effects = %+v, want stopForPark c", effects)
	}
	if run.StageID == "verify" {
		t.Errorf("stage = verify, want the run to wait")
	}
	EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: new("Outcome: complete"), Leg: new("r:complex")}, engineT0.Add(2*time.Second))
	if nfStateOf(nfLegOn(run, "complex")) != RunStateParked {
		t.Errorf("complex state = %q, want parked", nfStateOf(nfLegOn(run, "complex")))
	}
	if run.StageID == "verify" {
		t.Errorf("stage = verify, want the run to wait")
	}
	nfParkStopped(&run, "c", nil, engineT0.Add(2*time.Second))
	nfUnpark(&run, engineT0.Add(3*time.Second))
	complex := nfLegOn(run, "complex")
	if complex.State != RunStateQueued {
		t.Errorf("complex state = %q, want queued", complex.State)
	}
	if complex.Replays == nil {
		t.Errorf("complex replays = nil, want the stopped attempt")
	}
	if nfLegOn(run, "simple").State != simpleState {
		t.Errorf("simple state = %q, want %q", nfLegOn(run, "simple").State, simpleState)
	}
	if nfStageOf(nfLegOn(run, "simple")) != simpleStage {
		t.Errorf("simple stage = %q, want %q", nfStageOf(nfLegOn(run, "simple")), simpleStage)
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
	if run.StageID == "verify" {
		t.Errorf("stage = verify, want the run to wait")
	}
}

// TestAnOutcomeParkedRouteStaysParkedWhenTheRestOfTheRunIsParked
func TestAnOutcomeParkedRouteStaysParkedWhenTheRestOfTheRunIsParked(t *testing.T) {
	t.Parallel()
	run := engineStartRun(nfBothRoutes())
	nfFinish(&run, "ready to implement")
	spec := nfIndexOfStage(t, run.Stages, "spec")
	run.Stages[spec].Outcomes[0].Then = NewThen(ThenArgs{Action: ThenActionPark})
	EngineAdvance(&run, nfStarted("c", "f", new("r:complex")), engineT0)
	EngineAdvance(&run, nfEnded("Outcome: complete", new("r:complex")), engineT0)
	if nfStateOf(nfLegOn(run, "complex")) != RunStateParked {
		t.Errorf("complex state = %q, want parked", nfStateOf(nfLegOn(run, "complex")))
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
	if !run.CanButtonPark() {
		t.Errorf("canButtonPark = false, want true")
	}
	EngineAdvance(&run, nfStarted("s", "f", new("r:simple")), engineT0)
	nfParked(&run, engineT0)
	if nfStateOf(nfLegOn(run, "complex")) != RunStateParked {
		t.Errorf("complex state = %q, want parked", nfStateOf(nfLegOn(run, "complex")))
	}
	if reason := nfLegOn(run, "complex").Reason; reason == nil || *reason != "complete" {
		t.Errorf("complex reason = %v, want complete", reason)
	}
	if run.Parking == nil || len(run.Parking.Routes) != 1 || run.Parking.Routes[0].Leg != "r:simple" {
		t.Errorf("parking routes = %+v, want r:simple", run.Parking)
	}
	nfParkStopped(&run, "s", nil, engineT0)
	nfUnpark(&run, engineT0.Add(time.Second))
	if nfStateOf(nfLegOn(run, "complex")) != RunStateParked {
		t.Errorf("complex state = %q, want parked", nfStateOf(nfLegOn(run, "complex")))
	}
	if reason := nfLegOn(run, "complex").Reason; reason == nil || *reason != "complete" {
		t.Errorf("complex reason = %v, want complete", reason)
	}
	if nfStateOf(nfLegOn(run, "simple")) != RunStateQueued {
		t.Errorf("simple state = %q, want queued", nfStateOf(nfLegOn(run, "simple")))
	}
	if run.Parking != nil {
		t.Errorf("parking = %+v, want none", run.Parking)
	}
}
