package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// menuOutcome is StageMenuTests.outcome(_:_:): an outcome named and worded by id.
func menuOutcome(id string, then Then) Outcome {
	return Outcome{ID: id, Name: id, When: "when " + id, Tone: ToneNeutral, Then: then}
}

// menuBelt is StageMenuTests.belt(trigger:): triage, a fork with Complex (build) and
// Simple (quick-build), verify, and a watch.
func menuBelt(trigger Trigger) Belt {
	agent := func(id, name string, version int, outcomes []Outcome, route *string) Stage {
		return NewStage(StageArgs{ID: id, Type: StageTypeAgent, Name: new(name),
			Agent:    &AgentRef{ID: map[string]string{"triage": "triage", "build": "build", "verify": "verify"}[id], Version: version},
			Outcomes: outcomes, Route: route})
	}
	stages := []Stage{
		agent("triage", "Triage", 1, []Outcome{menuOutcome("go", Then{Action: ThenActionNext})}, nil),
		agent("build", "Build", 1, []Outcome{menuOutcome("built", Then{Action: ThenActionNext})}, new("complex")),
		agent("quick-build", "Quick build", 1, []Outcome{menuOutcome("built", Then{Action: ThenActionNext})}, new("simple")),
		agent("verify", "Verify", 1, []Outcome{menuOutcome("back", Then{Action: ThenActionGoto, To: []string{"build"}})}, nil),
		NewStage(StageArgs{ID: "watch", Type: StageTypeWatch, Name: new("Watch the pull request"), Outcomes: []Outcome{menuOutcome("merged", Then{Action: ThenActionNext})}}),
	}
	stages[1].Agent.ID = "build"
	stages[2].Agent.ID = "build"
	stages[3].Agent.ID = "verify"
	stages[0].Agent.ID = "triage"
	return NewBeltFromTrigger(BeltFromTriggerArgs{
		ID: "draft", Name: "Draft", Trigger: trigger, Repos: []string{"acme/app"}, Stages: stages,
		Forks: []Fork{{ID: "size", Routes: []Route{{ID: "complex", Name: "Complex"}, {ID: "simple", Name: "Simple"}}}},
	})
}

// menuIssuesTrigger is the issues trigger the menu tests use.
func issuesTrigger() Trigger {
	return Trigger{Kind: TriggerKindIssues, Issues: &IssueFilter{}}
}

// presMenu is the menu for stage id on belt, which must exist.
func presMenu(t *testing.T, belt Belt, id string) *StageMenu {
	t.Helper()
	menu := StageMenuMake(belt, id)
	if menu == nil {
		t.Fatalf("no menu for %s", id)
	}
	return menu
}

// optionIDs is the ids of options, in order.
func optionIDs(options []StageMenuOption) []string {
	ids := []string{}
	for _, option := range options {
		ids = append(ids, option.ID)
	}
	return ids
}

// presGotoTarget is the goto target with id, or nil.
func presGotoTarget(menu *StageMenu, id string) *StageMenuGotoTarget {
	for i := range menu.GotoTargets {
		if menu.GotoTargets[i].ID == id {
			return &menu.GotoTargets[i]
		}
	}
	return nil
}

// hasAction reports whether the menu offers the outcome action id.
func hasAction(menu *StageMenu, id string) bool {
	return slices.ContainsFunc(menu.Actions, func(action StageMenuOption) bool { return action.ID == id })
}

// TestAMissingStageHasNoMenu
func TestAMissingStageHasNoMenu(t *testing.T) {
	t.Parallel()
	if StageMenuMake(menuBelt(issuesTrigger()), "nope") != nil {
		t.Fatal("a missing stage has a menu")
	}
}

// TestAGotoTargetOffersTheCounterpartsAndTheOtherRoutesStages
func TestAGotoTargetOffersTheCounterpartsAndTheOtherRoutesStages(t *testing.T) {
	t.Parallel()
	menu := presMenu(t, menuBelt(issuesTrigger()), "verify")
	var ids []string
	for _, target := range menu.GotoTargets {
		ids = append(ids, target.ID)
	}
	if !reflect.DeepEqual(ids, []string{"triage", "build", "quick-build", "watch"}) {
		t.Fatalf("goto ids = %v", ids)
	}
	build := presGotoTarget(menu, "build")
	if build == nil || build.Label != "Build · Complex" {
		t.Fatalf("build target = %+v", build)
	}
	if !reflect.DeepEqual(build.Counterparts, []string{"quick-build"}) {
		t.Fatalf("counterparts = %v", build.Counterparts)
	}
	if len(build.Others) != 1 || build.Others[0].Route.ID != "simple" {
		t.Fatalf("others = %+v", build.Others)
	}
	if !reflect.DeepEqual(build.Others[0].Stages, []StageMenuOption{{ID: "quick-build", Label: "Simple: Quick build"}}) {
		t.Fatalf("other stages = %+v", build.Others[0].Stages)
	}
	if triage := presGotoTarget(menu, "triage"); triage == nil || len(triage.Counterparts) != 0 {
		t.Fatalf("triage counterparts = %+v", triage)
	}
}

