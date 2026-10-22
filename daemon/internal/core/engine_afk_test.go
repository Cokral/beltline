package core

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// afkTestStages are the stages of the belt the AFK tests stop on: triage (an agent
// that holds with "needs info"), spec, build and a pull request watch.
func afkTestStages() []Stage {
	hold := Then{Action: ThenActionHold}
	next := Then{Action: ThenActionNext}
	return []Stage{
		{ID: "triage", Type: StageTypeAgent, Name: "Triage", Outcomes: []Outcome{
			{ID: "triage-ready", Name: "ready to spec", When: "The spec is ready.", Tone: ToneGood, Then: next},
			{ID: "triage-needs", Name: "needs info", When: "It needs more.", Tone: ToneProblem, Then: hold},
		}},
		{ID: "spec", Type: StageTypeAgent, Name: "Spec", Outcomes: []Outcome{{ID: "spec-done", Name: "complete", When: "Done.", Tone: ToneGood, Then: next}}},
		{ID: "build", Type: StageTypeAgent, Name: "Build", Outcomes: []Outcome{{ID: "build-done", Name: "complete", When: "Done.", Tone: ToneGood, Then: next}}},
		{ID: "watch", Type: StageTypeWatch, Name: "Pull request", Outcomes: []Outcome{{ID: "watch-merged", Name: "merged", When: "merged", Tone: ToneGood, Then: next}}},
	}
}

// afkTestRun is a run on afkTestStages at stage, in state, with its attempts.
func afkTestRun(state RunState, stage string, attempts ...Attempt) Run {
	return Run{
		ID: "bug:acme/billing#142", BeltID: "bug", BeltName: "Bug", StageID: stage, State: state,
		Stages:   afkTestStages(),
		Subject:  Subject{Kind: SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Export invoices as CSV"},
		Attempts: attempts,
	}
}

// afkClosedAttempt is an attempt at stage that has ended with outcome (nil for none),
// with an agent session when session is set.
func afkClosedAttempt(stage string, number int, outcome *string, session bool) Attempt {
	ended := jsonx.Date{Time: testT0}
	attempt := Attempt{
		ID: AttemptID(stage, number, nil), StageID: stage, Number: number, Folder: stage + "-" + strconv.Itoa(number),
		Outcome: outcome, EndedAt: &ended,
	}
	if session {
		attempt.SessionID = new("session-" + stage + "-" + strconv.Itoa(number))
	}
	return attempt
}

// afkOn is the default AFK settings with the assistant switched on.
func afkOn() AfkSettings {
	settings := DefaultAfkSettings()
	settings.Enabled = true
	return settings
}

// afkEveryChoice is afkOn with dropping allowed too.
func afkEveryChoice() AfkSettings {
	settings := afkOn()
	settings.Drop = true
	return settings
}

// afkIDs are the ids of a situation's options, in order.
func afkIDs(situation *AfkSituation) []string {
	var ids []string
	for _, option := range situation.Options {
		ids = append(ids, option.ID)
	}
	return ids
}

// TestHeldQuestionsCanBeAnswered
func TestHeldQuestionsCanBeAnswered(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateHeld, "triage", afkClosedAttempt("triage", 1, new("needs info"), true))
	situation := AfkSituationOf(run, afkOn())
	if situation == nil || situation.Kind != AfkSituationKindQuestion {
		t.Fatalf("situation = %+v, want a question", situation)
	}
	if got := afkIDs(situation); !reflect.DeepEqual(got, []string{"answer", "leave"}) {
		t.Fatalf("options = %v", got)
	}
	off := afkOn()
	off.AnswerQuestions = false
	if got := AfkSituationOf(run, off); got != nil {
		t.Fatalf("answering off still offers %+v", got)
	}
}

// TestAFailedStageCanRunAgainOrBeContinued
func TestAFailedStageCanRunAgainOrBeContinued(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateHeld, "triage", afkClosedAttempt("triage", 1, nil, true))
	situation := AfkSituationOf(run, afkOn())
	if situation == nil || situation.Kind != AfkSituationKindFailure {
		t.Fatalf("situation = %+v, want a failure", situation)
	}
	if got := afkIDs(situation); !reflect.DeepEqual(got, []string{"rerun", "answer", "leave"}) {
		t.Fatalf("options = %v", got)
	}
	off := afkOn()
	off.RerunFailures = false
	if got := AfkSituationOf(run, off); got != nil {
		t.Fatalf("rerunning failures off still offers %+v", got)
	}
}

