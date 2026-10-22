// Tests of the catalog transports. The ones that run real CLIs are behind the integration tag
// (runner_catalog_integration_test.go); the coordinator and harness tests are not in this package.

package runner

import (
	"context"
	"slices"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// jsonRows decodes a JSON array the way the transports do, so numbers are float64.
func jsonRows(t testing.TB, text string) []any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("rows: %v", err)
	}
	rows, ok := value.([]any)
	if !ok {
		t.Fatalf("rows are not an array: %s", text)
	}
	return rows
}

func TestDiscoveryUsesConfiguredExecutableWithoutPromptOrWorkspace(t *testing.T) {
	t.Parallel()
	recorder := &shell.Recorder{Reply: func(shell.Call) shell.Result {
		return shell.Result{Stdout: "provider model context max-out thinking images\nvendor model 100K 10K yes no"}
	}}
	models, err := NewCLIModelCatalog(recorder, nil).Discover(context.Background(), core.BackendPi, "/custom/pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "vendor/model" {
		t.Fatalf("models = %+v", models)
	}
	calls := recorder.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	mustEqual(t, "/custom/pi", calls[0].Executable)
	mustEqual(t, []string{"--list-models"}, calls[0].Args)
	mustEqual(t, "", calls[0].Dir)
	if calls[0].Input != nil {
		t.Errorf("input = %q, want nil", *calls[0].Input)
	}
}

func TestStructuredCatalogRowsKeepExactIdentifiers(t *testing.T) {
	t.Parallel()
	for _, keys := range [][2]string{{"id", "name"}, {"model", "displayName"}, {"value", "displayName"}} {
		idKey, nameKey := keys[0], keys[1]
		rows := []any{map[string]any{idKey: "exact-id", nameKey: "Label"}}
		models, err := serverCatalogDecodeModels(rows, idKey, nameKey)
		if err != nil {
			t.Fatal(err)
		}
		mustEqual(t, []core.ModelChoice{{ID: "exact-id", Name: "Label"}}, models)
		if _, err := serverCatalogDecodeModels([]any{map[string]any{idKey: "exact-id"}}, idKey, nameKey); err == nil {
			t.Errorf("row without %s decoded", nameKey)
		}
	}
	models, err := CLIModelCatalogParseCursor("Available models\nexact-id - Display (current)\nTip: use --model <id>")
	if err != nil || len(models) != 1 || models[0].ID != "exact-id" {
		t.Fatalf("cursor = %+v, %v", models, err)
	}
}

func TestPiIdentifiersKeepProviderCollisionsDistinct(t *testing.T) {
	t.Parallel()
	models, err := CLIModelCatalogParsePi(`provider model context max-out thinking images
vendor-a same 100K 10K yes no
vendor-b same 100K 10K no yes
vendor-a only 100K 10K no yes
`)
	if err != nil {
		t.Fatal(err)
	}
	var ids, names []string
	for _, m := range models {
		ids = append(ids, m.ID)
		names = append(names, m.Name)
	}
	mustEqual(t, []string{"vendor-a/same", "vendor-b/same", "vendor-a/only"}, ids)
	mustEqual(t, []string{"vendor-a/same", "vendor-b/same", "only"}, names)
}

