// Tests of stage keys in nested belts: a send follows the chosen route
// (the brief and engine parts).

package core

import (
	"slices"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// nfPlaceholderNotes is the notes placeholder of stage on run.
func nfPlaceholderNotes(run Run, stage Stage, context BriefContext) string {
	return briefPlaceholders(run, stage, context)["notes"]
}

// TestNestedSendToFollowsTheChosenRoute
func TestNestedSendToFollowsTheChosenRoute(t *testing.T) {
	t.Parallel()
	belt := nfBelt(true, false, true)
	belt.Stages[0].Outcomes = []Outcome{{ID: "skip", Name: "skip", When: "skip", Tone: ToneGood, Then: nfRouteThen("skip")}}
	for _, id := range []string{"skip-build", "quick-build", "thorough-build"} {
		stage := &belt.Stages[nfStageIndex(t, belt, id)]
		stage.SetKey(new("notes"))
		stage.SetWrites(id[:len(id)-len("-build")] + ".md")
	}
	run := engineStartRun(belt)
	nfFinish(&run, "skip")
	nfFinish(&run, "done")
	ship := belt.Stages[nfStageIndex(t, belt, "ship")]
	if run.StageID != "ship" {
		t.Fatalf("stage = %q, want ship", run.StageID)
	}
	if got := run.Routes()["wave"]; !slices.Equal(got, []string{"skip"}) {
		t.Fatalf("wave = %v, want [skip]", got)
	}
	folder := "/runs/r"
	context := BriefContext{RunFolder: folder, AttemptFolder: folder + "/attempt", Base: "main", Branch: "b", Agents: placeholderTestAgents()}
	if got := nfPlaceholderNotes(run, ship, context); got != "/runs/r/f/skip.md" {
		t.Errorf("notes = %q, want /runs/r/f/skip.md", got)
	}

	run.State = RunStateHeld
	run.Legs[0].State = RunStateHeld
	EngineAdvance(&run, EventResume(new("quick-build"), nil, nil), engineT0)
	if run.StageID != "quick-build" {
		t.Errorf("stage = %q, want quick-build", run.StageID)
	}
	if got := run.Routes()["wave"]; !slices.Equal(got, []string{"build"}) {
		t.Errorf("wave = %v, want [build]", got)
	}
	if got := nfPlaceholderNotes(run, ship, context); got != "(no quick.md yet)" {
		t.Errorf("notes during = %q, want (no quick.md yet)", got)
	}
	nfFinish(&run, "done")
	if run.StageID != "ship" {
		t.Errorf("stage = %q, want ship", run.StageID)
	}
	if got := run.Routes()["wave"]; !slices.Equal(got, []string{"build"}) {
		t.Errorf("wave = %v, want [build]", got)
	}
	if got := nfPlaceholderNotes(run, ship, context); got != "/runs/r/f/quick.md" {
		t.Errorf("notes = %q, want /runs/r/f/quick.md", got)
	}

	// Changing the nested route hides the quick output until the selected
	// thorough writer finishes.
	run.State = RunStateHeld
	run.Legs[0].State = RunStateHeld
	EngineAdvance(&run, EventResume(new("thorough-build"), nil, nil), engineT0)
	if got := run.Routes()["pace"]; !slices.Equal(got, []string{"thorough"}) {
		t.Errorf("pace = %v, want [thorough]", got)
	}
	if got := nfPlaceholderNotes(run, ship, context); got != "(no thorough.md yet)" {
		t.Errorf("notes = %q, want (no thorough.md yet)", got)
	}
	data, err := jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded Run
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	run = reloaded
	if got := nfPlaceholderNotes(run, ship, context); got != "(no thorough.md yet)" {
		t.Errorf("notes after a reload = %q, want (no thorough.md yet)", got)
	}
	nfFinish(&run, "done")
	if got := nfPlaceholderNotes(run, ship, context); got != "/runs/r/f/thorough.md" {
		t.Errorf("notes = %q, want /runs/r/f/thorough.md", got)
	}

	// Going back to the quick route keeps its earlier output until quick-build
	// writes again.
	run.State = RunStateHeld
	run.Legs[0].State = RunStateHeld
	EngineAdvance(&run, EventResume(new("quick-build"), nil, nil), engineT0)
	if got := run.Routes()["pace"]; !slices.Equal(got, []string{"quick"}) {
		t.Errorf("pace = %v, want [quick]", got)
	}
	if got := nfPlaceholderNotes(run, ship, context); got != "/runs/r/f/quick.md" {
		t.Errorf("notes = %q, want /runs/r/f/quick.md", got)
	}
	EngineAdvance(&run, Event{Kind: EventKindSessionStarted, Session: "s2", SessionID: "id2", Folder: "quick-2"}, engineT0)
	EngineAdvance(&run, Event{Kind: EventKindSessionFinished, Output: new("Some output.\n\nOutcome: done")}, engineT0)
	if got := nfPlaceholderNotes(run, ship, context); got != "/runs/r/quick-2/quick.md" {
		t.Errorf("notes = %q, want /runs/r/quick-2/quick.md", got)
	}
	if n := len(nfAttemptsAt(run, "quick-build")); n != 2 {
		t.Errorf("quick-build attempts = %d, want 2", n)
	}
	data, err = jsonx.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	if got := nfPlaceholderNotes(reloaded, ship, context); got != "/runs/r/quick-2/quick.md" {
		t.Errorf("notes after a reload = %q, want /runs/r/quick-2/quick.md", got)
	}
}
