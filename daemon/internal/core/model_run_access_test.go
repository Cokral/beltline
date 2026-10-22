package core

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// testAccessRun is a run at stage build on two routes of a fork: the simple route
// has finished, the complex one is running. Its build attempts are one closed
// and one open, on the complex route.
func testAccessRun() Run {
	stage := func(id string, typ StageType) Stage { return Stage{ID: id, Type: typ, Name: id} }
	simple, complex := "r:simple", "r:complex"
	build := "build"
	ended := jsonx.Date{}
	return Run{
		ID: "bug:acme/billing#142", BeltID: "bug", StageID: build, State: RunStateRunning,
		Subject: Subject{Kind: SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Export"},
		Stages:  []Stage{stage("triage", StageTypeAgent), stage("build", StageTypeAgent), stage("watch", StageTypeWatch)},
		Legs: []Leg{
			{ID: "r:simple", Route: &simple, State: RunStateShipped},
			{ID: "r:complex", Route: &complex, StageID: &build, State: RunStateRunning},
		},
		Attempts: []Attempt{
			{ID: "triage-1", StageID: "triage", Number: 1, Folder: "f", EndedAt: &ended},
			{ID: "build-1", StageID: "build", Number: 1, Folder: "g", Leg: &complex, EndedAt: &ended, SessionID: new("s1")},
			{ID: "build-2", StageID: "build", Number: 2, Folder: "h", Leg: &complex},
		},
	}
}

// TestFocusIsTheFirstActiveLegOrTheLastWhenAllFinished checks the focus leg.
func TestFocusIsTheFirstActiveLegOrTheLastWhenAllFinished(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if focus := run.Focus(); focus == nil || focus.ID != "r:complex" {
		t.Fatalf("focus = %+v, want r:complex", focus)
	}
	run.Legs[1].State = RunStateShipped
	if focus := run.Focus(); focus == nil || focus.ID != "r:complex" {
		t.Errorf("all finished: focus = %+v, want the last leg", focus)
	}
	if focus := (Run{}).Focus(); focus != nil {
		t.Errorf("no legs: focus = %+v, want nil", focus)
	}
}

// TestLegAccessorsFindLegsByRouteStageAndID checks LegOn, LegAt, LegByID,
// ActiveLegs and WorkingLegs on a split run.
func TestLegAccessorsFindLegsByRouteStageAndID(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	simple, build := "r:simple", "build"
	if leg := run.LegOn(&simple); leg == nil || leg.ID != "r:simple" {
		t.Errorf("LegOn(simple) = %+v", leg)
	}
	if leg := run.LegOn(nil); leg != nil {
		t.Errorf("LegOn(line) on a split run = %+v, want nil", leg)
	}
	if leg := run.LegAt(&build); leg == nil || leg.ID != "r:complex" {
		t.Errorf("LegAt(build) = %+v", leg)
	}
	if leg := run.LegAt(nil); leg != nil {
		t.Errorf("LegAt(nil) = %+v", leg)
	}
	if leg := run.LegByID(&simple); leg == nil || leg.State != RunStateShipped {
		t.Errorf("LegByID = %+v", leg)
	}
	if active := run.ActiveLegs(); len(active) != 1 || active[0].ID != "r:complex" {
		t.Errorf("active legs = %+v", active)
	}
	if !run.IsSplit() || len(run.WorkingLegs()) != 2 {
		t.Errorf("a split run's working legs = %d", len(run.WorkingLegs()))
	}
}

// TestIndexLookupsFindStagesLegsAndJoinsByID checks the position lookups and the
// leg presence check, including the misses that return -1 and false.
func TestIndexLookupsFindStagesLegsAndJoinsByID(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	run.Joins = []Join{{ID: "join-1"}}
	if got := run.StageIndexOf("build"); got != 1 {
		t.Errorf("StageIndexOf(build) = %d, want 1", got)
	}
	if got := run.StageIndexOf("ship"); got != -1 {
		t.Errorf("StageIndexOf(ship) = %d, want -1", got)
	}
	if got := run.LegIndexOf("r:complex"); got != 1 {
		t.Errorf("LegIndexOf(r:complex) = %d, want 1", got)
	}
	if got := run.LegIndexOf("r:other"); got != -1 {
		t.Errorf("LegIndexOf(r:other) = %d, want -1", got)
	}
	if got := run.JoinIndexOf("join-1"); got != 0 {
		t.Errorf("JoinIndexOf(join-1) = %d, want 0", got)
	}
	if got := run.JoinIndexOf("join-2"); got != -1 {
		t.Errorf("JoinIndexOf(join-2) = %d, want -1", got)
	}
	if !run.HasLeg("r:simple") || run.HasLeg("r:other") {
		t.Errorf("HasLeg disagrees with the legs %+v", run.Legs)
	}
	if !run.HasAgentStage("build") || run.HasAgentStage("ship") {
		t.Errorf("HasAgentStage disagrees with the stages %+v", run.Stages)
	}
}

// TestWorkingLegsSpeakForTheRunOnItsOneLeg checks that a run on one leg reads
// its own stage, state and note for that leg.
func TestWorkingLegsSpeakForTheRunOnItsOneLeg(t *testing.T) {
	t.Parallel()
	note := "check the spec"
	run := Run{StageID: "spec", State: RunStateHeld, Note: &note, Legs: []Leg{{ID: "main", State: RunStateRunning, Branch: new("b")}}}
	working := run.WorkingLegs()
	if len(working) != 1 || working[0].StageID == nil || *working[0].StageID != "spec" || working[0].State != RunStateHeld ||
		working[0].Note == nil || working[0].Branch == nil || *working[0].Branch != "b" {
		t.Errorf("working legs = %+v", working)
	}
	if got := run.LegOn(nil); got == nil || got.ID != "main" {
		t.Errorf("LegOn(line) = %+v", got)
	}
}

// TestCurrentAndLastAttemptFollowTheFocusLeg checks that the current attempt is
// the open one at the stage on the focus leg, and the last attempt is the newest.
func TestCurrentAndLastAttemptFollowTheFocusLeg(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if current := run.CurrentAttempt(); current == nil || current.ID != "build-2" {
		t.Errorf("current attempt = %+v, want build-2", current)
	}
	if last := run.LastAttempt("build"); last == nil || last.ID != "build-2" {
		t.Errorf("last attempt = %+v, want build-2", last)
	}
	if last := run.LastAttempt("watch"); last != nil {
		t.Errorf("no watch attempt, but last = %+v", last)
	}
	if got := run.AttemptByID(new("build-1")); got == nil || got.Number != 1 {
		t.Errorf("attempt by id = %+v", got)
	}
	if got := run.AttemptByID(nil); got != nil {
		t.Errorf("attempt by nil id = %+v", got)
	}
}

// TestClosedAttemptsFilterByStageAndLeg checks the earlier work at a stage, on
// one leg or on any.
func TestClosedAttemptsFilterByStageAndLeg(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	complex, line := "r:complex", "main"
	if got := run.ClosedAttempts("build", nil); len(got) != 1 || got[0].ID != "build-1" {
		t.Errorf("closed on any leg = %+v", got)
	}
	if got := run.ClosedAttempts("build", &complex); len(got) != 1 {
		t.Errorf("closed on the complex route = %+v", got)
	}
	if got := run.ClosedAttempts("build", &line); len(got) != 0 {
		t.Errorf("closed on the line = %+v, want none", got)
	}
	if got := run.ClosedAttempts("triage", nil); len(got) != 1 || got[0].ID != "triage-1" {
		t.Errorf("closed at triage = %+v", got)
	}
}

// TestChangeCurrentAttemptEditsOnlyTheOpenOne checks that the change reaches the
// open attempt on the focus leg and leaves the closed ones alone.
func TestChangeCurrentAttemptEditsOnlyTheOpenOne(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	outcome := "ready"
	run.ChangeCurrentAttempt(func(a *Attempt) { a.Outcome = &outcome })
	if run.Attempts[2].Outcome == nil || run.Attempts[1].Outcome != nil || run.Attempts[0].Outcome != nil {
		t.Errorf("attempts = %+v", run.Attempts)
	}
}

// TestAnswerableIsAHeldRunsEndedAttemptWithASession checks when an attempt can be answered.
func TestAnswerableIsAHeldRunsEndedAttemptWithASession(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if run.Answerable() != nil {
		t.Error("a running run has an answerable attempt")
	}
	run.State = RunStateHeld
	run.StageID = "build"
	if run.Answerable() != nil {
		t.Error("an open last attempt is answerable")
	}
	run.Attempts[2].EndedAt = &jsonx.Date{}
	run.Attempts[2].SessionID = new("s2")
	if got := run.Answerable(); got == nil || got.ID != "build-2" {
		t.Errorf("answerable = %+v, want build-2", got)
	}
	run.Attempts[2].SessionID = nil
	if run.Answerable() != nil {
		t.Error("an attempt without a session is answerable")
	}
}

// TestStageAndReachedWatch checks the run's stage, its index, and whether it has been at a watch.
func TestStageAndReachedWatch(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if stage := run.Stage(); stage == nil || stage.ID != "build" {
		t.Errorf("stage = %+v", stage)
	}
	if index := run.StageIndex(); index == nil || *index != 1 {
		t.Errorf("stage index = %v, want 1", index)
	}
	if run.ReachedWatch() {
		t.Error("a run with no watch attempt has reached one")
	}
	run.Attempts = append(run.Attempts, Attempt{ID: "watch-1", StageID: "watch", Folder: "w"})
	if !run.ReachedWatch() {
		t.Error("a run with a watch attempt has not reached one")
	}
	run.StageID = "gone"
	if run.Stage() != nil || run.StageIndex() != nil {
		t.Error("a run at a stage not in its belt has a stage")
	}
}

// TestFolderAndRequestedBase checks the folder of each kind and the base asked
// for: the subject's base:, else the belt's, else none.
func TestFolderAndRequestedBase(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if got := run.Folder(); got != "bug/acme/billing/142" {
		t.Errorf("issue folder = %q", got)
	}
	if got := run.RequestedBase("acme/billing"); got != nil {
		t.Errorf("no base = %q, want nil", *got)
	}
	run.Base = "main"
	if got := run.RequestedBase("acme/billing"); got == nil || *got != "main" {
		t.Errorf("belt base = %v", got)
	}
	run.Subject.Base = new("develop")
	if got := run.RequestedBase("acme/billing"); got == nil || *got != "develop" {
		t.Errorf("subject base = %v, want develop", got)
	}
}

// TestRequestedBaseOrdersLabelOverrideAndShared checks the precedence of the
// requested branch in each repository: a base: label, then that repository's
// override, then the shared base, then none (the default).
func TestRequestedBaseOrdersLabelOverrideAndShared(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	run.Base = "develop"
	run.RepositoryBases = map[string]string{"acme/billing": "release/billing", "acme/web": "  ", "acme/other": ""}
	want := func(repo string, expected string) {
		t.Helper()
		got := run.RequestedBase(repo)
		if expected == "" {
			if got != nil {
				t.Errorf("%s: requested %q, want none", repo, *got)
			}
			return
		}
		if got == nil || *got != expected {
			t.Errorf("%s: requested %v, want %s", repo, got, expected)
		}
	}
	want("acme/billing", "release/billing")
	want("acme/web", "develop")
	want("acme/other", "develop")
	// A label the run started with is its base, and outranks both overrides.
	run.Subject.Labels = []string{"base:feature/epic"}
	run.Base = "feature/epic"
	run.BaseFromLabel = new(true)
	want("acme/billing", "feature/epic")
	want("acme/web", "feature/epic")
	run.Subject.Labels = nil
	run.Base = ""
	run.BaseFromLabel = nil
	want("acme/web", "")
	want("acme/billing", "release/billing")
}

// TestCanButtonParkWhileARouteIsUnparked checks that Park is offered only while
// some unfinished route is not already parked, and not for a finished run.
func TestCanButtonParkWhileARouteIsUnparked(t *testing.T) {
	t.Parallel()
	run := testAccessRun()
	if !run.CanButtonPark() {
		t.Error("a split run with an active route cannot be parked")
	}
	run.Legs[1].State = RunStateParked
	if run.CanButtonPark() {
		t.Error("all active routes are parked, but Park is offered")
	}
	run = testAccessRun()
	run.Parking = &Parking{}
	if run.CanButtonPark() || !run.IsButtonParked() {
		t.Error("a parked run offers Park or is not button-parked")
	}
	run = testAccessRun()
	run.State = RunStateShipped
	if run.CanButtonPark() {
		t.Error("a shipped run offers Park")
	}
}
