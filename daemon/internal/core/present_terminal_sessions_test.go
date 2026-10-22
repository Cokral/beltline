// Tests of terminal sessions: the read/write and take-over
// policy for terminal sessions. Pure: the kinds are built from their facts.

package core

import "testing"

// terminalAttemptKind is the attempt kind of a terminal session (isOpen, isPaused, headless).
func terminalAttemptKind(open, paused bool, headless *string) TerminalSessionKind {
	return TerminalSessionKind{Case: TerminalSessionKindAttempt, IsOpen: open, IsPaused: paused, Headless: headless}
}

// terminalKind is a kind without attempt facts (continuation, assistant, afkDecision).
func terminalKind(kind TerminalSessionKindCase) TerminalSessionKind {
	return TerminalSessionKind{Case: kind}
}

func TestSessionCategoriesShareTheReadWritePolicy(t *testing.T) {
	t.Parallel()
	mustMode := func(kind TerminalSessionKind, takeOver bool) TerminalSessionMode {
		t.Helper()
		mode, err := TerminalSessionsMode(kind, takeOver, false)
		if err != nil {
			t.Fatalf("mode(%+v, takeOver %v) failed: %v", kind, takeOver, err)
		}
		return mode
	}
	if mustMode(terminalKind(TerminalSessionKindContinuation), false).ReadOnly || mustMode(terminalKind(TerminalSessionKindContinuation), true).ReadOnly {
		t.Error("continuation is writable")
	}
	if mustMode(terminalKind(TerminalSessionKindAssistant), false).ReadOnly || mustMode(terminalKind(TerminalSessionKindAssistant), true).ReadOnly {
		t.Error("assistant is writable")
	}
	if !mustMode(terminalAttemptKind(true, false, nil), false).ReadOnly {
		t.Error("an open attempt is read-only without take over")
	}
	if mustMode(terminalAttemptKind(true, false, nil), true).ReadOnly {
		t.Error("an open attempt is writable with take over")
	}
	if !mustMode(terminalAttemptKind(false, false, nil), false).ReadOnly {
		t.Error("a finished attempt is read-only")
	}
	if !mustMode(terminalKind(TerminalSessionKindAfkDecision), false).ReadOnly {
		t.Error("an AFK decision is read-only")
	}
	headless := "Claude runs headless: nothing is drawn until the attempt ends."
	refused := []TerminalSessionKind{
		terminalAttemptKind(false, false, nil),
		terminalAttemptKind(true, true, nil),
		terminalAttemptKind(true, false, &headless),
		terminalKind(TerminalSessionKindAfkDecision),
	}
	for _, kind := range refused {
		if _, err := TerminalSessionsMode(kind, true, false); err == nil {
			t.Errorf("take over of %+v was accepted", kind)
		} else if _, ok := err.(Failure); !ok {
			t.Errorf("take over of %+v failed with %T, want a Failure", kind, err)
		}
	}
}

func TestTakeOverOfferAgreesWithTheDaemonsPolicy(t *testing.T) {
	t.Parallel()
	// Open, unpaused and interactive is the only attempt the policy lets a person take over.
	headless := "Claude runs headless: nothing is drawn until the attempt ends."
	kinds := []TerminalSessionKind{
		terminalAttemptKind(true, false, nil),
		terminalAttemptKind(false, false, nil),
		terminalAttemptKind(true, true, nil),
		terminalAttemptKind(true, false, &headless),
		terminalKind(TerminalSessionKindContinuation),
		terminalKind(TerminalSessionKindAssistant),
		terminalKind(TerminalSessionKindAfkDecision),
	}
	for _, kind := range kinds {
		offer := TerminalSessionsTakeOverOffer(kind)
		_, err := TerminalSessionsMode(kind, true, false)
		accepted := err == nil
		if offer.Enabled != accepted {
			t.Errorf("offer for %+v = %v, but the policy accepts %v", kind, offer.Enabled, accepted)
		}
		if offer.Enabled != (offer.Reason == nil) {
			t.Errorf("offer for %+v has reason %v while enabled %v", kind, offer.Reason, offer.Enabled)
		}
		if !offer.Enabled && kind.Case == TerminalSessionKindAttempt {
			want := ValueOr(kind.Headless, "This session cannot be taken over.")
			if offer.Reason == nil || *offer.Reason != want {
				t.Errorf("reason for %+v = %v, want %q", kind, offer.Reason, want)
			}
		}
	}
	if reason := TerminalSessionsTakeOverOffer(terminalAttemptKind(true, false, &headless)).Reason; reason == nil || *reason != headless {
		t.Errorf("headless reason = %v, want %q", reason, headless)
	}
	if reason := TerminalSessionsTakeOverOffer(terminalKind(TerminalSessionKindAfkDecision)).Reason; reason == nil || *reason != "AFK decision sessions cannot be taken over." {
		t.Errorf("AFK reason = %v", reason)
	}
}
