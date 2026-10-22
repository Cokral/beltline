package core

import (
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestShadowPickArmBalancesTheTestsRuns checks a new run goes to the version
// fewer of this test's runs took, counting no other test's runs, and that a tie
// draws both versions.
func TestShadowPickArmBalancesTheTestsRuns(t *testing.T) {
	t.Parallel()
	mark := func(test string, arm ShadowArm) Run { return Run{Shadow: &RunShadow{Test: test, Arm: arm}} }
	random := NewPoolRandom(7)
	runs := []Run{mark("t2", ShadowArmLive), mark("t2", ShadowArmLive), mark("t2", ShadowArmShadow), mark("t1", ShadowArmShadow), {}}
	if got := ShadowPickArm("t2", runs, &random); got != ShadowArmShadow {
		t.Fatalf("2 live, 1 shadow picked %s", got)
	}
	runs = append(runs, mark("t2", ShadowArmShadow), mark("t2", ShadowArmShadow))
	if got := ShadowPickArm("t2", runs, &random); got != ShadowArmLive {
		t.Fatalf("2 live, 3 shadow picked %s", got)
	}
	seen := map[ShadowArm]bool{}
	for range 40 {
		seen[ShadowPickArm("t3", runs, &random)] = true
	}
	if !seen[ShadowArmLive] || !seen[ShadowArmShadow] {
		t.Fatalf("a tie drew only %v", seen)
	}
}

// TestShadowDefinitionTakesLivesWork checks a run on the shadow keeps the
// shadow's stages and policies but live's triggers, name and bases.
func TestShadowDefinitionTakesLivesWork(t *testing.T) {
	t.Parallel()
	live := Belt{ID: "b", Name: "Live name", Version: 3, Base: "main", RepositoryBases: map[string]string{"a/b": "dev"},
		Triggers: []TriggerEntry{{ID: "1", Trigger: Trigger{Kind: TriggerKindManual}}}}
	shadow := Belt{ID: "b", Name: "Old name", Version: 4, Base: "other", Stages: []Stage{{ID: "s"}},
		Policies: Policies{RetriageOnEdit: true}, Afk: AfkModeAlways}
	got := ShadowDefinition(shadow, live)
	if got.Version != 4 || got.Name != "Live name" || got.Base != "main" || got.RepositoryBases["a/b"] != "dev" || len(got.Triggers) != 1 {
		t.Fatalf("definition %+v", got)
	}
	if len(got.Stages) != 1 || !got.Policies.RetriageOnEdit || got.Afk != AfkModeAlways {
		t.Fatalf("shadow's own settings lost: %+v", got)
	}
}

// TestShadowEndClosesOnlyTheRunningTest checks ending a test leaves earlier ones as they ended.
func TestShadowEndClosesOnlyTheRunningTest(t *testing.T) {
	t.Parallel()
	earlier := jsonx.Date{Time: time.Unix(1_600_000_000, 0)}
	now := jsonx.Date{Time: time.Unix(1_700_000_000, 0)}
	tests := []ShadowTest{{ID: "t1", EndedAt: &earlier, Result: ShadowResultDiscarded}, {ID: "t2"}}
	if ShadowRunning(tests).ID != "t2" || ShadowNextTestID(tests) != "t3" {
		t.Fatalf("running %+v", ShadowRunning(tests))
	}
	ended := ShadowEnd(tests, ShadowResultAccepted, now)
	if ShadowRunning(ended) != nil || ended[1].Result != ShadowResultAccepted || !ended[1].EndedAt.Equal(now.Time) {
		t.Fatalf("ended %+v", ended)
	}
	if ended[0].Result != ShadowResultDiscarded || !ended[0].EndedAt.Equal(earlier.Time) || tests[1].EndedAt != nil {
		t.Fatalf("earlier test or input changed: %+v %+v", ended, tests)
	}
}

// TestShadowRunsLeaveTheFloorForTheirOwnWorkSection checks a run on a shadow version is left off
// the Floor and listed in Work's Shadow section with its chip, while a live run of the test is not.
func TestShadowRunsLeaveTheFloorForTheirOwnWorkSection(t *testing.T) {
	t.Parallel()
	onShadow := Run{ID: "a", State: RunStateRunning, BeltVersion: 2, Shadow: &RunShadow{Test: "t1", Arm: ShadowArmShadow}}
	onLive := Run{ID: "b", State: RunStateRunning, BeltVersion: 1, Shadow: &RunShadow{Test: "t1", Arm: ShadowArmLive}}
	if floor := floorRuns([]Run{onShadow, onLive}); len(floor) != 1 || floor[0].ID != "b" {
		t.Fatalf("floor %+v", floor)
	}
	main, watching, shadow := WorkPresentationSections([]Run{onShadow, onLive}, GroupByNone)
	if len(main) != 1 || main[0].Runs[0].ID != "b" || len(watching) != 0 || len(shadow) != 1 || shadow[0].Runs[0].ID != "a" {
		t.Fatalf("sections %+v %+v %+v", main, watching, shadow)
	}
	if chip := ShadowChip(onShadow); chip == nil || *chip != "Shadow v2" || ShadowChip(onLive) != nil {
		t.Fatalf("chips %v %v", chip, ShadowChip(onLive))
	}
}
