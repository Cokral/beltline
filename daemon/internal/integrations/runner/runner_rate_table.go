package runner

import (
	"embed"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sync"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Decimal values are *big.Rat here: exact, and nil where a value is absent.

// rateFiles are the bundled rate tables (resources/rates).
// Edit the numbers in those files, not here.
//
//go:embed resources/rates/claude.json resources/rates/openai.json
var rateFiles embed.FS

// Prices is one tier's prices in dollars per million tokens. A nil price is a
// category this tier does not sell: tokens in it leave the call unpriced.
type Prices struct {
	// Input and Output are required for every tier.
	Input  *big.Rat
	Output *big.Rat
	// CacheRead is the price of a cache hit or refresh.
	CacheRead *big.Rat
	// CacheWrite is OpenAI's single cache-write price. A written token is billed
	// here instead of as input.
	CacheWrite *big.Rat
	// CacheWrite5m and CacheWrite1h are Claude's 5-minute and 1-hour cache writes.
	CacheWrite5m *big.Rat
	CacheWrite1h *big.Rat
}

// PriceTier is the price tier a call is billed at.
type PriceTier string

const (
	// PriceTierStandard is the standard tier.
	PriceTierStandard PriceTier = "standard"
	// PriceTierFast is fast mode (Claude's speed `fast`, OpenAI's `fast` or `priority`).
	PriceTierFast PriceTier = "fast"
)

// ModelRate is one model's prices: standard, and optionally long context and fast,
// each with its own long price.
type ModelRate struct {
	Standard Prices
	Long     *Prices
	Fast     *Prices
	FastLong *Prices
	Aliases  []string
}

// PricesFor is the prices for one request. input is its whole input, cached or
// not. Nil when the tier is not sold, or when the model has a long-context price
// and the request's size is unknown or above the threshold with no long price for
// that tier.
func (m ModelRate) PricesFor(tier PriceTier, input *int, longAbove *int) *Prices {
	short, longer := &m.Standard, m.Long
	if tier == PriceTierFast {
		short, longer = m.Fast, m.FastLong
	}
	if short == nil {
		return nil
	}
	if longAbove == nil || m.Long == nil {
		return short
	}
	if input == nil {
		return nil
	}
	if *input > *longAbove {
		return longer
	}
	return short
}

// RateTable is one provider's API list prices, read from
// `resources/rates/<provider>.json`. Models and aliases missing there stay unpriced.
type RateTable struct {
	Provider string
	Source   string
	// Checked is when the prices were last checked against Source. Saved estimates keep theirs as the revision.
	Checked string
	// LongContextAbove is the input-token count above which a model's long-context price applies.
	LongContextAbove *int
	// Models maps a model id to its rate.
	Models map[string]ModelRate
	// ids maps every model id and alias to its model id.
	ids map[string]string
}

// RateTableArgs are the parameters of NewRateTable.
type RateTableArgs struct {
	Provider         string
	Source           string
	Checked          string
	LongContextAbove *int
	Models           map[string]ModelRate
}

// NewRateTable builds a table and its id index, so each alias resolves to its model.
func NewRateTable(a RateTableArgs) RateTable {
	ids := map[string]string{}
	// Sorted, so an alias named by two models resolves the same way every time.
	modelIDs := slices.Sorted(maps.Keys(a.Models))
	for _, id := range modelIDs {
		for _, alias := range a.Models[id].Aliases {
			ids[alias] = id
		}
	}
	for _, id := range modelIDs {
		ids[id] = id
	}
	return RateTable{
		Provider: a.Provider, Source: a.Source, Checked: a.Checked, LongContextAbove: a.LongContextAbove,
		Models: a.Models, ids: ids,
	}
}

var (
	claudeRateTable = sync.OnceValue(func() RateTable { return bundledRateTable("claude") })
	openaiRateTable = sync.OnceValue(func() RateTable { return bundledRateTable("openai") })
)

// RateTableClaude is the bundled Claude table.
func RateTableClaude() RateTable { return claudeRateTable() }

// RateTableOpenAI is the bundled OpenAI table.
func RateTableOpenAI() RateTable { return openaiRateTable() }

// bundledRateTable reads a bundled table by name. A missing or broken file gives
// an empty table, which prices nothing.
func bundledRateTable(name string) RateTable {
	data, err := rateFiles.ReadFile("resources/rates/" + name + ".json")
	if err == nil {
		if table, err := DecodeRateTable(data); err == nil {
			return table
		}
	}
	return NewRateTable(RateTableArgs{Provider: name, Checked: "missing", Models: map[string]ModelRate{}})
}

// Rate is the model with this id or alias, or nil.
func (t RateTable) Rate(model string) *ModelRate {
	id, ok := t.ids[model]
	if !ok {
		return nil
	}
	rate, ok := t.Models[id]
	if !ok {
		return nil
	}
	return &rate
}

// DecodeRateTable decodes a rate file.
func DecodeRateTable(data []byte) (RateTable, error) {
	var file rateFile
	if err := json.Unmarshal(data, &file); err != nil {
		return RateTable{}, err
	}
	models := make(map[string]ModelRate, len(file.Models))
	for id, entry := range file.Models {
		if !entry.complete() {
			return RateTable{}, fmt.Errorf("rate table model %q needs an input and an output price", id)
		}
		rate := ModelRate{Standard: entry.Prices(), Aliases: []string{}}
		if entry.Aliases != nil {
			rate.Aliases = entry.Aliases
		}
		if entry.Long != nil {
			long := entry.Long.Prices()
			rate.Long = &long
		}
		if entry.Fast != nil {
			fast := entry.Fast.Prices()
			rate.Fast = &fast
			if entry.Fast.Long != nil {
				fastLong := entry.Fast.Long.Prices()
				rate.FastLong = &fastLong
			}
		}
		models[id] = rate
	}
	return NewRateTable(RateTableArgs{
		Provider: file.Provider, Source: file.Source, Checked: file.Checked,
		LongContextAbove: file.LongContextAbove, Models: models,
	}), nil
}

// RateTableNanos is the nano-dollars for tokens at a per-million-token price,
// rounded half up. Nil when the tokens are negative or the product does not fit.
func RateTableNanos(tokens int, perMillion *big.Rat) *int {
	if tokens < 0 {
		return nil
	}
	if tokens == 0 {
		zero := 0
		return &zero
	}
	if perMillion == nil {
		return nil
	}
	product := new(big.Rat).Mul(perMillion, new(big.Rat).SetInt64(int64(tokens)))
	product.Mul(product, big.NewRat(1000, 1))
	rounded, ok := intFromBig(roundHalfUp(product))
	if !ok || rounded < 0 {
		return nil
	}
	return &rounded
}

// rateTableNanosAt is the nano-dollars for a count at an optional price: zero
// tokens cost nothing, and tokens at a missing price are nil.
func rateTableNanosAt(tokens int, price *big.Rat) *int {
	if tokens == 0 {
		zero := 0
		return &zero
	}
	if price == nil {
		return nil
	}
	return RateTableNanos(tokens, price)
}

// RateTableSum adds the parts, or is nil when one is nil or the sum overflows.
func RateTableSum(parts []int) *int {
	sum := 0
	for _, part := range parts {
		next, ok := core.AddInts(sum, part)
		if !ok {
			return nil
		}
		sum = next
	}
	return &sum
}

// rateFile is the JSON layout of a rate file. Unknown keys such as `notes` are ignored.
type rateFile struct {
	Provider         string                `json:"provider"`
	Source           string                `json:"source"`
	Checked          string                `json:"checked"`
	LongContextAbove *int                  `json:"longContextAbove,omitzero"`
	Models           map[string]*rateEntry `json:"models"`
}

// rateEntry is one model's entry in a rate file. Its `long` and `fast` entries
// hold their own prices, and `fast.long` the fast long-context price.
type rateEntry struct {
	Aliases      []string   `json:"aliases,omitzero"`
	Input        ratePrice  `json:"input"`
	Output       ratePrice  `json:"output"`
	CacheRead    *ratePrice `json:"cacheRead,omitzero"`
	CacheWrite   *ratePrice `json:"cacheWrite,omitzero"`
	CacheWrite5m *ratePrice `json:"cacheWrite5m,omitzero"`
	CacheWrite1h *ratePrice `json:"cacheWrite1h,omitzero"`
	Long         *rateEntry `json:"long,omitzero"`
	Fast         *rateEntry `json:"fast,omitzero"`
}

// complete reports whether the entry, and each long and fast tier, has an input
// and an output price.
func (e *rateEntry) complete() bool {
	if e.Input.Value == nil || e.Output.Value == nil {
		return false
	}
	for _, tier := range []*rateEntry{e.Long, e.Fast} {
		if tier != nil && !tier.complete() {
			return false
		}
	}
	return true
}

// Prices is the entry's prices.
func (e *rateEntry) Prices() Prices {
	return Prices{
		Input: e.Input.Value, Output: e.Output.Value,
		CacheRead: priceValue(e.CacheRead), CacheWrite: priceValue(e.CacheWrite),
		CacheWrite5m: priceValue(e.CacheWrite5m), CacheWrite1h: priceValue(e.CacheWrite1h),
	}
}

// priceValue is the optional price's value, or nil.
func priceValue(p *ratePrice) *big.Rat {
	if p == nil {
		return nil
	}
	return p.Value
}

// ratePrice is one price: a non-negative number of dollars per million tokens,
// read back through its shortest decimal spelling so it is exact.
type ratePrice struct {
	Value *big.Rat
}

// UnmarshalJSON reads a price, failing on a negative or non-finite number.
func (p *ratePrice) UnmarshalJSON(data []byte) error {
	value, ok := new(big.Rat).SetString(string(data))
	if !ok {
		return errors.New("A price is a non-negative number of dollars per million tokens.") //nolint:staticcheck // ST1005: the wording is kept as the rate-table errors have always read.
	}
	if value.Sign() < 0 {
		return errors.New("A price is a non-negative number of dollars per million tokens.") //nolint:staticcheck // ST1005: the wording is kept as the rate-table errors have always read.
	}
	p.Value = value
	return nil
}

// usageDraft is the turns folded into one attempt's usage. A gap keeps the known
// subtotal and marks coverage.
type usageDraft struct {
	Cost int
	// HasCost is set once any call was priced.
	HasCost bool
	// Estimated is set when a priced call was estimated from the rate table, not reported.
	Estimated bool
	Gap       bool
	// CategoryGap is a counted category on an attributable record that was missing or unusable.
	// Coverage stays partial even when no dollars were priced.
	CategoryGap bool
	SawRecord   bool
	Input       *int
	Output      *int
	CacheRead   *int
	CacheWrite  *int
	Reasoning   *int
	Total       *int
	Models      []string
	Source      *string
	Revision    *string
}

// add folds one record's counts in. A nil count leaves the total as it was.
func (d *usageDraft) add(input, output, cacheRead, cacheWrite, reasoning, total *int) {
	d.SawRecord = true
	d.Input = d.fold(d.Input, input)
	d.Output = d.fold(d.Output, output)
	d.CacheRead = d.fold(d.CacheRead, cacheRead)
	d.CacheWrite = d.fold(d.CacheWrite, cacheWrite)
	d.Reasoning = d.fold(d.Reasoning, reasoning)
	d.Total = d.fold(d.Total, total)
}

// addCost folds one call's nano-dollars in. An overflowing sum is a gap.
func (d *usageDraft) addCost(nanos int, estimated bool, model *string, source *string, revision *string) {
	sum, ok := core.AddInts(d.Cost, nanos)
	if !ok {
		d.Gap = true
		return
	}
	d.Cost = sum
	d.HasCost = true
	if estimated {
		d.Estimated = true
		if model != nil && !slices.Contains(d.Models, *model) {
			d.Models = append(d.Models, *model)
		}
		if d.Source == nil {
			d.Source = source
		}
		if d.Revision == nil {
			d.Revision = revision
		}
	}
}

// usage is the folded usage, or nil when no record was seen.
func (d *usageDraft) usage() *core.Usage {
	if !d.SawRecord {
		return nil
	}
	coverage := core.UsageCoverageComplete
	switch {
	case d.CategoryGap:
		coverage = core.UsageCoveragePartial
	case !d.HasCost:
		coverage = core.UsageCoverageUntracked
	case d.Gap:
		coverage = core.UsageCoveragePartial
	}
	var provenance *core.CostProvenance
	if d.HasCost {
		p := core.CostProvenanceReported
		if d.Estimated {
			p = core.CostProvenanceEstimated
		}
		provenance = &p
	}
	var rate *core.RateProvenance
	if d.Estimated && d.HasCost && d.Revision != nil && d.Source != nil {
		models := append([]string{}, d.Models...)
		rate = &core.RateProvenance{Revision: *d.Revision, Source: *d.Source, Models: models}
	}
	var cost *int
	if d.HasCost {
		c := d.Cost
		cost = &c
	}
	usage := core.NewUsage(core.UsageArgs{
		CostNanos: cost, Input: d.Input, Output: d.Output, CacheRead: d.CacheRead, CacheWrite: d.CacheWrite,
		Reasoning: d.Reasoning, TotalTokens: d.Total, Provenance: provenance, Coverage: &coverage, Rate: rate,
	})
	return &usage
}

// fold adds next to current, keeping the one present when the other is nil. An
// overflowing sum keeps current and marks a gap.
func (d *usageDraft) fold(current, next *int) *int {
	switch {
	case current == nil && next == nil:
		return nil
	case current == nil:
		v := *next
		return &v
	case next == nil:
		return current
	}
	sum, ok := core.AddInts(*current, *next)
	if !ok {
		d.Gap = true
		return current
	}
	return &sum
}
