// Send to… and Abort on routes that are doing work, under a personal, belt or global
// pause: the move stops the displaced session, and the destination waits for you.

package coordinator

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// trailIndex is the position of the first trail entry with prefix, or -1.
func trailIndex(trail []string, prefix string) int {
	return slices.IndexFunc(trail, func(entry string) bool { return strings.HasPrefix(entry, prefix) })
}

// TestSendToMovesAPausedRunWithoutStartingItsDestination: a paused run's route is sent
// to another stage. The pause stays, the displaced session is stopped before anything
// starts, nothing starts until you resume, and resuming starts the chosen stage with no
// Continue to the displaced attempt.
func TestSendToMovesAPausedRunWithoutStartingItsDestination(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	sim := simOf(h.coord)
	displaced := runSession(h.Run())
	h.coord.PauseRun(context.Background(), id)
	if pausePhase(h, id) != core.RunPausePhasePaused {
		t.Fatalf("phase = %s, want paused", pausePhase(h, id))
	}
	note := "carry on"
	spec := "spec"
	h.coord.Resume(id, &spec, &note, false, nil)
	// The displaced session is stopped in background work; wait for it before reading the trail.
	h.coord.Idle()
	run := h.Run()
	if run.StageID != "spec" || run.State != core.RunStateQueued || pausePhase(h, id) != core.RunPausePhasePaused {
		t.Fatalf("after send to: stage %s state %s pause %s, want spec queued and still paused", run.StageID, run.State, pausePhase(h, id))
	}
	if n := len(run.Attempts); n != 1 || run.Attempts[0].IsOpen() {
		t.Fatalf("attempts = %d (open %v), want the displaced one closed and nothing new", n, n == 1 && run.Attempts[0].IsOpen())
	}
	stop := trailIndex(sim.Trail(), "stop "+displaced)
	if stop < 0 {
		t.Fatalf("trail %v has no stop of %s", sim.Trail(), displaced)
	}
	for range 3 {
		h.Tick()
	}
	if got := h.Run(); got.StageID != "spec" || got.State != core.RunStateQueued || len(got.Attempts) != 1 {
		t.Fatalf("paused destination moved: stage %s state %s attempts %d", got.StageID, got.State, len(got.Attempts))
	}
	if i := trailIndex(sim.Trail()[stop+1:], "start "); i >= 0 {
		t.Fatalf("a session started under the pause: %v", sim.Trail())
	}

	h.coord.ResumeRun(context.Background(), id)
	h.Until("the chosen stage starts after resume", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "spec" && run.State == core.RunStateRunning
	})
	if !noContinue(sim) {
		t.Errorf("a Continue went to the displaced attempt")
	}
	if got := h.Run(); got.Pause != nil {
		t.Errorf("pause = %+v after resume, want it lifted", got.Pause)
	}
	started := trailIndex(sim.Trail()[stop+1:], "start ")
	if started < 0 {
		t.Errorf("trail %v has no start of the chosen stage after the stop", sim.Trail())
	}
}

// TestSendToUnderABeltOrGlobalPauseKeepsTheDestinationQueued: the destination of a
// Send to waits while the belt or the app is paused, and starts when it is lifted.
func TestSendToUnderABeltOrGlobalPauseKeepsTheDestinationQueued(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	belt := h.Run().BeltID
	if err := h.coord.SetPaused(belt, true); err != nil {
		t.Fatal(err)
	}
	spec := "spec"
	h.coord.Resume(id, &spec, nil, false, nil)
	h.Tick()
	if got := h.Run(); got.StageID != "spec" || got.State != core.RunStateQueued {
		t.Fatalf("belt pause: stage %s state %s, want spec queued", got.StageID, got.State)
	}
	if err := h.coord.SetPaused(belt, false); err != nil {
		t.Fatal(err)
	}
	h.Until("the destination starts once the belt is resumed", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "spec" && run.State == core.RunStateRunning
	})

	h.coord.SetGloballyPaused(context.Background(), true)
	build := "build"
	h.coord.Resume(id, &build, nil, false, nil)
	h.Tick()
	if got := h.Run(); got.StageID != "build" || got.State != core.RunStateQueued {
		t.Fatalf("global pause: stage %s state %s, want build queued", got.StageID, got.State)
	}
	h.coord.SetGloballyPaused(context.Background(), false)
	h.Until("the destination starts once the app is resumed", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "build" && run.State == core.RunStateRunning
	})
}

// TestAbortingQueuedWorkUnderGlobalPauseLaunchesNothingWhenItLifts: queued work is
// aborted while the app is paused. It is held, creates no attempt, and stays held after the
// pause lifts and the loop runs.
func TestAbortingQueuedWorkUnderGlobalPauseLaunchesNothingWhenItLifts(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	sim := simOf(h.coord)
	h.coord.SetGloballyPaused(context.Background(), true)
	spec := "spec"
	h.coord.Resume(id, &spec, nil, false, nil)
	before := len(h.Run().Attempts)
	if _, err := h.coord.AbortCommand(id, nil); err != nil {
		t.Fatalf("abort queued work: %v", err)
	}
	got := h.Run()
	if got.State != core.RunStateHeld || got.StageID != "spec" || len(got.Attempts) != before {
		t.Fatalf("after abort: state %s stage %s attempts %d, want held at spec with %d attempts", got.State, got.StageID, len(got.Attempts), before)
	}
	starts := len(sim.Trail())
	h.coord.SetGloballyPaused(context.Background(), false)
	for range 3 {
		h.Tick()
	}
	if after := h.Run(); after.State != core.RunStateHeld || after.StageID != "spec" || len(after.Attempts) != before {
		t.Fatalf("after the pause lifted: state %s stage %s attempts %d", after.State, after.StageID, len(after.Attempts))
	}
	if i := trailIndex(sim.Trail()[starts:], "start "); i >= 0 {
		t.Fatalf("aborted work launched: %v", sim.Trail())
	}
}

