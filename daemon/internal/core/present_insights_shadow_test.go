package core

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestInsightsShadowComparesFinishedRunsOfTheTest checks a shadow test's view: only finished runs
// of the test count, runs still going are counted apart, stages both versions have are compared
// and a stage only the shadow has is listed on its own.
func TestInsightsShadowComparesFinishedRunsOfTheTest(t *testing.T) {
	t.Parallel()
	started := insightsNow.Add(-72 * time.Hour)
	base := insightsTestRun(t, 99, started)
	mark := func(run *Run, arm ShadowArm, version int) {
		// Every run copies the same definition, as the runs of one belt version do.
		run.Stages = slices.Clone(base.Stages)
		run.Shadow = &RunShadow{Test: "t1", Arm: arm}
		run.BeltVersion = version
	}
	finish := func(run *Run, shipped bool, tone Tone) {
		stage := insightsStageOf(t, *run, StageKindAgent, 0)
		insightsAdd(run, insightsAttemptArgs{stage: stage, number: 1, ended: run.CreatedAt.Add(time.Hour), tone: tone})
		run.State = RunStateShipped
		if !shipped {
			run.State = RunStateDropped
		}
		run.UpdatedAt = jsonx.Date{Time: run.CreatedAt.Add(time.Hour)}
	}
	var runs []Run
	for n := range 4 {
		run := insightsTestRun(t, n+1, started.Add(time.Duration(n)*time.Hour))
		mark(&run, ShadowArmLive, 1)
		finish(&run, n < 2, map[bool]Tone{true: ToneGood, false: ToneProblem}[n == 0])
		runs = append(runs, run)
	}
	for n := range 3 {
		run := insightsTestRun(t, n+10, started.Add(time.Duration(n)*time.Hour))
		mark(&run, ShadowArmShadow, 2)
		run.Stages = append(run.Stages, Stage{ID: "extra", Name: "Extra"})
		finish(&run, true, ToneGood)
		insightsPrice(&run, 1_000_000_000)
		if n == 2 {
			// The last one finished as Done: it is finished, but it is neither shipped nor in the
			// shipped lead time and cost, so it is long and dear to show when it would be counted.
			run.State = RunStateDone
			run.UpdatedAt = jsonx.Date{Time: run.CreatedAt.Add(4 * time.Hour)}
			insightsPrice(&run, 9_000_000_000)
		}
		runs = append(runs, run)
	}
	going := insightsTestRun(t, 20, started)
	mark(&going, ShadowArmShadow, 2)
	other := insightsTestRun(t, 21, started)
	finish(&other, false, ToneProblem)
	runs = append(runs, going, other)

	library := []InsightsLibraryBelt{{ID: going.BeltID, Name: "Belt", Version: 1, Stages: runs[0].Stages,
		ShadowTests: []ShadowTest{{ID: "t1", Live: 1, Shadow: 2, StartedAt: jsonx.Date{Time: started}}}}}
	query := InsightsQuery{Belt: going.BeltID, Shadow: "t1", Period: InsightsPeriodAll}
	if !InsightsHasBelt(query, library, runs) || InsightsHasBelt(InsightsQuery{Belt: going.BeltID, Shadow: "t9"}, library, runs) {
		t.Fatal("the shadow test is not found, or an unknown one is")
	}
	view := Insights(runs, library, query, insightsNow, time.UTC)
	shadow := view.Shadow
	if shadow == nil || !shadow.Running || shadow.Live.Finished != 4 || shadow.Shadow.Finished != 3 || shadow.Shadow.Going != 1 || shadow.Live.Going != 0 {
		t.Fatalf("arms %+v %+v", shadow.Live, shadow.Shadow)
	}
	if shadow.Live.Label != "Live v1" || shadow.Shadow.Label != "Shadow v2" {
		t.Fatalf("labels %q %q", shadow.Live.Label, shadow.Shadow.Label)
	}
	byID := map[InsightsKpiID]InsightsShadowFigure{}
	for _, figure := range shadow.Figures {
		byID[figure.ID] = figure
	}
	if f := byID[InsightsKpiShipRate]; f.Live.Value != "50%" || f.Shadow.Value != "67%" || f.Delta.Label != "+17 pts" || f.Delta.Tone != InsightsDeltaBetter {
		t.Fatalf("ship rate %+v, want 2 of 3 shadow runs shipped", f)
	}
	if f := byID[InsightsKpiLeadTime]; f.Shadow.Caption != "Median of 2 shipped" || f.Shadow.Value != StatusDuration(3600) {
		t.Fatalf("shadow lead time %+v, want the two shipped runs' hour", f)
	}
	if f := byID[InsightsKpiCost]; f.Shadow.Caption != "2 priced runs" || f.Shadow.Value != CostDollars(1_000_000_000) {
		t.Fatalf("shadow cost %+v, want the two shipped runs' $1.00", f)
	}
	if f := byID[InsightsKpiFirstTry]; f.Live.Value != "25%" || f.Shadow.Value != "100%" || f.Live.Caption != "1 of 4 visits" {
		t.Fatalf("first try %+v", f)
	}
	if len(shadow.OnlyShadow) != 1 || shadow.OnlyShadow[0].Key != "extra" || len(shadow.OnlyLive) != 0 {
		t.Fatalf("only %+v %+v", shadow.OnlyShadow, shadow.OnlyLive)
	}
	first := shadow.Stages[0]
	if first.Live.Visits != 4 || first.Shadow.Visits != 3 || first.Tones.FirstTry != InsightsDeltaBetter || first.Changed {
		t.Fatalf("first stage %+v", first)
	}
	if len(view.Stages) != 0 || view.Caption != "7 finished · 1 going" {
		t.Fatalf("view %q %d", view.Caption, len(view.Stages))
	}
	found := false
	for _, choice := range view.Versions {
		found = found || (choice.Shadow == "t1" && choice.Label == "Shadow test · v1 vs v2")
	}
	if !found {
		t.Fatalf("versions %+v", view.Versions)
	}

	// An archived belt keeps the same shadow comparison, and stays in the picker for the query that names it.
	library[0].Archived = true
	archived := Insights(runs, library, query, insightsNow, time.UTC)
	if !reflect.DeepEqual(archived.Shadow, view.Shadow) || archived.Caption != view.Caption || !slices.Contains(insightsBeltIDs(archived.Belts), going.BeltID) {
		t.Fatalf("archived shadow %+v, picker %v", archived.Shadow, archived.Belts)
	}
}
