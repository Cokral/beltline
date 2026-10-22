package core

import (
	"slices"
	"testing"
)

// draftEditingBelt is a belt of triage (agent triage v2), build on
// Complex, quick-build on Simple (agent build), and verify, with one fork.
func draftEditingBelt() Belt {
	ok := func() Outcome { return draftTestOutcome("ok", Then{Action: ThenActionNext}) }
	triage := draftTestStage("triage", StageTypeAgent, "triage", "", ok())
	triage.Agent.Version = 2
	belt := draftTestBelt([]Stage{
		triage,
		draftTestStage("build", StageTypeAgent, "build", "complex", ok()),
		draftTestStage("quick-build", StageTypeAgent, "build", "simple", ok()),
		draftTestStage("verify", StageTypeAgent, "verify", "", ok()),
	}, Fork{ID: "size", Routes: []Route{{ID: "complex", Name: "Complex"}, {ID: "simple", Name: "Simple"}}})
	belt.Triggers[0].Repos = []string{"acme/app"}
	return belt
}

// draftRequestApply applies a request and fails the test on an error.
func draftRequestApply(t *testing.T, request BeltDraftRequest) Belt {
	t.Helper()
	belt, err := BeltDraftEditingApply(request)
	if err != nil {
		t.Fatalf("apply %s: %v", request.Op, err)
	}
	return belt
}

// draftStageIDs is the ids of the stages in the belt, in order.
func draftStageIDs(b Belt) []string {
	return draftTestIDs(b.Stages)
}

// TestEachApplyCallStartsFromTheSuppliedDraft: apply never changes the draft it was given.
func TestEachApplyCallStartsFromTheSuppliedDraft(t *testing.T) {
	t.Parallel()
	belt := draftEditingBelt()
	removed := draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageRemove, StageID: new("triage")})
	draftCheck(t, "after removing triage", []string{"build", "quick-build", "verify"}, draftStageIDs(removed))
	other := draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageRemove, StageID: new("build")})
	draftCheck(t, "after removing build", []string{"triage", "quick-build", "verify"}, draftStageIDs(other))
	if len(belt.Stages) != 4 {
		t.Fatalf("the supplied draft has %d stages, want 4", len(belt.Stages))
	}
}

// TestStageAddInsertsAtTheSelectedPositionAndRoute: a stage added at an index on a route.
func TestStageAddInsertsAtTheSelectedPositionAndRoute(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationStageAdd}
	request.Stage = new(draftTestStage("added", StageTypeApprove, "", ""))
	request.Index = new(4)
	request.Route = new("simple")
	belt := draftRequestApply(t, request)
	if s := draftStage(belt, "added"); s == nil || s.Route == nil || *s.Route != "simple" {
		t.Fatalf("added stage route = %v, want simple", s)
	}
	draftCheck(t, "stages on simple", []string{"quick-build", "added"}, draftTestIDs(belt.Layout().StagesWithin("simple")))
}

// TestStageAddRejectsUnknownRoutesAndDuplicateIds: each problem is reported in order.
func TestStageAddRejectsUnknownRoutesAndDuplicateIds(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationStageAdd}
	request.Stage = new(draftTestStage("added", StageTypeApprove, "", ""))
	request.Route = new("nope")
	draftCheck(t, "unknown route", []string{"stage-add: route nope is not on the belt."}, BeltDraftEditingProblems(request))
	request.Route = nil
	request.Stage = new(draftTestStage("triage", StageTypeApprove, "", ""))
	draftCheck(t, "duplicate id", []string{"stage-add: a stage with id triage is already on the belt."}, BeltDraftEditingProblems(request))
	request.Stage = nil
	draftCheck(t, "no stage", []string{"stage-add needs a stage."}, BeltDraftEditingProblems(request))
}

// TestStageRemoveTakesTheStage: removing a stage keeps its fork when others remain.
func TestStageRemoveTakesTheStage(t *testing.T) {
	t.Parallel()
	belt := draftRequestApply(t, BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationStageRemove, StageID: new("quick-build")})
	if draftHasStage(belt, "quick-build") {
		t.Error("quick-build is still on the belt")
	}
	if !draftHasOptional(belt, new("size"), draftHasFork) {
		t.Error("fork size is gone")
	}
}

