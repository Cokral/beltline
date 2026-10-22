// Tests of the Evolution view: columns by version and by week, where recent starts, shadow runs,
// what a run counts (sends back, tries again, approvals, holds and waits) and the figures.

package core

import (
	"slices"
	"testing"
	"time"
)

// evolutionRun is a finished run of the default belt on version, numbered n, with one Triage attempt
// that ends at ended after length.
func evolutionRun(t *testing.T, n, version int, ended time.Time, length time.Duration, shipped bool) Run {
	t.Helper()
	run := insightsTestRun(t, n, ended.Add(-length))
	run.BeltVersion = version
	insightsAdd(&run, insightsAttemptArgs{stage: insightsStageOf(t, run, StageKindAgent, 0), number: 1, ended: ended, length: length, tone: ToneGood})
	run.State = RunStateDropped
	if shipped {
		run.State = RunStateShipped
	}
	return run
}

func evolutionLabels(view InsightsEvolutionView) []string {
	out := []string{}
	for _, column := range view.Columns {
		out = append(out, column.Label)
	}
	return out
}

func evolutionFigure(t *testing.T, view InsightsEvolutionView, id InsightsFigureID) InsightsFigure {
	t.Helper()
	for _, figure := range view.Figures {
		if figure.ID == id {
			return figure
		}
	}
	t.Fatalf("no figure %q", id)
	return InsightsFigure{}
}

// evolutionRuns is count runs on each version in order, an hour apart, every one shipped.
func evolutionRuns(t *testing.T, counts map[int]int, versions ...int) []Run {
	t.Helper()
	var runs []Run
	at := insightsNow.Add(-30 * 24 * time.Hour)
	for _, version := range versions {
		for range counts[version] {
			at = at.Add(time.Hour)
			runs = append(runs, evolutionRun(t, len(runs)+1, version, at, 10*time.Minute, true))
		}
	}
	return runs
}

// TestEvolutionMergesSmallVersions checks that versions merge until a column has five runs, that a
// short last column joins the one before it, and that Separate keeps every version apart.
func TestEvolutionMergesSmallVersions(t *testing.T) {
	t.Parallel()
	runs := evolutionRuns(t, map[int]int{1: 2, 2: 3, 3: 6, 4: 1, 5: 1}, 1, 2, 3, 4, 5)
	belt := runs[0].BeltID
	view := InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: belt}, time.UTC)
	if got := evolutionLabels(view); !slices.Equal(got, []string{"v1–2", "v3–5"}) {
		t.Fatalf("merged columns = %v", got)
	}
	if view.Columns[1].Runs != 8 {
		t.Fatalf("v3–5 has %d runs, want 8", view.Columns[1].Runs)
	}
	view = InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: belt, Separate: true}, time.UTC)
	if got := evolutionLabels(view); !slices.Equal(got, []string{"v1", "v2", "v3", "v4", "v5"}) {
		t.Fatalf("separate columns = %v", got)
	}
}

// TestEvolutionRecentStartsAtTheLastThird checks the default first recent column, one asked for, and
// one that is out of range.
func TestEvolutionRecentStartsAtTheLastThird(t *testing.T) {
	t.Parallel()
	runs := evolutionRuns(t, map[int]int{1: 3, 2: 3, 3: 3}, 1, 2, 3)
	belt := runs[0].BeltID
	view := InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: belt, Separate: true}, time.UTC)
	if view.From != 2 || view.Split != "Before is v1 to v2 (6 runs); recent is v3 (3 runs)." {
		t.Fatalf("from %d, split %q", view.From, view.Split)
	}
	if view = InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: belt, Separate: true, From: 1}, time.UTC); view.From != 1 {
		t.Fatalf("asked from 1, got %d", view.From)
	}
	if view = InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: belt, Separate: true, From: 9}, time.UTC); view.From != 2 {
		t.Fatalf("an out of range from should fall back to 2, got %d", view.From)
	}
	one := InsightsEvolution(runs[:3], nil, InsightsEvolutionQuery{Belt: belt}, time.UTC)
	if one.From != 0 || one.Split != "Too few runs on this belt to compare." {
		t.Fatalf("one column: from %d, split %q", one.From, one.Split)
	}
}

// TestEvolutionWeeks checks that weeks start on Monday in the given zone.
func TestEvolutionWeeks(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	runs := []Run{
		evolutionRun(t, 1, 1, monday, time.Minute, true),
		evolutionRun(t, 2, 1, monday.Add(6*24*time.Hour), time.Minute, true),
		evolutionRun(t, 3, 2, monday.Add(7*24*time.Hour), time.Minute, false),
	}
	view := InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: runs[0].BeltID, Axis: InsightsEvolutionByWeek}, time.UTC)
	if got := evolutionLabels(view); !slices.Equal(got, []string{"Oct 5", "Oct 12"}) || view.Columns[0].Runs != 2 {
		t.Fatalf("weeks = %v (%+v)", got, view.Columns)
	}
}

