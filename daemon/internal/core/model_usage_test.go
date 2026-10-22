package core

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestFormattingAndExactAddition checks exact usage addition: costs and
// categories add, and += adds in place.
func TestFormattingAndExactAddition(t *testing.T) {
	t.Parallel()
	if got := UsageAdd(NewUsage(UsageArgs{CostNanos: new(1)}), NewUsage(UsageArgs{CostNanos: new(2)})); got.CostNanos == nil || *got.CostNanos != 3 {
		t.Fatalf("1 + 2 = %+v, want 3", got.CostNanos)
	}
	usage := NewUsage(UsageArgs{CostNanos: new(1), Input: new(2), Output: new(3), CacheRead: new(4),
		CacheWrite: new(5), Reasoning: new(6), TotalTokens: new(20)})
	usage.AddUsage(usage)
	want := NewUsage(UsageArgs{CostNanos: new(2), Input: new(4), Output: new(6), CacheRead: new(8),
		CacheWrite: new(10), Reasoning: new(12), TotalTokens: new(40)})
	if d := testDiffEmpty(want, usage); d != "" {
		t.Fatalf("usage += usage: %s", d)
	}
}

// TestUsageAddOverflowIsNil checks that a category whose sum overflows reads as
// unknown, and that a category set on one side only keeps that value.
func TestUsageAddOverflowIsNil(t *testing.T) {
	t.Parallel()
	big := Usage{Input: new(math.MaxInt), Output: new(1)}
	sum := UsageAdd(big, Usage{Input: new(1), Output: new(2)})
	if sum.Input != nil {
		t.Errorf("overflowing input = %d, want nil", *sum.Input)
	}
	if sum.Output == nil || *sum.Output != 3 {
		t.Errorf("output = %v, want 3", sum.Output)
	}
	if one := UsageAdd(Usage{CacheRead: new(7)}, Usage{}); one.CacheRead == nil || *one.CacheRead != 7 {
		t.Errorf("one-sided cache read = %v, want 7", one.CacheRead)
	}
}

// TestUsageAddProvenanceAndCoverage checks that a sum is estimated when either
// side is, and partial when one side is incomplete, and untracked with no cost.
func TestUsageAddProvenanceAndCoverage(t *testing.T) {
	t.Parallel()
	reported := NewUsage(UsageArgs{CostNanos: new(10)})
	estimated := NewUsage(UsageArgs{CostNanos: new(5), Provenance: new(CostProvenanceEstimated)})
	both := UsageAdd(reported, estimated)
	if both.Provenance == nil || *both.Provenance != CostProvenanceEstimated || both.Coverage != UsageCoverageComplete {
		t.Errorf("reported + estimated = %+v", both)
	}
	partial := NewUsage(UsageArgs{CostNanos: new(1), Coverage: new(UsageCoveragePartial)})
	if got := UsageAdd(reported, partial); got.Coverage != UsageCoveragePartial {
		t.Errorf("coverage with a partial side = %s", got.Coverage)
	}
	none := UsageAdd(Usage{Coverage: UsageCoverageUntracked}, Usage{Coverage: UsageCoverageUntracked})
	if none.CostNanos != nil || none.Provenance != nil || none.Coverage != UsageCoverageUntracked {
		t.Errorf("untracked + untracked = %+v", none)
	}
}

// TestNewUsageDerivesProvenanceAndCoverage checks the defaults of NewUsage: a
// cost is reported and complete, and no cost is untracked, unless given.
func TestNewUsageDerivesProvenanceAndCoverage(t *testing.T) {
	t.Parallel()
	priced := NewUsage(UsageArgs{CostNanos: new(9)})
	if priced.Provenance == nil || *priced.Provenance != CostProvenanceReported || priced.Coverage != UsageCoverageComplete {
		t.Errorf("priced = %+v", priced)
	}
	tokens := NewUsage(UsageArgs{Input: new(3)})
	if tokens.Provenance != nil || tokens.Coverage != UsageCoverageUntracked || tokens.CostNanos != nil {
		t.Errorf("tokens only = %+v", tokens)
	}
}