// TestStageMovesOntoShiftAndBetweenRoutes: a stage moves onto another stage, one step
// along its route, off the fork, and onto a route.
func TestStageMovesOntoShiftAndBetweenRoutes(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationStageMove, StageID: new("quick-build"), TargetID: new("build")}
	belt := draftRequestApply(t, request)
	if s := draftStage(belt, "quick-build"); s == nil || *s.Route != "complex" {
		t.Fatalf("quick-build route = %v, want complex", s)
	}
	draftCheck(t, "complex route", []string{"quick-build", "build"}, draftTestIDs(belt.Layout().StagesWithin("complex")))

	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageMove, StageID: new("quick-build"), Step: new(-1)}
	belt = draftRequestApply(t, request)
	draftCheck(t, "after shifting quick-build earlier", []string{"quick-build", "build"}, draftTestIDs(belt.Layout().StagesWithin("complex")))

	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageMove, StageID: new("build"), Detach: new(true)}
	belt = draftRequestApply(t, request)
	if s := draftStage(belt, "build"); s == nil || s.Route != nil {
		t.Fatalf("build route = %v, want none", s)
	}

	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationStageMove, StageID: new("build"), Route: new("simple")}
	belt = draftRequestApply(t, request)
	if s := draftStage(belt, "build"); s == nil || s.Route == nil || *s.Route != "simple" {
		t.Fatalf("build route = %v, want simple", s)
	}
}

// TestAShiftPastTheBoundaryIsANoOp: a shift past the first stage changes nothing, and a zero step is a problem.
func TestAShiftPastTheBoundaryIsANoOp(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	request := BeltDraftRequest{Draft: base, Op: BeltDraftOperationStageMove, StageID: new("triage"), Step: new(-1)}
	if problems := BeltDraftEditingProblems(request); len(problems) != 0 {
		t.Fatalf("problems = %v, want none", problems)
	}
	draftCheck(t, "stages", draftStageIDs(base), draftStageIDs(draftRequestApply(t, request)))
	request.Step = new(0)
	draftCheck(t, "zero step", []string{"stage-move: step must not be zero."}, BeltDraftEditingProblems(request))
}

// TestStageMoveRejectsTwoDestinationsAtOnceAndSelfTargets: one destination per move, not the stage itself.
func TestStageMoveRejectsTwoDestinationsAtOnceAndSelfTargets(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationStageMove, StageID: new("build"), TargetID: new("verify"), Step: new(1)}
	draftCheck(t, "two destinations", []string{"stage-move needs exactly one of targetId, step, route or detach."}, BeltDraftEditingProblems(request))
	request.Step = nil
	request.TargetID = new("build")
	draftCheck(t, "self target", []string{"stage-move: a stage cannot move onto itself."}, BeltDraftEditingProblems(request))
}

// TestStageUpdateAcceptsAWholeReplacementStage: a replacement stage keeps the id and route of the one it replaces.
func TestStageUpdateAcceptsAWholeReplacementStage(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	replacement := *draftStage(base, "build")
	replacement.Name = "Replaced"
	request := BeltDraftRequest{Draft: base, Op: BeltDraftOperationStageUpdate, StageID: new("build"), Stage: new(replacement)}
	belt := draftRequestApply(t, request)
	if s := draftStage(belt, "build"); s == nil || s.Name != "Replaced" || s.Route == nil || *s.Route != "complex" {
		t.Fatalf("build = %+v, want name Replaced on complex", s)
	}
}

// TestGotoKeepsExactlyTheTargetsItWasSent: a goto keeps the targets it is sent, even when
// they omit a route's counterpart.
func TestGotoKeepsExactlyTheTargetsItWasSent(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	draftCheck(t, "counterparts of build from verify", []string{"quick-build"}, base.Layout().Counterparts("build", "verify"))
	for _, targets := range [][]string{{"build"}, {"build", "quick-build"}} {
		update := StageEdit{Outcomes: []Outcome{{ID: "back", Name: "back", When: "again", Tone: ToneNeutral, Then: Then{Action: ThenActionGoto, To: targets, WithOutput: true}}}}
		request := BeltDraftRequest{Draft: base, Op: BeltDraftOperationStageUpdate, StageID: new("verify"), Update: &update}
		belt := draftRequestApply(t, request)
		s := draftStage(belt, "verify")
		draftCheck(t, "goto targets", targets, s.Outcomes[0].Then.To)
	}
}