// TestEvolutionFoldsShadowRunsIntoTheirLiveVersion checks that a run on a shadow version counts
// towards the live version its test shadowed, and one of an unknown test keeps its own.
func TestEvolutionFoldsShadowRunsIntoTheirLiveVersion(t *testing.T) {
	t.Parallel()
	runs := evolutionRuns(t, map[int]int{3: 2}, 3)
	shadow := evolutionRun(t, 9, 7, insightsNow, time.Minute, true)
	shadow.Shadow = &RunShadow{Test: "t1", Arm: ShadowArmShadow}
	stray := evolutionRun(t, 10, 8, insightsNow, time.Minute, true)
	stray.Shadow = &RunShadow{Test: "t9", Arm: ShadowArmShadow}
	runs = append(runs, shadow, stray)
	library := []InsightsLibraryBelt{{ID: runs[0].BeltID, Name: "Belt", ShadowTests: []ShadowTest{{ID: "t1", Live: 3, Shadow: 7}}}}
	view := InsightsEvolution(runs, library, InsightsEvolutionQuery{Belt: runs[0].BeltID, Separate: true}, time.UTC)
	if got := evolutionLabels(view); !slices.Equal(got, []string{"v3", "v8"}) || view.Columns[0].Runs != 3 {
		t.Fatalf("columns = %v (%+v)", got, view.Columns)
	}
}

// TestEvolutionCountsWhatARunDid checks a run's sends back, tries again, approvals, holds, and the
// time it waited on you after a hold.
func TestEvolutionCountsWhatARunDid(t *testing.T) {
	t.Parallel()
	start := insightsNow.Add(-10 * time.Hour)
	run := insightsTestRun(t, 1, start)
	first, second := insightsStageOf(t, run, StageKindAgent, 0), insightsStageOf(t, run, StageKindAgent, 1)
	gate := insightsStageOf(t, run, StageKindGate, 0)
	insightsAdd(&run, insightsAttemptArgs{stage: first, number: 1, ended: start.Add(time.Hour), length: time.Hour, tone: ToneGood})
	insightsAdd(&run, insightsAttemptArgs{stage: second, number: 1, ended: start.Add(2 * time.Hour), length: time.Hour, tone: ToneProblem})
	run.Attempts[len(run.Attempts)-1].Route = new("Held for you")
	insightsAdd(&run, insightsAttemptArgs{stage: second, number: 2, ended: start.Add(4 * time.Hour), length: time.Hour, tone: ToneGood})
	insightsAdd(&run, insightsAttemptArgs{stage: first, number: 2, ended: start.Add(5 * time.Hour), length: time.Hour, tone: ToneGood})
	insightsAdd(&run, insightsAttemptArgs{stage: gate, number: 1, ended: start.Add(6 * time.Hour), length: time.Hour, tone: ToneGood})
	run.State = RunStateShipped
	firstIndex, secondIndex := run.StageIndexOf(first), run.StageIndexOf(second)
	if firstIndex > secondIndex {
		t.Fatalf("the test needs %s before %s", first, second)
	}
	f, ok := insightsFactsOf(&run, run.BeltVersion)
	if !ok {
		t.Fatal("a shipped run has facts")
	}
	want := run.Stages[secondIndex].Name + " → " + run.Stages[firstIndex].Name
	if !slices.Equal(f.sends, []string{want}) || f.retries != 1 || f.visits != 4 {
		t.Fatalf("sends %v, retries %d, visits %d", f.sends, f.retries, f.visits)
	}
	if f.holds != 1 || f.approvals != 1 || f.hold != 3600 || f.approve != 3600 || f.agent != 4*3600 || f.lead != 6*3600 {
		t.Fatalf("holds %d, approvals %d, hold %v, approve %v, agent %v, lead %v", f.holds, f.approvals, f.hold, f.approve, f.agent, f.lead)
	}
}

