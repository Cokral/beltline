package core

import (
	"reflect"
	"strings"
	"testing"
)

// TestEditBeforeBuildTriagesAgain: an edit before any stage changes code stops the
// session and starts again at triage.
func TestEditBeforeBuildTriagesAgain(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "ready to spec")
	EngineAdvance(&run, engineSessionStart("s"), engineT0)
	edited := run.Subject
	edited.Body = "Now with steps."
	effects := EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if len(effects) == 0 || effects[0].Kind != EffectKindStopSession {
		t.Errorf("effects = %+v, want stopSession first", effects)
	}
	if run.StageID != "triage" || run.State != RunStateQueued {
		t.Errorf("stage %q state %q", run.StageID, run.State)
	}
	if run.Note == nil || !strings.Contains(*run.Note, "edited") {
		t.Errorf("note = %q", engineStr(run.Note))
	}
}

// TestEditAnswersANeedsInfoHold: an edit to the issue while triage holds for an answer
// starts triage again.
func TestEditAnswersANeedsInfoHold(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "needs info")
	edited := run.Subject
	edited.Body += "\nIt's the invoices page."
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.State != RunStateQueued || run.StageID != "triage" {
		t.Errorf("state %q stage %q", run.State, run.StageID)
	}
}

// TestEditAfterBuildOnlyUpdatesTheIssue: once code has changed, an edit only updates
// the issue the run works on.
func TestEditAfterBuildOnlyUpdatesTheIssue(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "ready to implement")
	engineFinishOutcome(&run, "complete")
	EngineAdvance(&run, Event{Kind: EventKindDecided, Outcome: "approved"}, engineT0)
	engineFinishOutcome(&run, "done")
	edited := run.Subject
	edited.Title = "Export invoices"
	if effects := EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0); len(effects) != 0 {
		t.Errorf("effects = %+v, want none", effects)
	}
	if run.StageID != "verify" || run.Subject.Title != "Export invoices" {
		t.Errorf("stage %q title %q", run.StageID, run.Subject.Title)
	}
}

// TestLabelChangesAreNotEdits: a label change leaves the stage where it is.
func TestLabelChangesAreNotEdits(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "ready to spec")
	edited := run.Subject
	edited.Labels = []string{"agent", "billing"}
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.StageID != "spec" {
		t.Errorf("stage = %q, want spec", run.StageID)
	}
}

// TestCloseDropsAndStopsTheSession: closing the issue during a session drops the run,
// stops the session and cleans up.
func TestCloseDropsAndStopsTheSession(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	EngineAdvance(&run, engineSessionStart("s"), engineT0)
	effects := EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0)
	if run.State != RunStateDropped || engineStr(run.Reason) != "the issue was closed" {
		t.Errorf("state %q reason %q", run.State, engineStr(run.Reason))
	}
	want := []Effect{{Kind: EffectKindStopSession}, {Kind: EffectKindCleanUp}}
	if !reflect.DeepEqual(effects, want) {
		t.Errorf("effects = %+v, want %+v", effects, want)
	}
}

// TestClosingARunThatReachedItsWatchLetsTheWatchDecide: a run at its watch keeps going
// when its issue closes, so the watch can see the merge.
func TestClosingARunThatReachedItsWatchLetsTheWatchDecide(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineToWatch(&run)
	EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0)
	if run.State != RunStateRunning || run.StageID != "watch" {
		t.Fatalf("state %q stage %q", run.State, run.StageID)
	}
	EngineAdvance(&run, Event{Kind: EventKindWatched, Watch: WatchEventMerged, Detail: new("#201")}, engineT0)
	if run.StageID != "wrap-up" {
		t.Fatalf("stage %q, want wrap-up", run.StageID)
	}
	if effects := EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0); len(effects) != 0 {
		t.Errorf("effects = %+v, want none", effects)
	}
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
	engineFinishOutcome(&run, "done")
	if run.State != RunStateShipped {
		t.Errorf("after wrap-up: state %q, want shipped", run.State)
	}
}

// TestARunSentBackFromItsWatchStillIgnoresAClose: a run sent back from its watch
// keeps going when its issue closes.
func TestARunSentBackFromItsWatchStillIgnoresAClose(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineToWatch(&run)
	EngineAdvance(&run, Event{Kind: EventKindWatched, Watch: WatchEventChecksFailed, Detail: new("#201")}, engineT0)
	if run.StageID != "fix-checks" {
		t.Fatalf("stage %q, want fix-checks", run.StageID)
	}
	EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0)
	if run.State != RunStateQueued {
		t.Errorf("state = %q, want queued", run.State)
	}
}

// TestARunKeepsThePoliciesItStartedWith: a policy changed on the belt after a run
// started does not change that run.
func TestARunKeepsThePoliciesItStartedWith(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	run.Policies.StopOnClose = false
	run.Policies.RetriageOnEdit = false
	engineFinishOutcome(&run, "ready to spec")
	EngineAdvance(&run, Event{Kind: EventKindSubjectGone, Reason: "the issue was closed"}, engineT0)
	if run.State != RunStateQueued {
		t.Fatalf("state = %q, want queued", run.State)
	}
	edited := run.Subject
	edited.Body = "Something else."
	EngineAdvance(&run, Event{Kind: EventKindSubjectChanged, Subject: edited}, engineT0)
	if run.StageID != "spec" {
		t.Errorf("stage = %q, want spec", run.StageID)
	}
}

// TestResumeSendsAHeldRunToAStageWithANote: a held run resumed at a stage takes the
// note you sent with it.
func TestResumeSendsAHeldRunToAStageWithANote(t *testing.T) {
	t.Parallel()
	run := engineSampleRun()
	engineFinishOutcome(&run, "ready to spec")
	engineFinishOutcome(&run, "open questions")
	if run.State != RunStateHeld {
		t.Fatalf("state = %q, want held", run.State)
	}
	EngineAdvance(&run, EventResume(new("spec"), new("Only admins can export."), nil), engineT0)
	if run.State != RunStateQueued || engineStr(run.Note) != "Only admins can export." {
		t.Errorf("state %q note %q", run.State, engineStr(run.Note))
	}
}

// TestRunsKeepTheirBeltVersion: a belt edited after a run started does not change it.
func TestRunsKeepTheirBeltVersion(t *testing.T) {
	t.Parallel()
	belt := engineIssueBelt()
	run := engineStartRun(belt)
	belt.Stages = append(belt.Stages[:2:2], belt.Stages[3:]...)
	belt.Version = 2
	if len(run.Stages) != 10 {
		t.Errorf("stages = %d, want 10", len(run.Stages))
	}
	if run.BeltVersion != 1 {
		t.Errorf("beltVersion = %d, want 1", run.BeltVersion)
	}
}