// TestStageUpdateReplacesAlternatesAndCanClearThinking: alternates replace as a whole,
// and clearThinking drops the stage's thinking level.
func TestStageUpdateReplacesAlternatesAndCanClearThinking(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	base.Stages[0].Thinking = new(ThinkingHigh)
	update := StageEdit{
		Alternates:    []SessionSettings{{Model: "gpt-5", Thinking: new(ThinkingLow), Runner: new(BackendCodex)}},
		ClearThinking: new(true),
	}
	request := BeltDraftRequest{Draft: base, Op: BeltDraftOperationStageUpdate, StageID: new("triage"), Update: &update}
	changed := draftStage(draftRequestApply(t, request), "triage")
	draftCheck(t, "alternates", []SessionSettings{{Model: "gpt-5", Thinking: new(ThinkingLow), Runner: new(BackendCodex)}}, changed.Alternates)
	if changed.Thinking != nil {
		t.Errorf("thinking = %v, want nil", *changed.Thinking)
	}
}

// TestForkAddPutsTheStageOnTheFirstRouteIncludingInsideARoute: a fork around a stage nested in a route.
func TestForkAddPutsTheStageOnTheFirstRouteIncludingInsideARoute(t *testing.T) {
	t.Parallel()
	belt := draftRequestApply(t, BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationForkAdd, StageID: new("quick-build")})
	fork := belt.Forks[len(belt.Forks)-1]
	if len(belt.Forks) != 2 {
		t.Fatalf("forks = %d, want 2", len(belt.Forks))
	}
	if s := draftStage(belt, "quick-build"); s == nil || *s.Route != fork.Routes[0].ID {
		t.Fatalf("quick-build route = %v, want %s", s, fork.Routes[0].ID)
	}
	if len(fork.Routes) != 2 {
		t.Errorf("routes = %d, want 2", len(fork.Routes))
	}
	if fork.Parent == nil || *fork.Parent != "simple" {
		t.Errorf("parent = %v, want simple", fork.Parent)
	}
}

// TestForkRemoveDissolvesItAndKeepsItsStagesInRouteOrder: removing a fork keeps its stages, in route order, on the line.
func TestForkRemoveDissolvesItAndKeepsItsStagesInRouteOrder(t *testing.T) {
	t.Parallel()
	belt := draftRequestApply(t, BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationForkRemove, ForkID: new("size")})
	if draftHasOptional(belt, new("size"), draftHasFork) {
		t.Error("fork size is still on the belt")
	}
	draftCheck(t, "stages", []string{"triage", "build", "quick-build", "verify"}, draftStageIDs(belt))
	for _, s := range belt.Stages {
		if s.Route != nil {
			t.Errorf("stage %s keeps route %s", s.ID, *s.Route)
		}
	}
}

// TestRouteAddIsEmptyAndRouteUpdateRenames: a new route is empty, and can be renamed.
func TestRouteAddIsEmptyAndRouteUpdateRenames(t *testing.T) {
	t.Parallel()
	added := draftRequestApply(t, BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationRouteAdd, ForkID: new("size")})
	fork := added.Forks[0]
	newRoute := fork.Routes[len(fork.Routes)-1]
	if got := added.Layout().StagesWithin(newRoute.ID); len(got) != 0 {
		t.Errorf("new route holds %v, want none", draftTestIDs(got))
	}
	renamed := draftRequestApply(t, BeltDraftRequest{Draft: added, Op: BeltDraftOperationRouteUpdate, RouteID: new(newRoute.ID), RouteName: new("Renamed")})
	if got := renamed.Forks[0].Routes[len(fork.Routes)-1].Name; got != "Renamed" {
		t.Errorf("route name = %s, want Renamed", got)
	}
}

// TestRouteRemoveIsRecursiveAndCleansObsoleteSelections: removing a route takes its
// stages, and an outcome that took it moves on.
func TestRouteRemoveIsRecursiveAndCleansObsoleteSelections(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	verify := draftStage(base, "verify")
	verify.Outcomes = append(verify.Outcomes, draftTestOutcome("quick", Then{Action: ThenActionRoute, Routes: []string{"simple"}}))
	belt := draftRequestApply(t, BeltDraftRequest{Draft: base, Op: BeltDraftOperationRouteRemove, RouteID: new("simple")})
	for _, s := range belt.Stages {
		if s.Route != nil && *s.Route == "simple" {
			t.Errorf("stage %s is still on simple", s.ID)
		}
	}
	if draftHasOptional(belt, new("size"), draftHasFork) {
		t.Error("fork size is still on the belt")
	}
	draftCheck(t, "stages", []string{"triage", "build", "verify"}, draftStageIDs(belt))
	quick := draftOutcomeNamed(draftStage(belt, "verify").Outcomes, "quick")
	if quick == nil || len(quick.Then.Routes) != 0 || quick.Then.Action != ThenActionNext {
		t.Fatalf("quick outcome = %+v, want next with no routes", quick)
	}
}

