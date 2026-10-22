// Tests of the run metro: the labels, pills, forks, routes that go
// back or end the run, and the map's JSON round trip. Fixtures and helpers are in present_run_metro_test.go.

package core

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestRunMetroStationLabelsNeverOverlapEachOtherOrTheLine
func TestRunMetroStationLabelsNeverOverlapEachOtherOrTheLine(t *testing.T) {
	t.Parallel()
	_, metro := runMetroMetro(t, runMetroNestedBelt(true, true, true), nil)
	for i, a := range metro.Stations {
		for _, b := range metro.Stations[i+1:] {
			if runMetroOverlaps(a.Label, b.Label) {
				t.Fatalf("%s and %s overlap", a.ID, b.ID)
			}
		}
	}
	for _, s := range metro.Stations {
		if !(s.Label.Y > s.Y) {
			t.Fatalf("%s: label y %v is not below the line %v", s.ID, s.Label.Y, s.Y)
		}
		// A station's click area covers its glyph and its name, and its status sits above it.
		if !(s.Hit.X <= s.X-9 && s.Hit.X+s.Hit.Width >= s.X+9) {
			t.Fatalf("%s: click area %v does not cover its glyph", s.ID, s.Hit)
		}
		if !(s.Hit.X <= s.Label.X+0.001 && s.Hit.X+s.Hit.Width >= s.Label.X+s.Label.Width-0.001) {
			t.Fatalf("%s: click area %v does not cover its name", s.ID, s.Hit)
		}
		// The click area reaches past the glyph's halo above, and the status chip sits above the station's
		// glyph, clear of the halo, with the name still below it.
		if !(s.Hit.Y <= s.Y-11) {
			t.Fatalf("%s: click area does not reach past the halo", s.ID)
		}
		if !(s.StatusBox.Y+s.StatusBox.Height <= s.Y-11) {
			t.Fatalf("%s: status chip %v is not clear of the station's halo", s.ID, s.StatusBox)
		}
		if !(s.Label.Y >= s.Y+10) {
			t.Fatalf("%s: name %v is not under the station", s.ID, s.Label)
		}
	}
	maxRight, maxBottom := math.Inf(-1), math.Inf(-1)
	for _, s := range metro.Stations {
		maxRight = math.Max(maxRight, s.Label.X+s.Label.Width)
		maxBottom = math.Max(maxBottom, s.Label.Y+s.Label.Height)
	}
	if metro.Width < maxRight {
		t.Fatalf("width %v does not cover the names (%v)", metro.Width, maxRight)
	}
	if metro.Height < maxBottom {
		t.Fatalf("height %v does not cover the names (%v)", metro.Height, maxBottom)
	}
}

// TestRunMetroStatusPillsStayClearOfNamesRouteTagsAndTheNextRow
func TestRunMetroStatusPillsStayClearOfNamesRouteTagsAndTheNextRow(t *testing.T) {
	t.Parallel()
	for _, belt := range []Belt{runMetroNestedBelt(true, true, true), runMetroForkedBelt([]string{"build"})} {
		_, metro := runMetroMetro(t, belt, nil)
		// Every station's chip at the widest a status can be, plus the names and route pills it must keep clear of.
		var chips []RunMetroBox
		for _, s := range metro.Stations {
			chips = append(chips, RunMetroBox{X: s.X - runMetroLayoutPillReserve/2, Y: s.StatusBox.Y,
				Width: runMetroLayoutPillReserve, Height: s.StatusBox.Height})
		}
		var tags []RunMetroBox
		for _, tag := range metro.Routes {
			tags = append(tags, RunMetroBox{X: tag.X, Y: tag.Y, Width: tag.Width, Height: tag.Height})
		}
		for i, chip := range chips {
			for j := i + 1; j < len(chips); j++ {
				if runMetroOverlaps(chip, chips[j]) {
					t.Fatalf("chips %d and %d overlap", i, j)
				}
			}
			for _, s := range metro.Stations {
				if runMetroOverlaps(chip, s.Label) {
					t.Fatalf("%s's chip overlaps a name", s.ID)
				}
			}
			for _, tag := range tags {
				if runMetroOverlaps(chip, tag) {
					t.Fatalf("a chip overlaps a route pill")
				}
			}
			if chip.Y < 0 || chip.Y+chip.Height > metro.Height {
				t.Fatalf("chip %d runs past the map", i)
			}
		}
		for _, tag := range tags {
			for _, s := range metro.Stations {
				if runMetroOverlaps(tag, s.Label) {
					t.Fatalf("a route pill overlaps the name %s", s.ID)
				}
			}
		}
	}
}

