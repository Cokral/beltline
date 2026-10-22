package core

import (
	"encoding/json"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestAttemptIDAndKeyForLegs checks that an attempt's id and key tell one leg's
// attempt at a stage from another's, and that the line before a fork ("main")
// has no leg of its own.
func TestAttemptIDAndKeyForLegs(t *testing.T) {
	t.Parallel()
	simple := "r:simple"
	main := "main"
	cases := []struct {
		name   string
		gotID  string
		wantID string
		gotKey string
		wantK  string
	}{
		{"line before a fork", AttemptID("build", 2, nil), "build-2", AttemptKey("build", nil), "build"},
		{"main is no leg", AttemptID("build", 2, &main), "build-2", AttemptKey("build", &main), "build"},
		{"a route leg", AttemptID("build", 2, &simple), "build-r:simple-2", AttemptKey("build", &simple), "build|r:simple"},
	}
	for _, tc := range cases {
		if tc.gotID != tc.wantID || tc.gotKey != tc.wantK {
			t.Errorf("%s: id %q key %q, want %q %q", tc.name, tc.gotID, tc.gotKey, tc.wantID, tc.wantK)
		}
	}
}

// TestNewAttemptStoresNoMainLeg checks that a new attempt on the line keeps no
// leg, and that one on a route keeps its leg and id.
func TestNewAttemptStoresNoMainLeg(t *testing.T) {
	t.Parallel()
	main := "main"
	onLine := NewAttempt(AttemptArgs{StageID: "build", Number: 1, Folder: "01-build-1", Leg: &main})
	if onLine.Leg != nil || onLine.ID != "build-1" || !onLine.IsOpen() {
		t.Errorf("line attempt = %+v", onLine)
	}
	route := "r:simple"
	onRoute := NewAttempt(AttemptArgs{StageID: "build", Number: 1, Folder: "f", Leg: &route})
	if onRoute.Leg == nil || *onRoute.Leg != route || onRoute.ID != "build-r:simple-1" {
		t.Errorf("route attempt = %+v", onRoute)
	}
}

// TestAttemptRoundTripKeepsEverything checks an attempt with spend, settings, a
// checkpoint and an interruption round-trips, and that an attempt saved without
// usage reads it as nil and keeps its marks.
func TestAttemptRoundTripKeepsEverything(t *testing.T) {
	t.Parallel()
	cost := 340000000
	provenance := CostProvenanceReported
	reason := "stopped by you"
	conversation := "sid"
	backend := BackendCodex
	thinking := ThinkingMax
	session := "bl-build"
	attempt := Attempt{
		ID: "build-1", StageID: "build", Number: 1, StartedAt: jsonx.Date{}, Folder: "02-build-1", Session: &session,
		Settings:  &SessionSettings{Runner: &backend, Model: "gpt-5.3-codex", Thinking: &thinking},
		Usage:     &Usage{CostNanos: &cost, Coverage: UsageCoverageComplete, Provenance: &provenance},
		UsageMark: new(9),
		LogMark:   new(4),
		Interruption: &AttemptInterruption{
			Operation: "p", LegID: "main", Confirmation: AttemptInterruptionConfirmationConfirmed, Reason: &reason, Conversation: &conversation,
			Continuation: func() *AttemptInterruptionContinuation {
				c := NewAttemptInterruptionContinuation("p", AttemptInterruptionContinuationStatusQueued, nil, nil)
				return &c
			}(),
		},
	}
	data, err := jsonx.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	var back Attempt
	testDecode(t, string(data), &back)
	if d := testDiffEmpty(attempt, back); d != "" {
		t.Fatalf("round trip: %s", d)
	}
	var older Attempt
	testDecode(t, `{"id":"build-1","stageId":"build","number":1,"startedAt":"2026-07-22T02:18:00Z","folder":"f","logMark":4,"usageMark":9}`, &older)
	if older.Usage != nil || older.LogMark == nil || *older.LogMark != 4 || older.UsageMark == nil || *older.UsageMark != 9 {
		t.Errorf("older attempt = %+v", older)
	}
}

// TestRouteSetDecodesEitherShape checks the single-string and list forms, and
// that any other value, or null, reads as no routes.
func TestRouteSetDecodesEitherShape(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string][]string{
		`{"size":"simple"}`:             {"size": {"simple"}},
		`{"size":["simple","complex"]}`: {"size": {"simple", "complex"}},
		`{}`:                            {},
		`null`:                          {},
		`["size"]`:                      {},
		`{"size":"simple","x":["y"]}`:   {},
	}
	for data, want := range cases {
		var set RouteSet
		testDecode(t, data, &set)
		if d := testDiffEmpty(want, set.Routes); d != "" {
			t.Errorf("%s: %s", data, d)
		}
	}
}

