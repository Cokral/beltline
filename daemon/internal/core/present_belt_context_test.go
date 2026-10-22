// Tests of the belt context presentation, and of the editor showing both routes as taken
// and offering no all-routes option.

package core

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// beltContextTestOutcome is an outcome named id that does what then says.
func beltContextTestOutcome(id string, then Then) Outcome {
	return Outcome{ID: id, Name: id, When: "when " + id, Tone: ToneGood, Then: then}
}

// beltContextTestBelt is the editor's test belt: a manual trigger's belt with three stages, a
// fork named size and a schedule entry, with the policies every new belt starts with.
func beltContextTestBelt() Belt {
	triage := NewStage(StageArgs{ID: "triage", Type: StageTypeAgent, Name: new("Triage"), Outcomes: []Outcome{
		beltContextTestOutcome("next", Then{Action: ThenActionNext}),
		beltContextTestOutcome("jump", Then{Action: ThenActionGoto, To: []string{"verify"}}),
		beltContextTestOutcome("take", Then{Action: ThenActionRoute, Routes: []string{"simple"}}),
		beltContextTestOutcome("hold", Then{Action: ThenActionHold, Comment: true}),
	}})
	build := NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Name: new("Build"), Route: new("simple"), Outcomes: []Outcome{
		beltContextTestOutcome("done", Then{Action: ThenActionDone}),
	}})
	verify := NewStage(StageArgs{ID: "verify", Type: StageTypeAgent, Name: new("Verify"), Outcomes: []Outcome{
		beltContextTestOutcome("ok", Then{Action: ThenActionNext}),
	}})
	belt := DefaultBelt("b", "B", []TriggerEntry{{ID: "1", Trigger: TriggerKindManual.Fresh()}}, []Stage{triage, build, verify})
	belt.Forks = []Fork{{ID: "size", Routes: []Route{{ID: "complex", Name: "Complex"}, {ID: "simple", Name: "Simple"}}}}
	task := DefaultScheduledTask()
	task.Schedule = Schedule{Days: ScheduleDaysWeekdays, Weekday: 2, Hour: 7, Minute: 30}
	task.Title = "Sweep"
	task.Task = "Run it"
	belt.Triggers = []TriggerEntry{{ID: "1", Trigger: Trigger{Kind: TriggerKindSchedule, Schedule: &task}, Repos: []string{}}}
	return belt
}

// beltContextTestBeltOf is a belt whose entries are kinds, each with the fresh settings of its kind
// and the repository acme/app, as the policy tests build them.
func beltContextTestBeltOf(kinds ...Trigger) Belt {
	entries := []TriggerEntry{}
	for i, trigger := range kinds {
		entries = append(entries, TriggerEntry{ID: strconv.Itoa(i + 1), Trigger: trigger, Repos: []string{"acme/app"}})
	}
	return DefaultBelt("b", "B", entries, []Stage{})
}

// beltContextTestKeys are the policy option keys of a list, in order; never nil.
func beltContextTestKeys(options []BeltContextPresentationPolicyOption) []string {
	keys := []string{}
	for _, option := range options {
		keys = append(keys, option.Key)
	}
	return keys
}

func TestOutcomesNameTheStagesAndRoutesTheyReach(t *testing.T) {
	t.Parallel()
	labels := map[string]string{}
	for _, preview := range BeltContextPresentationOutcomes(beltContextTestBelt()) {
		labels[preview.Outcome] = preview.Label
	}
	want := map[string]string{"next": "Next stage", "jump": "Go to Verify", "take": "Take Simple", "hold": "Hold for me", "done": "Done"}
	for outcome, label := range want {
		if labels[outcome] != label {
			t.Errorf("label of %s = %q, want %q", outcome, labels[outcome], label)
		}
	}
}

func TestScheduleTriggersPreviewTheirSummary(t *testing.T) {
	t.Parallel()
	previews := BeltContextPresentationSchedules(beltContextTestBelt(), nil, nil)
	want := []BeltContextPresentationSchedulePreview{{Entry: "1", Summary: "weekdays at 07:30", Note: SchedulePreviewMissedTimes}}
	if !reflect.DeepEqual(previews, want) {
		t.Errorf("schedules = %+v, want %+v", previews, want)
	}
}

func TestPolicyOptionsAreTheBeltsPolicies(t *testing.T) {
	t.Parallel()
	belt := beltContextTestBelt()
	belt.Policies.CommentOnHold = false
	options := BeltContextPresentationPolicies(belt)
	if want := []string{"retriageOnEdit", "stopOnClose", "commentOnHold", "waitForBlockers"}; !reflect.DeepEqual(beltContextTestKeys(options), want) {
		t.Errorf("keys = %v, want %v", beltContextTestKeys(options), want)
	}
	for _, option := range options {
		if option.Key == "commentOnHold" && option.Enabled {
			t.Errorf("commentOnHold enabled, want disabled")
		}
		if option.Key == "stopOnClose" && !option.Enabled {
			t.Errorf("stopOnClose disabled, want enabled")
		}
	}
}