// TestAJumpWithinItsOwnForkAsksForNoOtherRoute
func TestAJumpWithinItsOwnForkAsksForNoOtherRoute(t *testing.T) {
	t.Parallel()
	menu := presMenu(t, menuBelt(issuesTrigger()), "build")
	if target := presGotoTarget(menu, "quick-build"); target == nil || len(target.Others) != 0 {
		t.Fatalf("quick-build others = %+v", target)
	}
}

// TestTakingARouteIsOfferedOnlyBeforeAFork
func TestTakingARouteIsOfferedOnlyBeforeAFork(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	before := presMenu(t, belt, "triage")
	if !hasAction(before, "route") {
		t.Fatal("route is not offered before the fork")
	}
	want := []StageMenuOption{{ID: "complex", Label: "Complex"}, {ID: "simple", Label: "Simple"}}
	if !reflect.DeepEqual(before.RoutesAfter, want) {
		t.Fatalf("routes after = %+v", before.RoutesAfter)
	}
	after := presMenu(t, belt, "verify")
	if hasAction(after, "route") {
		t.Fatal("route is offered after the fork")
	}
	if len(after.RoutesAfter) != 0 {
		t.Fatalf("routes after verify = %+v", after.RoutesAfter)
	}
}

// TestASavedRouteOutcomeKeepsTheActionWhereNoForkFollows
func TestASavedRouteOutcomeKeepsTheActionWhereNoForkFollows(t *testing.T) {
	t.Parallel()
	draft := menuBelt(issuesTrigger())
	draft.Stages[3].Outcomes = []Outcome{menuOutcome("take", Then{Action: ThenActionRoute, Routes: []string{"simple"}})}
	menu := presMenu(t, draft, "verify")
	if !hasAction(menu, "route") {
		t.Fatal("the saved route outcome lost its action")
	}
	if !reflect.DeepEqual(optionIDs(menu.RoutePaths), []string{"complex", "simple"}) {
		t.Fatalf("route paths = %v", optionIDs(menu.RoutePaths))
	}
}

// TestAStageOnARouteCanMoveToTheForksRoutesOrOff
func TestAStageOnARouteCanMoveToTheForksRoutesOrOff(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	menu := presMenu(t, belt, "build")
	if menu.Placement == nil || menu.Placement.Off != "None: on the line after the fork" {
		t.Fatalf("placement = %+v", menu.Placement)
	}
	if !reflect.DeepEqual(optionIDs(menu.Placement.Routes), []string{"complex", "simple"}) {
		t.Fatalf("placement routes = %+v", menu.Placement.Routes)
	}
	if presMenu(t, belt, "triage").Placement != nil {
		t.Fatal("a stage off the fork has a placement")
	}
}

// TestMovesFollowTheLine
func TestMovesFollowTheLine(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	if presMenu(t, belt, "triage").CanMoveEarlier {
		t.Fatal("triage can move earlier")
	}
	if presMenu(t, belt, "watch").CanMoveLater {
		t.Fatal("watch can move later")
	}
	if !presMenu(t, belt, "verify").CanMoveEarlier {
		t.Fatal("verify cannot move earlier")
	}
}

// TestWorkspacesAndWatchTargetsFollowWhatTheTriggersProvide
func TestWorkspacesAndWatchTargetsFollowWhatTheTriggersProvide(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	issues := presMenu(t, belt, "watch")
	if !reflect.DeepEqual(optionIDs(issues.WatchTargets), []string{"pullRequest", "ticket"}) {
		t.Fatalf("issue watch targets = %v", optionIDs(issues.WatchTargets))
	}
	var events []string
	for _, event := range WatchTargetPullRequest.Events() {
		events = append(events, event.When)
	}
	if !reflect.DeepEqual(optionIDs(issues.WatchEvents), events) {
		t.Fatalf("watch events = %v, want %v", optionIDs(issues.WatchEvents), events)
	}
	if issues.WatchNote == nil || !strings.HasPrefix(*issues.WatchNote, "Watches the run's pull request") {
		t.Fatalf("watch note = %v", issues.WatchNote)
	}
	manual := presMenu(t, menuBelt(Trigger{Kind: TriggerKindManual}), "watch")
	if !reflect.DeepEqual(optionIDs(manual.WatchTargets), []string{"pullRequest"}) {
		t.Fatalf("manual watch targets = %v", optionIDs(manual.WatchTargets))
	}
	agent := presMenu(t, belt, "triage")
	var wantWorkspaces []string
	inputs := belt.MenuInputs()
	for _, workspace := range AllWorkspace {
		if InputsSubset(workspace.Needs(), inputs) {
			wantWorkspaces = append(wantWorkspaces, string(workspace))
		}
	}
	if !reflect.DeepEqual(optionIDs(agent.Workspaces), wantWorkspaces) {
		t.Fatalf("workspaces = %v, want %v", optionIDs(agent.Workspaces), wantWorkspaces)
	}
	if len(agent.WatchTargets) != 0 || agent.WatchNote != nil {
		t.Fatal("an agent stage offers watch targets")
	}
}

