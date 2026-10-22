// The Evolution view's other charts: where the time goes, approvals and holds, sends back, cost by
// stage, and how runs end.

package core

import (
	"cmp"
	"slices"
	"strconv"
)

// InsightsStackSeries is one part of a stacked column chart: its mean per run in each column.
type InsightsStackSeries struct {
	Label    string    `json:"label"`
	Values   []float64 `json:"values"`
	Displays []string  `json:"displays"`
}

// InsightsStackLine is a share drawn over a stacked chart, from 0 at the bottom to 1 at the top.
type InsightsStackLine struct {
	Label    string    `json:"label"`
	Values   []float64 `json:"values"`
	Displays []string  `json:"displays"`
}

// InsightsStack is a stacked column chart, one stack per column. Runs says what each column counted.
type InsightsStack struct {
	Series []InsightsStackSeries `json:"series"`
	Line   *InsightsStackLine    `json:"line,omitzero"`
	Max    float64               `json:"max"`
	Ticks  []InsightsTick        `json:"ticks"`
	Runs   []string              `json:"runs"`
}

// InsightsGridRow is one send back, from a stage to an earlier one, per run in each column.
type InsightsGridRow struct {
	Label    string    `json:"label"`
	Values   []float64 `json:"values"`
	Displays []string  `json:"displays"`
}

// InsightsGrid is the sends back per run, one row each, in each column; Max is the largest value.
type InsightsGrid struct {
	Rows []InsightsGridRow `json:"rows"`
	Max  float64           `json:"max"`
}

// InsightsFunnelRow is how many before and recent runs reached a point.
type InsightsFunnelRow struct {
	Label  string       `json:"label"`
	Before InsightsRate `json:"before"`
	Recent InsightsRate `json:"recent"`
}

// InsightsDropRow is a stage runs were dropped at, before and recent, with the usual reason.
type InsightsDropRow struct {
	Stage  string `json:"stage"`
	Before int    `json:"before"`
	Recent int    `json:"recent"`
	Reason string `json:"reason"`
}

// InsightsEndings is how runs end: how far they got, and where the dropped ones stopped.
type InsightsEndings struct {
	Funnel []InsightsFunnelRow `json:"funnel"`
	Drops  []InsightsDropRow   `json:"drops"`
}

// insightsPart is one part of a stack: its value for a run.
type insightsPart struct {
	label string
	value func(f *insightsFacts) float64
}

// insightsStackOf is the mean of each part per run in each column of groups, labelled in unit.
func insightsStackOf(groups []*insightsColumnGroup, parts []insightsPart, unit insightsUnit) InsightsStack {
	stack := InsightsStack{Series: []InsightsStackSeries{}, Runs: []string{}}
	totals := make([]float64, len(groups))
	for _, part := range parts {
		series := InsightsStackSeries{Label: part.label, Values: []float64{}, Displays: []string{}}
		for c, group := range groups {
			values := make([]float64, 0, len(group.facts))
			for _, f := range group.facts {
				values = append(values, part.value(f))
			}
			mean := insightsMean(values)
			value := 0.0
			if mean != nil {
				value = *mean
			}
			totals[c] += value
			series.Values = append(series.Values, value)
			series.Displays = append(series.Displays, insightsShow(unit, &value))
		}
		stack.Series = append(stack.Series, series)
	}
	for c, group := range groups {
		stack.Max = max(stack.Max, totals[c])
		stack.Runs = append(stack.Runs, strconv.Itoa(len(group.facts))+" "+insightsPlural(len(group.facts), "run", "runs"))
	}
	if stack.Max == 0 {
		stack.Max = 1
		if unit == insightsNanos {
			stack.Max = 1e9
		}
	}
	stack.Ticks = insightsLinearTicks(unit, stack.Max)
	return stack
}

