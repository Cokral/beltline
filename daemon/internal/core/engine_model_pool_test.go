// Tests of the model pool: the pick, the run's own settings, the saved form and the checks.

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// modelPoolTestSettings builds one SessionSettings for the tests.
func modelPoolTestSettings(model string, thinking *Thinking, runner *Backend) SessionSettings {
	return SessionSettings{Model: model, Thinking: thinking, Runner: runner}
}

// modelPoolTestA, B and C are the three settings the pool tests use.
var (
	modelPoolTestA = modelPoolTestSettings("a", nil, new(BackendPi))
	modelPoolTestB = modelPoolTestSettings("b", new(ThinkingHigh), new(BackendClaude))
	modelPoolTestC = modelPoolTestSettings("c", nil, new(BackendCodex))
)

// modelPoolTestPick is ModelPool.pick with a seeded random source and the given settings.
func modelPoolTestPick(pool, running []SessionSettings, previous *SessionSettings, allowed func(SessionSettings) bool, seed uint64) *SessionSettings {
	if allowed == nil {
		allowed = func(SessionSettings) bool { return true }
	}
	random := NewPoolRandom(seed)
	return ModelPoolPick(pool, running, previous, allowed, &random)
}

// modelPoolTestSame reports whether got is the settings want.
func modelPoolTestSame(got *SessionSettings, want SessionSettings) bool {
	return got != nil && modelPoolSame(*got, want)
}

// TestPoolIsTheStagesOwnSettingsThenEachAlternateOnce
func TestPoolIsTheStagesOwnSettingsThenEachAlternateOnce(t *testing.T) {
	t.Parallel()
	pi := BackendPi
	stage := NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Runner: &pi, Model: "a",
		Alternates: []SessionSettings{modelPoolTestB, modelPoolTestA, modelPoolTestB, modelPoolTestSettings("c", nil, nil)}, Outcomes: []Outcome{}})
	want := []SessionSettings{modelPoolTestA, modelPoolTestB, modelPoolTestSettings("c", nil, new(BackendPi))}
	if got := stage.Pool(); !slices.EqualFunc(got, want, modelPoolSame) {
		t.Fatalf("pool = %+v, want %+v", got, want)
	}
	plain := NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Runner: &pi, Model: "a", Outcomes: []Outcome{}})
	if got := plain.Pool(); !slices.EqualFunc(got, []SessionSettings{modelPoolTestA}, modelPoolSame) {
		t.Fatalf("plain pool = %+v, want only a", got)
	}
	approve := NewStage(StageArgs{ID: "approve", Type: StageTypeApprove, Alternates: []SessionSettings{modelPoolTestB}, Outcomes: []Outcome{}})
	if len(approve.Alternates) != 0 {
		t.Fatalf("an approval kept alternates: %+v", approve.Alternates)
	}
}

// TestTakesTheSettingsFewestRunningSessionsUse
func TestTakesTheSettingsFewestRunningSessionsUse(t *testing.T) {
	t.Parallel()
	a, b, c := modelPoolTestA, modelPoolTestB, modelPoolTestC
	// Two sessions on A and one on B: the next one balances it on B.
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{a, a, b}, nil, nil, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("balanced pick = %+v, want b", got)
	}
	if got := modelPoolTestPick([]SessionSettings{a, b, c}, []SessionSettings{a, b, b, c, c}, nil, nil, 1); !modelPoolTestSame(got, a) {
		t.Fatalf("three-way pick = %+v, want a", got)
	}
	// A session on settings no longer in the pool doesn't count.
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{c, c, a}, nil, nil, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("pick with a stranger running = %+v, want b", got)
	}
	// A running session saved before runners were recorded is on Pi.
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{modelPoolTestSettings("a", nil, nil)}, nil, nil, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("pick with an unrecorded runner = %+v, want b", got)
	}
}

// TestDrawsAtRandomBetweenEquallyBusySettings
func TestDrawsAtRandomBetweenEquallyBusySettings(t *testing.T) {
	t.Parallel()
	a, b, c := modelPoolTestA, modelPoolTestB, modelPoolTestC
	drawn := map[string]struct{}{}
	for seed := uint64(0); seed < 64; seed++ {
		choice := modelPoolTestPick([]SessionSettings{a, b, c}, []SessionSettings{c}, nil, nil, seed)
		if choice == nil || (!modelPoolTestSame(choice, a) && !modelPoolTestSame(choice, b)) {
			t.Fatalf("seed %d drew %+v, want a or b", seed, choice)
		}
		drawn[choice.Model] = struct{}{}
	}
	if !reflect.DeepEqual(drawn, map[string]struct{}{"a": {}, "b": {}}) {
		t.Fatalf("drawn = %v, want a and b", drawn)
	}
	// The same seed draws the same way each time.
	first := modelPoolTestPick([]SessionSettings{a, b, c}, nil, nil, nil, 7)
	second := modelPoolTestPick([]SessionSettings{a, b, c}, nil, nil, nil, 7)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("seed 7 drew %+v then %+v", first, second)
	}
}

// TestARunComingBackKeepsItsSettings
func TestARunComingBackKeepsItsSettings(t *testing.T) {
	t.Parallel()
	a, b, c := modelPoolTestA, modelPoolTestB, modelPoolTestC
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{b, b}, &b, nil, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("returning run = %+v, want b", got)
	}
	// Unless they left the pool, or the run may no longer use them.
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{a}, &c, nil, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("returning run that left the pool = %+v, want b", got)
	}
	notA := func(s SessionSettings) bool { return s.Model != "a" }
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{b}, &a, notA, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("returning run that may not use its settings = %+v, want b", got)
	}
}

