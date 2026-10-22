package runner

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"slices"
)

// A Codex rollout is JSONL under `~/.codex/sessions/`. The first line is
// `session_meta` (`payload.id`, `payload.cwd`). Later `turn_context` events set
// `payload.model`. `event_msg` events with `payload.type == "token_count"` carry
// cumulative `payload.info.total_token_usage`: `input_tokens` includes
// `cached_input_tokens`, `output_tokens` includes `reasoning_output_tokens`, and
// `total_tokens` counts each token once. `last_token_usage` is not added. Each
// counter is parsed on its own. A missing or unusable counter does not discard
// the others and is not treated as zero.
// Only a counter valid on both this event and the last baseline that had it is
// differenced, and only when it did not move backwards. Repeated identical totals
// count once. A counter that moves backwards stops attribution: deltas already
// taken stay, coverage is incomplete, and later totals are not applied. A file
// that never reaches the saved baseline is not attributed.
// `cache_write_input_tokens` is part of `input_tokens`, like cached input, and is
// billed at the cache-write price instead of as input. A rollout without the field
// wrote none. Each step is priced at the tier of its request's size: a step of
// several requests is short context only when their input together is within the
// threshold. `service_tier` absent, `standard` or `default` is standard, `fast` or
// `priority` uses fast prices, and any other is not priced.

// CodexUsageOf is the usage of a rollout after its baseline. A nil baseline
// attributes nothing, including when the file holds an earlier session. A nil
// rates uses RateTableOpenAI.
func CodexUsageOf(log string, baseline *core.TokenTotals, rates *RateTable) *core.Usage {
	if baseline == nil {
		return nil
	}
	table := core.ValueOr(rates, RateTableOpenAI())
	parsed := codexSnapshots(log)
	draft := usageDraft{}
	if parsed.Gap {
		draft.Gap = true
	}
	passed := false
	base := newCodexCountersFromTotals(*baseline)
	for _, event := range parsed.Events {
		if event.Tier == nil {
			draft.Gap = true
		}
		if !passed {
			if event.Counters.Total != nil && base.Total != nil && *event.Counters.Total < *base.Total {
				continue
			}
			// A checkpoint saved before cache writes were reported matches on the rest.
			if codexSameCore(event.Counters, base) {
				passed = true
				base.adopt(event.Counters)
				continue
			}
		}
		delta := codexDiff(event.Counters, base)
		if delta.Backwards {
			if !passed {
				return nil
			}
			draft.Gap = true
			break
		}
		unchanged := codexIdle(event.Counters, base, delta)
		passed = true
		base.adopt(event.Counters)
		if unchanged {
			continue
		}
		codexAttribute(delta, event, table, &draft)
	}
	if !passed && (len(parsed.Events) != 0 || parsed.Gap) {
		if !passed && len(parsed.Events) != 0 {
			return nil
		}
		if draft.SawRecord {
			return draft.usage()
		}
		return nil
	}
	if passed && !draft.SawRecord && !draft.Gap && !draft.CategoryGap {
		zero := 0
		draft.add(&zero, &zero, &zero, nil, &zero, &zero)
		draft.addCost(0, false, nil, nil, nil)
	}
	return draft.usage()
}

// CodexBaseline is where a resumed attempt starts counting: zero only when the
// rollout has no token counts yet; nil when it has some but not every counter.
func CodexBaseline(log string) *core.TokenTotals {
	parsed := codexSnapshots(log)
	if len(parsed.Events) == 0 {
		if parsed.Gap {
			return nil
		}
		zero := core.TokenTotalsZero
		return &zero
	}
	return CodexCumulative(log)
}

// CodexCumulative is the merged cumulative counters of a rollout, or nil when
// some counter is missing.
func CodexCumulative(log string) *core.TokenTotals {
	var merged codexCounters
	saw := false
	for _, event := range codexSnapshots(log).Events {
		saw = true
		merged.adopt(event.Counters)
	}
	if !saw || merged.Input == nil || merged.CacheRead == nil || merged.Output == nil || merged.Reasoning == nil || merged.Total == nil {
		return nil
	}
	return &core.TokenTotals{
		Input: *merged.Input, CacheRead: *merged.CacheRead, Output: *merged.Output, Reasoning: *merged.Reasoning,
		Total: *merged.Total, CacheWrite: merged.CacheWrite,
	}
}

// codexCounters are the cumulative counters of one token_count event. A nil
// field is a counter that was not reported or was unusable. CacheWrite is nil
// for a rollout that never reports cache writes.
type codexCounters struct {
	Input      *int
	CacheRead  *int
	Output     *int
	Reasoning  *int
	Total      *int
	CacheWrite *int
}

