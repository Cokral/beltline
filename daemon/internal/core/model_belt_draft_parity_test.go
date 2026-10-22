package core

import (
	"slices"
	"testing"
)

// draftParityBelt is a belt with triage holding a goto to verify, verify, and one
// issues entry "1" on acme/app.
func draftParityBelt() Belt {
	belt := draftTestBelt([]Stage{
		draftTestStage("triage", StageTypeAgent, "", "",
			draftTestOutcome("go", Then{Action: ThenActionGoto, To: []string{"verify"}})),
		draftTestStage("verify", StageTypeAgent, "", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
	})
	belt.Triggers = []TriggerEntry{{ID: "1", Trigger: Trigger{Kind: TriggerKindIssues, Issues: draftIssueFilterPtr("agent")}, Repos: []string{"acme/app"}}}
	return belt
}

// TestPickingTheCurrentKindChangesNothingAndKeepsTheStash: choosing the kind an entry
// already has changes nothing and returns the stash as it was sent.
func TestPickingTheCurrentKindChangesNothingAndKeepsTheStash(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindIssues), Stash: &TriggerStash{Entry: "1"}}
	result, stash, err := BeltDraftEditingApplyResult(request)
	if err != nil {
		t.Fatal(err)
	}
	draftCheck(t, "draft", draftParityBelt(), result)
	draftCheck(t, "stash", TriggerStash{Entry: "1"}, *stash)
}

// TestSwitchingKindReturnsAStashHoldingTheOutgoingKind: the stash keeps what the entry was before the switch.
func TestSwitchingKindReturnsAStashHoldingTheOutgoingKind(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindReviews)}
	result, stash, err := BeltDraftEditingApplyResult(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Triggers[0].Trigger.Kind != TriggerKindReviews {
		t.Fatalf("kind = %s, want reviews", result.Triggers[0].Trigger.Kind)
	}
	if stash == nil {
		t.Fatal("no stash returned")
	}
	if got := stash.Trigger(TriggerKindIssues); got == nil || got.Kind != TriggerKindIssues {
		t.Fatalf("stash for issues = %v, want an issues trigger", got)
	}
}

// TestRouteAddCanAddAFeedingRoute: a feeding route can be added to a fork made around a stage.
func TestRouteAddCanAddAFeedingRoute(t *testing.T) {
	t.Parallel()
	draft := draftParityBelt()
	draft.ForkAround("verify")
	if len(draft.Forks) == 0 {
		t.Fatal("no fork after forking around verify")
	}
	request := BeltDraftRequest{Draft: draft, Op: BeltDraftOperationRouteAdd, ForkID: new(draft.Forks[0].ID), Feeds: new(true)}
	result := draftRequestApply(t, request)
	routes := result.Forks[0].Routes
	if !routes[len(routes)-1].Feeds {
		t.Fatal("the added route does not feed in")
	}
}

// TestGotoPickWithNothingChosenLeavesTheGotoAsItIs: a goto with no target is an empty goto.
func TestGotoPickWithNothingChosenLeavesTheGotoAsItIs(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationGotoPick, StageID: new("triage"), OutcomeID: new("go"), GotoTarget: new("")}
	result := draftRequestApply(t, request)
	if len(result.Stages[0].Outcomes) == 0 {
		t.Fatal("triage has no outcomes")
	}
	outcome := result.Stages[0].Outcomes[0]
	if outcome.Then.Action != ThenActionGoto {
		t.Errorf("action = %s, want goto", outcome.Then.Action)
	}
	if len(outcome.Then.To) != 0 {
		t.Errorf("goto targets = %v, want none", outcome.Then.To)
	}
}

// TestStageSlotsAreClampedToTheSteppersRange: slots are kept between one and six.
func TestStageSlotsAreClampedToTheSteppersRange(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationStageUpdate, StageID: new("verify")}
	request.Update = &StageEdit{Slots: new(9)}
	if got := draftRequestApply(t, request).Stages[1].Slots; got != 6 {
		t.Errorf("slots 9 -> %d, want 6", got)
	}
	request.Update = &StageEdit{Slots: new(0)}
	if got := draftRequestApply(t, request).Stages[1].Slots; got != 1 {
		t.Errorf("slots 0 -> %d, want 1", got)
	}
}

// TestOutcomeRepeatsAreClampedToNine: an outcome's repeat limit is kept to nine.
func TestOutcomeRepeatsAreClampedToNine(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationStageUpdate, StageID: new("verify")}
	request.Update = &StageEdit{Outcomes: []Outcome{draftTestOutcome("again", Then{Action: ThenActionNext, Max: new(12)})}}
	got := draftRequestApply(t, request).Stages[1].Outcomes[0].Then.Max
	if got == nil || *got != 9 {
		t.Errorf("max = %v, want 9", got)
	}
}

// TestASecondRepositoryIsRefusedForOneRepositoryKinds: a schedule keeps one repository.
func TestASecondRepositoryIsRefusedForOneRepositoryKinds(t *testing.T) {
	t.Parallel()
	draft := draftParityBelt()
	draft.Triggers[0].Repos = []string{"acme/app"}
	schedule := ScheduledTask{Schedule: DefaultSchedule(), Title: "Nightly", Task: "Sweep"}
	draft.Triggers[0].Trigger = Trigger{Kind: TriggerKindSchedule, Schedule: &schedule}
	request := BeltDraftRequest{Draft: draft, Op: BeltDraftOperationRepositoryAdd, EntryID: new("1"), Repository: new("acme/other")}
	draftCheck(t, "problems", []string{"This trigger works in one repository. Remove the current one first."}, BeltDraftEditingProblems(request))
}

