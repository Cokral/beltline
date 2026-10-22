// Tests of the terminal session lookup: which exact names the coordinator admits, and the kind each one
// gets from its run, attempt, assistant or AFK decision.

package coordinator

import (
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// terminalNoHeadless is the headless lookup of tests that have no headless backend.
func terminalNoHeadless(core.Run, core.Attempt) *string { return nil }

// terminalAttemptKind is the attempt kind: isOpen, isPaused and headless.
func terminalAttemptKind(open, paused bool, headless *string) core.TerminalSessionKind {
	return core.TerminalSessionKind{Case: core.TerminalSessionKindAttempt, IsOpen: open, IsPaused: paused, Headless: headless}
}

// terminalKind is a kind without attempt facts (continuation, assistant, afkDecision).
func terminalKind(kind core.TerminalSessionKindCase) core.TerminalSessionKind {
	return core.TerminalSessionKind{Case: kind}
}

func TestTerminalSessionsAdmitOnlyExactNamesOfThisCoordinator(t *testing.T) {
	t.Parallel()
	open := sessionSampleRun()
	core.EngineAdvance(&open, core.Event{Kind: core.EventKindSessionStarted, Session: "bl-open", SessionID: "open-id", Folder: "open"}, sessionSampleT0)
	finished := sessionSampleRun()
	finished.ID = "finished-run"
	core.EngineAdvance(&finished, core.Event{Kind: core.EventKindSessionStarted, Session: "bl-finished", SessionID: "finished-id", Folder: "finished"}, sessionSampleT0)
	core.EngineAdvance(&finished, core.Event{Kind: core.EventKindSessionFinished}, sessionSampleT0)
	decision := core.NewAfkDecision(core.AfkDecisionArgs{
		ID: "afk", Run: open, Situation: core.AfkSituation{Kind: core.AfkSituationKindApproval, Key: "hold"},
		StartedAt: jsonx.Date{Time: sessionSampleT0}, Folder: "afk/01",
	})
	decision.Session = new("bl-afk")
	runs := []core.Run{open, finished}
	decisions := []core.AfkDecision{decision}

	openKind := terminalAttemptKind(true, false, nil)
	finishedKind := terminalAttemptKind(false, false, nil)
	continuation := terminalKind(core.TerminalSessionKindContinuation)
	afk := terminalKind(core.TerminalSessionKindAfkDecision)
	cases := []struct {
		name string
		want *core.TerminalSessionKind
	}{
		{"bl-open", &openKind},
		{"bl-open-continue", nil},
		{"bl-finished", &finishedKind},
		{"bl-finished-continue", &continuation},
		{"bl-afk", &afk},
		{"bl-assistant", &core.TerminalSessionKind{Case: core.TerminalSessionKindAssistant}},
		{"bl-run", nil},
		{"bl-open*", nil},
		{"=bl-open", nil},
	}
	for _, c := range cases {
		got := terminalSessionsFind(c.name, runs, "bl-assistant", decisions, terminalNoHeadless)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("find %q = %+v, want nil", c.name, got)
		case c.want != nil && (got == nil || got.Kind != *c.want):
			t.Errorf("find %q = %+v, want kind %+v", c.name, got, *c.want)
		}
	}
	if got := terminalSessionsFind("bl-assistant-two", runs, "bl-assistant", nil, terminalNoHeadless); got != nil {
		t.Errorf("another coordinator's assistant resolved to %+v", got)
	}
}

func TestAttemptKindRecordsTheRunsPauseAndHeadlessFacts(t *testing.T) {
	t.Parallel()
	run := sessionSampleRun()
	attempt := sessionSampleAttempt(run.StageID, 1, "one")
	if got := core.TerminalSessionsAttemptKind(run, attempt, nil); got != terminalAttemptKind(true, false, nil) {
		t.Fatalf("kind = %+v", got)
	}
	run.Pause = &core.RunPause{Phase: core.RunPausePhasePaused, Operation: "pause", RequestedAt: jsonx.Date{Time: sessionSampleT0}}
	headless := "Claude runs headless: nothing is drawn until the attempt ends."
	if got := core.TerminalSessionsAttemptKind(run, attempt, &headless); got != terminalAttemptKind(true, true, &headless) {
		t.Fatalf("kind = %+v, want paused and headless", got)
	}
}