// TestRunMetroAForkSplitsTheLineEvenlyAboveAndBelowIt
func TestRunMetroAForkSplitsTheLineEvenlyAboveAndBelowIt(t *testing.T) {
	t.Parallel()
	belt := runMetroForkedBelt([]string{"build"})
	run, metro := runMetroMetro(t, belt, func(r *Run) { r.SetRoutes(map[string][]string{"size": {"complex"}}) })
	spec := runMetroStation(t, metro, "spec")
	quick := runMetroStation(t, metro, "quick-build")
	verify := runMetroStation(t, metro, "verify")
	// The line splits in two: one route half a row above it, the other half a row below, and they meet again.
	line := metro.Stations[0].Y
	if spec.Y != line-runMetroLayoutRowPitch/2 {
		t.Fatalf("spec is on row %v, want %v", spec.Y, line-runMetroLayoutRowPitch/2)
	}
	if quick.Y != line+runMetroLayoutRowPitch/2 {
		t.Fatalf("quick-build is on row %v, want %v", quick.Y, line+runMetroLayoutRowPitch/2)
	}
	if verify.Y != line {
		t.Fatalf("verify is on row %v, want %v", verify.Y, line)
	}
	if got := metroRouteNames(metro); !reflect.DeepEqual(got, []string{"complex", "simple"}) {
		t.Fatalf("routes %v, want [complex simple]", got)
	}
	complexTag := runMetroRoute(t, metro, "complex")
	simpleTag := runMetroRoute(t, metro, "simple")
	if complexTag.Taken == nil || !*complexTag.Taken {
		t.Fatalf("complex is not taken")
	}
	if simpleTag.Taken == nil || *simpleTag.Taken {
		t.Fatalf("simple is taken or undecided")
	}
	// Each pill is centred on its route's track, just past the split and before the route's first station.
	if simpleTag.Y+simpleTag.Height/2 != quick.Y {
		t.Fatalf("simple's pill is centred at %v, want on row %v", simpleTag.Y+simpleTag.Height/2, quick.Y)
	}
	if simpleTag.X < metro.Stations[0].X || simpleTag.X+simpleTag.Width > quick.Label.X {
		t.Fatalf("simple's pill %v does not sit between the split and quick-build's name", simpleTag)
	}
	if !reflect.DeepEqual(run.Routes()["size"], []string{"complex"}) {
		t.Fatalf("run routes %v, want size: [complex]", run.Routes())
	}
}

// metroRouteNames is the route names of the map's tags, in order.
func metroRouteNames(metro RunMetro) []string {
	var names []string
	for _, tag := range metro.Routes {
		names = append(names, tag.Route)
	}
	return names
}

// TestRunMetroARunOnARouteTakenShowsTravelBehindItAndTheRestAhead
func TestRunMetroARunOnARouteTakenShowsTravelBehindItAndTheRestAhead(t *testing.T) {
	t.Parallel()
	belt := runMetroForkedBelt([]string{"build"})
	run, metro := runMetroMetro(t, belt, func(r *Run) {
		r.SetRoutes(map[string][]string{"size": {"simple"}})
		r.StageID = "quick-build"
	})
	if run.StageID != "quick-build" {
		t.Fatalf("stage %q, want quick-build", run.StageID)
	}
	expect := func(id string, want RunMetroStationState) {
		t.Helper()
		if got := runMetroStation(t, metro, id).State; got != want {
			t.Fatalf("%s is %s, want %s", id, got, want)
		}
	}
	expect("quick-build", RunMetroStationStateCurrent)
	expect("triage", RunMetroStationStateDone)
	expect("verify", RunMetroStationStateTodo)
	expect("ship", RunMetroStationStateTodo)
	// The route the run did not take is drawn dashed, and so is the line through it.
	expect("spec", RunMetroStationStateSkip)
	expect("build", RunMetroStationStateSkip)
	if len(runMetroTravelled(metro, RunMetroTravelSkip)) == 0 {
		t.Fatalf("no dashed stretch")
	}
	// The taken route is solid behind the run and faded ahead of it.
	if len(runMetroTravelled(metro, RunMetroTravelDone)) == 0 {
		t.Fatalf("no solid stretch behind the run")
	}
	if len(runMetroTravelled(metro, RunMetroTravelTodo)) == 0 {
		t.Fatalf("no faded stretch ahead of the run")
	}
	simple := runMetroRoute(t, metro, "simple")
	if simple.Taken == nil || !*simple.Taken {
		t.Fatalf("simple is not taken")
	}
}