// insightsTimeStack is the mean active time per run: agents working, waiting on you, watching the
// pull request, and the rest.
func insightsTimeStack(groups []*insightsColumnGroup) InsightsStack {
	return insightsStackOf(groups, []insightsPart{
		{"Agents working", func(f *insightsFacts) float64 { return f.agent }},
		{"Waiting on you", func(f *insightsFacts) float64 { return f.approve + f.hold }},
		{"Watching the pull request", func(f *insightsFacts) float64 { return f.watch }},
		{"Queued and between stages", func(f *insightsFacts) float64 { return max(0, f.lead-f.agent-f.approve-f.hold-f.watch) }},
	}, insightsSeconds)
}

// insightsTouchStack is the approvals you gave, the holds you cleared and the AFK decisions per run,
// with the share of runs that had no approval or hold.
func insightsTouchStack(groups []*insightsColumnGroup) InsightsStack {
	stack := insightsStackOf(groups, []insightsPart{
		{"Approvals you gave", func(f *insightsFacts) float64 { return float64(f.approvals) }},
		{"Holds you cleared", func(f *insightsFacts) float64 { return float64(f.holds) }},
		{"Decided by AFK", func(f *insightsFacts) float64 { return float64(f.afk) }},
	}, insightsCount)
	line := InsightsStackLine{Label: "Share of runs with no approvals or holds", Values: []float64{}, Displays: []string{}}
	for _, group := range groups {
		none := 0
		for _, f := range group.facts {
			if f.approvals+f.holds == 0 {
				none++
			}
		}
		share := 0.0
		if len(group.facts) > 0 {
			share = float64(none) / float64(len(group.facts))
		}
		line.Values = append(line.Values, share)
		line.Displays = append(line.Displays, insightsPercent(share)+" of runs with no approvals or holds")
	}
	stack.Line = &line
	return stack
}

// insightsSendsBackRows is how many of the most common sends back the grid lists.
const insightsSendsBackRows = 8

// insightsSendsBack is the most common sends back per run in each column, then tries again at the
// same stage.
func insightsSendsBack(groups []*insightsColumnGroup) InsightsGrid {
	counts := map[string]int{}
	for _, group := range groups {
		for _, f := range group.facts {
			for _, send := range f.sends {
				counts[send]++
			}
		}
	}
	labels := make([]string, 0, len(counts))
	for label := range counts {
		labels = append(labels, label)
	}
	slices.SortFunc(labels, func(a, b string) int { return cmp.Or(counts[b]-counts[a], cmp.Compare(a, b)) })
	labels = labels[:min(len(labels), insightsSendsBackRows)]
	grid := InsightsGrid{Rows: []InsightsGridRow{}}
	row := func(label string, count func(f *insightsFacts) int) {
		out := InsightsGridRow{Label: label, Values: []float64{}, Displays: []string{}}
		for _, group := range groups {
			total := 0
			for _, f := range group.facts {
				total += count(f)
			}
			value := 0.0
			if len(group.facts) > 0 {
				value = float64(total) / float64(len(group.facts))
			}
			grid.Max = max(grid.Max, value)
			out.Values = append(out.Values, value)
			out.Displays = append(out.Displays, strconv.FormatFloat(value, 'f', 2, 64)+" per run")
		}
		grid.Rows = append(grid.Rows, out)
	}
	for _, label := range labels {
		row(label, func(f *insightsFacts) int {
			n := 0
			for _, send := range f.sends {
				if send == label {
					n++
				}
			}
			return n
		})
	}
	row("Tried again at the same stage", func(f *insightsFacts) int { return f.retries })
	return grid
}

// insightsCostParts is how many stages the cost chart names; the rest are Other stages.
const insightsCostParts = 7

