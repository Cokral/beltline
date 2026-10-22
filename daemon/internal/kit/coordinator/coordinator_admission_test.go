// Coordinator tests of model admission: the frozen admission of a run, the legacy repairs
// and the stage adjustments that check a model against it.

package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// coordCatalog is a catalog snapshot of backend listing models, refreshed at the contract time.
func coordCatalog(t *testing.T, backend core.Backend, models []core.ModelChoice) core.ModelCatalogSnapshot {
	t.Helper()
	snapshot, err := core.NewModelCatalogSnapshot(backend, backend.Executable(os.Getenv), testkit.ContractTime, models)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return snapshot
}

// coordPi, coordCodex, coordCursor are the backends the admission tests name.
var (
	coordPi     = core.BackendPi
	coordCodex  = core.BackendCodex
	coordCursor = core.BackendCursor
)

// TestAdmissionChecksLaterStagesBeforeCreatingAnyRun: a join that a later stage's model fails creates no run.
func TestAdmissionChecksLaterStagesBeforeCreatingAnyRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	record := c.Belts()[0]
	record.Belt.Stages[len(record.Belt.Stages)-1].Model = "outside"
	if _, err := coordJoin(c, record, harnessIssue(1, "Denied")); err == nil {
		t.Fatal("a stage with a model the run does not allow was admitted")
	}
	if runs := c.Runs(); len(runs) != 0 {
		t.Errorf("runs = %d, want none", len(runs))
	}
	if saved := h.storage.LoadRuns(); len(saved) != 0 {
		t.Errorf("saved runs = %d, want none", len(saved))
	}
}

// TestAdmittedModelSurvivesRevocationCatalogRemovalAndRestart: a run keeps the model it was admitted with.
func TestAdmittedModelSurvivesRevocationCatalogRemovalAndRestart(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run, err := coordJoin(c, c.Belts()[0], harnessIssue(1, "Admitted"))
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if run.ModelAdmission == nil {
		t.Fatal("the admitted run has no admission")
	}
	frozen := *run.ModelAdmission
	model := run.Stages[0].Model
	if err := c.SetModelAllowed(model, coordPi, false); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := c.SaveModelCatalog(coordCatalog(t, coordPi, nil)); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if !slices.ContainsFunc(c.AdjustmentModels(run, run.Stages[0], nil), func(choice core.ModelChoice) bool { return choice.ID == model }) {
		t.Error("the frozen model is not offered for adjustment")
	}
	if _, err := coordJoin(c, c.Belts()[0], harnessIssue(2, "Denied")); err == nil {
		t.Error("a new run was admitted with the revoked model")
	}
	sim := newH5m2AdmissionRunner(h)
	restarted := h.RestartWith(sim)
	coordStart(restarted, run.ID)
	specs := sim.Specs()
	if len(specs) == 0 || specs[0].Model != model {
		t.Fatalf("started with %+v, want model %s", specs, model)
	}
	if got := restarted.Run(run.ID); got == nil || got.ModelAdmission == nil || !coordSame(*got.ModelAdmission, frozen) {
		t.Errorf("admission after restart differs:\n%s", testkit.Diff(frozen, *got.ModelAdmission))
	}
}

// TestAdditionsDoNotExpandFrozenAdjustmentOrExecution: a model added after admission neither adjusts nor runs a run.
func TestAdditionsDoNotExpandFrozenAdjustmentOrExecution(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run, err := coordJoin(c, c.Belts()[0], harnessIssue(1, "Admitted"))
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	sim := newH5m2AdmissionRunner(h)
	restarted := h.RestartWith(sim)
	if _, err := restarted.AdjustStage(run.ID, run.StageID, new("outside"), nil, nil, nil); err == nil {
		t.Error("an adjustment to an unadmitted model was accepted")
	}
	coordUpdate(restarted, run.ID, func(r *core.Run) { r.Stages[0].Model = "outside" })
	coordStart(restarted, run.ID)
	if n := sim.Allocations(); n != 0 {
		t.Errorf("allocations = %d, want 0", n)
	}
	if n := len(sim.Specs()); n != 0 {
		t.Errorf("starts = %d, want 0", n)
	}
	got := restarted.Run(run.ID)
	if got == nil || got.State != core.RunStateHeld || len(got.Attempts) != 0 {
		t.Errorf("run = %+v, want held without attempts", got)
	}
}

