// Tests of what Insights counts as shipped: only Shipped runs make the ship rate's numerator and the
// shipped lead time and cost, while Done joins Shipped and Dropped among the finished runs.

package core

import (
	"testing"
	"time"
)

// insightsShipMix is one Shipped, one Done review and one Dropped run, of different durations and
// costs, all finished in the last week with a good first try except the dropped run. The shipped
// run is the cheapest, so a cost per run that counted the others would move.
func insightsShipMix(t *testing.T) []Run {
	t.Helper()
	shippedCost, reviewCost, droppedCost := 1_000_000_000, 9_000_000_000, 5_000_000_000
	shipped := insightsTestRun(t, 1, insightsNow.Add(-5*time.Hour))
	insightsAdd(&shipped, insightsAttemptArgs{stage: insightsStageOf(t, shipped, StageKindAgent, 0), number: 1, ended: insightsNow.Add(-time.Hour), length: 2 * time.Hour, tone: ToneGood, cost: &shippedCost, model: "sonnet"})
	shipped.State = RunStateShipped
	review := insightsTestRun(t, 2, insightsNow.Add(-4*time.Hour))
	insightsAdd(&review, insightsAttemptArgs{stage: insightsStageOf(t, review, StageKindAgent, 0), number: 1, ended: insightsNow.Add(-2 * time.Hour), length: time.Hour, tone: ToneGood, cost: &reviewCost, model: "sonnet"})
	review.State = RunStateDone
	dropped := insightsTestRun(t, 3, insightsNow.Add(-3*time.Hour))
	insightsAdd(&dropped, insightsAttemptArgs{stage: insightsStageOf(t, dropped, StageKindAgent, 0), number: 1, ended: insightsNow.Add(-2 * time.Hour), tone: ToneProblem, cost: &droppedCost, model: "sonnet"})
	dropped.State = RunStateDropped
	return []Run{shipped, review, dropped}
}

// TestInsightsCountsOnlyShippedRunsAsShipped checks the one-of-three example: the ship rate is 1 of 3
// finished runs, the shipped lead time is the Shipped run's alone, and Done's attempts still count
// towards the first try.
func TestInsightsCountsOnlyShippedRunsAsShipped(t *testing.T) {
	t.Parallel()
	row := Insights(insightsShipMix(t), nil, InsightsQuery{Period: InsightsPeriodWeek}, insightsNow, time.UTC).BeltRows[0]
	if row.ShipRate.Percent != "33%" {
		t.Errorf("ship rate = %+v, want 1 of 3", row.ShipRate)
	}
	if row.LeadTime != "4h 0m" {
		t.Errorf("lead time = %q, want the shipped run's 4h 0m alone", row.LeadTime)
	}
	if row.FirstTry.Percent != "67%" {
		t.Errorf("first try = %+v, want the review's attempt counted: 2 of 3", row.FirstTry)
	}
	if row.CostPerRun != CostDollars(1_000_000_000) {
		t.Errorf("cost per run = %q, want the shipped run's cost alone", row.CostPerRun)
	}
}

// TestInsightsStagesCountADoneRunsAttempts checks that a Done run's visit to a stage counts in the
// stage's figures and in its model's, as a Shipped run's does: it is finished, so it is not left out
// of the first try because it did not ship.
func TestInsightsStagesCountADoneRunsAttempts(t *testing.T) {
	t.Parallel()
	runs := insightsShipMix(t)
	view := Insights(runs, nil, InsightsQuery{Belt: runs[0].BeltID, Period: InsightsPeriodWeek}, insightsNow, time.UTC)
	stage := insightsStage(t, view, insightsStageOf(t, runs[0], StageKindAgent, 0))
	if stage.Figures.Visits != 3 || stage.Figures.FirstTry.Percent != "67%" {
		t.Errorf("stage visits %d, first try %+v, want all three visits and 2 of 3 first tries", stage.Figures.Visits, stage.Figures.FirstTry)
	}
	if len(stage.Models) != 1 || stage.Models[0].Model != "sonnet" || stage.Models[0].Figures.Visits != 3 {
		t.Errorf("models = %+v, want sonnet with all three visits", stage.Models)
	}
}

// TestInsightsDoneOnlyHasNoShippedSample checks that runs which only finished as Done ship nothing:
// a 0% ship rate with no shipped lead time or cost sample.
func TestInsightsDoneOnlyHasNoShippedSample(t *testing.T) {
	t.Parallel()
	runs := insightsShipMix(t)
	review := runs[1]
	review.ID = "review-only"
	review.Subject.Number = 9
	row := Insights([]Run{review}, nil, InsightsQuery{Period: InsightsPeriodWeek}, insightsNow, time.UTC).BeltRows[0]
	if row.ShipRate.Percent != "0%" {
		t.Errorf("ship rate = %+v, want 0%%", row.ShipRate)
	}
	if row.LeadTime != "–" || row.CostPerRun != "–" {
		t.Errorf("lead time %q and cost %q, want no shipped sample", row.LeadTime, row.CostPerRun)
	}
}

// TestRunStateDoneIsFinishedForInsightsEvolution checks that Done is a finished run of the Evolution
// view, and that only Shipped counts as shipped there.
func TestRunStateDoneIsFinishedForInsightsEvolution(t *testing.T) {
	t.Parallel()
	runs := insightsShipMix(t)
	for i := range runs {
		_, finished := insightsFactsOf(&runs[i], 1)
		if finished == runs[i].State.IsActive() {
			t.Errorf("%s: finished %t", runs[i].State, finished)
		}
	}
}