// insightsCostStack is the mean cost per priced run by stage, the costliest stages named.
func insightsCostStack(groups []*insightsColumnGroup) InsightsStack {
	priced := make([]*insightsColumnGroup, len(groups))
	totals := map[string]float64{}
	for c, group := range groups {
		priced[c] = &insightsColumnGroup{label: group.label}
		for _, f := range group.facts {
			if f.cost == nil {
				continue
			}
			priced[c].facts = append(priced[c].facts, f)
			for label, value := range f.costBy {
				totals[label] += value
			}
		}
	}
	labels := make([]string, 0, len(totals))
	for label := range totals {
		labels = append(labels, label)
	}
	slices.SortFunc(labels, func(a, b string) int { return cmp.Or(cmp.Compare(totals[b], totals[a]), cmp.Compare(a, b)) })
	named := labels[:min(len(labels), insightsCostParts)]
	parts := []insightsPart{}
	for _, label := range named {
		parts = append(parts, insightsPart{label, func(f *insightsFacts) float64 { return f.costBy[label] }})
	}
	if len(labels) > len(named) {
		parts = append(parts, insightsPart{"Other stages", func(f *insightsFacts) float64 {
			total := 0.0
			for label, value := range f.costBy {
				if !slices.Contains(named, label) {
					total += value
				}
			}
			return total
		}})
	}
	stack := insightsStackOf(priced, parts, insightsNanos)
	for c, group := range priced {
		stack.Runs[c] = strconv.Itoa(len(group.facts)) + " priced " + insightsPlural(len(group.facts), "run", "runs")
	}
	return stack
}

// insightsEndingsOf is how far before and recent runs got, and where the dropped ones stopped.
func insightsEndingsOf(before, recent []*insightsFacts) InsightsEndings {
	reached := func(facts []*insightsFacts, ok func(f *insightsFacts) bool) InsightsRate {
		count := 0
		for _, f := range facts {
			if ok(f) {
				count++
			}
		}
		return insightsRateOf(count, len(facts))
	}
	endings := InsightsEndings{Funnel: []InsightsFunnelRow{}, Drops: []InsightsDropRow{}}
	for _, step := range []struct {
		label string
		ok    func(f *insightsFacts) bool
	}{
		{"Started", func(*insightsFacts) bool { return true }},
		{"Opened a pull request", func(f *insightsFacts) bool { return f.toPR != nil }},
		{"Shipped", func(f *insightsFacts) bool { return f.run.State == RunStateShipped }},
	} {
		endings.Funnel = append(endings.Funnel, InsightsFunnelRow{Label: step.label, Before: reached(before, step.ok), Recent: reached(recent, step.ok)})
	}
	type drop struct {
		before, recent int
		reasons        map[string]int
	}
	drops := map[string]*drop{}
	count := func(facts []*insightsFacts, recent bool) {
		for _, f := range facts {
			if f.run.State != RunStateDropped {
				continue
			}
			stage := cmp.Or(f.dropAt, "–")
			d := drops[stage]
			if d == nil {
				d = &drop{reasons: map[string]int{}}
				drops[stage] = d
			}
			if recent {
				d.recent++
			} else {
				d.before++
			}
			d.reasons[cmp.Or(f.dropWhy, "no reason given")]++
		}
	}
	count(before, false)
	count(recent, true)
	for stage, d := range drops {
		reasons := make([]string, 0, len(d.reasons))
		for reason := range d.reasons {
			reasons = append(reasons, reason)
		}
		slices.SortFunc(reasons, func(a, b string) int { return cmp.Or(d.reasons[b]-d.reasons[a], cmp.Compare(a, b)) })
		endings.Drops = append(endings.Drops, InsightsDropRow{Stage: stage, Before: d.before, Recent: d.recent, Reason: reasons[0]})
	}
	slices.SortFunc(endings.Drops, func(a, b InsightsDropRow) int {
		return cmp.Or((b.Before+b.Recent)-(a.Before+a.Recent), cmp.Compare(a.Stage, b.Stage))
	})
	endings.Drops = endings.Drops[:min(len(endings.Drops), 7)]
	return endings
}
