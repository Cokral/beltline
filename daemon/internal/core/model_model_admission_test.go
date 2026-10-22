package core

import (
	"slices"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestModelAdmissionJSONRoundTrips checks that an admission reads back to itself
// and writes legacy repairs as a sorted array, with the attempt fallbacks only when present.
func TestModelAdmissionJSONRoundTrips(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(
		map[string][]ModelChoice{"pi": {{ID: "vendor/model", Name: "Model"}}},
		map[string]struct{}{"stage-b": {}, "stage-a": {}},
		map[string]SessionSettings{"attempt-1": {Model: "old/model"}},
	)
	data, err := jsonx.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"legacyAttemptSettings":{"attempt-1":{"model":"old/model"}},"legacyRepairs":["stage-a","stage-b"],` +
		`"models":{"pi":[{"id":"vendor/model","name":"Model"}]},"schemaVersion":1}`
	if !sameJSON(t, data, want) {
		t.Fatalf("admission JSON = %s\nwant %s", data, want)
	}
	var back ModelAdmission
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !agentEqual(back, admission) {
		t.Fatalf("round trip changed the admission:\n%s", testkit.Diff(admission, back))
	}
	plain, err := jsonx.Marshal(NewModelAdmission(map[string][]ModelChoice{}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "legacyAttemptSettings") {
		t.Fatalf("an admission without fallbacks writes them: %s", plain)
	}
}

// TestModelAdmissionValidityAndChoices checks version 1, known runners and
// non-blank ids, and that an invalid admission admits nothing.
func TestModelAdmissionValidityAndChoices(t *testing.T) {
	t.Parallel()
	valid := NewModelAdmission(map[string][]ModelChoice{"claude": {{ID: "m", Name: "M"}}}, nil, nil)
	if !valid.Valid() || !valid.Allows("m", BackendClaude) || valid.Allows("m", BackendPi) {
		t.Fatal("a valid admission admits the wrong choices")
	}
	if got := valid.Choices(BackendClaude); len(got) != 1 || got[0].ID != "m" {
		t.Fatalf("choices = %+v", got)
	}
	unknown := NewModelAdmission(map[string][]ModelChoice{"nope": {{ID: "m"}}}, nil, nil)
	blank := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "  \n"}}}, nil, nil)
	version := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "m"}}}, nil, nil)
	version.SchemaVersion = 2
	for name, admission := range map[string]ModelAdmission{"unknown runner": unknown, "blank id": blank, "version": version} {
		if admission.Valid() {
			t.Errorf("%s is valid", name)
		}
		if got := admission.Choices(BackendPi); len(got) != 0 {
			t.Errorf("%s admits %+v", name, got)
		}
		if admission.Allows("m", BackendPi) {
			t.Errorf("%s allows m", name)
		}
	}
}

// TestModelAdmissionRunnerModelFallsBackToTheModel checks that a model that is not admitted is passed as it is.
func TestModelAdmissionRunnerModelFallsBackToTheModel(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "m", Name: "M"}}}, nil, nil)
	if got := admission.RunnerModel("m", nil, BackendPi); got != "m" {
		t.Fatalf("admitted model = %s", got)
	}
	if got := admission.RunnerModel("other", nil, BackendPi); got != "other" {
		t.Fatalf("not admitted model = %s", got)
	}
}

// TestModelAdmissionRepairsOnce checks that a legacy blank stage can extend the
// snapshot once, and that only its own runner gains the choice.
func TestModelAdmissionRepairsOnce(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "old", Name: "Old"}}}, map[string]struct{}{"stage": {}}, nil)
	choice := ModelChoice{ID: "custom/model", Name: "Custom"}
	if !admission.Repair("stage", BackendPi, choice) {
		t.Fatal("the marked stage was not repaired")
	}
	if admission.Repair("stage", BackendPi, choice) {
		t.Fatal("a stage was repaired twice")
	}
	if _, ok := admission.LegacyRepairs["stage"]; ok {
		t.Fatal("the repair mark is still set")
	}
	if !admission.Allows("custom/model", BackendPi) || !admission.Allows("old", BackendPi) {
		t.Fatal("the repair did not extend the runner's choices")
	}
	if admission.Repair("unmarked", BackendPi, ModelChoice{ID: "other"}) {
		t.Fatal("an unmarked stage was repaired")
	}
}

// TestModelAdmissionRecordsFallbacksWithoutOverwriting checks that an id already
// recorded keeps its first fallback.
func TestModelAdmissionRecordsFallbacksWithoutOverwriting(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{}, nil, nil)
	admission.RecordLegacyAttemptSettings(map[string]SessionSettings{"a": {Model: "first"}})
	admission.RecordLegacyAttemptSettings(map[string]SessionSettings{"a": {Model: "second"}, "b": {Model: "b-model"}})
	if got := admission.LegacyAttemptSettings["a"].Model; got != "first" {
		t.Fatalf("a = %s, want first", got)
	}
	if got := admission.LegacyAttemptSettings["b"].Model; got != "b-model" {
		t.Fatalf("b = %s", got)
	}
	empty := NewModelAdmission(map[string][]ModelChoice{}, nil, nil)
	empty.RecordLegacyAttemptSettings(map[string]SessionSettings{})
	if empty.LegacyAttemptSettings != nil {
		t.Fatal("an empty record changed the admission")
	}
}

// TestModelPolicyWarningsNameTheProblem checks the warning for a model that is not admitted.
func TestModelPolicyWarningsNameTheProblem(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "allowed", Name: "Allowed"}}}, nil, nil)
	const message = "Build: %s is not allowed for Pi. Choose an allowed model or configure Settings → Allowed models."
	if warning := ModelPolicyWarning("allowed", BackendPi, "Build", admission, nil); warning != nil {
		t.Fatalf("an allowed model warns: %s", *warning)
	}
	if warning := ModelPolicyWarning("denied", BackendPi, "Build", admission, nil); warning == nil || *warning != strings.Replace(message, "%s", "denied", 1) {
		t.Fatalf("warning = %v", warning)
	}
	label := "Pretty Name"
	if warning := ModelPolicyWarning("denied", BackendPi, "Build", admission, &label); warning == nil || *warning != strings.Replace(message, "%s", label, 1) {
		t.Fatalf("labelled warning = %v", warning)
	}
	if warning := ModelPolicyWarning("  ", BackendPi, "Build", admission, nil); warning == nil || !strings.Contains(*warning, "The runner's default") {
		t.Fatalf("blank model warning = %v", warning)
	}
}

// TestModelPolicyWarningsForPoolKeepsEachSetting checks one warning for each
// denied setting of a pool, in order, with the catalog's label where given.
func TestModelPolicyWarningsForPoolKeepsEachSetting(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{"pi": {{ID: "ok", Name: "OK"}}}, nil, nil)
	claude := BackendClaude
	pool := []SessionSettings{
		{Model: "ok"},
		{Model: "first-denied"},
		{Runner: &claude, Model: "second-denied"},
	}
	label := func(model string, runner Backend) *string {
		if model == "second-denied" {
			shown := "Second"
			return &shown
		}
		return nil
	}
	got := ModelPolicyWarningsForPool(pool, "Verify", admission, label)
	want := []string{
		"Verify: first-denied is not allowed for Pi. Choose an allowed model or configure Settings → Allowed models.",
		"Verify: Second is not allowed for Claude Code. Choose an allowed model or configure Settings → Allowed models.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("warnings = %q, want %q", got, want)
	}
	if none := ModelPolicyWarningsForPool(nil, "Verify", admission, nil); len(none) != 0 {
		t.Fatalf("an empty pool warns: %v", none)
	}
}

// TestModelAdmissionDecodesStrictKeys checks that schemaVersion, models and
// legacyRepairs are required and not null, and that a corrupt schema version fails.
