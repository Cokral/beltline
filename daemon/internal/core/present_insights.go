// The Insights view: every belt at a glance, or one belt's stages in belt
// order, each stage with the models its sessions ran on. Computed from the runs alone, so an
// erased run leaves it too.

package core

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// InsightsPeriod is how far back the view looks: 7, 30 or 90 days, or all time.
type InsightsPeriod string

const (
	InsightsPeriodWeek    InsightsPeriod = "7d"
	InsightsPeriodMonth   InsightsPeriod = "30d"
	InsightsPeriodQuarter InsightsPeriod = "90d"
	InsightsPeriodAll     InsightsPeriod = "all"
)

// AllInsightsPeriod lists every InsightsPeriod in declaration order (CaseIterable).
var AllInsightsPeriod = []InsightsPeriod{InsightsPeriodWeek, InsightsPeriodMonth, InsightsPeriodQuarter, InsightsPeriodAll}

// days is how many days the period covers, 0 for all time.
func (p InsightsPeriod) days() int {
	switch p {
	case InsightsPeriodWeek:
		return 7
	case InsightsPeriodMonth:
		return 30
	case InsightsPeriodQuarter:
		return 90
	}
	return 0
}

// label is the period as the picker shows it.
func (p InsightsPeriod) label() string {
	if p == InsightsPeriodAll {
		return "All time"
	}
	return strconv.Itoa(p.days()) + " days"
}

// InsightsLowSample is how many visits a stage or a model needs before its figures stop being greyed.
const InsightsLowSample = 5

// InsightsQuery is what the page asks for: one belt, or every belt when Belt is empty, over Period.
// Version, with a belt, keeps to the runs of that version of it; zero is every version.
//
// Shadow, with a belt, compares one of its shadow tests instead, whatever the version and period.
//
// ShowArchived offers the archived belts in the picker and gives those with runs in the period an
// overview row. With no ShowArchived, an archived belt is left out of both, unless it is the belt chosen.
type InsightsQuery struct {
	Belt         string
	Version      int
	Period       InsightsPeriod
	Shadow       string
	ShowArchived bool
}

// InsightsLibraryBelt is a belt of the library as the view needs it: its name, and its current
// version and stages, which decide the stages the view lists.
type InsightsLibraryBelt struct {
	ID       string
	Name     string
	Version  int
	Stages   []Stage
	Archived bool
	// ShadowTests are the belt's shadow tests, which the version picker offers.
	ShadowTests []ShadowTest
}

// InsightsBelt is a belt the picker offers, with its name.
type InsightsBelt struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// InsightsVersionChoice is one version of the chosen belt the picker offers, such as "v12 · 5 runs".
// Value zero is every version. A shadow test's entry names its test in Shadow, with Value zero.
type InsightsVersionChoice struct {
	Value  int    `json:"value"`
	Shadow string `json:"shadow,omitzero"`
	Label  string `json:"label"`
}

// InsightsPeriodChoice is one period the picker offers.
type InsightsPeriodChoice struct {
	Value InsightsPeriod `json:"value"`
	Label string         `json:"label"`
}

// InsightsRate is a share of a total. Percent is its label, such as "66%", or "–" when Of is zero.
type InsightsRate struct {
	Count   int    `json:"count"`
	Of      int    `json:"of"`
	Percent string `json:"percent"`
}

// insightsRateOf is count of of as a rate.
func insightsRateOf(count, of int) InsightsRate {
	rate := InsightsRate{Count: count, Of: of, Percent: "–"}
	if of > 0 {
		rate.Percent = insightsPercent(float64(count) / float64(of))
	}
	return rate
}

// insightsPercent is a share as a whole percentage.
func insightsPercent(share float64) string {
	return strconv.Itoa(int(share*100+0.5)) + "%"
}

// InsightsKpiID names one of the four figures a shadow test compares.
type InsightsKpiID string

const (
	InsightsKpiFirstTry InsightsKpiID = "firstTry"
	InsightsKpiShipRate InsightsKpiID = "shipRate"
	InsightsKpiLeadTime InsightsKpiID = "leadTime"
	InsightsKpiCost     InsightsKpiID = "cost"
)

// AllInsightsKpiID lists every InsightsKpiID in declaration order (CaseIterable).
var AllInsightsKpiID = []InsightsKpiID{InsightsKpiFirstTry, InsightsKpiShipRate, InsightsKpiLeadTime, InsightsKpiCost}

// InsightsBeltRow is one belt in the overview of every belt.
type InsightsBeltRow struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Runs       int            `json:"runs"`
	ShipRate   InsightsRate   `json:"shipRate"`
	FirstTry   InsightsRate   `json:"firstTry"`
	LeadTime   string         `json:"leadTime"`
	CostPerRun string         `json:"costPerRun"`
	Trend      InsightsSeries `json:"trend"`
}

