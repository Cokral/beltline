package core

import "testing"

// TestMetroRangeOverlapsAsClosedRanges covers ClosedRange.overlaps, which the track search uses to tell
// whether two lanes' rows share a point: touching ends overlap, disjoint ranges do not.
func TestMetroRangeOverlapsAsClosedRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b MetroRange
		want bool
	}{
		{MetroRange{Lower: 0, Upper: 10}, MetroRange{Lower: 2, Upper: 3}, true},
		{MetroRange{Lower: 2, Upper: 3}, MetroRange{Lower: 0, Upper: 10}, true},
		{MetroRange{Lower: 0, Upper: 1}, MetroRange{Lower: 1, Upper: 2}, true},
		{MetroRange{Lower: 0, Upper: 1}, MetroRange{Lower: 1.5, Upper: 2}, false},
	}
	for _, c := range cases {
		if got := c.a.overlaps(c.b); got != c.want {
			t.Fatalf("%v overlaps %v = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestMetroLayoutWrapsLongNamesAtTheSpaceNearestTheMiddle covers MetroLayout.wrap: a name at most limit
// characters long stays on one line; a longer one breaks at the space nearest its middle.
func TestMetroLayoutWrapsLongNamesAtTheSpaceNearestTheMiddle(t *testing.T) {
	t.Parallel()
	if got := metroLayoutWrap("Build", 15); len(got) != 1 || got[0] != "Build" {
		t.Fatalf("short name wraps to %q", got)
	}
	got := metroLayoutWrap("Resolve conflicts now", 15)
	if len(got) != 2 || got[0] != "Resolve" || got[1] != "conflicts now" {
		t.Fatalf("long name wraps to %q, want [Resolve, conflicts now]", got)
	}
	if got := metroLayoutWrap("Averyveryverylongword", 15); len(got) != 1 {
		t.Fatalf("a name with no space wraps to %q", got)
	}
}

// metroLayoutRunAt is a run of belt on issue number, at stage in state.
func metroLayoutRunAt(belt Belt, number int, state RunState, stage string) Run {
	run := NewRun(RunArgs{Belt: belt, Subject: Subject{Kind: SubjectKindIssue, Repo: "a/b", Number: number, Title: "Issue"}, Now: testT0})
	run.StageID = stage
	run.State = state
	return run
}

// metroLayoutFor is the track of belts and its metro for runs.
func metroLayoutFor(runs []Run, belts ...Belt) (FactoryTrack, MetroLayout) {
	track := factoryTrackTestFloor(belts...)
	return track, NewMetroLayout(MetroLayoutArgs{Track: track, Runs: runs})
}

// metroLayoutStation is the station of stage name on the metro, failing the test when there is none.
func metroLayoutStation(t *testing.T, metro MetroLayout, name string) MetroLayoutStation {
	t.Helper()
	for _, station := range metro.Stations {
		if station.Name == name {
			return station
		}
	}
	t.Fatalf("the metro has no station named %q", name)
	return MetroLayoutStation{}
}

// TestMetroDotsStopAtThreeAndCountTheRest checks a station draws one dot per run up to three; the last dot holds
// the rest as overflow, and exactly three runs need no overflow.
func TestMetroDotsStopAtThreeAndCountTheRest(t *testing.T) {
	t.Parallel()
	belt := metroPlanTestBelt("a", []string{"Build"})
	stage := belt.Stages[0].ID
	var five []Run
	for n := 1; n <= 5; n++ {
		five = append(five, metroLayoutRunAt(belt, n, RunStateRunning, stage))
	}
	_, crowded := metroLayoutFor(five, belt)
	full, overflow := 0, -1
	for _, dot := range crowded.Dots {
		if dot.Overflow == 0 {
			full++
		} else if dot.Overflow > 0 {
			overflow = dot.Overflow
		}
	}
	if full != 2 || overflow != 3 {
		t.Fatalf("five runs drew %d full dots and overflow %d, want 2 and 3", full, overflow)
	}
	three := five[:3]
	_, exact := metroLayoutFor(three, belt)
	if len(exact.Dots) != 3 {
		t.Fatalf("%d dots for three runs, want 3", len(exact.Dots))
	}
	for _, dot := range exact.Dots {
		if dot.Overflow != 0 {
			t.Fatalf("three runs overflow")
		}
	}
}

// TestMetroLabelsNameRunsAndUrgentOnesAreUrgent checks a station's label names its runs, and a run that waits for
// you or is held gets an urgent label of its own.
func TestMetroLabelsNameRunsAndUrgentOnesAreUrgent(t *testing.T) {
	t.Parallel()
	belt := metroPlanTestBelt("a", []string{"Build"})
	stage := belt.Stages[0].ID
	runs := []Run{
		metroLayoutRunAt(belt, 1, RunStateRunning, stage),
		metroLayoutRunAt(belt, 2, RunStateRunning, stage),
		metroLayoutRunAt(belt, 3, RunStateWaiting, stage),
		metroLayoutRunAt(belt, 4, RunStateHeld, stage),
		metroLayoutRunAt(belt, 5, RunStateQueued, stage),
	}
	_, five := metroLayoutFor(runs, belt)
	var label *MetroLayoutLabel
	for i := range five.Labels {
		if len(five.Labels[i].RunIDs) == 5 {
			label = &five.Labels[i]
			break
		}
	}
	if label == nil {
		t.Fatalf("no label names five runs")
	}
	if label.Text != "#1 #2 held +3" {
		t.Fatalf("label %q, want %q", label.Text, "#1 #2 held +3")
	}
	if !label.NeedsYou {
		t.Fatalf("the five-run label does not need you")
	}
	_, you := metroLayoutFor([]Run{metroLayoutRunAt(belt, 9, RunStateWaiting, stage)}, belt)
	if len(you.Labels) == 0 || you.Labels[0].Text != "#9 you" {
		t.Fatalf("waiting label is not #9 you")
	}
	if you.Labels[0].Urgent == nil || *you.Labels[0].Urgent != MetroLayoutLookWaiting {
		t.Fatalf("waiting label is not urgent waiting")
	}
	_, held := metroLayoutFor([]Run{metroLayoutRunAt(belt, 8, RunStateHeld, stage)}, belt)
	if len(held.Labels) == 0 || held.Labels[0].Text != "#8 held" {
		t.Fatalf("held label is not #8 held")
	}
	if held.Labels[0].Urgent == nil || *held.Labels[0].Urgent != MetroLayoutLookHeld {
		t.Fatalf("held label is not urgent held")
	}
}

// TestMetroDotLookFollowsRunState checks each run state that has a look draws its dot with that look, with a halo
// exactly when the run needs you; shipped and dropped runs have no look and no dot.
func TestMetroDotLookFollowsRunState(t *testing.T) {
	t.Parallel()
	belt := metroPlanTestBelt("a", []string{"Build"})
	stage := belt.Stages[0].ID
	for _, state := range []RunState{RunStateRunning, RunStateWaiting, RunStateHeld, RunStateQueued, RunStateParked} {
		_, single := metroLayoutFor([]Run{metroLayoutRunAt(belt, 40, state, stage)}, belt)
		if len(single.Dots) == 0 {
			t.Fatalf("%s has no dot", state)
		}
		dot := single.Dots[0]
		want := MetroLayoutLookFor(state)
		if want == nil || dot.Look != *want {
			t.Fatalf("the dot of %s has look %v, want %v", state, dot.Look, want)
		}
		if dot.Halo != state.NeedsYou() {
			t.Fatalf("the dot of %s has halo %v, want %v", state, dot.Halo, state.NeedsYou())
		}
	}
	if MetroLayoutLookFor(RunStateShipped) != nil || MetroLayoutLookFor(RunStateDone) != nil || MetroLayoutLookFor(RunStateDropped) != nil {
		t.Fatalf("shipped, done or dropped runs have a look")
	}
	for _, state := range []RunState{RunStateShipped, RunStateDone} {
		_, finished := metroLayoutFor([]Run{metroLayoutRunAt(belt, 41, state, stage)}, belt)
		if len(finished.Dots) != 0 {
			t.Fatalf("a %s run has %d dots", state, len(finished.Dots))
		}
	}
}

// TestMetroStationStatusIsTheBoxesStatusOfItsRuns checks each station's chip shows the text and look the boxes
// Floor shows for the runs at its machine, every belt's runs counted once.
func TestMetroStationStatusIsTheBoxesStatusOfItsRuns(t *testing.T) {
	t.Parallel()
	a := metroPlanTestBelt("a", []string{"Triage", "Build", "Ship"})
	b := metroPlanTestBelt("b", []string{"Triage", "Ship"})
	c := metroPlanTestBelt("c", []string{"Build", "Ship"})
	d := metroPlanTestBelt("d", []string{"Review"})
	runs := []Run{
		metroLayoutRunAt(a, 1, RunStateRunning, "a-0"),
		metroLayoutRunAt(a, 2, RunStateRunning, "a-0"),
		metroLayoutRunAt(a, 3, RunStateRunning, "a-0"),
		metroLayoutRunAt(a, 4, RunStateRunning, "a-0"),
		metroLayoutRunAt(a, 5, RunStateRunning, "a-0"),
		metroLayoutRunAt(a, 6, RunStateWaiting, "a-1"),
		metroLayoutRunAt(c, 7, RunStateRunning, "c-0"),
		metroLayoutRunAt(b, 8, RunStateHeld, "b-1"),
		metroLayoutRunAt(b, 9, RunStateRunning, "b-0"),
		metroLayoutRunAt(a, 10, RunStateRunning, "a-2"),
		metroLayoutRunAt(b, 11, RunStateQueued, "b-0"),
	}
	track, metro := metroLayoutFor(runs, a, b, c, d)
	want := map[string]string{"Triage": "6 running", "Build": "1 waiting for you", "Ship": "1 held · 1 running", "Review": "Idle"}
	for name, text := range want {
		station := metroLayoutStation(t, metro, name)
		machine := track.Machine(station.ID)
		if machine == nil {
			t.Fatalf("%s has no machine", name)
		}
		here := track.RunsAt(*machine, runs)
		wantText, wantLook := OverviewBoxesStatus(here)
		if station.Status.Text != wantText || station.Status.Text != text {
			t.Fatalf("%s shows %q, want %q (the boxes status of its %d runs is %q)", name, station.Status.Text, text, len(here), wantText)
		}
		if station.Status.Look != wantLook {
			t.Fatalf("%s is %s, want %s", name, station.Status.Look, wantLook)
		}
	}
}

// TestMetroEmptyRouteRejoinsAfterItsFork checks a route with no stages leaves its fork as a branch with no stops
// and rejoins the line at the stage after the fork.
func TestMetroEmptyRouteRejoinsAfterItsFork(t *testing.T) {
	t.Parallel()
	belt := metroPlanTestBelt("a", []string{"Triage", "Build", "Ship"})
	belt.Forks = []Fork{{ID: "f", Routes: []Route{{ID: "work", Name: "Work"}, {ID: "skip", Name: "Skip"}}}}
	belt.Stages[1].Route = new("work")
	_, metro := metroLayoutFor(nil, belt)
	line := metro.Line("a")
	if line == nil {
		t.Fatalf("the metro has no line for a")
	}
	found := false
	for _, branch := range line.Branches {
		if branch.From != nil && *branch.From == "triage" && branch.To != nil && *branch.To == "ship" && len(branch.Stops) == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the empty route does not rejoin at ship")
	}
}

// TestMetroLinesSkipStationsTheyDoNotStopAt checks a belt that skips a shared stage gets a track outside the
// station's column, so no line runs through an interchange it does not stop at.
func TestMetroLinesSkipStationsTheyDoNotStopAt(t *testing.T) {
	t.Parallel()
	_, metro := metroLayoutFor(nil,
		metroPlanTestBelt("a", []string{"Triage", "Build", "Ship"}),
		metroPlanTestBelt("b", []string{"Triage", "Ship"}),
		metroPlanTestBelt("c", []string{"Triage", "Build", "Ship"}))
	if got := len(metroLayoutStation(t, metro, "Build").Pieces); got != 1 {
		t.Fatalf("build has %d pieces, want 1", got)
	}
	for _, line := range metro.Lines {
		for _, station := range metro.Stations {
			_, stops := station.Stops[line.BeltID]
			if stops || station.Glyph != MetroLayoutGlyphInterchange {
				continue
			}
			for _, piece := range station.Pieces {
				inner := MetroLayoutBox{X: station.X - 6, Y: piece.Lower - 4, Width: 12, Height: piece.Upper - piece.Lower + 8}
				for _, points := range line.Strokes() {
					if metroLayoutCrosses(points, inner) {
						t.Fatalf("%s runs through %s", line.BeltID, station.Name)
					}
				}
			}
		}
	}
}

// TestMetroStationGlyphFollowsStageKind checks a stage several belts share is an interchange, and a stage one belt
// alone runs is drawn as its kind's glyph: an agent is a circle.
func TestMetroStationGlyphFollowsStageKind(t *testing.T) {
	t.Parallel()
	factory, metro := metroLayoutFor(nil,
		metroPlanTestBelt("a", []string{"Triage", "Build"}),
		metroPlanTestBelt("b", []string{"Build"}))
	if got := metroLayoutStation(t, metro, "Build").Glyph; got != MetroLayoutGlyphInterchange {
		t.Fatalf("a shared build is %s, want an interchange", got)
	}
	_, single := metroLayoutFor(nil, metroPlanTestBelt("c", []string{"Triage"}))
	station := metroLayoutStation(t, single, "Triage")
	machine := factory.Machine(station.ID)
	if machine == nil {
		t.Fatalf("no machine for triage")
	}
	if kind := machine.Members[0].Stage.Kind(); kind != StageKindAgent || station.Glyph != MetroLayoutGlyphCircle {
		t.Fatalf("triage is an %s drawn as %s, want an agent drawn as a circle", kind, station.Glyph)
	}
}

// TestMetroReturnLaneIsDrawnForAGotoBack checks a goto that sends a run back is a return, drawn as a return lane
// on the line it leaves.
func TestMetroReturnLaneIsDrawnForAGotoBack(t *testing.T) {
	t.Parallel()
	track, metro := metroLayoutFor(nil, metroPlanTestBelt("a", []string{"Build", "Verify"}), metroPlanTestBelt("b", []string{"Verify", "Build"}))
	if len(track.Returns) != 1 {
		t.Fatalf("%d returns, want 1", len(track.Returns))
	}
	lanes := 0
	for _, line := range metro.Lines {
		lanes += len(line.Returns)
	}
	if lanes != len(track.Returns) {
		t.Fatalf("%d return lanes drawn for %d returns", lanes, len(track.Returns))
	}
}

// metroLayoutCrosses reports whether a polyline runs through a box.
func metroLayoutCrosses(points []MetroLayoutPoint, box MetroLayoutBox) bool {
	for i := 0; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		for step := 0; step <= 40; step++ {
			f := float64(step) / 40
			if box.Contains(MetroLayoutPoint{X: a.X + (b.X-a.X)*f, Y: a.Y + (b.Y-a.Y)*f}, 0) {
				return true
			}
		}
	}
	return false
}

// TestMetroRepeatedStageNamesGetNumberedStations checks a stage name a belt repeats becomes a station of its own,
// numbered from the second occurrence, placed after the first.
func TestMetroRepeatedStageNamesGetNumberedStations(t *testing.T) {
	t.Parallel()
	_, metro := metroLayoutFor(nil,
		metroPlanTestBelt("a", []string{"Triage", "Ship", "Verify", "Ship", "Wrap up"}),
		metroPlanTestBelt("b", []string{"Ship", "Ship"}))
	var first, second *MetroLayoutStation
	for i := range metro.Stations {
		switch metro.Stations[i].ID {
		case "ship":
			first = &metro.Stations[i]
		case "ship #2":
			second = &metro.Stations[i]
		}
	}
	if second == nil || first == nil {
		t.Fatalf("the metro has no station ship or ship #2")
	}
	if !(first.X < second.X) {
		t.Fatalf("ship is not before ship #2")
	}
}
