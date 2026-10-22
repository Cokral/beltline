package core

import (
	"strconv"
	"time"
)

// Spend is known dollars from ended agent attempts, plus whether that figure is estimated or incomplete.
// Nanos is nil when no attempt was priced.
type Spend struct {
	Nanos       *int
	Estimated   bool
	Partial     bool
	EndedAgents int
	LiveAgents  bool
}

// Copy used by Cost. Literal text is exact.
const (
	// CostEstimateExplanation says what the cost figures are.
	CostEstimateExplanation = "API-equivalent usage cost from the bundled rate table; not subscription charges or invoices."

	costNotTracked    = "cost not tracked"
	costPartialWord   = "partial"
	costEstimatedWord = "estimated"
	costPartSeparator = " · "
)

// CostIsAgent reports whether attempt in run is an agent session.
func CostIsAgent(attempt Attempt, run Run) bool {
	return costIsAgent(&attempt, &run)
}

// costIsAgent is CostIsAgent without copying the attempt, the run or its stages.
func costIsAgent(attempt *Attempt, run *Run) bool {
	for i := range run.Stages {
		if run.Stages[i].ID == attempt.StageID {
			return run.Stages[i].Kind() == StageKindAgent
		}
	}
	return attempt.Session != nil || attempt.SessionID != nil
}

// CostSpendOf is the spend of every agent attempt of run; live when one is still open.
func CostSpendOf(run Run) Spend {
	var agents []Attempt
	for _, attempt := range run.Attempts {
		if CostIsAgent(attempt, run) {
			agents = append(agents, attempt)
		}
	}
	var ended []Attempt
	live := false
	for _, attempt := range agents {
		if !attempt.IsOpen() {
			ended = append(ended, attempt)
		} else {
			live = true
		}
	}
	return costSpendOfAttempts(ended, live)
}

// CostToday is the ended agent attempts whose endedAt falls on now's day in loc.
func CostToday(runs []Run, loc *time.Location, now time.Time) Spend {
	if loc == nil {
		loc = time.Local
	}
	var attempts []Attempt
	day := localDayOf(now, loc)
	for i := range runs {
		run := &runs[i]
		for j := range run.Attempts {
			attempt := &run.Attempts[j]
			if attempt.EndedAt == nil || !day.holds(attempt.EndedAt.Time) || !costIsAgent(attempt, run) {
				continue
			}
			attempts = append(attempts, *attempt)
		}
	}
	return costSpendOfAttempts(attempts, false)
}

// CostDollars formats nanodollars as a USD currency label in en_US, with four places
// below one cent and two otherwise.
//
// NumberFormatter rounds half to even by default, which the integer rounding here
// reproduces.
func CostDollars(nanos int) string {
	places := 2
	if nanos > 0 && nanos < 10_000_000 {
		places = 4
	}
	negative := nanos < 0
	magnitude := nanos
	if negative {
		magnitude = -nanos
	}
	// Nanodollars in one unit of the last shown place.
	unit := 1
	for i := 0; i < 9-places; i++ {
		unit *= 10
	}
	quotient := magnitude / unit
	remainder := magnitude % unit
	if 2*remainder > unit || (2*remainder == unit && quotient%2 == 1) {
		quotient++
	}
	scale := 1
	for i := 0; i < places; i++ {
		scale *= 10
	}
	whole := quotient / scale
	fraction := strconv.Itoa(quotient % scale)
	for len(fraction) < places {
		fraction = "0" + fraction
	}
	label := "$" + costGrouped(whole)
	if places > 0 {
		label += "." + fraction
	}
	if negative {
		return "-" + label
	}
	return label
}

// CostRunLine is the run header. Nil when the run has no ended agent attempt.
func CostRunLine(spend Spend) *string {
	if spend.EndedAgents <= 0 {
		return nil
	}
	first := costNotTracked
	if spend.Nanos != nil {
		first = CostDollars(*spend.Nanos)
	}
	parts := []string{first}
	if spend.Nanos != nil && spend.Partial {
		parts = append(parts, costPartialWord)
	}
	if spend.Estimated {
		parts = append(parts, costEstimatedWord)
	}
	line := ""
	for i, part := range parts {
		if i > 0 {
			line += costPartSeparator
		}
		line += part
	}
	if spend.LiveAgents {
		line += " · ended sessions"
	}
	return &line
}