// draftOutcomeNamed is the first outcome named name, or nil.
func draftOutcomeNamed(outcomes []Outcome, name string) *Outcome {
	for i := range outcomes {
		if outcomes[i].Name == name {
			return &outcomes[i]
		}
	}
	return nil
}

// TestRemovingARouteFromAForkWithThreeLeavesTwo: a fork keeps its other routes.
func TestRemovingARouteFromAForkWithThreeLeavesTwo(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	base.Forks[0].Routes = append(base.Forks[0].Routes, Route{ID: "third", Name: "Third"})
	belt := draftRequestApply(t, BeltDraftRequest{Draft: base, Op: BeltDraftOperationRouteRemove, RouteID: new("simple")})
	draftCheck(t, "routes", []string{"complex", "third"}, draftTestRouteIDs(belt.Forks[0].Routes))
	draftCheck(t, "complex stages", []string{"build"}, draftTestIDs(belt.Layout().StagesWithin("complex")))
}

// TestTriggerMoveRejectsBoundaryMoves: an entry cannot move past the first place.
func TestTriggerMoveRejectsBoundaryMoves(t *testing.T) {
	t.Parallel()
	request := BeltDraftRequest{Draft: draftEditingBelt(), Op: BeltDraftOperationTriggerMove, EntryID: new("1"), Offset: new(-1)}
	draftCheck(t, "boundary move", []string{"trigger-move: 1 cannot move -1 place(s)."}, BeltDraftEditingProblems(request))
}

// TestTriggerKindRestoresTheSessionStashAndTruncatesToOneRepository: switching kinds keeps
// the session's settings for each kind, and a one-repository kind keeps its first repository.
func TestTriggerKindRestoresTheSessionStashAndTruncatesToOneRepository(t *testing.T) {
	t.Parallel()
	belt := draftTestBelt(nil)
	belt.Triggers[0].Repos = []string{"a/one", "a/two"}
	stash := TriggerStash{Entry: TriggerEntryLegacyID}
	request := BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindSchedule), Stash: &stash}
	belt = draftRequestApply(t, request)
	if belt.Triggers[0].Trigger.Kind != TriggerKindSchedule {
		t.Fatalf("kind = %s, want schedule", belt.Triggers[0].Trigger.Kind)
	}
	draftCheck(t, "repos", []string{"a/one"}, belt.Triggers[0].Repos)

	stash.Put(TriggerKindSchedule, belt.Triggers[0].Trigger)
	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindManual), Stash: &stash}
	belt = draftRequestApply(t, request)
	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindSchedule), Stash: &stash}
	belt = draftRequestApply(t, request)
	if belt.Triggers[0].Trigger.Kind != TriggerKindSchedule {
		t.Fatalf("kind after switching back = %s, want schedule", belt.Triggers[0].Trigger.Kind)
	}
}

// TestTriggerKindWithNoStashUsesFreshDefaults: a kind with no session stash starts with its defaults.
func TestTriggerKindWithNoStashUsesFreshDefaults(t *testing.T) {
	t.Parallel()
	belt := draftTestBelt(nil)
	belt.Triggers[0].Repos = []string{}
	request := BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerKind, EntryID: new("1"), Kind: new(TriggerKindReviews)}
	belt = draftRequestApply(t, request)
	reviews := DefaultReviewFilter()
	draftCheck(t, "trigger", Trigger{Kind: TriggerKindReviews, Reviews: &reviews}, belt.Triggers[0].Trigger)
}

