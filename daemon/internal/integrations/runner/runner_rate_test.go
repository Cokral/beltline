package runner

import (
	"strconv"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

func TestBothFilesLoadWithEveryModelPriced(t *testing.T) {
	t.Parallel()
	for _, table := range []RateTable{RateTableClaude(), RateTableOpenAI()} {
		if table.Checked == "missing" {
			t.Fatalf("%s did not load", table.Provider)
		}
		if !strings.HasPrefix(table.Source, "https://") {
			t.Errorf("%s source = %q", table.Provider, table.Source)
		}
		if len(table.Models) == 0 {
			t.Fatalf("%s has no models", table.Provider)
		}
		aliases := map[string]struct{}{}
		for id, rate := range table.Models {
			if rate.Standard.Input.Sign() <= 0 || rate.Standard.Output.Sign() <= 0 {
				t.Errorf("%s: standard price of %s is not positive", table.Provider, id)
			}
			if rate.FastLong != nil && rate.Fast == nil {
				t.Errorf("%s: %s has a fast long price but no fast price", table.Provider, id)
			}
			for _, alias := range rate.Aliases {
				if _, isModel := table.Models[alias]; isModel {
					t.Errorf("%s: %s is also a model", table.Provider, alias)
				}
				if _, dup := aliases[alias]; dup {
					t.Errorf("%s: %s names two models", table.Provider, alias)
				}
				aliases[alias] = struct{}{}
				found := table.Rate(alias)
				if found == nil {
					t.Errorf("%s: alias %s does not resolve", table.Provider, alias)
					continue
				}
				mustEqual(t, rate, *found)
			}
		}
	}
}

func TestPricesReadBackExactly(t *testing.T) {
	t.Parallel()
	openai := RateTableOpenAI()
	if openai.LongContextAbove == nil || *openai.LongContextAbove != 272_000 {
		t.Fatalf("openai long context = %v", openai.LongContextAbove)
	}
	luna := openai.Rate("gpt-6-luna")
	if luna == nil {
		t.Fatal("gpt-6-luna is not priced")
	}
	wantStandard := Prices{Input: rat(t, "0.1"), Output: rat(t, "0.5"), CacheRead: rat(t, "0.01"), CacheWrite: rat(t, "0.125")}
	if !pricesEqual(&luna.Standard, &wantStandard) {
		t.Errorf("standard = %+v", luna.Standard)
	}
	wantLong := Prices{Input: rat(t, "0.2"), Output: rat(t, "0.75"), CacheRead: rat(t, "0.02"), CacheWrite: rat(t, "0.25")}
	if !pricesEqual(luna.Long, &wantLong) {
		t.Errorf("long = %+v", luna.Long)
	}
	if luna.FastLong == nil || !ratEqual(luna.FastLong.Input, rat(t, "0.4")) {
		t.Errorf("fast long = %+v", luna.FastLong)
	}
	codex := openai.Rate("gpt-5.3-codex")
	if codex == nil || !ratEqual(codex.Standard.CacheRead, rat(t, "0.175")) {
		t.Errorf("codex = %+v", codex)
	}
	if codex != nil && codex.Long != nil {
		t.Errorf("codex has a long price")
	}
	sonnet := RateTableClaude().Rate("claude-sonnet-4-5")
	if sonnet == nil {
		t.Fatal("claude-sonnet-4-5 is not priced")
	}
	mustEqual(t, sonnet, RateTableClaude().Rate("claude-sonnet-4-5-20250929"))
	opus := RateTableClaude().Rate("claude-opus-5-5")
	if opus == nil || opus.Fast == nil || !ratEqual(opus.Fast.Input, rat(t, "8")) {
		t.Errorf("opus fast = %+v", opus)
	}
}

func TestABrokenFileIsRejected(t *testing.T) {
	t.Parallel()
	negative := `{"provider":"p","source":"s","checked":"c","models":{"m":{"input":-1,"output":1}}}`
	if _, err := DecodeRateTable([]byte(negative)); err == nil {
		t.Errorf("negative price accepted")
	}
	missing := `{"provider":"p","source":"s","checked":"c","models":{"m":{"input":1}}}`
	if _, err := DecodeRateTable([]byte(missing)); err == nil {
		t.Errorf("missing output accepted")
	}
}

// codexCount is one token_count line. total and last hold the counters in the order
// input, cached, cache write, output, total; a nil counter is left out. tier, when set,
// is the event's service tier.
func codexCount(t testing.TB, total []*int, last []*int, tier *string) string {
	t.Helper()
	keys := []string{"input_tokens", "cached_input_tokens", "cache_write_input_tokens", "output_tokens", "total_tokens"}
	usage := func(values []*int) map[string]any {
		object := map[string]any{"reasoning_output_tokens": 0}
		for i, value := range values {
			if value != nil {
				object[keys[i]] = *value
			}
		}
		return object
	}
	info := map[string]any{"total_token_usage": usage(total)}
	if last != nil {
		info["last_token_usage"] = usage(last)
	}
	payload := map[string]any{"type": "token_count", "info": info}
	if tier != nil {
		payload["service_tier"] = *tier
	}
	data, err := jsonx.Marshal(map[string]any{"type": "event_msg", "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// codexTurn is one turn_context line naming a model and, optionally, a tier.
func codexTurn(model string, tier *string) string {
	payload := map[string]any{"model": model}
	if tier != nil {
		payload["service_tier"] = *tier
	}
	return `{"type":"turn_context","payload":` + mustJSON(payload) + `}`
}

// mustJSON encodes a value as JSON text.
func mustJSON(value any) string {
	data, err := jsonx.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestEachTurnIsPricedAtItsOwnContextSize(t *testing.T) {
	t.Parallel()
	rates := NewRateTable(RateTableArgs{Provider: "test", Source: "s", Checked: "c", LongContextAbove: new(100), Models: map[string]ModelRate{
		"tiered": {Standard: Prices{Input: rat(t, "1"), Output: rat(t, "2"), CacheRead: rat(t, "1")}, Long: &Prices{Input: rat(t, "2"), Output: rat(t, "3"), CacheRead: rat(t, "1")}},
	}})
	lines := []string{
		codexTurn("tiered", nil),
		// One request of 50 input: short. 50×$1 + 10×$2.
		codexCount(t, []*int{new(50), new(0), nil, new(10), new(60)}, []*int{new(50), new(0), nil, new(10), new(60)}, nil),
		// One request of 200 input: long. 200×$2 + 10×$3.
		codexCount(t, []*int{new(250), new(0), nil, new(20), new(270)}, []*int{new(200), new(0), nil, new(10), new(210)}, nil),
		// Two requests of 150 together, not the last one alone: their sizes are unknown.
		codexCount(t, []*int{new(400), new(0), nil, new(30), new(430)}, []*int{new(100), new(0), nil, new(5), new(105)}, nil),
		// Several requests of 60 together: every one is short. 60×$1 + 10×$2.
		codexCount(t, []*int{new(460), new(0), nil, new(40), new(500)}, []*int{new(30), new(0), nil, new(5), new(35)}, nil),
	}
	usage := CodexUsageOf(strings.Join(lines, "\n"), &core.TokenTotals{}, &rates)
	if usage == nil || *usage.CostNanos != 70_000+430_000+80_000 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.Coverage != core.UsageCoveragePartial {
		t.Errorf("coverage = %v", usage.Coverage)
	}
	checkInt(t, "input", usage.Input, 460)
	checkInt(t, "output", usage.Output, 40)
}

func TestGpt6LunaIsPricedShortAndLongFromTheBundledTable(t *testing.T) {
	t.Parallel()
	short := []string{
		codexTurn("gpt-6-luna", nil),
		codexCount(t, []*int{new(200_000), new(190_000), new(0), new(1000), new(201_000)}, []*int{new(200_000), new(190_000), new(0), new(1000), new(201_000)}, nil),
	}
	// 10,000×$0.10 + 190,000×$0.01 + 1,000×$0.50 per million.
	shortUsage := CodexUsageOf(strings.Join(short, "\n"), &core.TokenTotals{}, nil)
	if shortUsage == nil || *shortUsage.CostNanos != 3_400_000 {
		t.Fatalf("short = %+v", shortUsage)
	}
	if shortUsage.Coverage != core.UsageCoverageComplete || *shortUsage.Provenance != core.CostProvenanceEstimated {
		t.Errorf("short coverage/provenance = %v %v", shortUsage.Coverage, *shortUsage.Provenance)
	}
	if shortUsage.Rate == nil || shortUsage.Rate.Source != RateTableOpenAI().Source {
		t.Errorf("short rate = %+v", shortUsage.Rate)
	}
	long := []string{
		codexTurn("gpt-6-luna", nil),
		codexCount(t, []*int{new(300_000), new(290_000), new(0), new(1000), new(301_000)}, []*int{new(300_000), new(290_000), new(0), new(1000), new(301_000)}, nil),
	}
	// 10,000×$0.20 + 290,000×$0.02 + 1,000×$0.75 per million.
	longUsage := CodexUsageOf(strings.Join(long, "\n"), &core.TokenTotals{}, nil)
	if longUsage == nil || *longUsage.CostNanos != 8_550_000 {
		t.Fatalf("long = %+v", longUsage)
	}
}

func TestCacheWritesAreBilledInsteadOfInput(t *testing.T) {
	t.Parallel()
	rates := NewRateTable(RateTableArgs{Provider: "test", Source: "s", Checked: "c", Models: map[string]ModelRate{
		"w":       {Standard: Prices{Input: rat(t, "1"), Output: rat(t, "2"), CacheRead: rat(t, "0.5"), CacheWrite: rat(t, "1.25")}},
		"nowrite": {Standard: Prices{Input: rat(t, "1"), Output: rat(t, "2"), CacheRead: rat(t, "0.5")}},
	}})
	first := codexCount(t, []*int{new(100), new(40), new(20), new(10), new(110)}, []*int{new(100), new(40), new(20), new(10), new(110)}, nil)
	// 40 uncached×$1 + 40 cached×$0.50 + 20 written×$1.25 + 10 out×$2.
	usage := CodexUsageOf(codexTurn("w", nil)+"\n"+first, &core.TokenTotals{}, &rates)
	if usage == nil || *usage.CostNanos != 105_000 {
		t.Fatalf("usage = %+v", usage)
	}
	checkInt(t, "input", usage.Input, 40)
	checkInt(t, "cacheRead", usage.CacheRead, 40)
	checkInt(t, "cacheWrite", usage.CacheWrite, 20)
	unsold := CodexUsageOf(codexTurn("nowrite", nil)+"\n"+first, &core.TokenTotals{}, &rates)
	if unsold == nil || unsold.CostNanos != nil || unsold.Coverage != core.UsageCoverageUntracked {
		t.Fatalf("unsold = %+v", unsold)
	}
	// A checkpoint saved before writes were reported still finds its place.
	second := codexCount(t, []*int{new(200), new(100), new(20), new(20), new(220)}, []*int{new(100), new(60), new(0), new(10), new(110)}, nil)
	old := core.TokenTotals{Input: 100, CacheRead: 40, Output: 10, Reasoning: 0, Total: 110}
	resumed := CodexUsageOf(codexTurn("w", nil)+"\n"+first+"\n"+second, &old, &rates)
	// 40 uncached×$1 + 60 cached×$0.50 + 10 out×$2.
	if resumed == nil || *resumed.CostNanos != 90_000 {
		t.Fatalf("resumed = %+v", resumed)
	}
	if resumed.Coverage != core.UsageCoverageComplete {
		t.Errorf("resumed coverage = %v", resumed.Coverage)
	}
	cumulative := CodexCumulative(first + "\n" + second)
	if cumulative == nil || cumulative.CacheWrite == nil || *cumulative.CacheWrite != 20 {
		t.Errorf("cumulative = %+v", cumulative)
	}
}

func TestFastTierUsesFastPricesAndOtherTiersStayUnpriced(t *testing.T) {
	t.Parallel()
	rates := NewRateTable(RateTableArgs{Provider: "test", Source: "s", Checked: "c", Models: map[string]ModelRate{
		"t": {Standard: Prices{Input: rat(t, "1"), Output: rat(t, "2"), CacheRead: rat(t, "1")}, Fast: &Prices{Input: rat(t, "2"), Output: rat(t, "4"), CacheRead: rat(t, "1")}},
	}})
	step := codexCount(t, []*int{new(10), new(0), nil, new(5), new(15)}, []*int{new(10), new(0), nil, new(5), new(15)}, nil)
	cost := func(lines ...string) *core.Usage {
		return CodexUsageOf(strings.Join(lines, "\n"), &core.TokenTotals{}, &rates)
	}
	expectCost := func(name string, usage *core.Usage, want int) {
		t.Helper()
		if usage == nil || usage.CostNanos == nil || *usage.CostNanos != want {
			t.Errorf("%s = %+v, want %d", name, usage, want)
		}
	}
	expectCost("standard", cost(codexTurn("t", nil), step), 20_000)
	expectCost("default tier", cost(codexTurn("t", new("default")), step), 20_000)
	expectCost("fast tier", cost(codexTurn("t", new("fast")), step), 40_000)
	expectCost("priority tier", cost(codexTurn("t", nil), codexCount(t, []*int{new(10), new(0), nil, new(5), new(15)}, []*int{new(10), new(0), nil, new(5), new(15)}, new("priority"))), 40_000)
	flex := cost(codexTurn("t", new("flex")), step)
	if flex == nil || flex.CostNanos != nil || flex.Coverage != core.UsageCoverageUntracked {
		t.Errorf("flex = %+v", flex)
	}
}

// claudeRateCall is one assistant line for the Claude rate tests: a call of the model with the counts.
func claudeRateCall(model string, input, cacheRead int, speed string) string {
	speedField := ""
	if speed != "" {
		speedField = `,"speed":"` + speed + `"`
	}
	return `{"type":"assistant","uuid":"u","sessionId":"s","message":{"id":"m","role":"assistant","model":"` + model +
		`","usage":{"input_tokens":` + strconv.Itoa(input) + `,"output_tokens":100,"cache_read_input_tokens":` + strconv.Itoa(cacheRead) + `,"cache_creation_input_tokens":0` + speedField + `}}}`
}

func TestClaudeCallsArePricedByModelSpeedAndContext(t *testing.T) {
	t.Parallel()
	// Each case: the call's model, input, cache-read counts, and speed, with the cost in nano-dollars (nil: unpriced).
	cases := []struct {
		name           string
		model          string
		input, cacheRd int
		speed          string
		want           *int
	}{
		// 1000×$4 + 100×$20 + 1000×$0.20 against the fast tier's 1000×$8 + 100×$40 + 1000×$0.40, per million.
		{"opus standard", "claude-opus-5-5", 1000, 1000, "standard", new(6_200_000)},
		{"opus fast", "claude-opus-5-5", 1000, 1000, "fast", new(12_400_000)},
		{"opus 4.7 has no fast price", "claude-opus-4-7", 1000, 1000, "fast", nil},
		// Haiku 5.5 at 100,000 in all: 60,000×$0.10 + 40,000×$0.01 + 100×$0.50.
		{"haiku short", "claude-haiku-5-5", 60_000, 40_000, "", new(6_450_000)},
		// 100,001 in all: 60,001×$0.50 + 40,000×$0.05 + 100×$2.50.
		{"haiku long", "claude-haiku-5-5", 60_001, 40_000, "", new(32_250_500)},
		// A model without a long price keeps its standard price past the threshold: 200,000×$4 + 100×$20.
		{"opus past the threshold", "claude-opus-5-5", 200_000, 0, "", new(802_000_000)},
	}
	for _, c := range cases {
		usage := ClaudeUsageUsage(claudeRateCall(c.model, c.input, c.cacheRd, c.speed), new(0), "s", nil)
		if usage == nil {
			t.Fatalf("%s: no usage", c.name)
		}
		if c.want == nil {
			if usage.CostNanos != nil || usage.Coverage != core.UsageCoverageUntracked {
				t.Errorf("%s: priced %+v, want unpriced", c.name, usage)
			}
			continue
		}
		if usage.CostNanos == nil || *usage.CostNanos != *c.want {
			t.Errorf("%s: cost = %v, want %d", c.name, usage.CostNanos, *c.want)
		}
	}
}

func TestALongContextPriceAppliesToTheWholeCall(t *testing.T) {
	t.Parallel()
	rates := NewRateTable(RateTableArgs{Provider: "test", Source: "s", Checked: "c", LongContextAbove: new(100), Models: map[string]ModelRate{
		"m": {
			Standard: Prices{Input: rat(t, "1"), Output: rat(t, "2"), CacheRead: rat(t, "0.1"), CacheWrite5m: rat(t, "1"), CacheWrite1h: rat(t, "1")},
			Long:     &Prices{Input: rat(t, "2"), Output: rat(t, "3"), CacheRead: rat(t, "0.2"), CacheWrite5m: rat(t, "1"), CacheWrite1h: rat(t, "1")},
		},
	}})
	// 50 + 40 input in all: 50×$1 + 40×$0.10 + 100×$2.
	if got := ClaudeUsageUsage(claudeRateCall("m", 50, 40, ""), new(0), "s", &rates); got == nil || *got.CostNanos != 254_000 {
		t.Errorf("short call = %+v", got)
	}
	// 50 + 60: 50×$2 + 60×$0.20 + 100×$3.
	if got := ClaudeUsageUsage(claudeRateCall("m", 50, 60, ""), new(0), "s", &rates); got == nil || *got.CostNanos != 412_000 {
		t.Errorf("long call = %+v", got)
	}
}