// TestAttemptOpenAndSessionName checks that an attempt is open until it ends,
// and that its agent session name is the agent's name when it has one.
func TestAttemptOpenAndSessionName(t *testing.T) {
	t.Parallel()
	session, agent := "bl-build", "bl-build-2"
	a := Attempt{Session: &session}
	if !a.IsOpen() || *a.AgentSessionName() != session {
		t.Errorf("open attempt: open %t name %v", a.IsOpen(), a.AgentSessionName())
	}
	a.AgentName = &agent
	if *a.AgentSessionName() != agent {
		t.Errorf("name = %q, want the agent name", *a.AgentSessionName())
	}
	ended := jsonx.Date{}
	a.EndedAt = &ended
	if a.IsOpen() {
		t.Error("an attempt with an end is open")
	}
}

// TestAttemptLaunchModeRoundTripsAndReadsOlderAttempts checks that an attempt's
// launch mode and completion token survive a save, and that attempts saved before
// them keep how they ran: Copilot's flag, else headless.
func TestAttemptLaunchModeRoundTripsAndReadsOlderAttempts(t *testing.T) {
	t.Parallel()
	mode, token := AttemptLaunchInteractive, "TOKEN-1"
	saved := NewAttempt(AttemptArgs{StageID: "build", Number: 1, Folder: "01-build-1"})
	saved.LaunchMode, saved.CompletionToken = &mode, &token
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var read Attempt
	if err := json.Unmarshal(data, &read); err != nil {
		t.Fatal(err)
	}
	if read.LaunchMode == nil || *read.LaunchMode != AttemptLaunchInteractive || read.CompletionToken == nil || *read.CompletionToken != token {
		t.Fatalf("round trip lost the launch: %s", data)
	}
	for _, backend := range AllBackend {
		if !read.Interactive(backend) {
			t.Errorf("%s: a saved interactive launch reads as headless", backend)
		}
	}
	headless := AttemptLaunchHeadless
	read.LaunchMode = &headless
	if read.Interactive(BackendCopilot) {
		t.Error("a saved headless launch reads as interactive")
	}
	var older Attempt
	if err := json.Unmarshal([]byte(`{"id":"build-1","stageId":"build","number":1,"startedAt":"2026-01-01T00:00:00Z","folder":"01-build-1","copilotInteractive":true}`), &older); err != nil {
		t.Fatal(err)
	}
	if !older.Interactive(BackendCopilot) {
		t.Error("an older interactive Copilot attempt is no longer recognised")
	}
	older.CopilotInteractive = nil
	for _, backend := range AllBackend {
		if older.Interactive(backend) {
			t.Errorf("%s: an older attempt without a mode reads as interactive", backend)
		}
	}
}

// TestEachLaunchHasAHandoffTokenOfItsOwn checks that a resumed launch's token
// differs from every earlier one of the same session.
func TestEachLaunchHasAHandoffTokenOfItsOwn(t *testing.T) {
	t.Parallel()
	first := AttemptCompletionToken("abc", nil)
	second := AttemptCompletionToken("abc", &first)
	third := AttemptCompletionToken("abc", &second)
	if first != "abc#1" || second != "abc#2" || third != "abc#3" {
		t.Fatalf("tokens %q %q %q, want abc#1 abc#2 abc#3", first, second, third)
	}
	other := "xyz#7"
	if got := AttemptCompletionToken("abc", &other); got != "abc#1" {
		t.Fatalf("another session's token continued: %q", got)
	}
}