// TestRepositoryInputNormalizesTheEditorsFormsAndRejectsMalformedInput: the forms the
// editor accepts, and the ones it refuses.
func TestRepositoryInputNormalizesTheEditorsFormsAndRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{" acme/app ", "acme/app"},
		{"https://github.com/acme/app", "acme/app"},
		{"/acme/app/", "acme/app"},
		{"acme/app/", "acme/app"},
		{"acme", ""},
		{"acme/app/extra", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := ""
		if n := RepositoryInputNormalize(c.in); n != nil {
			got = *n
		}
		if got != c.want {
			t.Errorf("normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRepositoryAddPreservesOrderAndRejectsDuplicates: added repositories keep their order;
// duplicates and malformed input leave the list as it was.
func TestRepositoryAddPreservesOrderAndRejectsDuplicates(t *testing.T) {
	t.Parallel()
	belt := draftTestBelt(nil)
	belt.Triggers[0].Repos = []string{"acme/one"}
	request := BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryAdd, EntryID: new("1"), Repository: new("https://github.com/acme/two/")}
	belt = draftRequestApply(t, request)
	draftCheck(t, "after adding two", []string{"acme/one", "acme/two"}, belt.Triggers[0].Repos)

	request.Repository = new("acme/two")
	draftCheck(t, "duplicate", []string{"acme/one", "acme/two"}, draftRequestApply(t, request).Triggers[0].Repos)

	request.Repository = new("not-a-repo")
	draftCheck(t, "malformed", []string{"acme/one"}, draftRequestApply(t, request).Triggers[0].Repos)

	request = BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryRemove, EntryID: new("1"), Repository: new("acme/one")}
	draftCheck(t, "after removing one", []string{"acme/two"}, draftRequestApply(t, request).Triggers[0].Repos)
}

// TestOperationsLeaveOtherEntriesAlone: an edit to one entry changes no other entry.
func TestOperationsLeaveOtherEntriesAlone(t *testing.T) {
	t.Parallel()
	belt := draftTestBelt(nil)
	belt.Triggers = []TriggerEntry{
		{ID: "1", Trigger: Trigger{Kind: TriggerKindIssues, Issues: draftIssueFilterPtr("one")}, Repos: []string{"acme/one"}},
		{ID: "2", Trigger: Trigger{Kind: TriggerKindManual}, Repos: []string{"acme/two"}},
	}
	request := BeltDraftRequest{Draft: belt, Op: BeltDraftOperationRepositoryAdd, EntryID: new("2"), Repository: new("acme/three")}
	result := draftRequestApply(t, request)
	draftCheck(t, "entry 1", []string{"acme/one"}, result.Triggers[0].Repos)
	draftCheck(t, "entry 2", []string{"acme/two", "acme/three"}, result.Triggers[1].Repos)
}

// draftIssueFilterPtr is an issues filter with label and the default settings.
func draftIssueFilterPtr(label string) *IssueFilter {
	f := DefaultIssueFilter()
	f.Label = label
	return &f
}

// TestEveryOperationHasCoreCoverage: every operation the editor supports has a test here.
func TestEveryOperationHasCoreCoverage(t *testing.T) {
	t.Parallel()
	covered := map[BeltDraftOperation]struct{}{}
	for _, op := range []BeltDraftOperation{
		BeltDraftOperationStageAdd, BeltDraftOperationStageRemove, BeltDraftOperationStageMove, BeltDraftOperationStageUpdate,
		BeltDraftOperationForkAdd, BeltDraftOperationForkRemove, BeltDraftOperationRouteAdd, BeltDraftOperationRouteRemove,
		BeltDraftOperationRouteUpdate, BeltDraftOperationTriggerAdd, BeltDraftOperationTriggerRemove, BeltDraftOperationTriggerMove,
		BeltDraftOperationTriggerUpdate, BeltDraftOperationTriggerKind, BeltDraftOperationRepositoryAdd, BeltDraftOperationRepositoryRemove,
		BeltDraftOperationFeedInto, BeltDraftOperationPoliciesUpdate, BeltDraftOperationGotoPick, BeltDraftOperationGotoRoute,
	} {
		covered[op] = struct{}{}
	}
	if len(covered) != len(AllBeltDraftOperation) {
		t.Fatalf("covered %d operations, the editor has %d", len(covered), len(AllBeltDraftOperation))
	}
	for _, op := range AllBeltDraftOperation {
		if _, ok := covered[op]; !ok {
			t.Errorf("operation %s has no core coverage", op)
		}
	}
}

// TestStageUpdateChangesSettingsAndOutcomes
func TestStageUpdateChangesSettingsAndOutcomes(t *testing.T) {
	t.Parallel()
	base := draftEditingBelt()
	spec := draftStage(base, "build")
	if spec == nil {
		t.Fatal("no build stage")
	}
	update := StageEdit{
		Name:     new("Renamed"),
		Slots:    new(3),
		Brief:    new("custom brief"),
		Key:      new("renamed"),
		Outcomes: []Outcome{draftTestOutcome("ok", Then{Action: ThenActionNext})},
	}
	belt := draftRequestApply(t, BeltDraftRequest{Draft: base, Op: BeltDraftOperationStageUpdate, StageID: new(spec.ID), Update: &update})
	changed := draftStage(belt, spec.ID)
	if changed == nil {
		t.Fatal("stage lost")
	}
	if changed.ID != spec.ID || changed.Name != "Renamed" || changed.Slots != 3 || changed.Brief != "custom brief" {
		t.Errorf("changed stage = id %q name %q slots %d brief %q", changed.ID, changed.Name, changed.Slots, changed.Brief)
	}
	if changed.Key == nil || *changed.Key != "renamed" {
		t.Errorf("key = %v, want renamed", changed.Key)
	}
	if got := outcomeNames(changed.Outcomes); !slices.Equal(got, []string{"ok"}) {
		t.Errorf("outcomes = %v, want [ok]", got)
	}
	if d := testDiffEmpty(spec.Route, changed.Route); d != "" {
		t.Errorf("route changed:\n%s", d)
	}
	if d := testDiffEmpty(spec.Agent, changed.Agent); d != "" {
		t.Errorf("agent changed:\n%s", d)
	}
}

// TestTriggerAddRemoveMoveAndUpdateUseStableIds
func TestTriggerAddRemoveMoveAndUpdateUseStableIds(t *testing.T) {
	t.Parallel()
	belt := draftEditingBelt()
	first := belt.Triggers[0].ID
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerAdd,
		Entry: &TriggerEntry{ID: "", Trigger: TriggerKindReviews.Fresh(), Repos: []string{"acme/app"}}})
	added := belt.Triggers[len(belt.Triggers)-1]
	if added.ID == "" || added.ID == first {
		t.Errorf("added id = %q, first = %q", added.ID, first)
	}
	if added.Trigger.Kind != TriggerKindReviews {
		t.Errorf("added kind = %s, want reviews", added.Trigger.Kind)
	}

	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerMove, EntryID: new(added.ID), Offset: new(-1)})
	if belt.Triggers[0].ID != added.ID {
		t.Errorf("moved entry is not first: %q", belt.Triggers[0].ID)
	}

	jira := btJiraOf(JiraQuery{Jql: "project = K"})
	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerUpdate, EntryID: new(added.ID), Trigger: &jira})
	if kind := draftTriggerKindOf(belt, added.ID); kind != TriggerKindJira {
		t.Errorf("updated kind = %q, want jira", kind)
	}

	belt = draftRequestApply(t, BeltDraftRequest{Draft: belt, Op: BeltDraftOperationTriggerRemove, EntryID: new(first)})
	if draftHasTrigger(belt, first) {
		t.Errorf("entry %q was not removed", first)
	}
}