// TestLegacyBlankStageGetsExactlyOneCurrentPolicyRepair: a blank legacy model is held once, and one adjustment repairs it.
func TestLegacyBlankStageGetsExactlyOneCurrentPolicyRepair(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	locked(c, func() {
		c.changeSettings(func(s *store.Settings) { s.AllowedModels = map[string]map[string]struct{}{} })
	})
	belt := c.Belts()[0].Belt
	belt.Stages[0].Model = ""
	run := coordAdd(c, core.NewRun(core.RunArgs{Belt: belt, Subject: harnessIssue(1, "Legacy"), Now: sessionSampleT0}))
	coordStart(c, run.ID)
	held := c.Run(run.ID)
	if held == nil || held.State != core.RunStateHeld || len(held.Attempts) != 0 {
		t.Fatalf("run = %+v, want held without attempts", held)
	}
	if held.ModelAdmission == nil || !watchesHas(held.ModelAdmission.LegacyRepairs, run.StageID) {
		t.Fatalf("admission = %+v, want the stage repaired", held.ModelAdmission)
	}
	if n := len(c.AdjustmentModels(*held, held.Stages[0], nil)); n != 0 {
		t.Errorf("adjustment models = %d, want none", n)
	}
	if err := c.SetModelAllowed("custom/model", coordPi, true); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("custom/model"), nil, new(""), nil); err != nil {
		t.Fatalf("adjust: %v", err)
	}
	repaired := c.Run(run.ID)
	if repaired == nil || repaired.ModelAdmission == nil || watchesHas(repaired.ModelAdmission.LegacyRepairs, run.StageID) {
		t.Fatalf("admission after repair = %+v", repaired)
	}
	if !repaired.ModelAdmission.Allows("custom/model", coordPi) {
		t.Error("the repaired model is not admitted")
	}
	if err := c.SetModelAllowed("another", coordPi, true); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("another"), nil, new(""), nil); err == nil {
		t.Error("a second adjustment after the repair was accepted for an unrelated model")
	}
	saved := h.storage.LoadRuns()
	if len(saved) == 0 || !coordSame(saved[0].ModelAdmission, repaired.ModelAdmission) {
		t.Errorf("saved admission differs from the repaired one")
	}
}

