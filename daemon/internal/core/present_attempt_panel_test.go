// Tests of the attempt panel, its log, and the session sidebar's Continue action.

package core

import (
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// presHistory is AttemptPanelTests.history(_:): a finished first attempt and a live second one.
func presHistory(stage string) []Attempt {
	first := NewAttempt(AttemptArgs{StageID: stage, Number: 1, StartedAt: jsonx.Date{Time: engineT0}, Note: new("Previous note"), Folder: "first"})
	ended := jsonx.Date{Time: engineT0.Add(10_000_000_000)}
	first.EndedAt = &ended
	first.Outcome = new("complete")
	first.Detail = new("Recorded result")
	first.Session = new("first-session")
	live := NewAttempt(AttemptArgs{StageID: stage, Number: 2, StartedAt: jsonx.Date{Time: engineT0.Add(20_000_000_000)}, Folder: "second"})
	live.Session = new("live-session")
	return []Attempt{first, live}
}

// attemptContent is the expected content of an attempt entry, with more as given.
func attemptContent(attempt Attempt, more *int) AttemptPanelContent {
	return AttemptPanelContent{Kind: AttemptPanelContentAttempt, Attempt: &attempt, More: more}
}

// TestAnOpenAttemptShowsOnlyLiveContent
func TestAnOpenAttemptShowsOnlyLiveContent(t *testing.T) {
	t.Parallel()
	attempts := []Attempt{presHistory("triage")[1]}
	if got := AttemptPanelContentFor(attempts, nil, StatusStepRunning); got.Kind != AttemptPanelContentRunning {
		t.Fatalf("content = %+v, want running", got)
	}
}

// TestPreviousOutputWinsOverStageRunningStatus
func TestPreviousOutputWinsOverStageRunningStatus(t *testing.T) {
	t.Parallel()
	attempts := presHistory("triage")
	if got := AttemptPanelContentFor(attempts, new(attempts[0].ID), StatusStepRunning); !reflect.DeepEqual(got, attemptContent(attempts[0], new(2))) {
		t.Fatalf("picked first = %+v", got)
	}
	if got := AttemptPanelContentFor(attempts, new(attempts[1].ID), StatusStepRunning); got.Kind != AttemptPanelContentRunning {
		t.Fatalf("picked live = %+v", got)
	}
	if got := AttemptPanelContentFor(attempts, nil, StatusStepRunning); got.Kind != AttemptPanelContentRunning {
		t.Fatalf("no pick = %+v", got)
	}
	if got := AttemptPanelCaption(attempts[0], attempts); got != "Attempt 1 of 2" {
		t.Fatalf("caption = %q", got)
	}
}

// TestBothControlsResolveTheSameRunLocalSelection
func TestBothControlsResolveTheSameRunLocalSelection(t *testing.T) {
	t.Parallel()
	attempts := presHistory("triage")
	for _, pick := range []*string{nil, new(attempts[0].ID), new(attempts[1].ID), new("spec-1"), new("missing")} {
		want := attempts[1].ID
		if pick != nil && *pick == attempts[0].ID {
			want = attempts[0].ID
		}
		shown := AttemptPanelShown(attempts, pick)
		if shown == nil || shown.ID != want {
			t.Fatalf("shown(%v) = %+v, want %s", pick, shown, want)
		}
	}
	if AttemptPanelShown(nil, new("missing")) != nil {
		t.Fatal("an empty history shows an attempt")
	}
}

// TestDefaultFollowsNewAttemptsAndStageChanges
func TestDefaultFollowsNewAttemptsAndStageChanges(t *testing.T) {
	t.Parallel()
	old := []Attempt{presHistory("triage")[0]}
	if shown := AttemptPanelShown(old, nil); shown == nil || !reflect.DeepEqual(*shown, old[0]) {
		t.Fatalf("old shown = %+v", shown)
	}
	if shown := AttemptPanelShown(presHistory("triage"), nil); shown == nil || shown.ID != "triage-2" {
		t.Fatalf("new shown = %+v", shown)
	}
	if shown := AttemptPanelShown(presHistory("spec"), new(old[0].ID)); shown == nil || shown.ID != "spec-2" {
		t.Fatalf("stage change shown = %+v", shown)
	}
	if got := AttemptPanelContentFor(old, nil, StatusStepDone); !reflect.DeepEqual(got, attemptContent(old[0], nil)) {
		t.Fatalf("finished content = %+v", got)
	}
}

// TestSessionsIncludeAllStagesAndAttemptsInStageOrder
func TestSessionsIncludeAllStagesAndAttemptsInStageOrder(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	gate := NewAttempt(AttemptArgs{StageID: "approve", Number: 1, StartedAt: jsonx.Date{Time: engineT0}, Folder: "gate"})
	run.Attempts = append(append(presHistory("spec"), presHistory("triage")...), gate)
	var ids []string
	for _, attempt := range AttemptPanelSessions(run) {
		ids = append(ids, attempt.ID)
	}
	if !reflect.DeepEqual(ids, []string{"triage-1", "triage-2", "spec-1", "spec-2"}) {
		t.Fatalf("session ids = %v", ids)
	}
	specs := AttemptPanelAttempts("spec", run)
	if shown := AttemptPanelShown(specs, nil); shown == nil || shown.ID != "spec-2" {
		t.Fatalf("spec shown = %+v", shown)
	}
}

// TestWatchesAndGatesShowTheirOwnHistory
func TestWatchesAndGatesShowTheirOwnHistory(t *testing.T) {
	t.Parallel()
	watch := presHistory("watch")
	if got := AttemptPanelContentFor(watch, new(watch[0].ID), StatusStepRunning); !reflect.DeepEqual(got, attemptContent(watch[0], new(2))) {
		t.Fatalf("watch picked = %+v", got)
	}
	if got := AttemptPanelContentFor(watch, nil, StatusStepRunning); got.Kind != AttemptPanelContentRunning {
		t.Fatalf("watch live = %+v", got)
	}
	gate := presHistory("approve")
	if got := AttemptPanelContentFor(gate, new(gate[0].ID), StatusStepWaiting); !reflect.DeepEqual(got, attemptContent(gate[0], new(2))) {
		t.Fatalf("gate picked = %+v", got)
	}
	if got := AttemptPanelContentFor(gate, nil, StatusStepWaiting); got.Kind != AttemptPanelContentDecision {
		t.Fatalf("gate live = %+v", got)
	}
}

// TestIdenticalAttemptIdsNeverSubstituteAnotherRunsData
func TestIdenticalAttemptIdsNeverSubstituteAnotherRunsData(t *testing.T) {
	t.Parallel()
	earlier := engineSampleRun()
	earlier.Attempts = presHistory("triage")
	later := engineSampleRun()
	later.ID += "-later"
	later.Attempts = presHistory("triage")
	later.Attempts[0].Note = new("Only in later run")
	later.Attempts[0].Session = new("later-session")
	shown := AttemptPanelShown(AttemptPanelAttempts("triage", later), new(earlier.Attempts[0].ID))
	if shown == nil || shown.Note == nil || *shown.Note != "Only in later run" {
		t.Fatalf("shown in later run = %+v", shown)
	}
	if sessions := AttemptPanelSessions(earlier); len(sessions) == 0 || sessions[0].Session == nil || *sessions[0].Session != "first-session" {
		t.Fatalf("earlier sessions = %+v", sessions)
	}
	if sessions := AttemptPanelSessions(later); len(sessions) == 0 || *sessions[0].Session != "later-session" {
		t.Fatalf("later sessions = %+v", sessions)
	}
}

// TestAttemptLogText checks the attempt log text.
func TestAttemptLogText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		session, continuing *string
		want                *string
	}{
		{new("agent"), new("chat"), new("agent\n─────\nchat")},
		{nil, new("chat"), new("chat")},
		{new("agent"), nil, new("agent")},
		{nil, nil, nil},
	}
	for _, c := range cases {
		got := AttemptLogText(c.session, c.continuing)
		if !SamePointee(got, c.want) {
			t.Fatalf("AttemptLogText(%v, %v) = %v, want %v", c.session, c.continuing, got, c.want)
		}
	}
}