// newCodexCountersFromTotals is the counters a checkpoint saved. A session with
// no tokens yet has written nothing, whether or not the checkpoint says so.
func newCodexCountersFromTotals(totals core.TokenTotals) codexCounters {
	input, cacheRead, output, reasoning, total := totals.Input, totals.CacheRead, totals.Output, totals.Reasoning, totals.Total
	c := codexCounters{Input: &input, CacheRead: &cacheRead, Output: &output, Reasoning: &reasoning, Total: &total}
	switch {
	case totals.CacheWrite != nil:
		write := *totals.CacheWrite
		c.CacheWrite = &write
	case totals.Total == 0:
		zero := 0
		c.CacheWrite = &zero
	}
	return c
}

// coreCounters are every counter but cache writes, which older rollouts and checkpoints lack.
func (c codexCounters) coreCounters() []*int {
	return []*int{c.Input, c.CacheRead, c.Output, c.Reasoning, c.Total}
}

// codexSameCore reports whether two counter sets hold the same core counters.
func codexSameCore(a, b codexCounters) bool {
	return slices.EqualFunc(a.coreCounters(), b.coreCounters(), core.SamePointee[int])
}

// adopt takes every counter the event reports, keeping the rest.
func (c *codexCounters) adopt(event codexCounters) {
	if event.Input != nil {
		c.Input = event.Input
	}
	if event.CacheRead != nil {
		c.CacheRead = event.CacheRead
	}
	if event.Output != nil {
		c.Output = event.Output
	}
	if event.Reasoning != nil {
		c.Reasoning = event.Reasoning
	}
	if event.Total != nil {
		c.Total = event.Total
	}
	if event.CacheWrite != nil {
		c.CacheWrite = event.CacheWrite
	}
}

// codexSnapshot is one token_count event: its totals, its last single request,
// the model and the price tier (nil for a tier with no prices).
type codexSnapshot struct {
	Counters codexCounters
	Last     *codexCounters
	Model    *string
	Tier     *PriceTier
}

// codexDelta is the counters moved since the baseline. Backwards is set when one moved back.
type codexDelta struct {
	Input      *int
	CacheRead  *int
	Output     *int
	Reasoning  *int
	Total      *int
	CacheWrite *int
	Backwards  bool
}

// codexDiff is the movement of event since base, counter by counter.
func codexDiff(event, base codexCounters) codexDelta {
	var delta codexDelta
	one := func(current, previous *int) *int {
		if current == nil || previous == nil {
			return nil
		}
		if *current < *previous {
			delta.Backwards = true
			return nil
		}
		change := *current - *previous
		return &change
	}
	delta.Input = one(event.Input, base.Input)
	delta.CacheRead = one(event.CacheRead, base.CacheRead)
	delta.Output = one(event.Output, base.Output)
	delta.Reasoning = one(event.Reasoning, base.Reasoning)
	delta.Total = one(event.Total, base.Total)
	delta.CacheWrite = one(event.CacheWrite, base.CacheWrite)
	return delta
}

// codexIdle reports a repeated report: every counter the baseline had is still
// present and did not move.
func codexIdle(event, base codexCounters, delta codexDelta) bool {
	rows := [][3]*int{
		{event.Input, base.Input, delta.Input}, {event.CacheRead, base.CacheRead, delta.CacheRead},
		{event.Output, base.Output, delta.Output}, {event.Reasoning, base.Reasoning, delta.Reasoning},
		{event.Total, base.Total, delta.Total},
	}
	for _, row := range rows {
		current, previous, change := row[0], row[1], row[2]
		if previous != nil && current == nil {
			return false
		}
		if change != nil && *change != 0 {
			return false
		}
		if current != nil && change == nil {
			return false
		}
	}
	return true
}

// codexAttribute prices one step's delta and folds it into the draft.
func codexAttribute(delta codexDelta, event codexSnapshot, rates RateTable, draft *usageDraft) {
	if delta.Input == nil || delta.CacheRead == nil || delta.Output == nil || delta.Reasoning == nil || delta.Total == nil {
		draft.CategoryGap = true
	}
	// A rollout that never reports cache writes wrote none; one that does needs a known delta.
	write := delta.CacheWrite
	if event.Counters.CacheWrite == nil {
		zero := 0
		write = &zero
	}
	var uncached *int
	priced := delta.Input != nil && delta.CacheRead != nil && delta.Output != nil && write != nil
	switch {
	case delta.Input != nil && delta.CacheRead != nil && write != nil:
		cached, ok := core.AddInts(*delta.CacheRead, *write)
		if !ok || *delta.Input < cached {
			priced = false
			draft.Gap = true
		} else {
			u := *delta.Input - cached
			uncached = &u
		}
	case write == nil:
		draft.Gap = true
	}
	if delta.Total != nil && delta.Input != nil && delta.Output != nil {
		sum, ok := core.AddInts(*delta.Input, *delta.Output)
		if !ok || *delta.Total != sum {
			priced = false
			draft.Gap = true
		}
	}
	var addWrite *int
	if event.Counters.CacheWrite != nil {
		addWrite = delta.CacheWrite
	}
	draft.add(uncached, delta.Output, delta.CacheRead, addWrite, delta.Reasoning, delta.Total)
	if !priced || uncached == nil || delta.CacheRead == nil || delta.Output == nil || write == nil ||
		event.Model == nil || event.Tier == nil {
		draft.Gap = true
		return
	}
	rate := rates.Rate(*event.Model)
	if rate == nil {
		draft.Gap = true
		return
	}
	prices := rate.PricesFor(*event.Tier, codexRequestInput(delta, event.Last, rates.LongContextAbove), rates.LongContextAbove)
	if prices == nil {
		draft.Gap = true
		return
	}
	inputNanos := RateTableNanos(*uncached, prices.Input)
	readNanos := rateTableNanosAt(*delta.CacheRead, prices.CacheRead)
	writeNanos := rateTableNanosAt(*write, prices.CacheWrite)
	outputNanos := RateTableNanos(*delta.Output, prices.Output)
	if inputNanos == nil || readNanos == nil || writeNanos == nil || outputNanos == nil {
		draft.Gap = true
		return
	}
	nanos := RateTableSum([]int{*inputNanos, *readNanos, *writeNanos, *outputNanos})
	if nanos == nil {
		draft.Gap = true
		return
	}
	source, revision := rates.Source, rates.Checked
	draft.addCost(*nanos, true, event.Model, &source, &revision)
}

