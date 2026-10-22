package core

import (
	"slices"
	"strings"
	"testing"
)

// nestedFixtureBelt is the nested belt for these tests: triage, then an outer
// fork "wave" (Build, Skip). Build holds assess, an inner fork "pace" (Quick,
// Thorough, parent build) with its stages, and verify. Ship follows. Skip is empty
// unless skipStage adds a stage there; verify and quick can be left out.
func nestedFixtureBelt(skipStage, verify, quick bool) Belt {
	wave := Fork{ID: "wave", Routes: []Route{{ID: "build", Name: "Build"}, {ID: "skip", Name: "Skip"}}}
	pace := Fork{ID: "pace", Parent: new("build"), Routes: []Route{{ID: "quick", Name: "Quick"}, {ID: "thorough", Name: "Thorough"}}}
	goOn := draftTestOutcome("go", Then{Action: ThenActionNext})
	done := draftTestOutcome("done", Then{Action: ThenActionNext})
	stages := []Stage{
		nestedFixtureStage("triage", "triage", "", "Triage", goOn),
		nestedFixtureStage("assess", "spec", "build", "Assess", goOn),
	}
	if quick {
		stages = append(stages, nestedFixtureStage("quick-build", "build", "quick", "Build", done))
	}
	stages = append(stages,
		nestedFixtureStage("thorough-spec", "spec", "thorough", "Spec", done),
		nestedFixtureStage("thorough-build", "build", "thorough", "Build", done),
	)
	if verify {
		stages = append(stages, nestedFixtureStage("verify", "verify", "build", "Verify", done))
	}
	if skipStage {
		stages = append(stages, nestedFixtureStage("skip-build", "build", "skip", "Build", done))
	}
	stages = append(stages, nestedFixtureStage("ship", "ship", "", "Ship", done))
	return draftTestBelt(stages, wave, pace)
}

// nestedFixtureStage is an agent stage of the nested fixture.
func nestedFixtureStage(id, agent, route, name string, outcome Outcome) Stage {
	stage := draftTestStage(id, StageTypeAgent, agent, route, outcome)
	stage.Name = name
	return stage
}

// TestCounterpartsNameEveryReenteredRoute
func TestCounterpartsNameEveryReenteredRoute(t *testing.T) {
	t.Parallel()
	layout := nestedFixtureBelt(false, true, true).Layout()
	matches := layout.Counterparts("quick-build", "ship")
	if !slices.Contains(matches, "thorough-build") {
		t.Fatalf("counterparts = %v, want thorough-build among them", matches)
	}
	draftCheck(t, "path of thorough-build", []string{"Build", "Thorough"}, layout.PathOf("thorough-build"))
	if got := layout.Target([]string{"quick-build", "thorough-build"}, map[string][]string{"pace": {"thorough"}, "wave": {"build"}}, nil); got == nil || *got != "thorough-build" {
		t.Fatalf("target = %v, want thorough-build", got)
	}
	if got := layout.Target([]string{"thorough-build"}, map[string][]string{}, nil); got != nil {
		t.Fatalf("target with no routes = %v, want nil", *got)
	}
}

// TestAFlatForkStillOmitsParentAndANestedBeltRoundTrips
func TestAFlatForkStillOmitsParentAndANestedBeltRoundTrips(t *testing.T) {
	t.Parallel()
	flat := btEncode(t, forkedBeltWith(nil).Forks[0])
	if strings.Contains(flat, "parent") {
		t.Fatalf("flat fork encodes a parent key: %s", flat)
	}
	belt := nestedFixtureBelt(false, true, true)
	loaded := btDecode[Belt](t, btEncode(t, belt))
	pace, ok := nestedFork(loaded, "pace")
	if !ok || pace.Parent == nil || *pace.Parent != "build" {
		t.Fatalf("pace after the round trip = %+v, want a child of build", pace)
	}
	if !loaded.Layout().Nested() {
		t.Fatal("a nested belt stops being nested after the round trip")
	}
	if !strings.Contains(btEncode(t, belt), "pace") {
		t.Fatal("the saved belt does not name pace")
	}
}

// nestedFork is the fork with id in belt, and whether there is one.
func nestedFork(belt Belt, id string) (Fork, bool) {
	for _, fork := range belt.Forks {
		if fork.ID == id {
			return fork, true
		}
	}
	return Fork{}, false
}

