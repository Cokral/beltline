package core

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// engineRestart is an overrule that restarts
// the run at stage. An empty attempt means the run's current attempt, or its last.
func engineRestart(run *Run, stage, note, decision, attempt string, now time.Time) []Effect {
	if attempt == "" {
		switch {
		case run.CurrentAttempt() != nil:
			attempt = run.CurrentAttempt().ID
		case len(run.Attempts) > 0:
			attempt = run.Attempts[len(run.Attempts)-1].ID
		default:
			attempt = "attempt"
		}
	}
	return EngineAdvance(run, Event{Kind: EventKindOverrule, RestartStage: stage, Comments: note, Decision: decision, AttemptID: attempt}, now)
}

// TestShippedAndDroppedRunsAreLeftUntouched: an overrule on a finished run changes nothing.
func TestShippedAndDroppedRunsAreLeftUntouched(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateShipped, RunStateDropped} {
		run := engineSampleRun()
		run.State = state
		run.Branch = new("beltline/issue-142")
		before := run.Clone()
		if effects := engineRestart(&run, "build", "Cover the empty export.", "d", "", engineT0.Add(5*time.Second)); len(effects) != 0 {
			t.Errorf("%s: effects = %+v, want none", state, effects)
		}
		if !reflect.DeepEqual(run, before) {
			t.Errorf("%s: the run changed:\n%s", state, testkit.Diff(before, run))
		}
	}
}

// TestAMissingOrEmptyTargetDoesNothing: an overrule to an approval, to a stage that
// is not there, with a blank note, or for another decision does nothing.
func TestAMissingOrEmptyTargetDoesNothing(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	before := run.Clone()
	if effects := engineRestart(&run, "approve", "Cover the empty export.", "d", "", engineT0); len(effects) != 0 {
		t.Errorf("approve: effects = %+v", effects)
	}
	if effects := engineRestart(&run, "missing", "Cover the empty export.", "d", "", engineT0); len(effects) != 0 {
		t.Errorf("missing: effects = %+v", effects)
	}
	if effects := engineRestart(&run, "build", "   ", "d", "", engineT0); len(effects) != 0 {
		t.Errorf("blank note: effects = %+v", effects)
	}
	if effects := engineRestart(&run, "build", "Cover the empty export.", "", "x", engineT0); len(effects) != 0 {
		t.Errorf("no decision: effects = %+v", effects)
	}
	if !reflect.DeepEqual(run, before) {
		t.Errorf("the run changed:\n%s", testkit.Diff(before, run))
	}
}