// codexRequestInput is the input of the step's one request, which picks short or
// long context. Nil when the step covers several requests whose input together
// is above the threshold.
func codexRequestInput(delta codexDelta, last *codexCounters, within *int) *int {
	if delta.Input == nil {
		return nil
	}
	input := delta.Input
	if last != nil && core.SamePointee(last.Input, input) && core.SamePointee(last.Total, delta.Total) {
		return input
	}
	if within == nil || *input > *within {
		return nil
	}
	return input
}

// codexParsed is every token_count event in a rollout, and whether some line was unusable.
type codexParsed struct {
	Events []codexSnapshot
	Gap    bool
}

// codexSnapshots reads a rollout's token_count events, with the model and tier in force.
func codexSnapshots(log string) codexParsed {
	var model *string
	var turnTier *string
	var parsed codexParsed
	for _, line := range ClaudeUsageLines(log) {
		if line == "" {
			continue
		}
		object := jsonObject(line)
		if object == nil {
			parsed.Gap = true
			continue
		}
		typ, _ := asString(object["type"])
		payload, _ := asObject(object["payload"])
		if typ == "session_meta" {
			if name, ok := asString(payload["model"]); ok {
				model = &name
			}
		}
		if typ == "turn_context" {
			if name, ok := asString(payload["model"]); ok {
				model = &name
			}
			if tier, ok := asString(payload["service_tier"]); ok {
				turnTier = &tier
			} else {
				turnTier = nil
			}
		}
		if typ != "event_msg" {
			continue
		}
		if kind, _ := asString(payload["type"]); kind != "token_count" {
			continue
		}
		info, okInfo := asObject(payload["info"])
		var total map[string]any
		if okInfo {
			total, okInfo = asObject(info["total_token_usage"])
		}
		var counters *codexCounters
		if okInfo {
			counters = codexCountersOf(total)
		}
		if counters == nil {
			parsed.Gap = true
			continue
		}
		name, ok := firstString(payload["service_tier"], info["service_tier"])
		var tierName *string
		if ok {
			tierName = &name
		} else {
			tierName = turnTier
		}
		var last *codexCounters
		if lastObject, ok := asObject(info["last_token_usage"]); ok {
			last = codexCountersOf(lastObject)
		}
		parsed.Events = append(parsed.Events, codexSnapshot{
			Counters: *counters, Last: last, Model: model, Tier: codexTier(tierName),
		})
	}
	return parsed
}

// codexTier maps a service tier name to its price tier; nil for one with no prices.
func codexTier(name *string) *PriceTier {
	standard, fast := PriceTierStandard, PriceTierFast
	if name == nil {
		return &standard
	}
	switch *name {
	case "standard", "default":
		return &standard
	case "fast", "priority":
		return &fast
	}
	return nil
}

// codexCountersOf reads the counters of a total_token_usage object. Nil when
// nothing on it is usable; invalid counters are left unknown and valid siblings stay.
func codexCountersOf(object map[string]any) *codexCounters {
	counters := codexCounters{
		Input:      claudeUsageExact(object["input_tokens"]),
		CacheRead:  claudeUsageExact(object["cached_input_tokens"]),
		Output:     claudeUsageExact(object["output_tokens"]),
		Reasoning:  claudeUsageExact(object["reasoning_output_tokens"]),
		Total:      claudeUsageExact(object["total_tokens"]),
		CacheWrite: claudeUsageExact(object["cache_write_input_tokens"]),
	}
	if counters.Input == nil && counters.CacheRead == nil && counters.Output == nil && counters.Reasoning == nil && counters.Total == nil {
		return nil
	}
	return &counters
}