// TestRunMetroARunInTheMiddleOfARouteMarksTheStationAndFadesWhatIsStillToCome
func TestRunMetroARunInTheMiddleOfARouteMarksTheStationAndFadesWhatIsStillToCome(t *testing.T) {
	t.Parallel()
	belt := runMetroForkedBelt([]string{"build"})
	_, metro := runMetroMetro(t, belt, func(r *Run) {
		r.SetRoutes(map[string][]string{"size": {"complex"}})
		r.StageID = "build"
		r.State = RunStateWaiting
	})
	build := runMetroStation(t, metro, "build")
	if build.State != RunMetroStationStateCurrent {
		t.Fatalf("build is %s, want current", build.State)
	}
	if build.Look != "waiting" {
		t.Fatalf("build's look is %q, want waiting", build.Look)
	}
	if build.Status != "WAITING FOR YOU" {
		t.Fatalf("build's status is %q", build.Status)
	}
	// Stages before it on the taken route are behind the run; the stage after the routes meet is still to come.
	if got := runMetroStation(t, metro, "spec").State; got != RunMetroStationStateDone {
		t.Fatalf("spec is %s, want done", got)
	}
	if got := runMetroStation(t, metro, "verify").State; got != RunMetroStationStateTodo {
		t.Fatalf("verify is %s, want todo", got)
	}
	// The untaken route is dashed, not solid, however far the run has got.
	if got := runMetroStation(t, metro, "quick-build").State; got != RunMetroStationStateSkip {
		t.Fatalf("quick-build is %s, want skip", got)
	}
	// The stretch into the run's station is behind it.
	found := false
	for _, segment := range metro.Segments {
		points := runMetroFirstSubpath(segment)
		if segment.State == RunMetroTravelDone && len(points) > 0 && math.Abs(points[len(points)-1].X-build.X) < 0.5 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no solid stretch ends at build")
	}
}

// TestRunMetroNestedForksTakeRowsUnderTheRouteTheySitIn
func TestRunMetroNestedForksTakeRowsUnderTheRouteTheySitIn(t *testing.T) {
	t.Parallel()
	belt := runMetroNestedBelt(true, true, true)
	_, metro := runMetroMetro(t, belt, func(r *Run) {
		r.SetRoutes(map[string][]string{"wave": {"build"}, "pace": {"quick"}})
	})
	ids := map[string]bool{}
	for _, tag := range metro.Routes {
		ids[tag.ID] = true
	}
	if len(ids) != len(metro.Routes) {
		t.Fatalf("route tag ids are not distinct: %v", metro.Routes)
	}
	quick := runMetroStation(t, metro, "quick-build")
	thorough := runMetroStation(t, metro, "thorough-build")
	skip := runMetroStation(t, metro, "skip-build")
	assess := runMetroStation(t, metro, "assess")
	verify := runMetroStation(t, metro, "verify")
	// The outer fork spreads its three rows about the line: the build route's two (assess runs through their
	// middle) above the skip route's one. Build's inner fork splits its own band the same way.
	if quick.Y != assess.Y-runMetroLayoutRowPitch/2 {
		t.Fatalf("quick-build is on row %v, want %v", quick.Y, assess.Y-runMetroLayoutRowPitch/2)
	}
	if thorough.Y != assess.Y+runMetroLayoutRowPitch/2 {
		t.Fatalf("thorough-build is on row %v, want %v", thorough.Y, assess.Y+runMetroLayoutRowPitch/2)
	}
	if skip.Y != assess.Y+1.5*runMetroLayoutRowPitch {
		t.Fatalf("skip-build is on row %v, want %v", skip.Y, assess.Y+1.5*runMetroLayoutRowPitch)
	}
	if verify.Y != assess.Y {
		t.Fatalf("verify is on row %v, want %v", verify.Y, assess.Y)
	}
	if quick.State != RunMetroStationStateTodo {
		t.Fatalf("quick-build is %s, want todo", quick.State)
	}
	if thorough.State != RunMetroStationStateSkip {
		t.Fatalf("thorough-build is %s, want skip", thorough.State)
	}
	if skip.State != RunMetroStationStateSkip {
		t.Fatalf("skip-build is %s, want skip", skip.State)
	}
	if tag := runMetroRoute(t, metro, "thorough"); tag.Taken == nil || *tag.Taken {
		t.Fatalf("thorough is taken or undecided")
	}
	if tag := runMetroRoute(t, metro, "build"); tag.Taken == nil || !*tag.Taken {
		t.Fatalf("build is not taken")
	}
	if metro.Height <= skip.Y {
		t.Fatalf("the map is %v high, not past skip-build at %v", metro.Height, skip.Y)
	}
}

