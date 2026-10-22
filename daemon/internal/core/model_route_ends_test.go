package core

import (
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// askBeltLiteral is triage, then a fork before Spec whose Main
// route is empty (the line) and whose "Needs details" route feeds into Spec
// (stage "ask"), then Spec, Build and Ship. Triage's "needs info" goes to ask.
// Outcomes are reduced to the ones the tests read.
func askBeltLiteral() Belt {
	details := Fork{ID: "details", Routes: []Route{{ID: "main", Name: "Main"}, {ID: "asking", Name: "Needs details", Feeds: true}}}
	stages := []Stage{
		draftTestStage("triage", StageTypeAgent, "triage", "",
			draftTestOutcome("ready to spec", Then{Action: ThenActionNext}),
			draftTestOutcome("ready to implement", Then{Action: ThenActionNext}),
			draftTestOutcome("needs info", Then{Action: ThenActionGoto, To: []string{"ask"}})),
		draftTestStage("ask", StageTypeAgent, "spec", "asking", draftTestOutcome("ready", Then{Action: ThenActionNext})),
		draftTestStage("spec", StageTypeAgent, "spec", ""),
		draftTestStage("build", StageTypeAgent, "build", ""),
		draftTestStage("ship", StageTypeAgent, "ship", ""),
	}
	return draftTestBelt(stages, details)
}

// TestARouteThatFeedsInDoesNotOpen: a route that feeds in is reached by a jump only,
// and it still joins the line after its stages.
func TestARouteThatFeedsInDoesNotOpen(t *testing.T) {
	t.Parallel()
	layout := askBeltLiteral().Layout()
	if !layout.Opens("main") {
		t.Error("main does not open")
	}
	if layout.Opens("asking") {
		t.Error("asking opens, want it to feed in")
	}
	if !layout.Joins("asking") {
		t.Error("asking does not join")
	}
	draftCheck(t, "routes after triage", []string{"main"}, draftTestRouteIDs(layout.RoutesAfter("triage")))
}

// TestARouteThatFeedsInSavesItsFlagOnlyWhenSet: feeds is written only when true,
// and read as false when missing.
func TestARouteThatFeedsInSavesItsFlagOnlyWhenSet(t *testing.T) {
	t.Parallel()
	plain, err := jsonx.Marshal(Route{ID: "a", Name: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"id":"a","name":"A"}` {
		t.Fatalf("plain route = %s, want no feeds key", plain)
	}
	data, err := jsonx.Marshal(Route{ID: "b", Name: "B", Feeds: true})
	if err != nil {
		t.Fatal(err)
	}
	var feeding Route
	if err := json.Unmarshal(data, &feeding); err != nil {
		t.Fatal(err)
	}
	if !feeding.Feeds {
		t.Error("feeding route decoded without feeds")
	}
	var missing Route
	if err := json.Unmarshal([]byte(`{"id":"c","name":"C"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Feeds {
		t.Error("route without feeds decoded as feeding")
	}
}

// TestFeedInHerePutsAForkBeforeTheStage: feed(into:with:) puts a fork with an empty
// line and a feeding route in front of the stage, and the fed stage sits on the feeding route.
func TestFeedInHerePutsAForkBeforeTheStage(t *testing.T) {
	t.Parallel()
	belt := draftTestBelt([]Stage{
		draftTestStage("triage", StageTypeAgent, "triage", ""),
		draftTestStage("spec", StageTypeAgent, "spec", ""),
		draftTestStage("build", StageTypeAgent, "build", ""),
		draftTestStage("ship", StageTypeAgent, "ship", ""),
	})
	ask := draftTestStage("ask-spec", StageTypeAgent, "spec", "")
	belt.FeedInto("spec", ask)
	if len(belt.Forks) != 1 {
		t.Fatalf("forks = %d, want 1", len(belt.Forks))
	}
	fork := belt.Forks[0]
	if len(fork.Routes) != 2 || fork.Routes[0].Feeds || !fork.Routes[1].Feeds {
		t.Fatalf("routes = %+v, want [line, feeding]", fork.Routes)
	}
	layout := belt.Layout()
	if got := layout.StagesOn(fork.Routes[0].ID); len(got) != 0 {
		t.Errorf("line route holds %v, want none", draftTestIDs(got))
	}
	draftCheck(t, "feeding route stages", []string{"ask-spec"}, draftTestIDs(layout.StagesOn(fork.Routes[1].ID)))
	if next := layout.Resume(fork); next.Stage == nil || *next.Stage != "spec" {
		t.Errorf("resume after the fork = %v, want spec", next.Stage)
	}
	var head []string
	for _, p := range layout.Pieces(nil) {
		if p.Kind == BeltLayoutPieceStage {
			head = append(head, p.Stage.ID)
		} else {
			head = append(head, p.Fork.ID)
		}
	}
	draftCheck(t, "pieces on the line", []string{"triage", fork.ID, "spec"}, head[:3])
	added := belt.AddRoute(fork.ID, true)
	if added == nil {
		t.Fatal("AddRoute returned nil for a fork on the belt")
	}
	draftCheck(t, "added feeding route", Route{ID: *added, Name: "Feed in 2", Feeds: true}, belt.Forks[0].Routes[len(belt.Forks[0].Routes)-1])
}

// TestRouteJoinsOnlyWhenItsLastStageMovesPast: a route joins the line when its last
// stage has an outcome that moves past the fork, or finishes the belt there.
func TestRouteJoinsOnlyWhenItsLastStageMovesPast(t *testing.T) {
	t.Parallel()
	fork := Fork{ID: "f", Routes: []Route{{ID: "x", Name: "X"}, {ID: "y", Name: "Y"}}}
	build := func(withShip bool, outcomes ...Outcome) Belt {
		stages := []Stage{
			draftTestStage("triage", StageTypeAgent, "triage", ""),
			draftTestStage("a", StageTypeAgent, "spec", "x"),
			draftTestStage("b", StageTypeAgent, "build", "y", outcomes...),
		}
		if withShip {
			stages = append(stages, draftTestStage("ship", StageTypeAgent, "ship", ""))
		}
		return draftTestBelt(stages, fork)
	}
	cases := []struct {
		name     string
		withShip bool
		outcomes []Outcome
		want     bool
	}{
		{"no outcomes", true, nil, true},
		{"hold only", true, []Outcome{draftTestOutcome("hold", Then{Action: ThenActionHold})}, false},
		{"retry, park and drop", true, []Outcome{
			draftTestOutcome("retry", Then{Action: ThenActionRetry}),
			draftTestOutcome("park", Then{Action: ThenActionPark}),
			draftTestOutcome("drop", Then{Action: ThenActionDrop}),
		}, false},
		{"next", true, []Outcome{draftTestOutcome("next", Then{Action: ThenActionNext})}, true},
		{"goto past the fork", true, []Outcome{draftTestOutcome("ship", Then{Action: ThenActionGoto, To: []string{"ship"}})}, true},
		{"goto back into the fork", true, []Outcome{draftTestOutcome("a", Then{Action: ThenActionGoto, To: []string{"a"}})}, false},
		{"done with a stage after the fork", true, []Outcome{draftTestOutcome("done", Then{Action: ThenActionDone})}, false},
		{"done at the end of the belt", false, []Outcome{draftTestOutcome("done", Then{Action: ThenActionDone})}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := build(c.withShip, c.outcomes...).Layout().Joins("y"); got != c.want {
				t.Errorf("joins(y) = %v, want %v", got, c.want)
			}
		})
	}
}