// TestEvolutionFigures checks a figure's recent value against before, and the ship rate per column.
// Lead time counts the shipped runs only: the dropped run's 30 minutes is left out of v1.
func TestEvolutionFigures(t *testing.T) {
	t.Parallel()
	var runs []Run
	at := insightsNow.Add(-10 * 24 * time.Hour)
	for i, minutes := range []int{10, 20, 30, 5, 5, 5} {
		version := 1 + i/3
		at = at.Add(time.Hour)
		runs = append(runs, evolutionRun(t, i+1, version, at, time.Duration(minutes)*time.Minute, i != 2))
	}
	view := InsightsEvolution(runs, nil, InsightsEvolutionQuery{Belt: runs[0].BeltID, Separate: true}, time.UTC)
	lead := evolutionFigure(t, view, InsightsFigureLead)
	if lead.Value != "5m 0s" || lead.Change != "↓ −67%" || len(lead.Dots) != 5 || lead.Spread == nil {
		t.Fatalf("lead %q, change %q, %d dots", lead.Value, lead.Change, len(lead.Dots))
	}
	if lead.Columns[0].Runs != 2 || lead.Columns[0].Value == nil || *lead.Columns[0].Value != 900 {
		t.Fatalf("v1 has %d shipped runs with median %v, want 2 and 900", lead.Columns[0].Runs, lead.Columns[0].Value)
	}
	ship := evolutionFigure(t, view, InsightsFigureShipRate)
	if ship.Value != "100%" || ship.Change != "↑ +33 pts" || ship.Columns[0].Display != "2 of 3 shipped · 67%" || ship.Spread != nil {
		t.Fatalf("ship rate %q, change %q, column %q", ship.Value, ship.Change, ship.Columns[0].Display)
	}
}

// TestEvolutionLeadAndCostCountOnlyShippedRuns checks the lead time and cost per run of a shipped run
// beside two longer, dearer Done reviews: the shipped run alone gives them, a Done-only set gives no
// sample, and the ship rate counts every finished run.
func TestEvolutionLeadAndCostCountOnlyShippedRuns(t *testing.T) {
	t.Parallel()
	shipped := evolutionRun(t, 1, 1, insightsNow.Add(-5*time.Hour), 10*time.Minute, true)
	insightsPrice(&shipped, 1e9)
	var reviews []Run
	for n := range 2 {
		review := evolutionRun(t, n+2, 1, insightsNow.Add(-time.Duration(n+2)*time.Hour), 3*time.Hour, false)
		review.State = RunStateDone
		insightsPrice(&review, 9e9)
		reviews = append(reviews, review)
	}

	mixed := InsightsEvolution(append([]Run{shipped}, reviews...), nil, InsightsEvolutionQuery{Belt: shipped.BeltID}, time.UTC)
	if lead := evolutionFigure(t, mixed, InsightsFigureLead); lead.Value != "10m 0s" || len(lead.Dots) != 1 {
		t.Errorf("lead %q with %d dots, want the shipped run alone", lead.Value, len(lead.Dots))
	}
	if cost := evolutionFigure(t, mixed, InsightsFigureCost); cost.Value != CostDollars(1e9) || len(cost.Dots) != 1 {
		t.Errorf("cost %q with %d dots, want the shipped run's $1.00 alone", cost.Value, len(cost.Dots))
	}
	if ship := evolutionFigure(t, mixed, InsightsFigureShipRate); ship.Value != "33%" || ship.Columns[0].Display != "1 of 3 shipped · 33%" {
		t.Errorf("ship rate %q, column %q, want 1 of 3", ship.Value, ship.Columns[0].Display)
	}

	done := InsightsEvolution(reviews, nil, InsightsEvolutionQuery{Belt: shipped.BeltID}, time.UTC)
	if lead := evolutionFigure(t, done, InsightsFigureLead); lead.Value != "–" || len(lead.Dots) != 0 {
		t.Errorf("done only: lead %q with %d dots, want no sample", lead.Value, len(lead.Dots))
	}
	if cost := evolutionFigure(t, done, InsightsFigureCost); cost.Value != "–" || len(cost.Dots) != 0 {
		t.Errorf("done only: cost %q with %d dots, want no sample", cost.Value, len(cost.Dots))
	}
	if ship := evolutionFigure(t, done, InsightsFigureShipRate); ship.Value != "0%" {
		t.Errorf("done only: ship rate %q, want 0%%", ship.Value)
	}
}

// TestEvolutionTicks checks that a scale always has ticks to read: tenths for a count under one, and
// the ends of a time range too narrow for any of the usual marks.
func TestEvolutionTicks(t *testing.T) {
	t.Parallel()
	counts := insightsLinearTicks(insightsCount, 0.9)
	if len(counts) < 3 || counts[1].Label != "0.2" {
		t.Fatalf("ticks under one = %+v", counts)
	}
	narrow := insightsLogTicks(insightsSeconds, 8400, 13000)
	if len(narrow) != 2 || narrow[0].Label != "2.3h" || narrow[1].Label != "3.6h" {
		t.Fatalf("narrow log ticks = %+v", narrow)
	}
}
