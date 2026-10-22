// Insights over time: each figure as a 7-day rolling line, one point a day, and its change over
// the last 7 days against the 7 before.

package core

import (
	"math"
	"slices"
	"strconv"
	"time"
)

// insightsWindow is the rolling window and the span each change compares.
const insightsWindow = 7 * 24 * time.Hour

// insightsMinWindow is how many items a day's window needs for a point: fewer leave a gap, so one
// dropped run cannot draw the ship rate down to 0%.
const insightsMinWindow = 3

// insightsMaxDays bounds a line's length: longer periods show their last days.
const insightsMaxDays = 120

// InsightsPoint is one day of a 7-day rolling line: the figure over the 7 days that end that night.
// Value is nil when the window holds fewer than three items; Display is its label and Count how many items the
// window holds.
type InsightsPoint struct {
	Label   string   `json:"label"`
	Value   *float64 `json:"value"`
	Display string   `json:"display"`
	Count   int      `json:"count"`
}

// InsightsDeltaTone says whether a change is better, worse, too small to call, or not known.
type InsightsDeltaTone string

const (
	InsightsDeltaBetter InsightsDeltaTone = "better"
	InsightsDeltaWorse  InsightsDeltaTone = "worse"
	InsightsDeltaFlat   InsightsDeltaTone = "flat"
	InsightsDeltaNone   InsightsDeltaTone = "none"
)

// AllInsightsDeltaTone lists every InsightsDeltaTone in declaration order (CaseIterable).
var AllInsightsDeltaTone = []InsightsDeltaTone{InsightsDeltaBetter, InsightsDeltaWorse, InsightsDeltaFlat, InsightsDeltaNone}

// InsightsDelta is a figure's change over the last 7 days against the 7 before, such as "+12 pts"
// for a share or "−39%" for an amount. Caption says what was compared, or why nothing could be.
type InsightsDelta struct {
	Label   string            `json:"label"`
	Tone    InsightsDeltaTone `json:"tone"`
	Caption string            `json:"caption"`
}

// InsightsSeries is a 7-day rolling line with its change. Max is the top of its scale: 1 for a share.
type InsightsSeries struct {
	Points []InsightsPoint `json:"points"`
	Delta  InsightsDelta   `json:"delta"`
	Max    float64         `json:"max"`
}

// insightsDays is each local midnight from start's day to now's, at most insightsMaxDays of them.
func insightsDays(start, now time.Time, loc *time.Location) []time.Time {
	year, month, day := now.In(loc).Date()
	last := time.Date(year, month, day, 0, 0, 0, 0, loc)
	year, month, day = start.In(loc).Date()
	first := time.Date(year, month, day, 0, 0, 0, 0, loc)
	var days []time.Time
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	if len(days) > insightsMaxDays {
		days = days[len(days)-insightsMaxDays:]
	}
	if len(days) == 0 {
		days = []time.Time{last}
	}
	return days
}

// insightsMeasure is how a line is computed: the figure over a window of items, and its label.
type insightsMeasure[T any] struct {
	at      func(T) time.Time
	value   func([]T) *float64
	display func(float64) string
	// share is a figure from 0 to 1, whose change is in points; otherwise a change is a percentage.
	share bool
	// lower says a fall is better, as for time and cost.
	lower bool
}

// insightsWithin is the items whose time is after from and at or before to.
func insightsWithin[T any](items []T, at func(T) time.Time, from, to time.Time) []T {
	var out []T
	for _, item := range items {
		if t := at(item); t.After(from) && !t.After(to) {
			out = append(out, item)
		}
	}
	return out
}

// insightsSeriesOf is the rolling line of items over days, with its change at now.
func insightsSeriesOf[T any](items []T, m insightsMeasure[T], days []time.Time, now time.Time) InsightsSeries {
	series := InsightsSeries{Points: make([]InsightsPoint, 0, len(days)), Max: 1}
	top := 0.0
	for _, day := range days {
		end := day.AddDate(0, 0, 1)
		window := insightsWithin(items, m.at, end.Add(-insightsWindow), end)
		point := InsightsPoint{Label: day.Format("Jan 2"), Display: "–", Count: len(window)}
		if len(window) >= insightsMinWindow {
			if v := m.value(window); v != nil {
				point.Value = v
				point.Display = m.display(*v)
				top = math.Max(top, *v)
			}
		}
		series.Points = append(series.Points, point)
	}
	if !m.share && top > 0 {
		series.Max = top
	}
	series.Delta = insightsDeltaOf(items, m, now)
	return series
}

// insightsDeltaOf compares the last 7 days with the 7 before. Each needs InsightsLowSample items,
// and a change under 5 points, or under 5%, is flat.
func insightsDeltaOf[T any](items []T, m insightsMeasure[T], now time.Time) InsightsDelta {
	recent := insightsWithin(items, m.at, now.Add(-insightsWindow), now)
	before := insightsWithin(items, m.at, now.Add(-2*insightsWindow), now.Add(-insightsWindow))
	if len(recent) < InsightsLowSample || len(before) < InsightsLowSample {
		return InsightsDelta{Label: "–", Tone: InsightsDeltaNone, Caption: "Too few in the last two weeks to compare"}
	}
	a, b := m.value(recent), m.value(before)
	if a == nil || b == nil {
		return InsightsDelta{Label: "–", Tone: InsightsDeltaNone, Caption: "Nothing to compare"}
	}
	var change int
	unit := "%"
	if m.share {
		change = int(math.Round(100 * (*a - *b)))
		unit = " pts"
	} else if *b != 0 {
		change = int(math.Round(100 * (*a - *b) / *b))
	}
	sign := "±"
	if change > 0 {
		sign = "+"
	} else if change < 0 {
		sign = "−"
	}
	tone := InsightsDeltaFlat
	if math.Abs(float64(change)) >= 5 {
		tone = InsightsDeltaWorse
		if (change > 0) != m.lower {
			tone = InsightsDeltaBetter
		}
	}
	return InsightsDelta{
		Label: sign + strconv.Itoa(int(math.Abs(float64(change)))) + unit, Tone: tone,
		Caption: "Last 7 days against the 7 before (" + strconv.Itoa(len(recent)) + " and " + strconv.Itoa(len(before)) + ")",
	}
}

// insightsMedian is the median of values, nil when there are none.
func insightsMedian(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	middle := len(sorted) / 2
	value := sorted[middle]
	if len(sorted)%2 == 0 {
		value = (sorted[middle-1] + sorted[middle]) / 2
	}
	return &value
}

// insightsDuration labels seconds the way stage statuses do, "–" when unknown.
func insightsDuration(seconds *float64) string {
	if seconds == nil {
		return "–"
	}
	return StatusDuration(*seconds)
}

// insightsDollars labels nanodollars, "–" when unknown.
func insightsDollars(nanos *float64) string {
	if nanos == nil {
		return "–"
	}
	return CostDollars(int(math.Round(*nanos)))
}