// CostAmount is Overview's primary figure: only the amount, `$0.00` when nothing has
// ended or none of it was priced.
func CostAmount(spend Spend) string {
	nanos := ValueOf(spend.Nanos)
	return CostDollars(nanos)
}

// CostTodayCaptionFor is Overview's caption. Today is always scoped to ended sessions.
func CostTodayCaptionFor(spend Spend) string {
	return "ended sessions · all belts"
}

// CostSession is the one-line summary of an attempt: "live" while open, else its duration,
// with the agent's cost for an agent.
func CostSession(attempt Attempt, agent bool, now time.Time) string {
	if attempt.IsOpen() {
		return "live"
	}
	end := now
	if attempt.EndedAt != nil {
		end = attempt.EndedAt.Time
	}
	duration := StatusDuration(end.Sub(attempt.StartedAt.Time).Seconds())
	if !agent {
		return duration
	}
	return duration + costPartSeparator + CostAttemptLabel(attempt.Usage)
}

// CostAttemptLabel is the cost label of one attempt's usage.
func CostAttemptLabel(usage *Usage) string {
	if usage == nil || usage.CostNanos == nil {
		return costNotTracked
	}
	parts := []string{CostDollars(*usage.CostNanos)}
	if usage.Coverage != UsageCoverageComplete {
		parts = append(parts, costPartialWord)
	}
	if usage.Provenance != nil && *usage.Provenance == CostProvenanceEstimated {
		parts = append(parts, costEstimatedWord)
	}
	label := ""
	for i, part := range parts {
		if i > 0 {
			label += costPartSeparator
		}
		label += part
	}
	return label
}

// CostTokenDetail lists the categories the attempt actually reported. Nil when none were.
func CostTokenDetail(usage Usage) *string {
	var parts []string
	if usage.Input != nil {
		parts = append(parts, costGrouped(*usage.Input)+" in")
	}
	if usage.Output != nil {
		parts = append(parts, costGrouped(*usage.Output)+" out")
	}
	if usage.CacheRead != nil {
		parts = append(parts, costGrouped(*usage.CacheRead)+" cache read")
	}
	if usage.CacheWrite != nil {
		parts = append(parts, costGrouped(*usage.CacheWrite)+" cache write")
	}
	if usage.Reasoning != nil {
		parts = append(parts, costGrouped(*usage.Reasoning)+" reasoning")
	}
	if usage.TotalTokens != nil {
		parts = append(parts, costGrouped(*usage.TotalTokens)+" total")
	}
	if len(parts) == 0 {
		return nil
	}
	line := ""
	for i, part := range parts {
		if i > 0 {
			line += costPartSeparator
		}
		line += part
	}
	return &line
}

// CostExplainsEstimate reports whether a usage's cost is estimated.
func CostExplainsEstimate(usage *Usage) bool {
	return usage != nil && usage.Provenance != nil && *usage.Provenance == CostProvenanceEstimated
}

// costSpendOfAttempts aggregates attempts; live marks an open one among them.
func costSpendOfAttempts(attempts []Attempt, live bool) Spend {
	var nanos *int
	estimated := false
	partial := false
	for _, attempt := range attempts {
		if attempt.Usage == nil {
			partial = true
			continue
		}
		usage := attempt.Usage
		if usage.Provenance != nil && *usage.Provenance == CostProvenanceEstimated {
			estimated = true
		}
		if usage.Coverage != UsageCoverageComplete || usage.CostNanos == nil {
			partial = true
		}
		if usage.CostNanos == nil {
			continue
		}
		base := ValueOf(nanos)
		cost := *usage.CostNanos
		sum := base + cost
		if (cost > 0 && sum < base) || (cost < 0 && sum > base) {
			partial = true
			continue
		}
		nanos = &sum
	}
	return Spend{Nanos: nanos, Estimated: estimated && nanos != nil, Partial: partial, EndedAgents: len(attempts), LiveAgents: live}
}

// costGrouped formats value with thousands separators in en_US.
func costGrouped(value int) string {
	negative := value < 0
	digits := strconv.Itoa(value)
	if negative {
		digits = digits[1:]
	}
	var grouped []byte
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped = append(grouped, ',')
		}
		grouped = append(grouped, digits[i])
	}
	if negative {
		return "-" + string(grouped)
	}
	return string(grouped)
}