// TestARunYouStoppedIsLeftAlone
func TestARunYouStoppedIsLeftAlone(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateHeld, "triage", afkClosedAttempt("triage", 1, nil, true))
	run.Reason = new("stopped by you")
	if got := AfkSituationOf(run, afkEveryChoice()); got != nil {
		t.Fatalf("a run you stopped has situation %+v", got)
	}
}

// TestAHeldWatchCanWatchAgainOrSendTheRunElsewhere
func TestAHeldWatchCanWatchAgainOrSendTheRunElsewhere(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateHeld, "watch", afkClosedAttempt("watch", 1, new("closed"), false))
	situation := AfkSituationOf(run, afkOn())
	if situation == nil || situation.Kind != AfkSituationKindHold {
		t.Fatalf("situation = %+v, want a hold", situation)
	}
	ids := afkIDs(situation)
	if len(ids) == 0 || ids[0] != "rerun" {
		t.Fatalf("options = %v, want rerun first", ids)
	}
	if !slices.Contains(ids, "send build") {
		t.Fatalf("options = %v, want send build", ids)
	}
	if slices.Contains(ids, "send watch") {
		t.Fatalf("options = %v, must not send the run to the stage it is held at", ids)
	}
	resolve := afkOn()
	resolve.ResolveHolds = false
	if got := AfkSituationOf(run, resolve); got != nil {
		t.Fatalf("resolving holds off still offers %+v", got)
	}
}

// TestRunsThatDontWaitForYouHaveNoSituation
func TestRunsThatDontWaitForYouHaveNoSituation(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateQueued, "triage")
	if got := AfkSituationOf(run, afkEveryChoice()); got != nil {
		t.Fatalf("a queued run has situation %+v", got)
	}
	run.State = RunStateRunning
	run.Attempts = []Attempt{NewAttempt(AttemptArgs{StageID: "triage", Number: 1, StartedAt: jsonx.Date{Time: testT0}, Folder: "f"})}
	if got := AfkSituationOf(run, afkEveryChoice()); got != nil {
		t.Fatalf("a running run has situation %+v", got)
	}
}

// TestANewStopIsANewKey The second stop
// is the same run held again after one more attempt.
func TestANewStopIsANewKey(t *testing.T) {
	t.Parallel()
	first := afkTestRun(RunStateHeld, "triage", afkClosedAttempt("triage", 1, new("needs info"), true))
	second := afkTestRun(RunStateHeld, "triage",
		afkClosedAttempt("triage", 1, new("ready to spec"), true),
		afkClosedAttempt("triage", 2, new("needs info"), true))
	a := AfkSituationOf(first, afkEveryChoice())
	b := AfkSituationOf(second, afkEveryChoice())
	if a == nil || b == nil {
		t.Fatalf("situations = %+v, %+v", a, b)
	}
	if a.Key == b.Key {
		t.Fatalf("both stops have key %q", a.Key)
	}
}

// afkDecisionOptions are the options a decision file may name in the tests.
func afkDecisionOptions() []AfkOption {
	return []AfkOption{
		{ID: "decide approved", Action: AfkAction{Kind: AfkActionKindDecide, Outcome: "approved"}},
		{ID: "answer", Action: AfkAction{Kind: AfkActionKindAnswer}},
		{ID: "leave", Action: AfkAction{Kind: AfkActionKindLeave}},
	}
}

// TestAnUnknownOrMissingDecisionIsNoDecision
func TestAnUnknownOrMissingDecisionIsNoDecision(t *testing.T) {
	t.Parallel()
	options := afkDecisionOptions()
	if got := AfkParse("Decision: merge it", options); got != nil {
		t.Fatalf("unknown decision parsed as %+v", got)
	}
	if got := AfkParse("## Why\n\nNo line.", options); got != nil {
		t.Fatalf("missing decision parsed as %+v", got)
	}
}