// TestCurrentAttemptAndDirectoryAreBothRequired checks that both are required:
// Continue needs the attempt's checkout, a run still at its stage, a live run, and the attempt as the
// current one.
func TestCurrentAttemptAndDirectoryAreBothRequired(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "needs info")
	attempt := run.Attempts[0]
	if reason := AttemptPanelCanContinue(run, attempt, true); reason != nil {
		t.Fatalf("with its checkout = %q, want allowed", *reason)
	}
	if reason := AttemptPanelCanContinue(run, attempt, false); reason == nil || *reason != "the run's checkout is gone" {
		t.Fatalf("without its checkout = %v, want the checkout reason", reason)
	}
	run.StageID = "spec"
	if reason := AttemptPanelCanContinue(run, attempt, true); reason == nil || *reason != "the run has left Triage" {
		t.Fatalf("after the stage moved = %v, want the left-stage reason", reason)
	}
	run.State = RunStateShipped
	if reason := AttemptPanelCanContinue(run, attempt, true); reason == nil || *reason != "the run is shipped" {
		t.Fatalf("shipped = %v, want the shipped reason", reason)
	}
	run.State = RunStateHeld
	run.StageID = attempt.StageID
	run.Attempts = append(run.Attempts, NewAttempt(AttemptArgs{StageID: attempt.StageID, Number: 2, StartedAt: jsonx.Date{Time: engineT0}, Folder: "second"}))
	if reason := AttemptPanelCanContinue(run, attempt, true); reason == nil {
		t.Fatal("an attempt that is no longer the current one can continue")
	}
}
