package runner

import (
	"math"
	"math/big"
	"strconv"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Claude Code writes one JSON object per line to
// `~/.claude/projects/<dashed-cwd>/<session-id>.jsonl`.
//
// An assistant record's `message.usage` counts that API call, including tool-use
// turns: `input_tokens` is uncached input, `cache_read_input_tokens` and
// `cache_creation_input_tokens` are separate, and `output_tokens` includes
// thinking. `cache_creation.ephemeral_5m_input_tokens` and
// `ephemeral_1h_input_tokens` split the cache write; they are not extra tokens.
// Each count is kept on its own. A missing, negative, non-integer, or overflowing
// count drops that field only and leaves a coverage gap; it is not stored as zero
// and does not discard its siblings. Absent `reasoning_tokens` stays unknown and
// is not a gap; a present unusable reasoning value is rejected and is a gap.
// `totalTokens` is recorded only when all four input, output, and cache counts
// are valid. A call is priced only when every category the rate needs is valid,
// including a cache-write split when the write count is non-zero.
// A `costUSD` / `cost_usd` / `total_cost_usd` number, when present and non-negative, is
// the reported dollar amount and wins over an estimate, including a reported zero.
// `speed` `fast` uses the model's fast prices. `service_tier` other than `standard`,
// any other `speed`, and `inference_geo` other than absent, `global` or `not_available`
// are not priced. A model with a long-context price uses it for a call whose input,
// cache reads and cache writes together exceed the table's threshold.
// Sidechain lines in this file are this attempt's subagent calls and count once,
// deduped by `message.id`, or by `uuid` when a line has no message id. Synthetic
// (`model` `<synthetic>`), user, and metadata lines do not. A different
// `sessionId` is not attributed.

// ClaudeUsageLines splits a transcript into its lines, without a trailing empty one.
func ClaudeUsageLines(log string) []string {
	if log == "" {
		return nil
	}
	parts := splitLines(log, false)
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// ClaudeUsageUsage is the usage of the lines after `after`, the line count saved
// when the attempt started. A nil baseline attributes nothing. A nil rates uses
// RateTableClaude.
func ClaudeUsageUsage(log string, after *int, sessionID string, rates *RateTable) *core.Usage {
	if after == nil || *after < 0 {
		return nil
	}
	lines := ClaudeUsageLines(log)
	if len(lines) < *after {
		return nil
	}
	table := core.ValueOr(rates, RateTableClaude())
	draft := usageDraft{}
	seen := map[string]struct{}{}
	for _, line := range lines[*after:] {
		if line == "" {
			continue
		}
		object := jsonObject(line)
		if object == nil {
			draft.Gap = true
			continue
		}
		claudeUsageConsume(object, sessionID, table, seen, &draft)
	}
	return draft.usage()
}

// claudeUsageConsume folds one transcript object into the draft. seen holds the
// message ids and uuids already counted.
func claudeUsageConsume(object map[string]any, sessionID string, rates RateTable, seen map[string]struct{}, draft *usageDraft) {
	if other, ok := asString(object["sessionId"]); ok && other != "" && other != sessionID {
		draft.Gap = true
		return
	}
	if typ, _ := asString(object["type"]); typ != "assistant" {
		return
	}
	message, ok := asObject(object["message"])
	if !ok {
		return
	}
	if role, _ := asString(message["role"]); role != "assistant" {
		return
	}
	if model, _ := asString(message["model"]); model == "<synthetic>" {
		return
	}
	// One API response is written as one line per content block, each with
	// its own `uuid` but the same `message.id` and the same usage.
	key, hasKey := "", false
	if id, _ := asString(message["id"]); id != "" {
		key, hasKey = id, true
	} else if uuid, _ := asString(object["uuid"]); uuid != "" {
		key, hasKey = uuid, true
	}
	if hasKey {
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
	}
	usage, ok := asObject(message["usage"])
	if !ok {
		draft.Gap = true
		return
	}
	call := claudeUsageCall(usage)
	if call.CategoryGap {
		draft.CategoryGap = true
	}
	var total *int
	if call.Input != nil && call.Output != nil && call.CacheRead != nil && call.CacheWrite != nil {
		total = RateTableSum([]int{*call.Input, *call.Output, *call.CacheRead, *call.CacheWrite})
		if total == nil {
			draft.Gap = true
		}
	}
	if call.Input != nil || call.Output != nil || call.CacheRead != nil || call.CacheWrite != nil || call.Reasoning != nil {
		draft.add(call.Input, call.Output, call.CacheRead, call.CacheWrite, call.Reasoning, total)
	}
	price := claudeUsagePrice(object, message, usage, call, rates)
	switch price.Kind {
	case claudePricePriced:
		source, revision := rates.Source, rates.Checked
		draft.addCost(price.Nanos, price.Estimated, price.Model, &source, &revision)
	default:
		draft.Gap = true
	}
}

// claudeCall is one API call's usage. Each count is nil when missing or unusable;
// CategoryGap is set when a required count was not usable.
type claudeCall struct {
	Input         *int
	Output        *int
	CacheRead     *int
	CacheWrite    *int
	Reasoning     *int
	CategoryGap   bool
	Five          *int
	Hour          *int
	SplitUnusable bool
}

// claudeUsageCall keeps each valid count of a call's usage. A required field that
// is absent or unusable gaps that field only.
func claudeUsageCall(usage map[string]any) claudeCall {
	var call claudeCall
	required := func(key string) *int {
		value := claudeUsageCount(usage, key)
		if value == nil {
			call.CategoryGap = true
		}
		return value
	}
	call.Input = required("input_tokens")
	call.Output = required("output_tokens")
	call.CacheRead = required("cache_read_input_tokens")
	call.CacheWrite = required("cache_creation_input_tokens")
	if _, present := usage["reasoning_tokens"]; present {
		if value := claudeUsageCount(usage, "reasoning_tokens"); value != nil {
			call.Reasoning = value
		} else {
			call.CategoryGap = true
		}
	}
	if call.CacheWrite != nil && *call.CacheWrite > 0 {
		creation, ok := asObject(usage["cache_creation"])
		var five, hour *int
		if ok {
			five = claudeUsageCount(creation, "ephemeral_5m_input_tokens")
			hour = claudeUsageCount(creation, "ephemeral_1h_input_tokens")
		}
		if ok && five != nil && hour != nil && *five+*hour == *call.CacheWrite {
			call.Five = five
			call.Hour = hour
		} else {
			call.SplitUnusable = true
		}
	}
	return call
}

// claudePriceKind is whether a call was priced or not.
type claudePriceKind string

const (
	claudePricePriced   claudePriceKind = "priced"
	claudePriceUnpriced claudePriceKind = "unpriced"
)

// claudePrice is a call's price in nano-dollars, with whether it was estimated and its model.
type claudePrice struct {
	Kind      claudePriceKind
	Nanos     int
	Estimated bool
	Model     *string
}

// claudeUsagePrice prices a call: the reported dollars when present, else the
// rate table's estimate.
func claudeUsagePrice(object, message, usage map[string]any, call claudeCall, rates RateTable) claudePrice {
	unpriced := claudePrice{Kind: claudePriceUnpriced}
	money := claudeUsageMoney(object, message, usage)
	switch money.Kind {
	case claudeMoneyInvalid:
		return unpriced
	case claudeMoneyValue:
		nanos := claudeUsageDirect(money.Value)
		if nanos == nil {
			return unpriced
		}
		return claudePrice{Kind: claudePricePriced, Nanos: *nanos}
	}
	if call.Input == nil || call.Output == nil || call.CacheRead == nil || call.CacheWrite == nil || call.SplitUnusable {
		return unpriced
	}
	tier := claudeUsageTier(usage, message, object)
	if tier == nil {
		return unpriced
	}
	model, ok := asString(message["model"])
	if !ok {
		return unpriced
	}
	rate := rates.Rate(model)
	if rate == nil {
		return unpriced
	}
	input := RateTableSum([]int{*call.Input, *call.CacheRead, *call.CacheWrite})
	prices := rate.PricesFor(*tier, input, rates.LongContextAbove)
	if prices == nil {
		return unpriced
	}
	inputNanos := RateTableNanos(*call.Input, prices.Input)
	outputNanos := RateTableNanos(*call.Output, prices.Output)
	readNanos := rateTableNanosAt(*call.CacheRead, prices.CacheRead)
	if inputNanos == nil || outputNanos == nil || readNanos == nil {
		return unpriced
	}
	var write int
	switch {
	case *call.CacheWrite == 0:
		write = 0
	case call.Five != nil && call.Hour != nil:
		fiveNanos := rateTableNanosAt(*call.Five, prices.CacheWrite5m)
		hourNanos := rateTableNanosAt(*call.Hour, prices.CacheWrite1h)
		if fiveNanos == nil || hourNanos == nil {
			return unpriced
		}
		sum := RateTableSum([]int{*fiveNanos, *hourNanos})
		if sum == nil {
			return unpriced
		}
		write = *sum
	default:
		return unpriced
	}
	nanos := RateTableSum([]int{*inputNanos, *outputNanos, *readNanos, write})
	if nanos == nil {
		return unpriced
	}
	return claudePrice{Kind: claudePricePriced, Nanos: *nanos, Estimated: true, Model: &model}
}

// claudeMoneyKind is whether a call reported a cost, and whether it is usable.
type claudeMoneyKind string

const (
	claudeMoneyAbsent  claudeMoneyKind = "absent"
	claudeMoneyValue   claudeMoneyKind = "value"
	claudeMoneyInvalid claudeMoneyKind = "invalid"
)

// claudeMoney is a reported cost in dollars, when Kind is claudeMoneyValue.
type claudeMoney struct {
	Kind  claudeMoneyKind
	Value *big.Rat
}

// claudeUsageMoney reads the first reported cost field, in object, message, usage order.
func claudeUsageMoney(object, message, usage map[string]any) claudeMoney {
	for _, source := range []map[string]any{object, message, usage} {
		for _, key := range []string{"costUSD", "cost_usd", "total_cost_usd"} {
			value, present := source[key]
			if !present {
				continue
			}
			decimal := claudeUsageDecimal(value)
			if decimal == nil || decimal.Sign() < 0 {
				return claudeMoney{Kind: claudeMoneyInvalid}
			}
			return claudeMoney{Kind: claudeMoneyValue, Value: decimal}
		}
	}
	return claudeMoney{Kind: claudeMoneyAbsent}
}

// claudeUsageDirect converts reported dollars to nano-dollars, rounding half up.
// Nil when the value is negative or does not fit.
func claudeUsageDirect(dollars *big.Rat) *int {
	product := new(big.Rat).Mul(dollars, big.NewRat(1_000_000_000, 1))
	rounded, ok := intFromBig(roundHalfUp(product))
	if !ok || rounded < 0 {
		return nil
	}
	return &rounded
}

// claudeUsageTier is the price tier of a call from its speed. Nil for a call the
// table has no prices for.
func claudeUsageTier(usage, message, object map[string]any) *PriceTier {
	if tier, ok := asString(usage["service_tier"]); ok && tier != "standard" {
		return nil
	}
	if geo, ok := asString(usage["inference_geo"]); ok && geo != "global" && geo != "not_available" {
		return nil
	}
	speed, found := firstString(usage["speed"], message["speed"], object["speed"])
	standard, fast := PriceTierStandard, PriceTierFast
	if !found {
		return &standard
	}
	switch speed {
	case "standard":
		return &standard
	case "fast":
		return &fast
	}
	return nil
}

// claudeUsageCount is the exact count stored under key, or nil when it is missing or unusable.
func claudeUsageCount(object map[string]any, key string) *int {
	value, ok := object[key]
	if !ok {
		return nil
	}
	return claudeUsageExact(value)
}

// claudeUsageExact is value as an exact non-negative integer (at most 2^53), or
// nil. Booleans and fractions are not counts.
func claudeUsageExact(value any) *int {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	if number < 0 || number > float64(1<<53) || math.Trunc(number) != number {
		return nil
	}
	n := int(number)
	return &n
}

// claudeUsageDecimal is a JSON number as an exact decimal, or nil for any other value.
func claudeUsageDecimal(value any) *big.Rat {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	// The shortest decimal that reads back to the same float, which is the number as written.
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(number, 'f', -1, 64))
	if !ok {
		return nil
	}
	return r
}