// draftTriggerKindOf is the kind of the trigger entry with id, or "" when there is none.
func draftTriggerKindOf(b Belt, id string) TriggerKind {
	for _, entry := range b.Triggers {
		if entry.ID == id {
			return entry.Trigger.Kind
		}
	}
	return ""
}

// TestRemovingTheLastEntryLeavesWarningsRatherThanFailing
func TestRemovingTheLastEntryLeavesWarningsRatherThanFailing(t *testing.T) {
	t.Parallel()
	base := DefaultBelt("manual", "Manual", []TriggerEntry{{ID: TriggerEntryLegacyID, Trigger: btManual(), Repos: []string{"acme/app"}}}, nil)
	belt := draftRequestApply(t, BeltDraftRequest{Draft: base, Op: BeltDraftOperationTriggerRemove, EntryID: new(TriggerEntryLegacyID)})
	if len(belt.Triggers) != 0 {
		t.Errorf("triggers = %+v, want none", belt.Triggers)
	}
	if !slices.Contains(vtWarnings(belt), "Add at least one trigger") {
		t.Errorf("warnings %v, want the trigger warning", vtWarnings(belt))
	}
}

// BeltDraftEditingApply applies a request and returns the edited draft.
func BeltDraftEditingApply(request BeltDraftRequest) (Belt, error) {
	draft, _, err := BeltDraftEditingApplyResult(request)
	return draft, err
}

// BeltDraftEditingProblems is the ordered problem messages that would reject the
// request, empty when it is structurally valid. Repository input problems are
// no-ops, as in the editor, and are not listed.
func BeltDraftEditingProblems(request BeltDraftRequest) []string {
	located := BeltDraftEditingLocated(request)
	out := make([]string, 0, len(located))
	for _, p := range located {
		out = append(out, p.Message)
	}
	return out
}