func TestRoutesAndStagesAreTheMenusTheEditorOffers(t *testing.T) {
	t.Parallel()
	belt := beltContextTestBelt()
	routes := BeltContextPresentationRoutes(belt)
	ids := []string{}
	for _, route := range routes {
		ids = append(ids, route.ID)
	}
	if !reflect.DeepEqual(ids, []string{"complex", "simple"}) {
		t.Fatalf("routes = %+v, want complex then simple", routes)
	}
	if routes[0].Fork != "size" {
		t.Errorf("first route fork = %q, want size", routes[0].Fork)
	}
	names := []string{}
	for _, stage := range BeltContextPresentationStages(belt) {
		names = append(names, stage.Name)
	}
	if !reflect.DeepEqual(names, []string{"Triage", "Build", "Verify"}) {
		t.Errorf("stage names = %v, want Triage Build Verify", names)
	}
}

func TestAGotoWithNoTargetFallsBackToTheActionLabel(t *testing.T) {
	t.Parallel()
	if got := BeltContextPresentationLabel(Then{Action: ThenActionGoto}, beltContextTestBelt()); got != "Go to a stage" {
		t.Errorf("label = %q, want Go to a stage", got)
	}
}

func TestThePolicySectionIsWordedForTheTriggerKinds(t *testing.T) {
	t.Parallel()
	issues := BeltContextPresentationPolicySectionOf(beltContextTestBeltOf(TriggerKindIssues.Fresh()))
	if issues == nil {
		t.Fatal("issues: no policy section")
	}
	if issues.Heading != "While an issue is on the belt" {
		t.Errorf("heading = %q", issues.Heading)
	}
	if want := []string{"retriageOnEdit", "stopOnClose", "commentOnHold", "waitForBlockers"}; !reflect.DeepEqual(beltContextTestKeys(issues.Options), want) {
		t.Errorf("keys = %v, want %v", beltContextTestKeys(issues.Options), want)
	}
	if got := issues.Options[1].Label; got != "Issue closed or label removed → stop its work, unless it reached a watch" {
		t.Errorf("stop label = %q", got)
	}
	if issues.Note == nil || !strings.HasPrefix(*issues.Note, "A “base:” label") {
		t.Errorf("note = %v, want the base label note", issues.Note)
	}

	reviews := BeltContextPresentationPolicySectionOf(beltContextTestBeltOf(TriggerKindReviews.Fresh()))
	if reviews == nil {
		t.Fatal("reviews: no policy section")
	}
	if want := []string{"retriageOnEdit", "stopOnClose", "commentOnHold"}; !reflect.DeepEqual(beltContextTestKeys(reviews.Options), want) {
		t.Errorf("review keys = %v, want %v", beltContextTestKeys(reviews.Options), want)
	}
	if got := reviews.Options[0].Label; got != "Pull request edited → review it again" {
		t.Errorf("review retriage label = %q", got)
	}
	if reviews.Note != nil {
		t.Errorf("review note = %q, want none", *reviews.Note)
	}

	mixed := BeltContextPresentationPolicySectionOf(beltContextTestBeltOf(TriggerKindReviews.Fresh(), TriggerKindJira.Fresh()))
	if mixed == nil {
		t.Fatal("mixed: no policy section")
	}
	if mixed.Heading != "While a subject is on the belt" {
		t.Errorf("mixed heading = %q", mixed.Heading)
	}
	if got := mixed.Options[1].Label; got != "Closed, done, or no longer matching → stop its work, unless it reached a watch" {
		t.Errorf("mixed stop label = %q", got)
	}
	found := false
	for _, key := range beltContextTestKeys(mixed.Options) {
		found = found || key == "waitForBlockers"
	}
	if !found {
		t.Errorf("mixed keys = %v, want waitForBlockers", beltContextTestKeys(mixed.Options))
	}
}

func TestSchedulesAndManualTasksHaveNoPolicies(t *testing.T) {
	t.Parallel()
	if section := BeltContextPresentationPolicySectionOf(beltContextTestBeltOf(TriggerKindManual.Fresh(), TriggerKindSchedule.Fresh())); section != nil {
		t.Errorf("policy section = %+v, want nil", *section)
	}
}