// TestSendToAbortAndDropAreSavedToDisk: each move, stop and drop is written to the data
// folder, so a reload of the stored runs shows the same state.
func TestSendToAbortAndDropAreSavedToDisk(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	spec := "spec"
	h.coord.Resume(id, &spec, nil, false, nil)
	h.coord.Idle()
	savedRun := func() core.Run {
		t.Helper()
		for _, run := range h.storage.LoadRuns() {
			if run.ID == id {
				return run
			}
		}
		t.Fatalf("run %s is not in the stored runs", id)
		return core.Run{}
	}
	if saved := savedRun(); saved.StageID != "spec" || !saved.State.IsActive() || saved.Attempts[0].IsOpen() {
		t.Fatalf("saved after send to: stage %s state %s, displaced attempt open %v", saved.StageID, saved.State, saved.Attempts[0].IsOpen())
	}
	if _, err := h.coord.AbortCommand(id, nil); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if saved := savedRun(); saved.State != core.RunStateHeld || saved.Reason == nil || *saved.Reason != "stopped by you" {
		t.Fatalf("saved after abort: state %s reason %v", saved.State, saved.Reason)
	}
	if _, err := h.coord.DropCommand(id, nil); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if saved := savedRun(); saved.State != core.RunStateDropped || saved.Reason == nil || *saved.Reason != "dropped by you" || saved.Pause != nil {
		t.Fatalf("saved after drop: state %s reason %v pause %v", saved.State, saved.Reason, saved.Pause)
	}
}

// TestSendToStartsNoDestinationWhileTheDisplacedSessionStops: the displaced session's stop is
// held open, and a tick runs meanwhile. The destination must not start until the stop has
// finished, and it starts once it has.
func TestSendToStartsNoDestinationWhileTheDisplacedSessionStops(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	runningRun(h)
	id := h.Run().ID
	sim := simOf(h.coord)
	displaced := runSession(h.Run())
	stopping := make(chan struct{})
	release := make(chan struct{})
	var once, freed sync.Once
	// A failing test must not leave the held stop blocking the coordinator's Close.
	t.Cleanup(func() { freed.Do(func() { close(release) }) })
	sim.BeforeStop = func(session string) {
		if session == displaced {
			once.Do(func() { close(stopping) })
			<-release
		}
	}
	before := len(sim.Trail())
	note := "carry on"
	spec := "spec"
	h.coord.Resume(id, &spec, &note, false, nil)
	<-stopping
	h.coord.Tick(context.Background())
	if i := trailIndex(sim.Trail()[before:], "start "); i >= 0 {
		t.Fatalf("a session started while the displaced one was still stopping: %v", sim.Trail())
	}
	freed.Do(func() { close(release) })
	h.coord.Idle()
	h.Until("the chosen stage starts once the displaced session has stopped", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "spec" && run.State == core.RunStateRunning
	})
	trail := sim.Trail()[before:]
	stop := trailIndex(trail, "stop "+displaced)
	started := trailIndex(trail, "start ")
	if stop < 0 || started < 0 || started < stop {
		t.Fatalf("trail %v: want the stop of %s before the start of the chosen stage", trail, displaced)
	}
}

// TestSendToWhileARouteIsPreparingLaunchesOnlyTheChosenStage: a queued route is sent to another
// stage while its launch is preparing. The launch it was prepared for must not start, and only
// the chosen stage starts, with an attempt of its own.
func TestSendToWhileARouteIsPreparingLaunchesOnlyTheChosenStage(t *testing.T) {
	t.Parallel()
	h := coordLongRun(t, nil, nil)
	sim := simOf(h.coord)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once, freed sync.Once
	t.Cleanup(func() { freed.Do(func() { close(release) }) })
	sim.BeforePrepare = func(string) {
		once.Do(func() { close(entered) })
		<-release
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.coord.Tick(context.Background())
	}()
	<-entered
	first := h.Run()
	if first == nil || first.StageID == "spec" {
		t.Fatalf("the launch was not preparing a stage other than spec: %+v", first)
	}
	id := first.ID
	note := "carry on"
	spec := "spec"
	h.coord.Resume(id, &spec, &note, false, nil)
	freed.Do(func() { close(release) })
	<-done
	h.coord.Idle()
	// The chosen stage may already have started once the background work settled.
	if got := h.Run(); got.StageID != "spec" {
		t.Fatalf("after the send: stage %s, want spec", got.StageID)
	}
	h.Until("the chosen stage starts", func() bool {
		run := h.nowRun()
		return run != nil && run.StageID == "spec" && run.State == core.RunStateRunning
	})
	run := h.Run()
	if len(run.Attempts) != 1 || run.Attempts[0].StageID != "spec" || run.Attempts[0].Session == nil {
		t.Fatalf("attempts = %+v, want one at spec with a session", run.Attempts)
	}
	if trail := sim.Trail(); len(trail) != 1 || trail[0] != "start "+*run.Attempts[0].Session {
		t.Fatalf("sessions = %v, want only the chosen stage's start", trail)
	}
}