// TestSkipsSettingsTheRunDoesntAllow
func TestSkipsSettingsTheRunDoesntAllow(t *testing.T) {
	t.Parallel()
	a, b := modelPoolTestA, modelPoolTestB
	notA := func(s SessionSettings) bool { return s.Model != "a" }
	if got := modelPoolTestPick([]SessionSettings{a, b}, []SessionSettings{b, b}, nil, notA, 1); !modelPoolTestSame(got, b) {
		t.Fatalf("pick without a = %+v, want b", got)
	}
	none := func(SessionSettings) bool { return false }
	if got := modelPoolTestPick([]SessionSettings{a, b}, nil, nil, none, 1); got != nil {
		t.Fatalf("pick with nothing allowed = %+v, want nil", got)
	}
}

// TestSavesAlternatesOnlyWhenThereAreAny
func TestSavesAlternatesOnlyWhenThereAreAny(t *testing.T) {
	t.Parallel()
	pi := BackendPi
	plain := NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Runner: &pi, Model: "a", Outcomes: []Outcome{}})
	data, err := jsonx.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "alternates") {
		t.Fatalf("a stage without alternates saves them: %s", data)
	}
	pooled := plain
	pooled.Alternates = []SessionSettings{modelPoolTestB}
	data, err = jsonx.Marshal(pooled)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Stage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, pooled) {
		t.Fatalf("round trip changed the stage:\n%s", reflect.ValueOf(decoded).String())
	}
	data, err = jsonx.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded Stage
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Alternates) != 0 {
		t.Fatalf("a plain stage loaded alternates: %+v", reloaded.Alternates)
	}
}

// TestAgentFilesKeepAlternates
func TestAgentFilesKeepAlternates(t *testing.T) {
	t.Parallel()
	pi := BackendPi
	next := NewThen(ThenArgs{Action: ThenActionNext})
	agent := NewAgent(AgentArgs{ID: "build", Name: "Build", Runner: &pi, Model: "a",
		Outcomes: []AgentOutcome{{Name: "done", When: "", Tone: ToneGood, Then: next}}, Brief: "Build it."})
	low := ThinkingLow
	codex := BackendCodex
	agent.Alternates = []SessionSettings{modelPoolTestB, {Model: "", Thinking: &low, Runner: &codex}}
	text := AgentFileWrite(agent)
	want := "alternates:\n  - runner: claude\n    model: b\n    thinking: high\n  - runner: codex\n    thinking: low\n"
	if !strings.Contains(text, want) {
		t.Fatalf("agent file misses the alternates:\n%s", text)
	}
	parsed, err := AgentFileParse(text, "build", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !agentEqual(parsed, agent) {
		t.Fatalf("parsed agent differs:\n%s", reflect.ValueOf(parsed).String())
	}
	bare := NewAgent(AgentArgs{ID: "x", Name: "X", Brief: "b"})
	bareParsed, err := AgentFileParse(AgentFileWrite(bare), "x", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(bareParsed.Alternates) != 0 {
		t.Fatalf("a bare agent parsed alternates: %+v", bareParsed.Alternates)
	}
	// A new stage with the agent starts with them.
	if stage := DefaultsStageForAgent(agent, nil, nil, nil, TriggerKindIssues); !reflect.DeepEqual(stage.Alternates, agent.Alternates) {
		t.Fatalf("new stage alternates = %+v, want %+v", stage.Alternates, agent.Alternates)
	}
}

// TestAgentFileRejectsAnAlternateWithoutItsDash
func TestAgentFileRejectsAnAlternateWithoutItsDash(t *testing.T) {
	t.Parallel()
	text := "---\nname: X\nalternates:\n    model: b\noutcomes:\n  - name: done\n---\nBrief\n"
	if _, err := AgentFileParse(text, "x", 1); err == nil {
		t.Fatal("an alternate without its dash parsed")
	}
}

// TestEveryModelInThePoolMustBeAllowed
func TestEveryModelInThePoolMustBeAllowed(t *testing.T) {
	t.Parallel()
	admission := NewModelAdmission(map[string][]ModelChoice{
		"pi":     {{ID: "a", Name: "A"}},
		"claude": {{ID: "b", Name: "B"}},
	}, nil, nil)
	pi := BackendPi
	stage := NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Name: new("Build"), Runner: &pi, Model: "a",
		Alternates: []SessionSettings{modelPoolTestB}, Outcomes: []Outcome{}})
	if got := ModelPolicyWarnings([]Stage{stage}, admission, nil); len(got) != 0 {
		t.Fatalf("allowed pool warned: %v", got)
	}
	stage.Alternates = append(stage.Alternates, modelPoolTestC)
	want := []string{"Build: c is not allowed for " + BackendCodex.Label() + ". Choose an allowed model or configure Settings → Allowed models."}
	if got := ModelPolicyWarnings([]Stage{stage}, admission, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
}

// TestSeededPoolRandomIsSplitMix64 pins the seeded source to SplitMix64's
// sequence, which PoolRandom(seed:) produces (review/phase2.md).
func TestSeededPoolRandomIsSplitMix64(t *testing.T) {
	t.Parallel()
	r := NewPoolRandom(0)
	for i, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4} {
		if got := r.Next(); got != want {
			t.Fatalf("draw %d = %#x, want %#x", i, got, want)
		}
	}
}
