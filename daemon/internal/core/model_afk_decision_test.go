package core

import (
	"encoding/json"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestSettingsSavedBeforeAfkReadAsOff checks the AFK part of settings saved before AFK mode existed:
// AfkTests.settingsSavedBeforeAfkReadAsOff: settings with none of the keys
// read as the defaults, and a partial one takes the rest from them. The
func TestSettingsSavedBeforeAfkReadAsOff(t *testing.T) {
	t.Parallel()
	var settings AfkSettings
	testDecode(t, `{}`, &settings)
	if d := testDiffEmpty(DefaultAfkSettings(), settings); d != "" {
		t.Fatalf("empty settings: %s", d)
	}
	if settings.Enabled {
		t.Error("AFK is on by default")
	}
	var partial AfkSettings
	testDecode(t, `{"enabled":true}`, &partial)
	if !partial.Enabled || !partial.Approve || partial.Drop || partial.Limit != 3 {
		t.Errorf("partial settings = %+v", partial)
	}
}

// TestAfkSettingsFallBackForUnreadableFields checks that an unknown backend
// falls back to the default, and an unknown thinking to none, while a readable
// one is kept.
func TestAfkSettingsFallBackForUnreadableFields(t *testing.T) {
	t.Parallel()
	var bad AfkSettings
	testDecode(t, `{"backend":"bogus","thinking":"extreme","model":"m"}`, &bad)
	if bad.Backend != BackendPi || bad.Thinking != nil || bad.Model != "m" {
		t.Errorf("unreadable backend and thinking = %+v", bad)
	}
	var good AfkSettings
	testDecode(t, `{"backend":"claude","thinking":"high","drop":true,"limit":5,"guidance":"be careful"}`, &good)
	if good.Backend != BackendClaude || good.Thinking == nil || *good.Thinking != ThinkingHigh || !good.Drop || good.Limit != 5 || good.Guidance != "be careful" {
		t.Errorf("readable settings = %+v", good)
	}
	var nullThinking AfkSettings
	testDecode(t, `{"thinking":null,"backend":null}`, &nullThinking)
	if nullThinking.Thinking != nil || nullThinking.Backend != BackendPi {
		t.Errorf("null thinking and backend = %+v", nullThinking)
	}
	var notObject AfkSettings
	if err := json.Unmarshal([]byte(`[1]`), &notObject); err == nil {
		t.Error("settings from an array decoded")
	}
}

// TestAfkSettingsRoundTrip checks settings with a thinking and a model round-trip.
func TestAfkSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	thinking := ThinkingMax
	settings := DefaultAfkSettings()
	settings.Enabled = true
	settings.Backend = BackendCodex
	settings.Model = "gpt-5.3-codex"
	settings.Thinking = &thinking
	data, err := jsonx.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var back AfkSettings
	testDecode(t, string(data), &back)
	if d := testDiffEmpty(settings, back); d != "" {
		t.Fatalf("round trip: %s", d)
	}
}

// testDecisionRun is a run waiting at stage spec, for the decision fixtures.
func testDecisionRun() Run {
	return Run{ID: "bug:acme/billing#142", BeltName: "Bug", StageID: "spec", Subject: Subject{Kind: SubjectKindIssue,
		Repo: "acme/billing", Number: 142, Title: "Export"}, Reason: new("Approval needed")}
}

// TestAfkDecisionWithoutOverruleDecodes checks that a decision saved before
// overrules reads with no overrule, and that a decision round-trips. The
// model-only half of AfkTests.oldDecisionsAndRunsDecodeWithoutAnOverrule.
func TestAfkDecisionWithoutOverruleDecodes(t *testing.T) {
	t.Parallel()
	started := jsonx.Date{}
	decision := NewAfkDecision(AfkDecisionArgs{ID: "d1", Run: testDecisionRun(),
		Situation: AfkSituation{Kind: AfkSituationKindApproval, Key: "approval:spec"}, StartedAt: started, Folder: "afk/1-spec"})
	decision.Status = AfkDecisionStatusApplied
	data, err := jsonx.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	delete(saved, "overrule")
	older, _ := json.Marshal(saved)
	var decoded AfkDecision
	testDecode(t, string(older), &decoded)
	if decoded.Overrule != nil || decoded.Status != AfkDecisionStatusApplied {
		t.Errorf("older decision = %+v", decoded)
	}
	var round AfkDecision
	testDecode(t, string(data), &round)
	if d := testDiffEmpty(decision, round); d != "" {
		t.Fatalf("round trip: %s", d)
	}
}

// TestNewAfkDecisionStartsDeciding checks a new decision's run fields, status and review flag.
func TestNewAfkDecisionStartsDeciding(t *testing.T) {
	t.Parallel()
	decision := NewAfkDecision(AfkDecisionArgs{ID: "d2", Run: testDecisionRun(),
		Situation: AfkSituation{Kind: AfkSituationKindQuestion, Key: "q:1"}, Folder: "afk/2-spec"})
	if decision.Status != AfkDecisionStatusDeciding || decision.Reviewed || decision.SubjectRef != "#142" ||
		decision.StageName != "spec" || decision.Situation != AfkSituationKindQuestion || decision.Key != "q:1" ||
		decision.Reason == nil || *decision.Reason != "Approval needed" || decision.BeltName != "Bug" {
		t.Errorf("new decision = %+v", decision)
	}
}

// TestAfkDecisionNeedsReview checks which decisions wait for a person to check them.
func TestAfkDecisionNeedsReview(t *testing.T) {
	t.Parallel()
	overrule := &AfkOverrule{}
	cases := []struct {
		name     string
		status   AfkDecisionStatus
		reviewed bool
		overrule *AfkOverrule
		want     bool
	}{
		{"applied and unchecked", AfkDecisionStatusApplied, false, nil, true},
		{"gave up and unchecked", AfkDecisionStatusFailed, false, nil, true},
		{"still deciding", AfkDecisionStatusDeciding, false, nil, false},
		{"superseded", AfkDecisionStatusSuperseded, false, nil, false},
		{"checked", AfkDecisionStatusApplied, true, nil, false},
		{"overruled", AfkDecisionStatusApplied, false, overrule, false},
	}
	for _, tc := range cases {
		d := AfkDecision{Status: tc.status, Reviewed: tc.reviewed, Overrule: tc.overrule}
		if d.NeedsReview() != tc.want {
			t.Errorf("%s: needsReview %t, want %t", tc.name, d.NeedsReview(), tc.want)
		}
	}
}