func TestASchedulePreviewNamesItsNextRunFromTheClockItIsGiven(t *testing.T) {
	t.Parallel()
	task := DefaultScheduledTask()
	task.Schedule = Schedule{Days: ScheduleDaysEveryDay, Weekday: 2, Hour: 9, Minute: 30}
	task.Title = "Sweep"
	task.Task = "Sweep it"
	belt := beltContextTestBeltOf(Trigger{Kind: TriggerKindSchedule, Schedule: &task})
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	previews := BeltContextPresentationSchedules(belt, &now, time.UTC)
	if len(previews) != 1 {
		t.Fatalf("previews = %d, want 1", len(previews))
	}
	preview := previews[0]
	want := time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC)
	if preview.Next == nil || !preview.Next.Equal(want) {
		t.Errorf("next = %v, want %v", preview.Next, want)
	}
	if preview.Summary != "every day at 09:30" {
		t.Errorf("summary = %q", preview.Summary)
	}
	if preview.Note != SchedulePreviewMissedTimes {
		t.Errorf("note = %q", preview.Note)
	}
	if without := BeltContextPresentationSchedules(belt, nil, nil); without[0].Next != nil {
		t.Errorf("next without a clock = %v, want nil", without[0].Next)
	}
}

func TestTheTriggerFormOffersTheNativeChoices(t *testing.T) {
	t.Parallel()
	form := TriggerFormPresentationMake(beltContextTestBeltOf(TriggerKindIssues.Fresh()))
	kindIDs := []string{}
	for _, kind := range form.Kinds {
		kindIDs = append(kindIDs, kind.ID)
	}
	allKinds := []string{}
	for _, kind := range AllTriggerKind {
		allKinds = append(allKinds, string(kind))
	}
	if !reflect.DeepEqual(kindIDs, allKinds) {
		t.Errorf("kinds = %v, want %v", kindIDs, allKinds)
	}
	if form.Heading != "When a GitHub issue matches" {
		t.Errorf("heading = %q", form.Heading)
	}
	pollValues := []string{}
	for _, choice := range form.PollChoices {
		pollValues = append(pollValues, choice.Value)
	}
	if !reflect.DeepEqual(pollValues, []string{"30", "60", "300", "900"}) {
		t.Errorf("poll values = %v", pollValues)
	}
	assignees := []string{}
	for _, choice := range form.Assignees {
		assignees = append(assignees, choice.Value)
	}
	if !reflect.DeepEqual(assignees, []string{"anyone", "me"}) {
		t.Errorf("assignees = %v", assignees)
	}
	reviewers := []string{}
	for _, choice := range form.Reviewers {
		reviewers = append(reviewers, choice.Label)
	}
	if !reflect.DeepEqual(reviewers, []string{"Me", "Me or my teams"}) {
		t.Errorf("reviewers = %v", reviewers)
	}
	if len(form.Minutes) != 12 || len(form.Hours) != 24 || form.Weekdays[0].Label != "Sunday" {
		t.Errorf("minutes %d, hours %d, first weekday %q", len(form.Minutes), len(form.Hours), form.Weekdays[0].Label)
	}
	for _, kind := range form.Kinds {
		if kind.ID == "schedule" && !kind.OneRepo {
			t.Errorf("schedule kind is not one repository")
		}
		if kind.ID == "jira" && kind.RepositoriesTitle != "Repositories agents may work in" {
			t.Errorf("jira repositories title = %q", kind.RepositoriesTitle)
		}
	}
	if several := TriggerFormPresentationMake(beltContextTestBeltOf(TriggerKindManual.Fresh(), TriggerKindManual.Fresh())); several.Heading != "Every one feeds this belt" {
		t.Errorf("several heading = %q", several.Heading)
	}
}

func TestTheEditorShowsBothRoutesAsTakenAndOffersNoAllRoutes(t *testing.T) {
	t.Parallel()
	belt := nfBothRoutesOf([]string{"build"})
	belt.Stages[0].Outcomes[0].Then = Then{Action: ThenActionRoute, Routes: []string{"complex", "simple"}}
	taken := map[string]bool{}
	for _, id := range belt.Stages[0].Outcomes[0].Then.Routes {
		taken[id] = true
	}
	fork := belt.Forks[0]
	if len(taken) != len(fork.Routes) {
		t.Fatalf("taken routes = %v, fork routes = %d", taken, len(fork.Routes))
	}
	for _, route := range fork.Routes {
		if !taken[route.ID] {
			t.Errorf("route %s is not taken", route.ID)
		}
	}
	if nfContains(nfWarnings(belt), "Nothing takes") {
		t.Errorf("warnings = %v, want no Nothing takes warning", nfWarnings(belt))
	}
}