// nestedChildFork is the first fork nested in parent, and whether there is one.
func nestedChildFork(belt Belt, parent string) (Fork, bool) {
	for _, fork := range belt.Forks {
		if fork.Parent != nil && *fork.Parent == parent {
			return fork, true
		}
	}
	return Fork{}, false
}

// TestForkHereOnARoutedStageNestsAndASecondForkNestsAgain
func TestForkHereOnARoutedStageNestsAndASecondForkNestsAgain(t *testing.T) {
	t.Parallel()
	belt := nestedFixtureBelt(false, true, true)
	before := draftTestIDs(belt.Stages)
	belt.ForkAround("thorough-build")
	made, ok := nestedChildFork(belt, "thorough")
	if !ok {
		t.Fatal("no fork nested in thorough")
	}
	if len(made.Routes) != 2 {
		t.Fatalf("made routes = %d, want 2", len(made.Routes))
	}
	stage := nestedStageNamed(belt, "thorough-build")
	if stage == nil || stage.Route == nil || *stage.Route != made.Routes[0].ID {
		t.Fatalf("thorough-build route = %v, want %s", stage, made.Routes[0].ID)
	}
	if got := belt.Layout().StagesOn(made.Routes[1].ID); len(got) != 0 {
		t.Fatalf("second route stages = %v, want none", draftTestIDs(got))
	}
	found := slices.ContainsFunc(belt.Layout().piecesIn("thorough"), func(piece BeltLayoutPiece) bool {
		return piece.Fork != nil && piece.Fork.ID == made.ID
	})
	if !found {
		t.Fatal("thorough does not hold the new fork")
	}
	var others []string
	for _, id := range draftTestIDs(belt.Stages) {
		if id != "thorough-build" {
			others = append(others, id)
		}
	}
	var wantOthers []string
	for _, id := range before {
		if id != "thorough-build" {
			wantOthers = append(wantOthers, id)
		}
	}
	draftCheck(t, "stages other than thorough-build keep their order", wantOthers, others)

	belt.ForkAround("thorough-build")
	var deeper []Fork
	for _, fork := range belt.Forks {
		if fork.ID != "wave" && fork.ID != "pace" && fork.ID != made.ID {
			deeper = append(deeper, fork)
		}
	}
	if len(deeper) != 1 {
		t.Fatalf("deeper forks = %d, want 1", len(deeper))
	}
	if deeper[0].Parent == nil || *deeper[0].Parent != made.Routes[0].ID {
		t.Fatalf("deeper parent = %v, want %s", deeper[0].Parent, made.Routes[0].ID)
	}
	parents := map[string]struct{}{}
	withParent := 0
	for _, fork := range belt.Forks {
		if fork.Parent != nil {
			parents[*fork.Parent] = struct{}{}
			withParent++
		}
	}
	if len(parents) != withParent {
		t.Fatalf("distinct parents = %d, forks with a parent = %d", len(parents), withParent)
	}
}

// TestEditingInsideARouteKeepsTheParentAndDeeperForks
func TestEditingInsideARouteKeepsTheParentAndDeeperForks(t *testing.T) {
	t.Parallel()
	belt := nestedFixtureBelt(false, true, true)
	belt.AddRoute("pace", false)
	pace, _ := nestedFork(belt, "pace")
	if len(pace.Routes) != 3 {
		t.Fatalf("pace routes = %d, want 3", len(pace.Routes))
	}
	for i := range belt.Forks {
		if belt.Forks[i].ID == "pace" {
			belt.Forks[i].Routes[2].Name = "Careful"
		}
	}
	extra := draftTestStage("quick-extra", StageTypeAgent, "build", "quick")
	belt.Insert(extra, belt.End("quick"))
	draftCheck(t, "quick after insert", []string{"quick-build", "quick-extra"}, draftTestIDs(belt.Layout().StagesOn("quick")))
	belt.Shift("quick-extra", -1)
	draftCheck(t, "quick after shift", []string{"quick-extra", "quick-build"}, draftTestIDs(belt.Layout().StagesOn("quick")))
	belt.MoveOnto("quick-extra", "assess")
	if s := nestedStageNamed(belt, "quick-extra"); s == nil || s.Route == nil || *s.Route != "build" {
		t.Fatalf("quick-extra route = %v, want build", s)
	}
	belt.MoveTo("thorough-spec", nil)
	if s := nestedStageNamed(belt, "thorough-spec"); s == nil || s.Route == nil || *s.Route != "build" {
		t.Fatalf("thorough-spec route = %v, want build", s)
	}
	var afterFork []string
	for _, piece := range belt.Layout().piecesIn("build") {
		if piece.Stage != nil {
			afterFork = append(afterFork, piece.Stage.ID)
		} else if piece.Fork != nil {
			afterFork = append(afterFork, piece.Fork.ID)
		}
	}
	paceAt, specAt := -1, -1
	for i, id := range afterFork {
		switch id {
		case "pace":
			paceAt = i
		case "thorough-spec":
			specAt = i
		}
	}
	if paceAt < 0 || specAt < 0 || paceAt >= specAt {
		t.Fatalf("build order %v: pace must come before thorough-spec", afterFork)
	}
}