// TestAnActiveRunRestartsAtTheAgent: an overrule restarts a queued, running, waiting,
// held or parked run at an agent stage, stopping what it must and keeping the rest.
func TestAnActiveRunRestartsAtTheAgent(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateQueued, RunStateRunning, RunStateWaiting, RunStateHeld, RunStateParked} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			run := engineSampleRun()
			run.Branch = new("beltline/issue-142")
			run.Uses = map[string]int{"build:done": 2}
			switch state {
			case RunStateRunning:
				EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "bl-test", SessionID: "id", Folder: "01-triage-1"}, engineT0)
			case RunStateWaiting:
				run = engineGateRun()
				run.Branch = new("beltline/issue-142")
				run.Uses = map[string]int{"build:done": 2}
			case RunStateHeld:
				engineFinishOutcome(&run, "needs info")
			case RunStateParked:
				engineFinishOutcome(&run, "needs info")
				run.State = RunStateParked
			}
			version, agents, generation := run.BeltVersion, run.Agents, run.Generation
			var heldRoute *string
			if state == RunStateHeld || state == RunStateParked {
				heldRoute = run.Attempts[len(run.Attempts)-1].Route
			}
			open := ""
			if current := run.CurrentAttempt(); current != nil {
				open = current.ID
			}
			effects := engineRestart(&run, "spec", "Cover the empty export.", "d", "", engineT0)
			if run.State != RunStateQueued || run.StageID != "spec" {
				t.Fatalf("state %q stage %q", run.State, run.StageID)
			}
			if engineStr(run.Note) != "Cover the empty export." || run.Continues != nil {
				t.Errorf("note %q continues %q", engineStr(run.Note), engineStr(run.Continues))
			}
			if run.Branch == nil || *run.Branch != "beltline/issue-142" || run.BeltVersion != version {
				t.Errorf("branch or belt version changed")
			}
			if !reflect.DeepEqual(run.Agents, agents) || run.Generation != generation {
				t.Errorf("agents or generation changed")
			}
			if run.Uses["build:done"] != 2 || run.Operation != 1 {
				t.Errorf("uses %v operation %d", run.Uses, run.Operation)
			}
			if run.Restart == nil || run.Restart.Phase != RestartIntentPhaseApplied || run.Restart.Comments != "Cover the empty export." || run.Restart.StageID != "spec" {
				t.Errorf("restart = %+v", run.Restart)
			}
			switch state {
			case RunStateRunning:
				if !reflect.DeepEqual(effects, []Effect{{Kind: EffectKindStopSession}}) {
					t.Errorf("effects = %+v, want stopSession", effects)
				}
				if a := attemptWithID(run, open); a == nil || engineStr(a.Route) != "Stopped: overruled" || engineStr(a.Detail) != "Overruled" {
					t.Errorf("stopped attempt = %+v", a)
				}
			case RunStateWaiting:
				if engineHasEffect(effects, EffectKindStopSession) {
					t.Errorf("effects = %+v, want no stopSession", effects)
				}
				if a := attemptWithID(run, open); a == nil || engineStr(a.Route) != "Stopped: overruled" {
					t.Errorf("stopped approval = %+v", a)
				}
			case RunStateHeld, RunStateParked:
				last := lastAttemptAt(run, "triage")
				if last == nil || !reflect.DeepEqual(last.Route, heldRoute) {
					t.Errorf("triage route = %+v, want %v", last, engineStr(heldRoute))
				}
				if last == nil || last.Detail == nil || !strings.Contains(*last.Detail, "Overruled") {
					t.Errorf("triage detail = %+v", last)
				}
			default:
				if len(effects) != 0 {
					t.Errorf("effects = %+v, want none", effects)
				}
			}
			again := run.Clone()
			if effects := engineRestart(&run, "spec", "Cover the empty export.", "d", "", engineT0); len(effects) != 0 {
				t.Errorf("second overrule: effects = %+v", effects)
			}
			if !reflect.DeepEqual(run, again) {
				t.Errorf("a repeated overrule changed the run:\n%s", testkit.Diff(again, run))
			}
		})
	}
}

// attemptWithID is the run's attempt with id, or nil.
func attemptWithID(run Run, id string) *Attempt {
	for i := range run.Attempts {
		if run.Attempts[i].ID == id {
			return &run.Attempts[i]
		}
	}
	return nil
}

// lastAttemptAt is the run's last attempt at stage, or nil.
func lastAttemptAt(run Run, stage string) *Attempt {
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		if run.Attempts[i].StageID == stage {
			return &run.Attempts[i]
		}
	}
	return nil
}

// TestCommentsAndEarlierOutputAreKeptForAFreshSession: an overrule to spec keeps the
// comment and the earlier spec work for the session it starts.
func TestCommentsAndEarlierOutputAreKeptForAFreshSession(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	run.Continues = new("triage-1")
	closed := run.ClosedAttempts("spec", nil)
	spec := closed[len(closed)-1].ID
	engineRestart(&run, "spec", "  Look again.  ", "d", "", engineT0)
	if engineStr(run.Note) != "Look again." || !engineIDIs(run.Replays, spec) || run.Continues != nil {
		t.Fatalf("note %q replays %q continues %q", engineStr(run.Note), engineStr(run.Replays), engineStr(run.Continues))
	}
	engineFinishOutcome(&run, "complete")
	attempt := lastAttemptAt(run, "spec")
	if attempt == nil || engineStr(attempt.Note) != "Look again." || !engineIDIs(attempt.Replays, spec) || attempt.Continues != nil {
		t.Errorf("spec attempt = %+v", attempt)
	}
	if run.StageID != "approve" {
		t.Errorf("stage = %q, want approve", run.StageID)
	}
}

