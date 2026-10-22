package core

import (
	"strings"
	"testing"
)

// runMenuSummary is each row's command, label and how it opens, with a "|" before a divided row.
func runMenuSummary(rows []RunMenuRow) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		part := string(row.Command) + ":" + row.Label + ":" + string(row.Opens)
		if row.Divided {
			part = "| " + part
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// TestRunMenuRowsFollowTheRunsState covers which rows each state shows, in order.
func TestRunMenuRowsFollowTheRunsState(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"running": "pause:Pause:command, park:Park:command, resume:Send to…:send, | abort:Abort…:confirm, drop:Drop…:confirm",
		"held":    "pause:Pause:command, park:Park:command, resume:Run Triage again:sendAgain, resume:Send to…:send, | abort:Abort…:confirm, drop:Drop…:confirm",
		"parked":  "pause:Pause:command, unpark:Unpark:command, resume:Send to…:send, | abort:Abort…:confirm, drop:Drop…:confirm",
		"shipped": "erase:Erase…:confirm",
		"dropped": "erase:Erase…:confirm",
	}
	for name, want := range cases {
		run := presState(name)
		rows := RunMenu(run, RunActionContextDefault(run).Actions())
		if got := runMenuSummary(rows); got != want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want)
		}
		for _, row := range rows {
			if row.Action == nil {
				t.Errorf("%s: %s has no action record", name, row.Label)
			}
		}
	}
}

// TestRunMenuPauseRowFollowsThePausePhase covers the pause row's words and command per phase,
// and Pausing…, which only reports.
func TestRunMenuPauseRowFollowsThePausePhase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		phase   RunPausePhase
		command RunCommand
		label   string
	}{
		{RunPausePhasePausing, RunCommandPause, "Pausing…"},
		{RunPausePhasePaused, RunCommandResumeRun, "Resume"},
		{RunPausePhaseResuming, RunCommandResumeRun, "Resume"},
		{RunPausePhaseIncomplete, RunCommandPause, "Retry pause"},
		{RunPausePhaseReconcileFailed, RunCommandResumeRun, "Retry"},
	}
	for _, c := range cases {
		run := presState("running")
		run.Pause = &RunPause{Phase: c.phase}
		row := RunMenu(run, RunActionContextDefault(run).Actions())[0]
		if row.Command != c.command || row.Label != c.label {
			t.Errorf("%s: got %s %q, want %s %q", c.phase, row.Command, row.Label, c.command, c.label)
		}
		if (row.Action == nil) != (c.phase == RunPausePhasePausing) {
			t.Errorf("%s: action %v", c.phase, row.Action)
		}
	}
}

// TestRunMenuSendOffersTheOtherStages covers Send to's targets and the record
// it is enabled from: the first stage that isn't the run's own.
func TestRunMenuSendOffersTheOtherStages(t *testing.T) {
	t.Parallel()
	run := presState("held")
	rows := RunMenu(run, RunActionContextDefault(run).Actions())
	var send RunMenuRow
	for _, row := range rows {
		if row.Opens == RunMenuOpensSend {
			send = row
		}
	}
	if len(send.Targets) != len(run.Stages)-1 {
		t.Fatalf("targets %v, want every stage but %s", send.Targets, run.StageID)
	}
	for _, target := range send.Targets {
		if target.Stage == run.StageID {
			t.Fatalf("targets offer the run's own stage %s", run.StageID)
		}
	}
	if send.Stage == nil || *send.Stage != send.Targets[0].Stage || send.Action == nil || send.Action.Stage == nil || *send.Action.Stage != *send.Stage {
		t.Fatalf("send opens on %v with action %+v, want the first target's record", send.Stage, send.Action)
	}
}