func TestMalformedAndPartialPiResultsAreRefused(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"",
		"model names",
		"provider model context max-out thinking images\nvendor model",
		"provider model context max-out thinking images\nvendor model 10K 1K maybe no",
	} {
		if _, err := CLIModelCatalogParsePi(text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestCompleteEmptyCatalogIsSuccessful(t *testing.T) {
	t.Parallel()
	models, err := CLIModelCatalogParsePi("provider model context max-out thinking images")
	if err != nil || len(models) != 0 {
		t.Fatalf("models = %+v, %v", models, err)
	}
}

func TestPiContextLabelsTheExistingIdAndIgnoresMaxOut(t *testing.T) {
	t.Parallel()
	models, err := CLIModelCatalogParsePi(`provider model context max-out thinking images
vendor opus 250K 1M yes no
vendor plain 100000 32000 no yes
vendor big 1048576 10K yes no
vendor round 1000000 1K no no
other opus 250k 1M yes yes
`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var sizes []int
	var labels []string
	for _, m := range models {
		ids = append(ids, m.ID)
		if m.ContextTokens != nil {
			sizes = append(sizes, *m.ContextTokens)
		}
		labels = append(labels, m.DisplayLabel())
		if m.Variants != nil {
			t.Errorf("%s has variants", m.ID)
		}
	}
	mustEqual(t, []string{"vendor/opus", "vendor/plain", "vendor/big", "vendor/round", "other/opus"}, ids)
	mustEqual(t, []int{250_000, 100_000, 1_048_576, 1_000_000, 250_000}, sizes)
	mustEqual(t, []string{"vendor/opus - 250k", "plain - 100k", "big - 1.048576M", "round - 1M", "other/opus - 250k"}, labels)
	if got := core.ContextSizeLabel(250_000); got != "250k" {
		t.Errorf("label = %q", got)
	}
	if got := core.ContextSizeLabel(1_000_000); got != "1M" {
		t.Errorf("label = %q", got)
	}
	if got := core.ContextSizeLabel(1_048_576); got != "1.048576M" {
		t.Errorf("label = %q", got)
	}
	if got := core.ContextSizeParse("1.048576M", true); got == nil || *got != 1_048_576 {
		t.Errorf("parse = %v", got)
	}
	if got := core.ContextSizeParse("nope", true); got != nil {
		t.Errorf("parse(nope) = %v", *got)
	}
}

func TestReportedAndUnspecifiedSizesStayDistinctAcrossDecoders(t *testing.T) {
	t.Parallel()
	copilot, err := serverCatalogDecodeModels(jsonRows(t, `[
		{"id":"copilot-large","name":"Opus 5.5","capabilities":{"limits":{"max_context_window_tokens":1000000,"max_output_tokens":32000}}},
		{"id":"copilot-small","name":"Opus 5.5","capabilities":{"limits":{"max_context_window_tokens":250000,"max_output_tokens":1000000}}},
		{"id":"copilot-plain","name":"Composer","capabilities":{"limits":{"max_output_tokens":1000000}}},
		{"id":"copilot-unknown","name":"Unknown","capabilities":{"limits":{"max_context_window_tokens":null}}}
	]`), "id", "name")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range copilot {
		ids = append(ids, m.ID)
	}
	mustEqual(t, []string{"copilot-large", "copilot-small", "copilot-plain", "copilot-unknown"}, ids)
	sizes := []*int{copilot[0].ContextTokens, copilot[1].ContextTokens, copilot[2].ContextTokens, copilot[3].ContextTokens}
	if sizes[0] == nil || *sizes[0] != 1_000_000 || sizes[1] == nil || *sizes[1] != 250_000 || sizes[2] != nil || sizes[3] != nil {
		t.Errorf("sizes = %v", sizes)
	}
	if copilot[0].DisplayLabel() != "Opus 5.5 - 1M" || copilot[1].DisplayLabel() != "Opus 5.5 - 250k" || copilot[2].DisplayLabel() != "Composer" {
		t.Errorf("labels = %q %q %q", copilot[0].DisplayLabel(), copilot[1].DisplayLabel(), copilot[2].DisplayLabel())
	}
	codex, err := serverCatalogDecodeModels(jsonRows(t, `[
		{"model":"codex-exact","displayName":"Codex","context_window":272000,"max_context_window":272000},
		{"model":"codex-second","displayName":"Second"}
	]`), "model", "displayName")
	if err != nil {
		t.Fatal(err)
	}
	if codex[0].ContextTokens != nil || codex[1].ContextTokens != nil {
		t.Errorf("codex sizes = %v %v", codex[0].ContextTokens, codex[1].ContextTokens)
	}
	if codex[0].DisplayLabel() != "Codex" || codex[1].DisplayLabel() != "Second" {
		t.Errorf("codex labels = %q %q", codex[0].DisplayLabel(), codex[1].DisplayLabel())
	}
	for _, text := range []string{
		`[{"id":"bad","name":"Bad","capabilities":{"limits":{"max_context_window_tokens":"lots"}}}]`,
		`[{"id":"bad","name":"Bad","capabilities":{"limits":{"max_context_window_tokens":1.5}}}]`,
	} {
		if _, err := serverCatalogDecodeModels(jsonRows(t, text), "id", "name"); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestCopilotLongContextTierIsASecondChoice(t *testing.T) {
	t.Parallel()
	models, err := serverCatalogDecodeCopilot(jsonRows(t, `[
		{"id":"claude-opus-4.6","name":"Claude Opus 4.6","capabilities":{"limits":{"max_context_window_tokens":1064000,"max_output_tokens":64000}},
		 "billing":{"tokenPrices":{"maxPromptTokens":136000,"longContext":{"maxPromptTokens":1000000}}}},
		{"id":"gpt-5","name":"GPT-5","capabilities":{"limits":{"max_context_window_tokens":400000}}},
		{"id":"hosted","name":"Hosted","supportedContextTiers":["default","long_context"]},
		{"id":"legacy","name":"Legacy","capabilities":{"limits":{"max_output_tokens":8000}},
		 "billing":{"tokenPrices":{"contextMax":92000,"longContext":{"contextMax":92000}}}}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	var ids, labels []string
	var sizes []*int
	for _, m := range models {
		ids = append(ids, m.ID)
		labels = append(labels, m.DisplayLabel())
		sizes = append(sizes, m.ContextTokens)
	}
	mustEqual(t, []string{"claude-opus-4.6", "claude-opus-4.6[long_context]", "gpt-5", "hosted", "hosted[long_context]", "legacy"}, ids)
	want := []int{200_000, 1_064_000, 400_000}
	if *sizes[0] != want[0] || *sizes[1] != want[1] || *sizes[2] != want[2] || sizes[3] != nil || sizes[4] != nil || *sizes[5] != 100_000 {
		t.Errorf("sizes = %v", sizes)
	}
	mustEqual(t, []string{"Claude Opus 4.6 - 200k", "Claude Opus 4.6 - 1.064M", "GPT-5 - 400k", "Hosted", "Hosted (long context)", "Legacy - 100k"}, labels)
	if _, err := serverCatalogDecodeCopilot(jsonRows(t, `[{"id":"bad","name":"Bad","billing":{"tokenPrices":{"longContext":{"maxPromptTokens":"lots"}}}}]`)); err == nil {
		t.Errorf("a text prompt budget was accepted")
	}
}

func TestCopilotLongContextIdStartsTheLongTier(t *testing.T) {
	t.Parallel()
	mustEqual(t, []string{"--model", "claude-opus-4.6", "--context", "long_context"}, CopilotModelArguments("claude-opus-4.6[long_context]"))
	mustEqual(t, []string{"--model", "gpt-5"}, CopilotModelArguments(" gpt-5 "))
	if got := CopilotModelArguments(""); len(got) != 0 {
		t.Errorf("blank model arguments = %v", got)
	}
	adapter := CopilotAdapter{StateRoot: t.TempDir()}
	arguments := adapter.Arguments("id", "bl-x", "claude-opus-4.6[long_context]", nil)
	if !slices.Contains(arguments, "--context") || !slices.Contains(arguments, "claude-opus-4.6") || slices.Contains(arguments, "claude-opus-4.6[long_context]") {
		t.Errorf("arguments = %v", arguments)
	}
	conversation := adapter.ConversationArguments("id", "bl-x", "claude-opus-4.6[long_context]", nil)
	mustEqual(t, []string{"--model", "claude-opus-4.6", "--context", "long_context"}, conversation[len(conversation)-4:])
}

func TestUnknownPiContextIsAnUnspecifiedChoice(t *testing.T) {
	t.Parallel()
	models, err := CLIModelCatalogParsePi(`provider model context max-out thinking images
vendor mystery unknown 10K yes no
vendor blank nope 10K yes no
vendor dash - 10K no no
vendor sized 250K 10K yes no
`)
	if err != nil {
		t.Fatal(err)
	}
	var ids, labels []string
	for _, m := range models {
		ids = append(ids, m.ID)
		labels = append(labels, m.DisplayLabel())
	}
	mustEqual(t, []string{"vendor/mystery", "vendor/blank", "vendor/dash", "vendor/sized"}, ids)
	if models[0].ContextTokens != nil || models[1].ContextTokens != nil || models[2].ContextTokens != nil || *models[3].ContextTokens != 250_000 {
		t.Errorf("contexts = %v %v %v %v", models[0].ContextTokens, models[1].ContextTokens, models[2].ContextTokens, models[3].ContextTokens)
	}
	mustEqual(t, []string{"mystery", "blank", "dash", "sized - 250k"}, labels)
}

func TestInvalidPiCapacityAndContradictoryIdsAreRefused(t *testing.T) {
	t.Parallel()
	for _, cell := range []string{"0", "0K", "1.2345K"} {
		if _, err := CLIModelCatalogParsePi("provider model context max-out thinking images\nvendor model " + cell + " 10K yes no"); err == nil {
			t.Errorf("accepted capacity %q", cell)
		}
	}
	conflict, err := CLIModelCatalogParsePi(`provider model context max-out thinking images
vendor opus 250K 10K yes no
vendor opus 1M 10K yes no
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflict) != 2 || conflict[0].ID != "vendor/opus" || conflict[1].ID != "vendor/opus" {
		t.Errorf("conflict = %+v", conflict)
	}
}
