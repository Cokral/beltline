// Checks of the validation functions, which call the pure parts directly.

package core

import (
	"fmt"
	"reflect"
	"testing"
)

// TestValidationFailureKeepsProblemsAndLocations checks the three failure constructors
// and Error: a failure without problems reports its message as the one problem.
func TestValidationFailureKeepsProblemsAndLocations(t *testing.T) {
	t.Parallel()
	bare := NewValidationFailure("Nothing to save.", nil)
	if !reflect.DeepEqual(bare.Problems, []string{"Nothing to save."}) || bare.Error() != "Nothing to save." || len(bare.Locations) != 1 {
		t.Fatalf("bare failure %+v", bare)
	}
	stageA := ProblemLocationStage("a")
	located := ValidationFailureLocated([]LocatedProblem{
		{Message: "Give the belt a name.", Location: nil},
		{Message: "Stage a is used twice.", Location: &stageA},
	})
	if located.Error() != "Give the belt a name. Stage a is used twice." {
		t.Fatalf("message %q", located.Error())
	}
	if located.Locations[0] != nil || located.Locations[1] == nil || !reflect.DeepEqual(*located.Locations[1], ProblemLocationStage("a")) {
		t.Fatalf("locations %+v", located.Locations)
	}
}

// TestValidationNamesJoinRouteNames checks "the Simple route" and "the Quick and Simple routes".
func TestValidationNamesJoinRouteNames(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		routes []string
		want   string
	}{
		"one":   {[]string{"Simple"}, "Simple route"},
		"two":   {[]string{"Quick", "Simple"}, "Quick and Simple routes"},
		"three": {[]string{"A", "B", "C"}, "A, B and C routes"},
	}
	for name, c := range cases {
		if got := validationNames(c.routes); got != c.want {
			t.Fatalf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// TestValidationMissingInputMessages checks the reason each input is missing, word for word.
func TestValidationMissingInputMessages(t *testing.T) {
	t.Parallel()
	stage := Stage{Name: "Ship"}
	cases := map[Input]string{
		InputPullRequest: "Ship comes before any stage that changes code, so there is no pull request to watch.",
		InputBaseBranch:  "Ship changes code, but a review works on someone else's pull request. Give it a read-only checkout.",
		InputBranch:      "Ship reads the worktree before any stage changes code, so there is nothing to look at yet.",
		InputTicket:      "Ship watches the issue or ticket, but this belt's runs aren't about one.",
	}
	for input, want := range cases {
		if got := validationMissing(input, stage); got != want {
			t.Fatalf("%s: %q, want %q", input, got, want)
		}
	}
}

// TestValidationGotoChecksItsTargets checks the goto messages that need no stage keys.
func TestValidationGotoChecksItsTargets(t *testing.T) {
	t.Parallel()
	stages := []Stage{
		draftTestStage("triage", StageTypeAgent, "", "", draftTestOutcome("go", Then{Action: ThenActionGoto, To: []string{"triage"}})),
		draftTestStage("build", StageTypeAgent, "", ""),
	}
	layout := draftTestBelt(stages).Layout()
	self := validationGoto(stages[0].Outcomes[0], stages[0], 0, layout)
	// A draft stage's name is its type's title, here "Agent".
	if !reflect.DeepEqual(self, []string{"Agent: “go” goes to itself. Use “Run again” instead."}) {
		t.Fatalf("self goto %v", self)
	}
	missing := validationGoto(draftTestOutcome("jump", Then{Action: ThenActionGoto, To: []string{"nowhere"}}), stages[1], 1, layout)
	if !reflect.DeepEqual(missing, []string{"Agent: “jump” goes to a stage that isn't on this belt anymore."}) {
		t.Fatalf("missing target %v", missing)
	}
	none := validationGoto(draftTestOutcome("jump", Then{Action: ThenActionGoto}), stages[1], 1, layout)
	if !reflect.DeepEqual(none, []string{"Agent: “jump” goes to a stage, but names none."}) {
		t.Fatalf("no target %v", none)
	}
}

// TestValidationSourceWarningsCheckEachKind checks the source checks of each trigger kind.
func TestValidationSourceWarningsCheckEachKind(t *testing.T) {
	t.Parallel()
	blankJira := DefaultJiraQuery()
	blankJira.Jql = " "
	jira := Trigger{Kind: TriggerKindJira, Jira: &blankJira}
	if got := ValidationSourceWarnings(jira, []string{"a/b", "c/d"}); !reflect.DeepEqual(got, []string{"Write the JQL query that picks the tickets."}) {
		t.Fatalf("jira %v", got)
	}
	if got := ValidationSourceWarnings(jira, nil); !reflect.DeepEqual(got, []string{"Add at least one repository to the trigger.", "Write the JQL query that picks the tickets."}) {
		t.Fatalf("jira without repositories %v", got)
	}

	blankLabel := DefaultIssueFilter()
	blankLabel.Label = ""
	issues := Trigger{Kind: TriggerKindIssues, Issues: &blankLabel}
	if got := ValidationSourceWarnings(issues, []string{"a/b"}); !reflect.DeepEqual(got, []string{"Set the label the trigger watches for."}) {
		t.Fatalf("issues %v", got)
	}

	untitled := DefaultScheduledTask()
	untitled.Title = ""
	untitled.Task = "Fix them."
	schedule := Trigger{Kind: TriggerKindSchedule, Schedule: &untitled}
	if got := ValidationSourceWarnings(schedule, nil); !reflect.DeepEqual(got, []string{"Set the repository the work happens in.", "Give the scheduled task a title."}) {
		t.Fatalf("schedule without repository %v", got)
	}
	if got := ValidationSourceWarnings(schedule, []string{"a/b", "c/d"}); !reflect.DeepEqual(got, []string{
		"A schedule belt works in one repository. Remove the others.", "Give the scheduled task a title.",
	}) {
		t.Fatalf("schedule with two repositories %v", got)
	}

	timed := DefaultScheduledTask()
	timed.Title = "Sweep"
	timed.Task = "Fix"
	timed.Schedule.Hour = 24
	bad := Trigger{Kind: TriggerKindSchedule, Schedule: &timed}
	if got := ValidationSourceWarnings(bad, []string{"a/b"}); !reflect.DeepEqual(got, []string{"The schedule's time isn't a real time of day."}) {
		t.Fatalf("hour 24 %v", got)
	}
	if got := ValidationSourceWarnings(Trigger{Kind: TriggerKindManual}, []string{"a/b", "c/d"}); len(got) != 0 {
		t.Fatalf("manual %v", got)
	}
}

// TestValidationAgentWarningsNameWhatIsWrong checks the agent checks in their order.
func TestValidationAgentWarningsNameWhatIsWrong(t *testing.T) {
	t.Parallel()
	agent := Agent{
		ID: "custom", Name: "Custom", Runner: BackendPi, Slots: 2, Brief: "Do it",
		Outcomes: []AgentOutcome{{Name: "done", Tone: ToneNeutral, Then: Then{Action: ThenActionHold}}},
	}
	clash := Agent{ID: "custom", Name: "Other"}
	want := []string{
		"Another agent already has the id “custom”.",
		"Add a good outcome. The run page uses it to know when the stage went well.",
		"Add an outcome that lets a run move on.",
	}
	if got := ValidationAgentWarnings(agent, []Agent{clash}); !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings %v, want %v", got, want)
	}
	if got := ValidationAgentWarnings(agent, nil); len(got) != 2 {
		t.Fatalf("without the clash %v", got)
	}
	blankID := Agent{ID: "Bad Id", Name: " ", Runner: BackendPi, Slots: 7}
	got := ValidationAgentWarnings(blankID, nil)
	wantStart := []string{
		"The agent's id may only use lowercase letters, digits, - and _.",
		"Give the agent a name.",
		"Allow at most 6 runs at once.",
		"Add at least one outcome.",
		"Write the agent's brief.",
	}
	if !reflect.DeepEqual(got, wantStart) {
		t.Fatalf("blank agent %v, want %v", got, wantStart)
	}
}

// TestValidationThinkingNamesTheRunner checks the thinking message, and that a
// runner with no thinking setting ignores the level.
func TestValidationThinkingNamesTheRunner(t *testing.T) {
	t.Parallel()
	level := ThinkingMax
	only := map[Thinking]struct{}{ThinkingLow: {}}
	want := fmt.Sprintf("Build: %s doesn't take the thinking level %s.", BackendPi.Label(), ThinkingMax.Label())
	if got := validationThinkingWarnings("Build", BackendPi, &level, only); !reflect.DeepEqual(got, []string{want}) {
		t.Fatalf("warnings %v, want %s", got, want)
	}
	if got := validationThinkingWarnings("Build", Backend("unknown"), &level, nil); len(got) != 0 {
		t.Fatalf("a runner without thinking warned: %v", got)
	}
	if got := validationThinkingWarnings("Build", BackendPi, nil, only); len(got) != 0 {
		t.Fatalf("no level warned: %v", got)
	}
}

// TestValidationLooksLikePlaceholderUsesTheSlugCharacters checks the placeholder name rule.
func TestValidationLooksLikePlaceholderUsesTheSlugCharacters(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{"spec": true, "spec-2_x": true, "": false, "Spec": false, "a b": false, "café": false} {
		if got := validationLooksLikePlaceholder(name); got != want {
			t.Fatalf("%q: %v, want %v", name, got, want)
		}
	}
}