// TestAStageThatNeverRanGetsNoFabricatedReplay: an overrule to a stage that never ran
// replays nothing.
func TestAStageThatNeverRanGetsNoFabricatedReplay(t *testing.T) {
	t.Parallel()
	run := engineGateRun()
	engineRestart(&run, "build", "Cover the empty export.", "d", "", engineT0)
	if run.Replays != nil {
		t.Errorf("replays = %q, want nil", *run.Replays)
	}
	engineFinishOutcome(&run, "done")
	attempt := lastAttemptAt(run, "build")
	if attempt == nil || attempt.Replays != nil || engineStr(attempt.Note) != "Cover the empty export." {
		t.Errorf("build attempt = %+v", attempt)
	}
	if run.StageID != "verify" {
		t.Errorf("stage = %q, want verify", run.StageID)
	}
}

// TestAForkRouteIsTakenAndLaterChoicesClear: an overrule into a route takes it; one
// from the top clears the routes it had.
func TestAForkRouteIsTakenAndLaterChoicesClear(t *testing.T) {
	t.Parallel()
	run := engineStartRun(engineForkedBelt([]string{"build"}))
	engineFinishOutcome(&run, "ready to spec")
	if got := run.Routes()["size"]; len(got) != 1 || got[0] != "complex" {
		t.Fatalf("routes = %v, want size complex", run.Routes())
	}
	engineRestart(&run, "quick-build", "Take the simple route", "d", "", engineT0)
	if run.StageID != "quick-build" {
		t.Fatalf("stage = %q, want quick-build", run.StageID)
	}
	if got := run.Routes()["size"]; len(got) != 1 || got[0] != "simple" {
		t.Errorf("routes = %v, want size simple", run.Routes())
	}

	later := engineSampleRun()
	later.SetRoutes(map[string][]string{"after-merge": {"merged"}})
	later.Branch = new("beltline/issue-142")
	engineRestart(&later, "triage", "From the top", "d", "", engineT0)
	if _, ok := later.Routes()["after-merge"]; ok {
		t.Errorf("routes = %v, want after-merge cleared", later.Routes())
	}
	if later.Branch == nil || *later.Branch != "beltline/issue-142" || later.StageID != "triage" {
		t.Errorf("branch %v stage %q", later.Branch, later.StageID)
	}
}

// TestAWatchAndAQueueAreClosedWithoutASecondSession: an overrule from a watch closes
// the watch's attempt as overruled, with the pull requests it found kept.
func TestAWatchAndAQueueAreClosedWithoutASecondSession(t *testing.T) {
	t.Parallel()
	watch := engineSampleRun()
	engineToWatch(&watch)
	watchAttempt := watch.CurrentAttempt().ID
	watch.PullRequests = []PullRequest{{Number: 7, URL: "https://github.com/acme/billing/pull/7", Repo: "acme/billing"}}
	pulls := cloneEach(watch.PullRequests, func(p PullRequest) PullRequest { return p })
	effects := engineRestart(&watch, "build", "The watch can wait", "d", "", engineT0)
	if engineHasEffect(effects, EffectKindStopSession) {
		t.Errorf("effects = %+v, want no stopSession", effects)
	}
	if watch.StageID != "build" || watch.State != RunStateQueued {
		t.Errorf("stage %q state %q", watch.StageID, watch.State)
	}
	if a := attemptWithID(watch, watchAttempt); a == nil || engineStr(a.Route) != "Stopped: overruled" {
		t.Errorf("watch attempt = %+v", a)
	}
	if !reflect.DeepEqual(watch.PullRequests, pulls) {
		t.Errorf("pull requests = %+v, want %+v", watch.PullRequests, pulls)
	}
	for _, a := range watch.Attempts {
		if a.IsOpen() {
			t.Errorf("attempt %s is still open", a.ID)
		}
	}
}