// TestRemovingARouteRemovesTheSubtreeAndDissolvingKeepsDeeperForks
func TestRemovingARouteRemovesTheSubtreeAndDissolvingKeepsDeeperForks(t *testing.T) {
	t.Parallel()
	belt := nestedFixtureBelt(false, true, true)
	belt.ForkAround("thorough-build")
	child, ok := nestedChildFork(belt, "thorough")
	if !ok {
		t.Fatal("no fork nested in thorough")
	}
	belt.Stages[0].Outcomes[0].Then = Then{Action: ThenActionRoute, Routes: []string{"skip"}}
	belt.RemoveRoute("build")
	if _, ok := nestedFork(belt, "pace"); ok {
		t.Fatal("pace survived removing build")
	}
	if _, ok := nestedFork(belt, child.ID); ok {
		t.Fatal("the fork nested in thorough survived removing build")
	}
	for _, id := range []string{"assess", "quick-build", "thorough-build"} {
		if nestedStageNamed(belt, id) != nil {
			t.Fatalf("%s survived removing build", id)
		}
	}
	if got := belt.Stages[0].Outcomes[0].Then.Action; got != ThenActionNext {
		t.Fatalf("triage outcome action = %s, want next", got)
	}

	belt = nestedFixtureBelt(false, true, true)
	belt.ForkAround("thorough-build")
	kept, _ := nestedChildFork(belt, "thorough")
	belt.Dissolve("pace")
	if f, ok := nestedFork(belt, kept.ID); !ok || f.Parent == nil || *f.Parent != "build" {
		t.Fatalf("dissolving pace should keep %s under build", kept.ID)
	}
	if s := nestedStageNamed(belt, "quick-build"); s == nil || s.Route == nil || *s.Route != "build" {
		t.Fatalf("quick-build route after dissolve = %v, want build", s)
	}
	belt.Dissolve("wave")
	if f, ok := nestedFork(belt, kept.ID); !ok || f.Parent != nil {
		t.Fatalf("dissolving wave should leave %s on the line", kept.ID)
	}
	if s := nestedStageNamed(belt, "assess"); s == nil || s.Route != nil {
		t.Fatalf("assess route after dissolving wave = %v, want none", s)
	}

	belt = nestedFixtureBelt(false, true, true)
	belt.RemoveRoute("thorough")
	if _, ok := nestedFork(belt, "pace"); ok {
		t.Fatal("pace survived removing its last sibling route")
	}
	if !nestedHasRouteStage(belt, "build") {
		t.Fatal("removing thorough should keep the build route's stages")
	}

	belt = nestedFixtureBelt(false, true, true)
	belt.Remove("quick-build")
	belt.Remove("thorough-spec")
	belt.Remove("thorough-build")
	if _, ok := nestedFork(belt, "pace"); ok {
		t.Fatal("pace survived losing every stage")
	}
}

// nestedStageNamed is the stage with id in belt, or nil.
func nestedStageNamed(belt Belt, id string) *Stage {
	for i := range belt.Stages {
		if belt.Stages[i].ID == id {
			return &belt.Stages[i]
		}
	}
	return nil
}

// nestedHasRouteStage reports whether any stage of belt is on route.
func nestedHasRouteStage(belt Belt, route string) bool {
	return slices.ContainsFunc(belt.Stages, func(stage Stage) bool {
		return stage.Route != nil && *stage.Route == route
	})
}
