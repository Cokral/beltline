// The run page's metro line: one belt's shape, the
// run's place on it and the travel along it.

package core

import (
	"reflect"
	"testing"
	"time"
)

// runMetroT0 is the clock every metro run starts at.
var runMetroT0 = time.Unix(1800000000, 0)

// runMetroOutcome is an outcome named name that moves the run by then.
func runMetroOutcome(name string, tone Tone, then Then) Outcome {
	return Outcome{ID: name, Name: name, When: name, Tone: tone, Then: then}
}

// runMetroBelt is a belt of the trigger kind over repos.
func runMetroBelt(id, name string, kind TriggerKind, repos []string, stages []Stage, forks []Fork) Belt {
	entry := TriggerEntry{ID: TriggerEntryLegacyID, Trigger: Trigger{Kind: kind}, Repos: repos}
	if kind == TriggerKindIssues {
		filter := DefaultIssueFilter()
		filter.Label = "agent"
		entry.Trigger.Issues = &filter
	}
	return Belt{ID: id, Name: name, Version: 1, Triggers: []TriggerEntry{entry}, Policies: DefaultPolicies(), Stages: stages, Forks: forks, Afk: AfkModeGlobal}
}

// runMetroAgentStage is a stage running the built-in agent at place with an id of its own and no outcomes
// (DefaultsStageForAgentAt with the id set afterwards).
func runMetroAgentStage(agent string, place DefaultsPlace, existing []Stage) Stage {
	return DefaultsStageForAgentAt(*DefaultsBuiltIn(agent, nil), place, existing, TriggerKindIssues)
}

// runMetroForkedBelt is triage, then spec and build on the complex route and
// the simple agents on the simple route (quick-<agent>), then verify and ship.
func runMetroForkedBelt(simple []string) Belt {
	fork := Fork{ID: "size", Routes: []Route{{ID: "complex", Name: "Complex"}, {ID: "simple", Name: "Simple"}}}
	var stages []Stage
	add := func(agent string, route *string, id string) {
		stage := runMetroAgentStage(agent, DefaultsPlace{Before: stages, Forks: []Fork{fork}, Route: route}, stages)
		if id != "" {
			stage.ID = id
		}
		stages = append(stages, stage)
	}
	complex := "complex"
	simpleRoute := "simple"
	add("triage", nil, "")
	add("spec", &complex, "")
	add("build", &complex, "")
	for _, agent := range simple {
		add(agent, &simpleRoute, "quick-"+agent)
	}
	add("verify", nil, "")
	add("ship", nil, "")
	stages[0].Outcomes[0].Then = Then{Action: ThenActionRoute, Routes: []string{"simple"}}
	return runMetroBelt("sized", "Sized", TriggerKindIssues, []string{"acme/billing"}, stages, []Fork{fork})
}

// runMetroNestedBelt is an outer fork "wave" with routes build
// and skip, and build holds a nested fork "pace" with routes quick and thorough.
func runMetroNestedBelt(skipStage, verify, quick bool) Belt {
	outer := Fork{ID: "wave", Routes: []Route{{ID: "build", Name: "Build"}, {ID: "skip", Name: "Skip"}}}
	inner := Fork{ID: "pace", Routes: []Route{{ID: "quick", Name: "Quick"}, {ID: "thorough", Name: "Thorough"}}, Parent: new("build")}
	stage := func(id, agent string, route *string, outcome Outcome) Stage {
		s := runMetroAgentStage(agent, DefaultsPlace{Route: route}, nil)
		s.ID = id
		switch id {
		case "quick-build", "thorough-build", "skip-build":
			s.Name = "Build"
		case "thorough-spec":
			s.Name = "Spec"
		case "assess":
			s.Name = "Assess"
		default:
			s.Name = runMetroTitleCase(id)
		}
		s.Route = route
		s.Outcomes = []Outcome{outcome}
		return s
	}
	goOn := runMetroOutcome("go", ToneNeutral, Then{Action: ThenActionNext})
	done := runMetroOutcome("done", ToneNeutral, Then{Action: ThenActionNext})
	complex := "build"
	quickRoute := "quick"
	thoroughRoute := "thorough"
	skipRoute := "skip"
	stages := []Stage{
		stage("triage", "triage", nil, goOn),
		stage("assess", "spec", &complex, goOn),
	}
	if quick {
		stages = append(stages, stage("quick-build", "build", &quickRoute, done))
	}
	stages = append(stages,
		stage("thorough-spec", "spec", &thoroughRoute, done),
		stage("thorough-build", "build", &thoroughRoute, done),
	)
	if verify {
		stages = append(stages, stage("verify", "verify", &complex, done))
	}
	if skipStage {
		stages = append(stages, stage("skip-build", "build", &skipRoute, done))
	}
	stages = append(stages, stage("ship", "ship", nil, runMetroOutcome("opened", ToneNeutral, Then{Action: ThenActionDone})))
	return runMetroBelt("nested", "Nested", TriggerKindIssues, []string{"acme/billing"}, stages, []Fork{outer, inner})
}

