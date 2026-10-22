package core

import (
	"slices"
	"testing"
	"time"

	"encoding/json"
)

// policyAllowance builds a ModelAllowance from runner -> identifiers.
func policyAllowance(identifiers map[Backend][]string) ModelAllowance {
	allowance := ModelAllowance{Identifiers: map[Backend]map[string]struct{}{}}
	for runner, ids := range identifiers {
		set := map[string]struct{}{}
		for _, id := range ids {
			set[id] = struct{}{}
		}
		allowance.Identifiers[runner] = set
	}
	return allowance
}

// policyIDs are the ids of the choices, in order.
func policyIDs(choices []ModelChoice) []string {
	ids := []string{}
	for _, choice := range choices {
		ids = append(ids, choice.ID)
	}
	return ids
}

// policyLabels are the display labels of the choices, in order.
func policyLabels(choices []ModelChoice) []string {
	labels := []string{}
	for _, choice := range choices {
		labels = append(labels, choice.DisplayLabel())
	}
	return labels
}

// TestCatalogDeduplicatesSortsAndRequiresMatchingExecutable
func TestCatalogDeduplicatesSortsAndRequiresMatchingExecutable(t *testing.T) {
	t.Parallel()
	snapshot, err := NewModelCatalogSnapshot(BackendPi, "/custom/pi", time.Unix(100, 0), []ModelChoice{
		{ID: "z", Name: "Same"},
		{ID: "a", Name: "Same"},
		{ID: "z", Name: "Same"},
		{ID: "b", Name: "Alpha"},
		{ID: "", Name: "Ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := policyIDs(snapshot.Models); !slices.Equal(got, []string{"b", "a", "z"}) {
		t.Fatalf("ids = %v, want [b a z]", got)
	}
	if !snapshot.IsUsable(BackendPi, "/custom/pi") {
		t.Fatal("the snapshot is not usable with its own executable")
	}
	if snapshot.IsUsable(BackendPi, "pi") {
		t.Fatal("the snapshot is usable with another executable")
	}
	if snapshot.IsUsable(BackendCodex, "/custom/pi") {
		t.Fatal("the snapshot is usable for another runner")
	}
}

// TestNewModelCatalogSnapshotRefusesContradictions checks that one id reported
// with two context sizes is a ModelCatalogContradiction, even when names match.
func TestNewModelCatalogSnapshotRefusesContradictions(t *testing.T) {
	t.Parallel()
	_, err := NewModelCatalogSnapshot(BackendClaude, "claude", time.Unix(0, 0), []ModelChoice{
		{ID: "opus", Name: "Opus", ContextTokens: new(250_000)},
		{ID: "opus", Name: "Opus", ContextTokens: new(1_000_000)},
	})
	if _, ok := err.(ModelCatalogContradiction); !ok {
		t.Fatalf("error = %v, want a ModelCatalogContradiction", err)
	}
}

// TestCurrentChoicesArePermissionCatalogIntersection
func TestCurrentChoicesArePermissionCatalogIntersection(t *testing.T) {
	t.Parallel()
	snapshot, err := NewModelCatalogSnapshot(BackendCursor, "cursor-agent", time.Time{}, []ModelChoice{
		{ID: "one", Name: "First"}, {ID: "two", Name: "Second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := policyAllowance(map[Backend][]string{BackendCursor: {"one", "removed"}})
	if got := policyIDs(policy.Choices(BackendCursor, &snapshot, "cursor-agent", "")); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("choices = %v, want [one]", got)
	}
	if got := policy.Choices(BackendCursor, &snapshot, "other", ""); len(got) != 0 {
		t.Fatalf("choices with another executable = %v", got)
	}
	if got := policy.Choices(BackendPi, &snapshot, "cursor-agent", ""); len(got) != 0 {
		t.Fatalf("choices for another runner = %v", got)
	}
	if got := policyIDs(policy.Choices(BackendCursor, &snapshot, "cursor-agent", "FIRST")); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("search FIRST = %v, want [one]", got)
	}
	if got := policy.Choices(BackendCursor, &snapshot, "cursor-agent", "two"); len(got) != 0 {
		t.Fatalf("search two = %v, want none", got)
	}
	if got := policy.UnavailableIdentifiers(BackendCursor, &snapshot, "cursor-agent"); !slices.Equal(got, []string{"removed"}) {
		t.Fatalf("unavailable = %v, want [removed]", got)
	}
	if got := policy.UnavailableIdentifiers(BackendCursor, nil, "cursor-agent"); !slices.Equal(got, []string{"one", "removed"}) {
		t.Fatalf("unavailable without a catalog = %v, want [one removed]", got)
	}
}

// TestSearchMatchesDisplayedContextSize
func TestSearchMatchesDisplayedContextSize(t *testing.T) {
	t.Parallel()
	snapshot, err := NewModelCatalogSnapshot(BackendClaude, "claude", time.Time{}, []ModelChoice{
		{ID: "claude-opus-5-5", Name: "Opus 5.5", ContextTokens: new(250_000)},
		{ID: "claude-opus-5-5[1m]", Name: "Opus 5.5", ContextTokens: new(1_000_000)},
		{ID: "composer-2.5", Name: "Composer 2.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := policyAllowance(map[Backend][]string{BackendClaude: {"claude-opus-5-5", "claude-opus-5-5[1m]", "composer-2.5"}})
	search := func(query string) []string {
		return policyIDs(policy.Choices(BackendClaude, &snapshot, "claude", query))
	}
	checks := []struct {
		query string
		want  []string
	}{
		{"250k", []string{"claude-opus-5-5"}},
		{"1M", []string{"claude-opus-5-5[1m]"}},
		{"Opus", []string{"claude-opus-5-5", "claude-opus-5-5[1m]"}},
		{"[1m]", []string{"claude-opus-5-5[1m]"}},
	}
	for _, check := range checks {
		if got := search(check.query); !slices.Equal(got, check.want) {
			t.Errorf("search %q = %v, want %v", check.query, got, check.want)
		}
	}
	if got := policyLabels(policy.Choices(BackendClaude, &snapshot, "claude", "")); !slices.Equal(got, []string{"Composer 2.5", "Opus 5.5 - 250k", "Opus 5.5 - 1M"}) {
		t.Errorf("labels = %v", got)
	}
	smallerOnly := policyAllowance(map[Backend][]string{BackendClaude: {"claude-opus-5-5"}})
	if got := policyIDs(smallerOnly.Choices(BackendClaude, &snapshot, "claude", "")); !slices.Equal(got, []string{"claude-opus-5-5"}) {
		t.Errorf("smaller only = %v", got)
	}
}

// TestAliasedVariantsFollowTheThinkingLevel checks the aliased variants that need no catalog parse.
func TestAliasedVariantsFollowTheThinkingLevel(t *testing.T) {
	t.Parallel()
	// claude-opus-5-5 as ModelVariantTests.cursorFoldsLevelsIntoOneChoicePerModelAndSpeed reads it.
	opus := ModelChoice{
		ID: "claude-opus-5-5", Name: "Claude Opus 5.5 1M", ContextTokens: new(1_000_000),
		Variants: map[string]string{
			"default": "claude-opus-5-5-medium", "low": "claude-opus-5-5-low", "medium": "claude-opus-5-5-medium",
			"high": "claude-opus-5-5-high", "xhigh": "claude-opus-5-5-xhigh", "max": "claude-opus-5-5-max",
		},
	}
	if got := opus.Identifier(nil); got != "claude-opus-5-5-medium" {
		t.Errorf("no level = %s", got)
	}
	high, max := ThinkingHigh, ThinkingMax
	if got := opus.Identifier(&high); got != "claude-opus-5-5-high" {
		t.Errorf("high = %s", got)
	}
	if got := opus.Identifier(&max); got != "claude-opus-5-5-max" {
		t.Errorf("max = %s", got)
	}
	// Without variants the identifier is passed as is, at any level.
	if got := (ModelChoice{ID: "auto", Name: "Auto"}).Identifier(&high); got != "auto" {
		t.Errorf("no variants = %s", got)
	}
	// A level missing above and below falls back to the default variant, then the id.
	onlyDefault := ModelChoice{ID: "m", Name: "M", Variants: map[string]string{"default": "m-default"}}
	if got := onlyDefault.Identifier(&high); got != "m-default" {
		t.Errorf("only default = %s", got)
	}
	noDefault := ModelChoice{ID: "m", Name: "M", Variants: map[string]string{"xhigh": "m-xhigh"}}
	if got := noDefault.Identifier(&high); got != "m-xhigh" {
		t.Errorf("nearest above = %s", got)
	}
}

// TestAdmissionResolvesTheVariantFromItsFrozenChoice checks that admission resolves the variant
// from the frozen choice, which is written out here.
func TestAdmissionResolvesTheVariantFromItsFrozenChoice(t *testing.T) {
	t.Parallel()
	opus := ModelChoice{
		ID: "claude-opus-5-5", Name: "Claude Opus 5.5 1M", ContextTokens: new(1_000_000),
		Variants: map[string]string{
			"default": "claude-opus-5-5-medium", "low": "claude-opus-5-5-low", "medium": "claude-opus-5-5-medium",
			"high": "claude-opus-5-5-high", "xhigh": "claude-opus-5-5-xhigh", "max": "claude-opus-5-5-max",
		},
	}
	admission := NewModelAdmission(map[string][]ModelChoice{"cursor": {opus}}, nil, nil)
	if !admission.Allows("claude-opus-5-5", BackendCursor) {
		t.Fatal("the frozen choice is not allowed")
	}
	if admission.Allows("claude-opus-5-5-high", BackendCursor) {
		t.Fatal("a variant identifier is allowed")
	}
	low := ThinkingLow
	if got := admission.RunnerModel("claude-opus-5-5", &low, BackendCursor); got != "claude-opus-5-5-low" {
		t.Errorf("runner model = %s", got)
	}
	// A run saved before variants keeps passing its exact identifier.
	if got := admission.RunnerModel("claude-opus-5-5-high", &low, BackendCursor); got != "claude-opus-5-5-high" {
		t.Errorf("exact identifier = %s", got)
	}
	var decoded ModelChoice
	if err := json.Unmarshal([]byte(`{"id":"exact","name":"Exact"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !agentEqual(decoded, ModelChoice{ID: "exact", Name: "Exact"}) || decoded.Variants != nil {
		t.Fatalf("decoded = %+v", decoded)
	}
}

// TestThinkingStaysInsideTheSelectedContext checks the identifier and admission
// assertions for the selected context. The
// choices are written out as the Cursor list gives them.
func TestThinkingStaysInsideTheSelectedContext(t *testing.T) {
	t.Parallel()
	million := ModelChoice{
		ID: "claude-opus-5-5", Name: "Claude Opus 5.5 1M", ContextTokens: new(1_000_000),
		Variants: map[string]string{"low": "claude-opus-5-5-low", "high": "claude-opus-5-5-high"},
	}
	smaller := ModelChoice{
		ID: "claude-opus-5-5-250k", Name: "Claude Opus 5.5 250k", ContextTokens: new(250_000),
		Variants: map[string]string{
			"low": "claude-opus-5-5-250k-low", "high": "claude-opus-5-5-250k-high", "default": "claude-opus-5-5-250k",
		},
	}
	if million.DisplayLabel() != "Claude Opus 5.5 - 1M" || smaller.DisplayLabel() != "Claude Opus 5.5 - 250k" {
		t.Fatalf("labels = %q, %q", million.DisplayLabel(), smaller.DisplayLabel())
	}
	high, low, max := ThinkingHigh, ThinkingLow, ThinkingMax
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"million high", million.Identifier(&high), "claude-opus-5-5-high"},
		{"million max", million.Identifier(&max), "claude-opus-5-5-high"},
		{"million low", million.Identifier(&low), "claude-opus-5-5-low"},
		{"smaller high", smaller.Identifier(&high), "claude-opus-5-5-250k-high"},
		{"smaller low", smaller.Identifier(&low), "claude-opus-5-5-250k-low"},
		{"smaller max", smaller.Identifier(&max), "claude-opus-5-5-250k-high"},
		{"smaller none", smaller.Identifier(nil), "claude-opus-5-5-250k"},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("%s = %s, want %s", check.name, check.got, check.want)
		}
	}
	admission := NewModelAdmission(map[string][]ModelChoice{
		"cursor": {million, smaller},
		"claude": {{ID: "claude-opus-5-5[1m]", Name: "Opus 5.5", ContextTokens: new(1_000_000)}},
	}, nil, nil)
	if got := admission.RunnerModel("claude-opus-5-5", &max, BackendCursor); got != "claude-opus-5-5-high" {
		t.Errorf("admitted max = %s", got)
	}
	if got := admission.RunnerModel("claude-opus-5-5-250k", &low, BackendCursor); got != "claude-opus-5-5-250k-low" {
		t.Errorf("admitted smaller low = %s", got)
	}
	if got := admission.RunnerModel("claude-opus-5-5[1m]", &high, BackendClaude); got != "claude-opus-5-5[1m]" {
		t.Errorf("claude 1m = %s", got)
	}
}

// TestContextSizeLabelAndParse checks the decimal k and M labels and the reader's
// forms: suffixed sizes, a raw count when allowed, and the refusals.
func TestContextSizeLabelAndParse(t *testing.T) {
	t.Parallel()
	labels := map[int]string{
		999: "999", 1_000: "1k", 250_000: "250k", 1_000_000: "1M", 1_048_576: "1.048576M", 1_000_500: "1.0005M", 1_500: "1.5k", 1_005: "1.005k",
	}
	for tokens, want := range labels {
		if got := ContextSizeLabel(tokens); got != want {
			t.Errorf("label(%d) = %s, want %s", tokens, got, want)
		}
	}
	parse := map[string]int{"250k": 250_000, "100K": 100_000, "1M": 1_000_000, "1.048576M": 1_048_576, "+2k": 2_000, "1048576": 1_048_576}
	for text, want := range parse {
		got := ContextSizeParse(text, true)
		if got == nil || *got != want {
			t.Errorf("parse(%q) = %v, want %d", text, got, want)
		}
	}
	for _, text := range []string{"", "k", "1.k", "1.5.5k", "1.0000001M", "-2k", "0k", "1048576 tokens"} {
		if got := ContextSizeParse(text, true); got != nil {
			t.Errorf("parse(%q) = %d, want nil", text, *got)
		}
	}
	if got := ContextSizeParse("1048576", false); got != nil {
		t.Errorf("raw parse without raw = %d, want nil", *got)
	}
}

// TestSavedAgentAndStageIdentifiersStayStrings checks that saved agent and stage identifiers stay strings.
func TestSavedAgentAndStageIdentifiersStayStrings(t *testing.T) {
	t.Parallel()
	agent := mustBuiltInAgent(t, "triage")
	agent.Model = "claude-opus-5-5[1m]"
	parsed, err := AgentFileParse(AgentFileWrite(agent), agent.ID, agent.Version)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Model != "claude-opus-5-5[1m]" {
		t.Fatalf("model = %q", parsed.Model)
	}
}

// TestModelChoiceDecodesStrictKeys checks a model choice's required keys.
// TestModelCatalogSnapshotDecodesStrictKeys checks a catalog snapshot's required
// keys and that its runner must be a case.