// TestRunMetroAFeedingRouteStartsOnItsOwnRowWithNoDepartureFromTheLine
func TestRunMetroAFeedingRouteStartsOnItsOwnRowWithNoDepartureFromTheLine(t *testing.T) {
	t.Parallel()
	belt := runMetroForkedBelt([]string{"build"})
	belt.Forks[0].Routes[1].Feeds = true
	_, metro := runMetroMetro(t, belt, func(r *Run) { r.SetRoutes(map[string][]string{"size": {"simple"}}) })
	simple := runMetroRoute(t, metro, "simple")
	if !simple.Feeds {
		t.Fatalf("simple does not feed")
	}
	quick := runMetroStation(t, metro, "quick-build")
	// The route's row starts open where it begins, not on a 45° stretch leaving the line.
	open := false
	for _, end := range metro.Ends {
		// The pill sits just past the split, where the route begins.
		if math.Abs(end.X-(simple.X-runMetroLayoutPillInset)) < 0.5 && math.Abs(end.Y-quick.Y) < 0.5 {
			open = true
		}
	}
	if !open {
		t.Fatalf("no open end where simple begins")
	}
	// The terminus, the open start, and an open end when the route does not move on past the fork.
	want := 3
	if belt.Layout().Joins("simple") {
		want = 2
	}
	if len(metro.Ends) != want {
		t.Fatalf("%d ends, want %d", len(metro.Ends), want)
	}
}

// TestRunMetroARouteThatGoesBackRunsOnPastItsLastStationWithoutABar
func TestRunMetroARouteThatGoesBackRunsOnPastItsLastStationWithoutABar(t *testing.T) {
	t.Parallel()
	// Resolve conflicts and Fix checks go back to the watch; Wrap up joins the line again.
	_, metro := runMetroMetro(t, DefaultsBelt([]string{"acme/billing"}, "agent", DefaultsBuiltInAgents(), true), nil)
	var points []MapPoint
	for _, segment := range metro.Segments {
		for _, subpath := range segment.Path.Subpaths {
			points = append(points, subpath...)
		}
	}
	for _, id := range []string{"resolve-conflicts", "fix-checks"} {
		stop := runMetroStation(t, metro, id)
		// The line runs on past the station rather than stopping at its glyph.
		found := slices.ContainsFunc(points, func(p MapPoint) bool {
			return math.Abs(p.X-(stop.X+runMetroLayoutBackStub)) < 0.5 && math.Abs(p.Y-stop.Y) < 0.5
		})
		if !found {
			t.Fatalf("the line does not run on past %s", id)
		}
	}
	// The terminus is the only bar: none sits on a route that goes back.
	if len(metro.Ends) != 1 {
		t.Fatalf("%d ends, want 1", len(metro.Ends))
	}
}