// runMetroTitleCase is s with its first letter upper-cased.
func runMetroTitleCase(s string) string {
	if s == "" {
		return s
	}
	first := s[:1]
	if first >= "a" && first <= "z" {
		first = string(first[0] - 'a' + 'A')
	}
	return first + s[1:]
}

// runMetroMetro starts a run of belt on its first stage, sets the run's routes, stage and state through
// configure, and returns the run with the map the run page sends.
func runMetroMetro(t *testing.T, belt Belt, configure func(run *Run)) (Run, RunMetro) {
	t.Helper()
	run := NewRun(RunArgs{Belt: belt, Subject: Subject{Kind: SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Work", Body: ""}, Now: runMetroT0})
	_ = EngineStart(&run, runMetroT0)
	// The tests place the run by its stage and routes alone, so no leg from the start is left behind.
	run.Legs = nil
	run.State = RunStateRunning
	if configure != nil {
		configure(&run)
	}
	panels := RunPresentationProject(run, 1080, false, Pause{}, runMetroT0, map[string]struct{}{}, nil, nil).Stages
	return run, RunMetroLayoutMap(run, panels)
}

// runMetroStation is the station with id, failing the test when the map has none.
func runMetroStation(t *testing.T, metro RunMetro, id string) RunMetroStation {
	t.Helper()
	for _, station := range metro.Stations {
		if station.ID == id {
			return station
		}
	}
	t.Fatalf("the map has no station %q", id)
	return RunMetroStation{}
}

// runMetroRoute is the route tag with route name, failing the test when the map has none.
func runMetroRoute(t *testing.T, metro RunMetro, route string) RunMetroRouteTag {
	t.Helper()
	for _, tag := range metro.Routes {
		if tag.Route == route {
			return tag
		}
	}
	t.Fatalf("the map has no route %q", route)
	return RunMetroRouteTag{}
}

// runMetroTravelled are the segments of the map in state.
func runMetroTravelled(metro RunMetro, state RunMetroTravel) []RunMetroSegment {
	var out []RunMetroSegment
	for _, segment := range metro.Segments {
		if segment.State == state {
			out = append(out, segment)
		}
	}
	return out
}

// runMetroOverlaps reports whether two boxes overlap.
func runMetroOverlaps(a, b RunMetroBox) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

// runMetroFirstSubpath is the first subpath of a segment's path, or nil.
func runMetroFirstSubpath(segment RunMetroSegment) []MapPoint {
	if len(segment.Path.Subpaths) == 0 {
		return nil
	}
	return segment.Path.Subpaths[0]
}

// runMetroBeltOf is the trigger kind's shorthand for the straight-line fixture: a manual belt of the stages.
func runMetroStraightBelt() Belt {
	return runMetroBelt("straight", "Straight", TriggerKindManual, nil, DefaultsStages(TriggerKindManual, DefaultsBuiltInAgents(), true), nil)
}

// TestRunMetroStraightBeltRunsOneLineFromItsPillThroughEveryStation
func TestRunMetroStraightBeltRunsOneLineFromItsPillThroughEveryStation(t *testing.T) {
	t.Parallel()
	belt := runMetroStraightBelt()
	run, metro := runMetroMetro(t, belt, nil)

	var ids []string
	for _, stage := range belt.Stages {
		ids = append(ids, stage.ID)
	}
	var stationIDs []string
	for _, station := range metro.Stations {
		stationIDs = append(stationIDs, station.ID)
	}
	if !reflect.DeepEqual(stationIDs, ids) {
		t.Fatalf("stations %v, want %v", stationIDs, ids)
	}
	if len(metro.Routes) != 0 {
		t.Fatalf("routes %v, want none", metro.Routes)
	}
	if metro.Trigger.Text != belt.Name {
		t.Fatalf("trigger %q, want %q", metro.Trigger.Text, belt.Name)
	}
	if metro.Colour != MetroLayoutColourIndex(belt.ID) {
		t.Fatalf("colour %d, want %d", metro.Colour, MetroLayoutColourIndex(belt.ID))
	}
	for _, station := range metro.Stations {
		if station.Y != metro.Stations[0].Y {
			t.Fatalf("station %s is on row %v, want one line", station.ID, station.Y)
		}
	}
	for i := 1; i < len(metro.Stations); i++ {
		if metro.Stations[i].X < metro.Stations[i-1].X {
			t.Fatalf("stations are not in x order: %v then %v", metro.Stations[i-1].X, metro.Stations[i].X)
		}
	}
	// The line starts at the pill and ends at the terminus, the only open end.
	if metro.Trigger.Box.X+metro.Trigger.Box.Width > metro.Stations[0].X {
		t.Fatalf("the pill runs past the first station")
	}
	if len(metro.Ends) != 1 {
		t.Fatalf("ends %v, want one", metro.Ends)
	}
	if metro.Ends[0].X <= metro.Stations[len(metro.Stations)-1].X {
		t.Fatalf("the terminus %v is not past the last station", metro.Ends[0])
	}
	for _, segment := range metro.Segments {
		if len(segment.Path.Subpaths) != 1 {
			t.Fatalf("segment %s has %d subpaths, want one", segment.ID, len(segment.Path.Subpaths))
		}
	}
	// The run is on the first stage: it is current and everything after is still to come.
	if runMetroStation(t, metro, run.StageID).State != RunMetroStationStateCurrent {
		t.Fatalf("the run's stage is not current")
	}
	for _, station := range metro.Stations[1:] {
		if station.State != RunMetroStationStateTodo {
			t.Fatalf("station %s is %s, want todo", station.ID, station.State)
		}
	}
	if len(runMetroTravelled(metro, RunMetroTravelSkip)) != 0 {
		t.Fatalf("a straight belt has a skipped stretch")
	}
	for _, segment := range runMetroTravelled(metro, RunMetroTravelDone) {
		for _, p := range runMetroFirstSubpath(segment) {
			if p.X > metro.Stations[0].X+0.5 {
				t.Fatalf("done stretch %s runs past the run at %v", segment.ID, p)
			}
		}
	}
	if len(runMetroTravelled(metro, RunMetroTravelTodo)) == 0 {
		t.Fatalf("no stretch is still to come")
	}
}

// A station carries its status chip only when it is done or current and has a status: the stations still to come, and
// the skipped ones, are drawn without one.
func TestRunMetroStationIsTaggedOnlyWhenDoneOrCurrentWithAStatus(t *testing.T) {
	t.Parallel()
	belt := runMetroStraightBelt()
	run, metro := runMetroMetro(t, belt, nil)

	current := runMetroStation(t, metro, run.StageID)
	if current.Status == "" || !current.Tagged {
		t.Fatalf("the current station %q is tagged %v with status %q, want tagged with a status", current.ID, current.Tagged, current.Status)
	}
	for _, station := range metro.Stations {
		want := (station.State == RunMetroStationStateDone || station.State == RunMetroStationStateCurrent) && station.Status != ""
		if station.Tagged != want {
			t.Fatalf("station %s (%s, status %q) is tagged %v, want %v", station.ID, station.State, station.Status, station.Tagged, want)
		}
	}
}