// draftFeedBelt is a belt of triage (agent triage v2) and verify.
func draftFeedBelt() Belt {
	belt := draftTestBelt([]Stage{
		draftTestStage("triage", StageTypeAgent, "triage", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
		draftTestStage("verify", StageTypeAgent, "verify", "", draftTestOutcome("ok", Then{Action: ThenActionNext})),
	})
	belt.Triggers[0].Repos = []string{"acme/app"}
	return belt
}

// draftFeeder is an agent stage with the given id, on the build agent with one outcome.
func draftFeeder(id string) Stage {
	return draftTestStage(id, StageTypeAgent, "build", "", draftTestOutcome("ok", Then{Action: ThenActionNext}))
}

// TestFeedIntoPutsAFeedForkInFrontOfTheStageAndHoldsTheFedStage: feed-into adds a feeding
// fork before the stage, with the new stage on the feeding route.
func TestFeedIntoPutsAFeedForkInFrontOfTheStageAndHoldsTheFedStage(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationFeedInto, StageID: new("verify")}
	feeder := draftFeeder("fix")
	request.Stage = &feeder
	result := draftRequestApply(t, request)
	if len(result.Forks) == 0 {
		t.Fatal("no fork")
	}
	fork := result.Forks[0]
	if len(fork.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(fork.Routes))
	}
	last := fork.Routes[len(fork.Routes)-1]
	if !last.Feeds {
		t.Error("the last route does not feed in")
	}
	fed := draftStage(result, "fix")
	if fed == nil || fed.Route == nil || *fed.Route != last.ID {
		t.Fatalf("fed stage = %v, want on %s", fed, last.ID)
	}
	ids := draftStageIDs(result)
	if slices.Index(ids, "fix") >= slices.Index(ids, "verify") {
		t.Errorf("stages %v: fix should come before verify", ids)
	}
}

// TestFeedIntoRejectsAMissingStageAndAStageAlreadyOnTheBelt: feed-into needs a new stage.
func TestFeedIntoRejectsAMissingStageAndAStageAlreadyOnTheBelt(t *testing.T) {
	t.Parallel()
	missing := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationFeedInto, StageID: new("verify")}
	draftCheck(t, "missing stage", []string{"feed-into needs a stage."}, BeltDraftEditingProblems(missing))
	duplicate := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationFeedInto, StageID: new("verify")}
	feeder := draftFeeder("triage")
	duplicate.Stage = &feeder
	draftCheck(t, "duplicate stage", []string{"feed-into: a stage with id triage is already on the belt."}, BeltDraftEditingProblems(duplicate))
}

// TestRouteAndForkProblemsNameTheirForkOrRoute: a problem about a route or fork is located at it.
func TestRouteAndForkProblemsNameTheirForkOrRoute(t *testing.T) {
	t.Parallel()
	route := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationRouteRemove, RouteID: new("nowhere")}
	located := BeltDraftEditingLocated(route)
	if len(located) == 0 {
		t.Fatal("no problems for a missing route")
	}
	draftCheck(t, "route location", ProblemLocationRoute("nowhere"), *located[0].Location)
	fork := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationRouteAdd, ForkID: new("missing")}
	located = BeltDraftEditingLocated(fork)
	if len(located) == 0 {
		t.Fatal("no problems for a missing fork")
	}
	draftCheck(t, "fork location", ProblemLocationFork("missing"), *located[0].Location)
}

// TestTriggerProblemsNameTheirEntry: a problem about a trigger entry is located at it.
func TestTriggerProblemsNameTheirEntry(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftFeedBelt(), Op: BeltDraftOperationTriggerRemove, EntryID: new("gone")}
	located := BeltDraftEditingLocated(request)
	if len(located) == 0 {
		t.Fatal("no problems for a missing entry")
	}
	draftCheck(t, "trigger location", ProblemLocationTrigger("gone"), *located[0].Location)
}

// TestAPollingTriggerTakesTheNearestOfferedInterval
func TestAPollingTriggerTakesTheNearestOfferedInterval(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationTriggerUpdate, EntryID: new("1"),
		Trigger: new(btIssuesOf(IssueFilter{Label: "agent", Poll: 100}))}
	poll := draftRequestApply(t, request).Triggers[0].Trigger.Poll()
	if poll == nil || *poll != 60 {
		t.Errorf("poll = %v, want 60", poll)
	}
}

// TestTriggerAddDefaultsToAnIssuesTriggerWithTheAgentLabel
func TestTriggerAddDefaultsToAnIssuesTriggerWithTheAgentLabel(t *testing.T) {
	t.Parallel()
	result := draftRequestApply(t, BeltDraftRequest{Draft: draftParityBelt(), Op: BeltDraftOperationTriggerAdd})
	added := result.Triggers[len(result.Triggers)-1]
	if added.Trigger.Kind != TriggerKindIssues {
		t.Fatalf("added kind = %s, want issues", added.Trigger.Kind)
	}
	if added.Trigger.Issues == nil || added.Trigger.Issues.Label != "agent" {
		t.Errorf("issues filter = %+v, want the agent label", added.Trigger.Issues)
	}
}
