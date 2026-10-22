package core

import (
	"testing"
	"time"
)

// TestStagePanelStateChipOfStartsTheClockOnlyWhileRunning checks that a running agent stage's chip carries
// the clock's start, and that a paused app shows PAUSED with no clock.
func TestStagePanelStateChipOfStartsTheClockOnlyWhileRunning(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	stage := run.Stages[0]
	attempt := run.CurrentAttempt()
	if attempt == nil {
		t.Fatal("the running agent has no attempt")
	}
	now := engineT0.Add(90 * time.Second)

	chip := StagePanelStateChipOf(run, stage, StatusStepRunning, now, false, PauseLive)
	if chip.Text != "RUNNING" || chip.Tone != "running" {
		t.Errorf("running chip = %+v", chip)
	}
	if chip.ClockSince == nil || !chip.ClockSince.Equal(attempt.StartedAt.Time) {
		t.Errorf("clock since = %v, want the attempt's start %v", chip.ClockSince, attempt.StartedAt.Time)
	}

	paused := StagePanelStateChipOf(run, stage, StatusStepRunning, now, false, Pause{IsPaused: true, PausedAt: &now})
	if paused.Text != "PAUSED" || paused.Tone != "running" || paused.ClockSince != nil {
		t.Errorf("paused chip = %+v", paused)
	}
}

// TestStagePanelStateChipOfWatchIsWatchingNotRunning checks that a watch never shows a clock.
func TestStagePanelStateChipOfWatchIsWatchingNotRunning(t *testing.T) {
	t.Parallel()
	run := engineRunningWatch()
	stage := run.Stages[engineWatchIndex(t, run)]

	chip := StagePanelStateChipOf(run, stage, StatusStepRunning, engineT0, false, PauseLive)
	if chip.Text != "WATCHING" || chip.Tone != "running" || chip.ClockSince != nil {
		t.Errorf("watch chip = %+v", chip)
	}
}

// TestStagePanelStateChipOfNamesEachStep pins the chip text and tone for the steps a stage can stand at.
func TestStagePanelStateChipOfNamesEachStep(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	stage := run.Stages[0]
	reason := "tests failed"
	held := run
	held.Reason = &reason

	cases := []struct {
		name       string
		run        Run
		step       StatusStep
		beltPaused bool
		text, tone string
	}{
		{"done", run, StatusStepDone, false, "DONE", "good"},
		{"waiting", run, StatusStepWaiting, false, "WAITING FOR YOU", "you"},
		{"held", held, StatusStepHeld, false, "TESTS FAILED", "problem"},
		{"parked", run, StatusStepParked, false, "PARKED", "grey"},
		{"dropped", run, StatusStepDropped, false, "DROPPED", "grey"},
		{"queued", run, StatusStepQueued, false, "QUEUED", "grey"},
		{"queued on a paused belt", run, StatusStepQueued, true, "PAUSED", "grey"},
		{"redo", run, StatusStepRedo, false, "WILL RUN AGAIN", "running"},
		{"skipped", run, StatusStepSkipped, false, "NOT TAKEN", "grey"},
		{"todo", run, StatusStepTodo, false, "NOT STARTED", "grey"},
	}
	for _, tc := range cases {
		chip := StagePanelStateChipOf(tc.run, stage, tc.step, engineT0, tc.beltPaused, PauseLive)
		if chip.Text != tc.text || chip.Tone != tc.tone || chip.ClockSince != nil {
			t.Errorf("%s: chip = %+v, want %s %s", tc.name, chip, tc.text, tc.tone)
		}
	}
}

// TestStagePanelKindChipOfNamesGatesAndWatches checks that a gate and a watch name themselves in the
// panel's head, and that an agent stage shows the runner it ran on.
func TestStagePanelKindChipOfNamesGatesAndWatches(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	var gate, agent Stage
	for _, stage := range run.Stages {
		switch stage.Kind() {
		case StageKindGate:
			gate = stage
		case StageKindAgent:
			if agent.ID == "" {
				agent = stage
			}
		}
	}
	if got := StagePanelKindChipOf(gate, "CLAUDE · OPUS"); got != "YOUR DECISION" {
		t.Errorf("gate kind = %q", got)
	}
	if got := StagePanelKindChipOf(agent, "CLAUDE · OPUS"); got != "CLAUDE · OPUS" {
		t.Errorf("agent kind = %q", got)
	}
	watch := engineRunningWatch().Stages[engineWatchIndex(t, engineRunningWatch())]
	if got := StagePanelKindChipOf(watch, "CLAUDE · OPUS"); got != "WATCH" {
		t.Errorf("watch kind = %q", got)
	}
}

// TestStagePanelWorkingOfNamesTheAgentRunner checks that an agent stage says which runner is working on it.
func TestStagePanelWorkingOfNamesTheAgentRunner(t *testing.T) {
	t.Parallel()
	run := engineRunningAgent()
	stage := run.Stages[0]
	if got, want := StagePanelWorkingOf(run, stage, nil), BackendPi.Label()+" is working on it."; got != want {
		t.Errorf("working = %q, want %q", got, want)
	}
}