// TestMalformedPresentAdmissionNeverGetsLegacyCompatibility: a present but corrupt admission is refused, never repaired.
func TestMalformedPresentAdmissionNeverGetsLegacyCompatibility(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	raw, err := jsonx.Marshal(coordSampleRun(defaultBelt()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Insert a corrupt admission as the run's last member.
	text := strings.TrimSuffix(strings.TrimSpace(string(raw)), "}") + `,"modelAdmission":{"schemaVersion":"corrupt"}}`
	var run core.Run
	if err := json.Unmarshal([]byte(text), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	run.State = core.RunStateQueued
	if err := h.storage.SaveRun(run); err != nil {
		t.Fatalf("save: %v", err)
	}
	sim := newH5m2AdmissionRunner(h)
	c := h.RestartWith(sim)
	coordStart(c, run.ID)
	if n := sim.Allocations(); n != 0 {
		t.Errorf("allocations = %d, want 0", n)
	}
	got := c.Run(run.ID)
	if got == nil || got.State != core.RunStateHeld {
		t.Fatalf("run = %+v, want held", got)
	}
	if got.ModelAdmission == nil || len(got.ModelAdmission.Models) != 0 {
		t.Errorf("admission = %+v, want no models", got.ModelAdmission)
	}
}

// TestLegacyExplicitModelsAreGrandfatheredButNewRunsAreDenied: an old run keeps its explicit model; a new run with none is refused.
func TestLegacyExplicitModelsAreGrandfatheredButNewRunsAreDenied(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	locked(c, func() {
		c.changeSettings(func(s *store.Settings) { s.AllowedModels = map[string]map[string]struct{}{} })
	})
	belt := c.Belts()[0].Belt
	if _, err := coordJoin(c, c.Belts()[0], harnessIssue(2, "New denied")); err == nil {
		t.Error("a new run was admitted with no allowed models")
	}
	legacy := coordAdd(c, core.NewRun(core.RunArgs{Belt: belt, Subject: harnessIssue(1, "Legacy"), Now: sessionSampleT0}))
	sim := newH5m2AdmissionRunner(h)
	restarted := h.RestartWith(sim)
	coordStart(restarted, legacy.ID)
	specs := sim.Specs()
	if len(specs) == 0 || specs[0].Model != belt.Stages[0].Model {
		t.Fatalf("started with %+v, want %s", specs, belt.Stages[0].Model)
	}
	got := restarted.Run(legacy.ID)
	if got == nil || got.ModelAdmission == nil || !got.ModelAdmission.Allows(belt.Stages[0].Model, coordPi) {
		t.Errorf("admission = %+v, want the legacy model allowed", got)
	}
}

// TestFailedAdmissionWriteNeverPublishesEligibleRun: a run that cannot be saved is never admitted.
func TestFailedAdmissionWriteNeverPublishesEligibleRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	if err := os.WriteFile(c.Paths().Runs(), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := coordJoin(c, c.Belts()[0], harnessIssue(1, "Storage denied")); err == nil {
		t.Error("the join succeeded although the admission could not be saved")
	}
	if runs := c.Runs(); len(runs) != 0 {
		t.Errorf("runs = %d, want none", len(runs))
	}
}

// TestFrozenContextChoicesDoNotFollowCatalogOrPermissionChanges: a run's cursor and claude context choices stay as admitted.
func TestFrozenContextChoicesDoNotFollowCatalogOrPermissionChanges(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	small := core.ModelChoice{
		ID: "claude-opus-5-5-250k", Name: "Opus 5.5",
		Variants: map[string]string{
			"low": "claude-opus-5-5-250k-low", "high": "claude-opus-5-5-250k-high", "default": "claude-opus-5-5-250k", "medium": "claude-opus-5-5-250k",
		},
		ContextTokens: new(250_000),
	}
	large := core.ModelChoice{
		ID: "claude-opus-5-5", Name: "Opus 5.5",
		Variants: map[string]string{
			"low": "claude-opus-5-5-low", "high": "claude-opus-5-5-high", "default": "claude-opus-5-5-medium", "medium": "claude-opus-5-5-medium",
		},
		ContextTokens: new(1_000_000),
	}
	claude := core.ModelChoice{ID: "claude-opus-5-5[1m]", Name: "Opus 5.5", ContextTokens: new(1_000_000)}
	if err := c.SaveModelCatalog(coordCatalog(t, coordCursor, []core.ModelChoice{small, large})); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := c.SaveModelCatalog(coordCatalog(t, coordCodex, nil)); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := c.SaveModelCatalog(coordCatalog(t, core.BackendClaude, []core.ModelChoice{claude, {ID: "claude-opus-5-5", Name: "Opus 5.5"}})); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	for _, allow := range []struct {
		id      string
		backend core.Backend
	}{{small.ID, coordCursor}, {large.ID, coordCursor}, {claude.ID, core.BackendClaude}} {
		if err := c.SetModelAllowed(allow.id, allow.backend, true); err != nil {
			t.Fatalf("allow %s: %v", allow.id, err)
		}
	}
	record := c.Belts()[0]
	record.Belt.Stages[0].Runner = &coordCursor
	record.Belt.Stages[0].Model = small.ID
	record.Belt.Stages[0].Thinking = new(core.ThinkingHigh)
	run, err := coordJoin(c, record, harnessIssue(1, "Context"))
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	frozen := findChoice(run.ModelAdmission.Choices(coordCursor), small.ID)
	if frozen == nil || frozen.ContextTokens == nil || *frozen.ContextTokens != 250_000 || frozen.DisplayLabel() != "Opus 5.5 - 250k" {
		t.Fatalf("frozen choice = %+v, want 250k with label Opus 5.5 - 250k", frozen)
	}
	if err := c.SetModelAllowed(small.ID, coordCursor, false); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := c.SaveModelCatalog(coordCatalog(t, coordCursor, []core.ModelChoice{{
		ID: large.ID, Name: "Opus 5.5", Variants: large.Variants, ContextTokens: new(250_000),
	}})); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	stored := h.storage.LoadRuns()
	if len(stored) == 0 {
		t.Fatal("the run was not saved")
	}
	if !coordSame(stored[0].ModelAdmission, run.ModelAdmission) {
		t.Errorf("stored admission differs from the admitted one:\n%s", testkit.Diff(*run.ModelAdmission, *stored[0].ModelAdmission))
	}
	storedSmall := findChoice(stored[0].ModelAdmission.Choices(coordCursor), small.ID)
	if storedSmall == nil || storedSmall.ContextTokens == nil || *storedSmall.ContextTokens != 250_000 {
		t.Errorf("stored small choice = %+v, want 250k", storedSmall)
	}
	adjust := c.AdjustmentModels(stored[0], stored[0].Stages[0], nil)
	if s := findChoice(adjust, small.ID); s == nil || s.ContextTokens == nil || *s.ContextTokens != 250_000 {
		t.Errorf("adjustment small choice = %+v, want 250k", s)
	}
	if l := findChoice(adjust, large.ID); l == nil || l.ContextTokens == nil || *l.ContextTokens != 1_000_000 {
		t.Errorf("adjustment large choice = %+v, want 1M", l)
	}
	sim := newH5m2AdmissionRunner(h)
	restarted := h.RestartWith(sim)
	coordStart(restarted, run.ID)
	specs := sim.Specs()
	if len(specs) == 0 || specs[0].Model != "claude-opus-5-5-250k-high" || specs[0].Thinking == nil || *specs[0].Thinking != core.ThinkingHigh {
		t.Fatalf("started with %+v, want claude-opus-5-5-250k-high at high", specs)
	}
	started := restarted.Run(run.ID)
	last := started.Attempts[len(started.Attempts)-1]
	if last.Settings == nil || last.Settings.Model != small.ID {
		t.Errorf("attempt settings = %+v, want %s", last.Settings, small.ID)
	}
	if got := restarted.Spec(*started, last, ""); got == nil || got.Model != "claude-opus-5-5-250k-high" {
		t.Errorf("rebuilt spec = %+v, want claude-opus-5-5-250k-high", got)
	}
	claudeBelt := record
	claudeRunner := core.BackendClaude
	claudeBelt.Belt.Stages[0].Runner = &claudeRunner
	claudeBelt.Belt.Stages[0].Model = claude.ID
	claudeBelt.Belt.Stages[0].Thinking = new(core.ThinkingMax)
	claudeRun, err := coordJoin(c, claudeBelt, harnessIssue(2, "Claude context"))
	if err != nil {
		t.Fatalf("join claude: %v", err)
	}
	claudeSim := newH5m2AdmissionRunner(h)
	claudeCoordinator := h.RestartWith(claudeSim)
	coordStart(claudeCoordinator, claudeRun.ID)
	if specs := claudeSim.Specs(); len(specs) == 0 || specs[0].Model != "claude-opus-5-5[1m]" {
		t.Fatalf("claude started with %+v, want claude-opus-5-5[1m]", specs)
	}
	if err := c.SaveModelCatalog(coordCatalog(t, core.BackendClaude, nil)); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if err := c.SetModelAllowed(claude.ID, core.BackendClaude, false); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	var savedClaude *core.Run
	for _, saved := range h.storage.LoadRuns() {
		if saved.ID == claudeRun.ID {
			savedClaude = &saved
		}
	}
	if savedClaude == nil || savedClaude.ModelAdmission == nil || !savedClaude.ModelAdmission.Allows(claude.ID, core.BackendClaude) {
		t.Errorf("claude run admission lost %s", claude.ID)
	}
	if !slices.ContainsFunc(c.AdjustmentModels(claudeRun, claudeRun.Stages[0], nil), func(choice core.ModelChoice) bool { return choice.ID == claude.ID }) {
		t.Errorf("claude adjustment models lost %s", claude.ID)
	}
	if got := findChoice(c.AdjustmentModels(claudeRun, claudeRun.Stages[0], nil), claude.ID); got == nil || got.ContextTokens == nil || *got.ContextTokens != 1_000_000 {
		t.Errorf("claude adjustment choice = %+v, want 1M", got)
	}
}

// findChoice is the choice with id in choices, or nil.
func findChoice(choices []core.ModelChoice, id string) *core.ModelChoice {
	for index := range choices {
		if choices[index].ID == id {
			return &choices[index]
		}
	}
	return nil
}

// TestAdjustmentValidatesTheSelectedBackendAndIgnoresLaterSettings: an adjustment is checked against the backend it names.
func TestAdjustmentValidatesTheSelectedBackendAndIgnoresLaterSettings(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	locked(c, func() {
		c.changeSettings(func(s *store.Settings) {
			s.AllowedModels["pi"] = map[string]struct{}{"openrouter/stealth/space-bunny-alpha": {}, "custom/model": {}}
			s.AllowedModels["codex"] = map[string]struct{}{"another": {}}
			s.AllowedModels["cursor"] = map[string]struct{}{}
		})
	})
	run, err := coordJoin(c, c.Belts()[0], harnessIssue(4, "Cross backend"))
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	before := c.Run(run.ID)
	if before == nil {
		t.Fatal("the run is gone")
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("custom/model"), nil, nil, &coordCodex); err == nil {
		t.Error("a model not allowed on codex was saved")
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("  "), nil, nil, &coordPi); err == nil {
		t.Error("a blank model was saved")
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("missing"), nil, nil, &coordPi); err == nil {
		t.Error("a missing model was saved")
	}
	if _, err := c.AdjustStage(run.ID, run.StageID, new("another"), nil, nil, &coordCursor); err == nil {
		t.Error("an empty allowance was saved")
	} else if err.Error() != core.StageAdjustNoModelsAllowed {
		t.Errorf("error = %q, want %q", err.Error(), core.StageAdjustNoModelsAllowed)
	}
	if got := c.Run(run.ID); !coordSame(got, before) {
		t.Errorf("the run changed after the refused adjustments")
	}
	if err := c.SetModelAllowed("custom/model", coordCursor, true); err != nil {
		t.Fatalf("allow: %v", err)
	}
	current := c.Run(run.ID)
	if n := len(c.AdjustmentModels(*current, current.Stages[0], &coordCursor)); n != 0 {
		t.Errorf("cursor adjustment models = %d, want none", n)
	}
	if got := c.Run(run.ID); !coordSame(got.Adjustments, before.Adjustments) || !coordSame(got.Stages, before.Stages) {
		t.Errorf("adjustments or stages changed by the allowance")
	}
	saved, err := c.AdjustStage(run.ID, run.StageID, new("another"), new(core.ThinkingMax), new(""), &coordCodex)
	if err != nil || saved == nil {
		t.Fatalf("adjust to codex: %v", err)
	}
	if !slices.Contains(saved.Fields, core.AdjustmentFieldThinking) {
		t.Errorf("fields = %+v, want thinking among them", saved.Fields)
	}
	stage := findStage(*c.Run(run.ID), run.StageID)
	if stage == nil || stage.Runner == nil || *stage.Runner != core.BackendCodex {
		t.Errorf("stage runner = %+v, want codex", stage)
	}
	if stage == nil || stage.Thinking == nil || *stage.Thinking != core.ThinkingMax {
		t.Errorf("stage thinking = %+v, want max", stage)
	}
}

// TestMissingRunnerIsRecoveredBeforeAndAfterMigration: an attempt saved without a runner keeps the backend its stage had.
func TestMissingRunnerIsRecoveredBeforeAndAfterMigration(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	earlyBelt := defaultBelt()
	earlyBelt.ID = "legacy-before"
	early := coordSampleRun(earlyBelt)
	coordFinish(&early, new("needs info"))
	model := early.Stages[0].Model
	original := core.StageAdjustBackend(early.Stages[0])
	early.Attempts[0].Settings = &core.SessionSettings{Model: model, Thinking: new(core.ThinkingLow)}
	if err := h.storage.SaveRun(early); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := findSaved(h.storage.LoadRuns(), early.ID)
	if loaded == nil || loaded.ModelAdmission != nil || loaded.Attempts[0].Settings == nil || loaded.Attempts[0].Settings.Runner != nil {
		t.Fatalf("loaded run = %+v, want no admission and no attempt runner", loaded)
	}
	first := h.RestartWith(h.services.Runner)
	if _, err := first.AdjustStage(early.ID, early.StageID, new("another"), nil, new(""), &coordCodex); err != nil {
		t.Fatalf("adjust: %v", err)
	}
	adjusted := first.Run(early.ID)
	if adjusted == nil {
		t.Fatal("the run is gone")
	}
	if !coordSame(adjusted.Attempts[0].Settings, early.Attempts[0].Settings) {
		t.Errorf("attempt settings changed by the adjustment")
	}
	if fallback, ok := adjusted.ModelAdmission.LegacyAttemptSettings[early.Attempts[0].ID]; !ok || fallback.Runner == nil || *fallback.Runner != original {
		t.Errorf("legacy fallback = %+v, want runner %s", fallback, original)
	}
	rebuilt := first.Spec(*adjusted, adjusted.Attempts[0], "")
	if rebuilt == nil || rebuilt.Backend != original || rebuilt.Model != model || rebuilt.Thinking == nil || *rebuilt.Thinking != core.ThinkingLow {
		t.Errorf("rebuilt spec = %+v, want %s, %s, low", rebuilt, original, model)
	}
	restarted := h.RestartWith(h.services.Runner)
	again := restarted.Run(early.ID)
	if again == nil {
		t.Fatal("the run is gone after the restart")
	}
	if got := restarted.Spec(*again, again.Attempts[0], ""); got == nil || got.Backend != original {
		t.Errorf("spec after restart = %+v, want backend %s", got, original)
	}
	if _, err := restarted.ContinueSession(context.Background(), early.ID, again.Attempts[0]); err == nil {
		t.Error("the continuation launched without a checkout")
	} else if strings.Contains(err.Error(), "cannot safely be recovered") || strings.Contains(err.Error(), "no saved explicit model") {
		t.Errorf("continuation refused for a recovery reason: %v", err)
	}

	lateBelt := defaultBelt()
	lateBelt.ID = "legacy-after"
	late := coordSampleRun(lateBelt)
	coordFinish(&late, new("needs info"))
	late.Attempts[0].Settings = &core.SessionSettings{Model: model, Thinking: new(core.ThinkingHigh)}
	admission := core.NewModelAdmission(map[string][]core.ModelChoice{
		"pi":    {{ID: model, Name: model}},
		"codex": {{ID: "another", Name: "another"}},
	}, nil, nil)
	late.ModelAdmission = &admission
	if err := h.storage.SaveRun(late); err != nil {
		t.Fatalf("save: %v", err)
	}
	second := h.RestartWith(h.services.Runner)
	if _, err := second.AdjustStage(late.ID, late.StageID, new("another"), nil, new(""), &coordCodex); err != nil {
		t.Fatalf("adjust late: %v", err)
	}
	migrated := second.Run(late.ID)
	if migrated == nil {
		t.Fatal("the late run is gone")
	}
	if migrated.Attempts[0].Settings == nil || migrated.Attempts[0].Settings.Runner != nil {
		t.Errorf("late attempt runner = %+v, want none", migrated.Attempts[0].Settings)
	}
	if fallback, ok := migrated.ModelAdmission.LegacyAttemptSettings[late.Attempts[0].ID]; !ok || fallback.Runner == nil || *fallback.Runner != original {
		t.Errorf("late legacy fallback = %+v, want runner %s", fallback, original)
	}
	if got := second.Spec(*migrated, migrated.Attempts[0], ""); got == nil || got.Backend != original || got.Model != model {
		t.Errorf("late spec = %+v, want %s on %s", got, original, model)
	}
	secondRestart := h.RestartWith(h.services.Runner)
	reloaded := secondRestart.Run(late.ID)
	if reloaded == nil {
		t.Fatal("the late run is gone after the restart")
	}
	if got := secondRestart.Spec(*reloaded, late.Attempts[0], ""); got == nil || got.Backend != original {
		t.Errorf("late spec after restart = %+v, want backend %s", got, original)
	}
}

// findSaved is the saved run with id, or nil.
func findSaved(runs []core.Run, id string) *core.Run {
	for index := range runs {
		if runs[index].ID == id {
			return &runs[index]
		}
	}
	return nil
}