// TestEveryRunnerListsTheThinkingItTakes
func TestEveryRunnerListsTheThinkingItTakes(t *testing.T) {
	t.Parallel()
	menu := presMenu(t, menuBelt(issuesTrigger()), "triage")
	var runners, thinking []string
	for _, backend := range AllBackend {
		runners = append(runners, string(backend))
	}
	for _, level := range AllThinking {
		thinking = append(thinking, string(level))
	}
	var gotRunners []string
	for _, runner := range menu.Runners {
		gotRunners = append(gotRunners, runner.ID)
		if !reflect.DeepEqual(optionIDs(runner.Thinking), thinking) || runner.ThinkingNote != nil {
			t.Fatalf("runner %s thinking = %v note = %v", runner.ID, optionIDs(runner.Thinking), runner.ThinkingNote)
		}
	}
	if !reflect.DeepEqual(gotRunners, runners) {
		t.Fatalf("runners = %v, want %v", gotRunners, runners)
	}
	if menu.DefaultRunner != "pi" {
		t.Fatalf("default runner = %q", menu.DefaultRunner)
	}
}

// TestThePreviewMatchesTheStageKind
func TestThePreviewMatchesTheStageKind(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	agent := presMenu(t, belt, "triage")
	if agent.Preview.Title != "Added to the end of the brief" {
		t.Fatalf("agent preview title = %q", agent.Preview.Title)
	}
	if strings.Join(agent.Preview.Lines, "\n") != BriefFooter(belt.Stages[0], belt.Stages[0].Writes()) {
		t.Fatal("agent preview lines do not match the brief footer")
	}
	if agent.WhenTitle != "Pick it when" || agent.Kind != "Agent stage" || agent.Number != 1 {
		t.Fatalf("agent = %+v", agent)
	}
	watch := presMenu(t, belt, "watch")
	if watch.Preview.Title != "How Beltline decides" {
		t.Fatalf("watch preview title = %q", watch.Preview.Title)
	}
	if !reflect.DeepEqual(watch.Preview.Lines, []string{"When when merged → merged"}) {
		t.Fatalf("watch preview lines = %v", watch.Preview.Lines)
	}
	if watch.WhenTitle != "Happens when" || watch.Kind != "Watcher" {
		t.Fatalf("watch = %+v", watch)
	}
}

// TestANestedRouteIsNamedByItsWholePath
func TestANestedRouteIsNamedByItsWholePath(t *testing.T) {
	t.Parallel()
	draft := menuBelt(issuesTrigger())
	draft.ForkAround("build")
	inner := draft.Forks[len(draft.Forks)-1]
	menu := presMenu(t, draft, "triage")
	var path string
	for _, option := range menu.RoutePaths {
		if option.ID == inner.Routes[0].ID {
			path = option.Label
		}
	}
	if want := "Complex / " + inner.Routes[0].Name; path != want {
		t.Fatalf("nested route path = %q, want %q", path, want)
	}
}

// TestDoneAndShipAreOfferedOnEveryStageKind checks that the outcome menu offers Ship beside Done
// for an agent, an approval and a watch stage.
func TestDoneAndShipAreOfferedOnEveryStageKind(t *testing.T) {
	t.Parallel()
	belt := menuBelt(issuesTrigger())
	belt.Stages = append(belt.Stages, NewStage(StageArgs{ID: "approve", Type: StageTypeApprove, Name: new("Approve"),
		Outcomes: []Outcome{menuOutcome("approved", Then{Action: ThenActionNext})}}))
	for _, id := range []string{"build", "approve", "watch"} {
		menu := StageMenuMake(belt, id)
		if menu == nil {
			t.Fatalf("%s has no menu", id)
		}
		var ids []string
		for _, action := range menu.Actions {
			ids = append(ids, action.ID)
		}
		if !slices.Contains(ids, string(ThenActionDone)) || !slices.Contains(ids, string(ThenActionShip)) {
			t.Errorf("%s offers %v, want done and ship", id, ids)
		}
	}
}

// TestShipAndDoneDescribeTheirDestinations checks that the outcome's line names each destination.
func TestShipAndDoneDescribeTheirDestinations(t *testing.T) {
	t.Parallel()
	layout := BeltLayout{}
	if got := EngineDescribe(NewThen(ThenArgs{Action: ThenActionDone}), layout, "build", nil); got != "Done" {
		t.Errorf("done describes as %q", got)
	}
	if got := EngineDescribe(NewThen(ThenArgs{Action: ThenActionShip}), layout, "build", nil); got != "Shipped" {
		t.Errorf("ship describes as %q", got)
	}
}