// TestUsageRoundTripWithRate checks a usage with a rate round-trips, and that
// its coverage is always written.
func TestUsageRoundTripWithRate(t *testing.T) {
	t.Parallel()
	usage := NewUsage(UsageArgs{CostNanos: new(12), TotalTokens: new(4),
		Rate: &RateProvenance{Revision: "2026-07", Source: "https://example.test/rates", Models: []string{"m-1"}}})
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	var back Usage
	testDecode(t, string(data), &back)
	if d := testDiffEmpty(usage, back); d != "" {
		t.Fatalf("round trip: %s", d)
	}
	var untracked Usage
	testDecode(t, `{}`, &untracked)
	out, err := jsonx.Marshal(untracked)
	if err != nil || string(out) != `{"coverage":"untracked"}` {
		t.Fatalf("untracked encodes as %s (%v)", out, err)
	}
}

// TestTokenTotalsDelta checks the counters moved since a baseline, and that a
// counter that moved backwards, or a cache write that did, reads as no delta.
func TestTokenTotalsDelta(t *testing.T) {
	t.Parallel()
	base := TokenTotals{Input: 10, CacheRead: 5, Output: 3, Reasoning: 1, Total: 19, CacheWrite: new(2)}
	next := TokenTotals{Input: 15, CacheRead: 9, Output: 4, Reasoning: 1, Total: 29, CacheWrite: new(5)}
	got := next.Delta(base)
	if got == nil || got.Input != 5 || got.CacheRead != 4 || got.Output != 1 || got.Reasoning != 0 || got.Total != 10 ||
		got.CacheWrite == nil || *got.CacheWrite != 3 {
		t.Fatalf("delta = %+v", got)
	}
	if back := base.Delta(next); back != nil {
		t.Errorf("a backwards delta = %+v, want nil", back)
	}
	noWrite := TokenTotals{Input: 15, CacheRead: 9, Output: 4, Reasoning: 1, Total: 29}
	if d := noWrite.Delta(base); d == nil || d.CacheWrite != nil {
		t.Errorf("a missing cache write baseline = %+v, want no cache write", d)
	}
	if d := TokenTotalsZero.Delta(TokenTotalsZero); d == nil || d.Total != 0 {
		t.Errorf("zero delta = %+v", d)
	}
}

// TestUsageCheckpointEncodesOnlyWhatIsSet checks the optional keys of a checkpoint.
func TestUsageCheckpointEncodesOnlyWhatIsSet(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(UsageCheckpoint{Lines: new(3)})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"lines":3}` {
		t.Errorf("checkpoint = %s", data)
	}
}

// TestAttemptsAndRunsRoundTripWithAndWithoutUsage
// The run is testModelRun.
func TestAttemptsAndRunsRoundTripWithAndWithoutUsage(t *testing.T) {
	t.Parallel()
	run := testModelRun()
	run.Attempts = []Attempt{NewAttempt(AttemptArgs{StageID: "build", Number: 1, StartedAt: jsonx.Date{Time: testT0}, Folder: "build"})}
	old := btEncode(t, run)
	if strings.Contains(old, `"usage"`) || strings.Contains(old, `"usageMark"`) {
		t.Errorf("an attempt without usage wrote usage keys: %s", old)
	}
	if btDecode[Run](t, old).Attempts[0].Usage != nil {
		t.Error("an attempt saved without usage read one back")
	}
	usage := NewUsage(UsageArgs{CostNanos: new(123), Input: new(5), TotalTokens: new(5)})
	run.Attempts[0].Usage = &usage
	run.Attempts[0].UsageMark = new(789)
	loaded := btDecode[Run](t, btEncode(t, run))
	if d := testDiffEmpty(run, loaded); d != "" {
		t.Errorf("run with usage does not round trip:\n%s", d)
	}
}