// InsightsView is the Insights page for one query. With no belt chosen, BeltRows lists every belt
// with runs in the period and Versions and Stages are empty; with a belt, the reverse. The
// stages are those of the chosen version, or of the belt's current version for every version, so a
// stage a later version removed is not listed.
type InsightsView struct {
	Belt               string                  `json:"belt"`
	Version            int                     `json:"version"`
	Versions           []InsightsVersionChoice `json:"versions"`
	Period             InsightsPeriod          `json:"period"`
	Belts              []InsightsBelt          `json:"belts"`
	Periods            []InsightsPeriodChoice  `json:"periods"`
	Title              string                  `json:"title"`
	Caption            string                  `json:"caption"`
	LowSampleThreshold int                     `json:"lowSampleThreshold"`
	BeltRows           []InsightsBeltRow       `json:"beltRows"`
	Stages             []InsightsStage         `json:"stages"`
	// Shadow is the shadow test the query compares; nil otherwise, when Stages are the view.
	Shadow *InsightsShadow `json:"shadow,omitzero"`
}

// InsightsHasBelt reports whether query names a belt the view can show, one of the library's or one a
// run came from, and a version of it that is current or that a run used. Every version is always there.
func InsightsHasBelt(query InsightsQuery, library []InsightsLibraryBelt, runs []Run) bool {
	if query.Belt == "" {
		return true
	}
	if query.Shadow != "" {
		return insightsShadowTest(library, query.Belt, query.Shadow) != nil
	}
	for _, belt := range library {
		if belt.ID == query.Belt && (query.Version == 0 || query.Version == belt.Version) {
			return true
		}
	}
	return slices.ContainsFunc(runs, func(run Run) bool {
		return run.BeltID == query.Belt && (query.Version == 0 || run.BeltVersion == query.Version)
	})
}

// Insights builds the view for query from runs and the library's belts. A belt only runs name,
// because it was deleted, takes its name and stages from its newest run. now ends the period, and
// days are counted in loc.
func Insights(runs []Run, library []InsightsLibraryBelt, query InsightsQuery, now time.Time, loc *time.Location) InsightsView {
	if loc == nil {
		loc = time.Local
	}
	period := query.Period
	if !slices.Contains(AllInsightsPeriod, period) {
		period = InsightsPeriodAll
	}
	since := time.Time{}
	if days := period.days(); days > 0 {
		since = now.AddDate(0, 0, -days)
	}
	view := InsightsView{
		Belt: query.Belt, Period: period, Belts: insightsPickerBelts(runs, library, query),
		LowSampleThreshold: InsightsLowSample, Versions: []InsightsVersionChoice{},
		BeltRows: []InsightsBeltRow{}, Stages: []InsightsStage{},
	}
	if query.Belt != "" {
		view.Version = query.Version
		view.Versions = append(insightsVersionChoices(runs, library, query.Belt), insightsShadowChoices(library, query.Belt, loc)...)
	}
	for _, p := range AllInsightsPeriod {
		view.Periods = append(view.Periods, InsightsPeriodChoice{Value: p, Label: p.label()})
	}
	// The figures count the period; the lines count every run, so the 7-day window of the period's
	// first days is as full as any other, and the change can compare the last 7 days with the 7 before
	// whatever the period.
	scopes := map[string]*insightsScope{}
	history := map[string]*insightsScope{}
	for i := range runs {
		run := &runs[i]
		if query.Belt != "" && (run.BeltID != query.Belt || (query.Version != 0 && run.BeltVersion != query.Version)) {
			continue
		}
		if scopes[run.BeltID] == nil {
			scopes[run.BeltID], history[run.BeltID] = &insightsScope{}, &insightsScope{}
		}
		scopes[run.BeltID].add(run, since)
		history[run.BeltID].add(run, time.Time{})
	}
	// The lines start at the period's start, or later when the first counted attempt came later.
	first := time.Time{}
	for _, scope := range history {
		if start := scope.first(); !start.IsZero() && (first.IsZero() || start.Before(first)) {
			first = start
		}
	}
	start := since
	if first.IsZero() {
		start = now
	} else if since.IsZero() || first.After(since) {
		start = first
	}
	days := insightsDays(start, now, loc)

	if query.Belt == "" {
		view.Title = "All belts"
		for _, belt := range view.Belts {
			scope := scopes[belt.ID]
			if scope == nil || len(scope.runs) == 0 {
				continue
			}
			view.BeltRows = append(view.BeltRows, scope.beltRow(belt, history[belt.ID], days, now))
		}
		view.Caption = strconv.Itoa(len(view.BeltRows)) + " " + insightsPlural(len(view.BeltRows), "belt", "belts") + " with runs"
		return view
	}

	view.Title = query.Belt
	for _, belt := range view.Belts {
		if belt.ID == query.Belt {
			view.Title = belt.Label
		}
	}
	if test := insightsShadowTest(library, query.Belt, query.Shadow); test != nil {
		shadow := insightsShadowOf(runs, query.Belt, *test, loc)
		view.Version, view.Shadow = 0, &shadow
		finished, going := shadow.Live.Finished+shadow.Shadow.Finished, shadow.Live.Going+shadow.Shadow.Going
		view.Caption = strconv.Itoa(finished) + " finished · " + strconv.Itoa(going) + " going"
		return view
	}
	scope := scopes[query.Belt]
	if scope == nil {
		scope = &insightsScope{}
	}
	view.Caption = costGrouped(len(scope.runs)) + " " + insightsPlural(len(scope.runs), "run", "runs") + " · " +
		costGrouped(len(scope.attempts)) + " " + insightsPlural(len(scope.attempts), "attempt", "attempts")
	view.Stages = scope.stages(insightsDefinition(runs, library, query))
	return view
}