// TestEachOptionHasItsEvent
func TestEachOptionHasItsEvent(t *testing.T) {
	t.Parallel()
	options := afkDecisionOptions()
	again := "Again"
	looksRight := " Looks right. "
	spec := "spec"
	cases := []struct {
		name    string
		option  AfkOption
		message *string
		want    *Event
	}{
		{"decide", options[0], new(looksRight), &Event{Kind: EventKindDecided, Outcome: "approved", Note: new("Looks right.")}},
		{"answer with nothing", options[1], new(""), &Event{Kind: EventKindAnswered}},
		{"rerun", AfkOption{ID: "rerun", Action: AfkAction{Kind: AfkActionKindRerun}}, &again, &Event{Kind: EventKindResume, Note: &again, Earlier: true}},
		{"send", AfkOption{ID: "send spec", Action: AfkAction{Kind: AfkActionKindSend, Stage: spec}}, nil, &Event{Kind: EventKindResume, Stage: &spec, Earlier: true}},
		{"drop", AfkOption{ID: "drop", Action: AfkAction{Kind: AfkActionKindDrop}}, nil, &Event{Kind: EventKindDrop, Reason: "dropped by the AFK assistant"}},
		{"leave", options[2], new("x"), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := AfkEventFor(c.option, c.message)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("event = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestAfkBriefRendersItsSections checks the brief of a hand-built approval
// situation: the options, the decision file and the sections.
func TestAfkBriefRendersItsSections(t *testing.T) {
	t.Parallel()
	run := afkTestRun(RunStateWaiting, "watch")
	run.Stages[3] = Stage{ID: "approve", Type: StageTypeApprove, Name: "Approve", Outcomes: []Outcome{
		{ID: "approve-approved", Name: "approved", When: "Ship it", Tone: ToneGood, Then: Then{Action: ThenActionNext}},
	}}
	run.StageID = "approve"
	situation := AfkSituation{Kind: AfkSituationKindApproval, Key: "k", Options: []AfkOption{
		{ID: "decide approved", Action: AfkAction{Kind: AfkActionKindDecide, Outcome: "approved"}, Label: "Choose “approved”"},
		{ID: "leave", Action: AfkAction{Kind: AfkActionKindLeave}, Label: "Leave it for the person to decide"},
	}}
	latest := "# Spec\n\nThe goal."
	brief := AfkBriefRender(run, situation, AfkBriefContext{
		RunFolder: "/runs/x", Decision: "/runs/x/afk/01-approve/decision.md", LatestOutput: &latest, Guidance: "Never approve billing changes.",
	})
	for _, want := range []string{
		"- `decide approved`: Choose “approved”",
		"- `leave`: Leave it for the person to decide",
		"/runs/x/afk/01-approve/decision.md",
		"Never approve billing changes.",
		"# Spec",
		"Export invoices as CSV",
		"Approve is an approval stage: it waits for a decision on the work so far. Its choices:\n\n- approved (Ship it)",
		"Write its words after `Decision:` exactly as shown, such as `Decision: decide approved`.",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q", want)
		}
	}
	const ending = "\n\n## Your decision\n\nWrite it to /runs/x/afk/01-approve/decision.md, in this shape:\n\n```\n## Message\n\n" +
		"What you tell the agent, or the note that goes with your choice. For questions, answer every one; say what you assumed. " +
		"Leave it empty when there is nothing to say.\n\n## Why\n\nYour reasons, in a few sentences, for the person to check when they are back.\n\n" +
		"Decision: <one of the options above, as written>\n```\n\nThe last `Decision:` line decides.\n"
	if !strings.HasSuffix(brief, ending) {
		t.Fatalf("brief ends with %q", brief[max(0, len(brief)-len(ending)-40):])
	}
}

// TestARunAfkChoiceRoundTripsThroughStorage checks that a run's choice is read
// when it is missing or null, and that AfkEffective follows the run's choice before the global switch.
func TestARunAfkChoiceRoundTripsThroughStorage(t *testing.T) {
	t.Parallel()
	object := testRunObject()
	object["afkEnabled"] = true
	if run := testRunFrom(t, object); run.AfkEnabled == nil || !*run.AfkEnabled {
		t.Fatalf("afkEnabled true reads as %v", run.AfkEnabled)
	}
	delete(object, "afkEnabled")
	if run := testRunFrom(t, object); run.AfkEnabled != nil {
		t.Fatalf("missing afkEnabled reads as %v", *run.AfkEnabled)
	}
	object["afkEnabled"] = nil
	if run := testRunFrom(t, object); run.AfkEnabled != nil {
		t.Fatalf("null afkEnabled reads as %v", *run.AfkEnabled)
	}

	run := testRunFrom(t, testRunObject())
	if !run.AfkEffective(true) || run.AfkEffective(false) {
		t.Fatal("an inherited choice follows the global switch")
	}
	run.AfkEnabled = new(false)
	if run.AfkEffective(true) {
		t.Fatal("an explicit off ignores the global switch")
	}
	run.AfkEnabled = new(true)
	if !run.AfkEffective(false) {
		t.Fatal("an explicit on ignores the global switch")
	}
}