// TestRunMetroARouteThatEndsTheRunRunsOnToABarPastItsLastStation
func TestRunMetroARouteThatEndsTheRunRunsOnToABarPastItsLastStation(t *testing.T) {
	t.Parallel()
	belt := runMetroForkedBelt([]string{"build"})
	// Quick build drops the run, so the simple route never meets the line again.
	index := -1
	for i, stage := range belt.Stages {
		if stage.ID == "quick-build" {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("the belt has no quick-build stage")
	}
	belt.Stages[index].Outcomes = []Outcome{runMetroOutcome("dropped", ToneNeutral, Then{Action: ThenActionDrop})}
	_, metro := runMetroMetro(t, belt, nil)
	quick := runMetroStation(t, metro, "quick-build")
	found := slices.ContainsFunc(metro.Ends, func(end MapPoint) bool {
		return math.Abs(end.X-(quick.X+runMetroLayoutEndStub)) < 0.5 && math.Abs(end.Y-quick.Y) < 0.5
	})
	if !found {
		t.Fatalf("no bar past quick-build")
	}
}

// TestRunMetroAShippedRunHasNoCurrentStationAndTravelsTheWholeTakenLine checks a completed run, shipped
// or done, has no current station and travels its whole taken line.
func TestRunMetroAShippedRunHasNoCurrentStationAndTravelsTheWholeTakenLine(t *testing.T) {
	t.Parallel()
	for _, state := range []RunState{RunStateShipped, RunStateDone} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			belt := runMetroForkedBelt([]string{"build"})
			_, metro := runMetroMetro(t, belt, func(r *Run) {
				r.SetRoutes(map[string][]string{"size": {"complex"}})
				r.StageID = "ship"
				r.State = state
			})
			for _, station := range metro.Stations {
				if station.State == RunMetroStationStateCurrent {
					t.Fatalf("%s is current on a %s run", station.ID, state)
				}
			}
			if got := runMetroStation(t, metro, "verify").State; got != RunMetroStationStateDone {
				t.Fatalf("verify is %s, want done", got)
			}
			if got := runMetroStation(t, metro, "quick-build").State; got != RunMetroStationStateSkip {
				t.Fatalf("quick-build is %s, want skip", got)
			}
			if len(runMetroTravelled(metro, RunMetroTravelTodo)) != 0 {
				t.Fatalf("a %s run still has a stretch to come", state)
			}
		})
	}
}

// TestRunMetroTheMapRoundTripsThroughJSON
func TestRunMetroTheMapRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	_, metro := runMetroMetro(t, runMetroNestedBelt(false, true, true), func(r *Run) {
		r.SetRoutes(map[string][]string{"wave": {"build"}, "pace": {"quick"}})
	})
	data, err := jsonx.Marshal(metro)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded RunMetro
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(decoded, metro) {
		t.Fatalf("the map does not round trip")
	}
}

// TestRunMetroRoutePillsSitOnTheirTrackBeforeTheirFirstStation checks, on the forked, feeding and nested fixtures,
// that each route's name pill is centred on the track its first station sits on, that it ends before that station,
// and that no station, name or chip sits under it.
func TestRunMetroRoutePillsSitOnTheirTrackBeforeTheirFirstStation(t *testing.T) {
	t.Parallel()
	feeding := runMetroForkedBelt([]string{"build"})
	feeding.Forks[0].Routes[1].Feeds = true
	for _, belt := range []Belt{runMetroForkedBelt([]string{"build"}), feeding, runMetroNestedBelt(true, true, true)} {
		_, metro := runMetroMetro(t, belt, nil)
		if len(metro.Routes) == 0 {
			t.Fatalf("%s has no route pills", belt.ID)
		}
		for _, tag := range metro.Routes {
			if tag.Width <= 0 || tag.Height != runMetroLayoutPillHeight {
				t.Fatalf("%s: pill %v has no size", tag.ID, tag)
			}
			centre := tag.Y + tag.Height/2
			var first *RunMetroStation
			for i := range metro.Stations {
				station := &metro.Stations[i]
				if station.Y == centre && station.X > tag.X && (first == nil || station.X < first.X) {
					first = station
				}
			}
			if first != nil && tag.X+tag.Width > first.X-runMetroLayoutGlyphReach {
				t.Fatalf("%s: pill %v runs into %s", tag.ID, tag, first.ID)
			}
			if tag.X < metro.Trigger.Box.X+metro.Trigger.Box.Width {
				t.Fatalf("%s: pill %v starts before the belt's name", tag.ID, tag)
			}
			pill := RunMetroBox{X: tag.X, Y: tag.Y, Width: tag.Width, Height: tag.Height}
			for _, station := range metro.Stations {
				glyph := RunMetroBox{X: station.X - runMetroLayoutGlyphReach, Y: station.Y - runMetroLayoutGlyphReach,
					Width: 2 * runMetroLayoutGlyphReach, Height: 2 * runMetroLayoutGlyphReach}
				if runMetroOverlaps(pill, glyph) || runMetroOverlaps(pill, station.Label) || runMetroOverlaps(pill, station.StatusBox) {
					t.Fatalf("%s: pill %v covers %s", tag.ID, tag, station.ID)
				}
			}
		}
	}
}