// insightsPickerBelts is the belts the picker offers, most runs first. Archived belts are offered only
// with ShowArchived, or when chosen; they are filtered after the run-only belts are added, so runs of an
// archived belt cannot bring it back as one of those.
func insightsPickerBelts(runs []Run, library []InsightsLibraryBelt, query InsightsQuery) []InsightsBelt {
	belts := make([]InsightsBelt, 0, len(library))
	archived := map[string]bool{}
	for _, belt := range library {
		belts = append(belts, InsightsBelt{ID: belt.ID, Label: belt.Name})
		archived[belt.ID] = belt.Archived
	}
	return slices.DeleteFunc(insightsBeltChoices(runs, belts), func(belt InsightsBelt) bool {
		return archived[belt.ID] && !query.ShowArchived && belt.ID != query.Belt
	})
}

// insightsDefinition is the stages the view lists for query: those of the chosen version, as its
// newest run copied them, or for every version those of the belt's current version. A belt the
// library no longer has takes its newest run's.
func insightsDefinition(runs []Run, library []InsightsLibraryBelt, query InsightsQuery) []Stage {
	if query.Version == 0 {
		for _, belt := range library {
			if belt.ID == query.Belt {
				return belt.Stages
			}
		}
	}
	var newest *Run
	for i := range runs {
		run := &runs[i]
		if run.BeltID != query.Belt || (query.Version != 0 && run.BeltVersion != query.Version) {
			continue
		}
		if newest == nil || run.CreatedAt.After(newest.CreatedAt.Time) {
			newest = run
		}
	}
	if newest != nil {
		return newest.Stages
	}
	for _, belt := range library {
		if belt.ID == query.Belt && belt.Version == query.Version {
			return belt.Stages
		}
	}
	return nil
}

// insightsVersionChoices is every version, then each version of the belt that runs used or that is
// current, newest first, with how many runs used it.
func insightsVersionChoices(runs []Run, library []InsightsLibraryBelt, beltID string) []InsightsVersionChoice {
	counts := map[int]int{}
	current := 0
	for _, belt := range library {
		if belt.ID == beltID {
			current = belt.Version
			counts[current] += 0
		}
	}
	total := 0
	for i := range runs {
		if runs[i].BeltID == beltID {
			counts[runs[i].BeltVersion]++
			total++
		}
	}
	versions := make([]int, 0, len(counts))
	for version := range counts {
		versions = append(versions, version)
	}
	slices.Sort(versions)
	slices.Reverse(versions)
	out := []InsightsVersionChoice{{Value: 0, Label: "All versions · " + strconv.Itoa(total) + " " + insightsPlural(total, "run", "runs")}}
	for _, version := range versions {
		label := "v" + strconv.Itoa(version)
		if version == current {
			label += " (current)"
		}
		out = append(out, InsightsVersionChoice{Value: version, Label: label + " · " + strconv.Itoa(counts[version]) + " " + insightsPlural(counts[version], "run", "runs")})
	}
	return out
}

// insightsBeltChoices is the library's belts, then any belt only runs name, most runs first.
func insightsBeltChoices(runs []Run, belts []InsightsBelt) []InsightsBelt {
	counts := map[string]int{}
	names := map[string]string{}
	newest := map[string]time.Time{}
	for i := range runs {
		run := &runs[i]
		counts[run.BeltID]++
		if run.CreatedAt.After(newest[run.BeltID]) || names[run.BeltID] == "" {
			newest[run.BeltID] = run.CreatedAt.Time
			names[run.BeltID] = run.BeltName
		}
	}
	out := make([]InsightsBelt, 0, len(belts))
	known := map[string]bool{}
	for _, belt := range belts {
		known[belt.ID] = true
		out = append(out, belt)
	}
	for id, name := range names {
		if !known[id] {
			out = append(out, InsightsBelt{ID: id, Label: name})
		}
	}
	slices.SortStableFunc(out, func(a, b InsightsBelt) int {
		if counts[a.ID] != counts[b.ID] {
			return counts[b.ID] - counts[a.ID]
		}
		if a.Label != b.Label {
			return strings.Compare(a.Label, b.Label)
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func insightsPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
