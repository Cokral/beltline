// The read/write policy
// for the terminal sessions the coordinator records, and whether an agent attempt may be
// taken over. The coordinator finds the records (it owns the runs, the assistant and the
// AFK log) and passes the facts in; the rules live here so the offer and the refusal agree.

package core

// TerminalSessionKindCase is the case of a TerminalSessionKind.
type TerminalSessionKindCase string

const (
	// TerminalSessionKindAttempt is an agent attempt's own session.
	TerminalSessionKindAttempt TerminalSessionKindCase = "attempt"
	// TerminalSessionKindContinuation is a Continue conversation.
	TerminalSessionKindContinuation TerminalSessionKindCase = "continuation"
	// TerminalSessionKindAssistant is the configuration assistant's conversation.
	TerminalSessionKindAssistant TerminalSessionKindCase = "assistant"
	// TerminalSessionKindAfkDecision is an AFK decision's session.
	TerminalSessionKindAfkDecision TerminalSessionKindCase = "afkDecision"
)

// TerminalSessionKind is the kind of coordinator-owned terminal target, with the
// current facts that determine whether an agent attempt may be taken over
// .
type TerminalSessionKind struct {
	Case TerminalSessionKindCase
	// IsOpen and IsPaused are the attempt facts; only the attempt case reads them.
	IsOpen   bool
	IsPaused bool
	// Headless is the adapter's running note when the backend draws nothing until
	// it ends; nil otherwise. Only the attempt case reads it.
	Headless *string
}

// TerminalSessionMode says whether a PTY attachment accepts terminal input.
type TerminalSessionMode struct {
	ReadOnly bool
}

// TerminalSessionsMode is the shared read/write policy: continue and assistant
// conversations are writable; other known sessions stay read-only unless an open,
// unpaused, interactive agent attempt is taken over. ignoresPause keeps an
// attachment already taken over writable while the run pauses.
// Lock: none (pure).
func TerminalSessionsMode(kind TerminalSessionKind, takeOver bool, ignoresPause bool) (TerminalSessionMode, error) {
	switch kind.Case {
	case TerminalSessionKindContinuation, TerminalSessionKindAssistant:
		return TerminalSessionMode{ReadOnly: false}, nil
	case TerminalSessionKindAttempt:
		if !takeOver {
			return TerminalSessionMode{ReadOnly: true}, nil
		}
		if kind.Headless != nil {
			return TerminalSessionMode{}, NewFailure(*kind.Headless)
		}
		if !kind.IsOpen || (!ignoresPause && kind.IsPaused) {
			return TerminalSessionMode{}, NewFailure("This session cannot be taken over.")
		}
		return TerminalSessionMode{ReadOnly: false}, nil
	case TerminalSessionKindAfkDecision:
		if takeOver {
			return TerminalSessionMode{}, NewFailure("AFK decision sessions cannot be taken over.")
		}
		return TerminalSessionMode{ReadOnly: true}, nil
	}
	return TerminalSessionMode{ReadOnly: true}, nil
}

// TerminalSessionsTakeOverOffer says whether Take over may be requested for a
// session of this kind, and the reason when it may not. It derives from
// TerminalSessionsMode, so the offer and the refusal cannot disagree.
// Lock: none (pure).
func TerminalSessionsTakeOverOffer(kind TerminalSessionKind) SessionOffer {
	if _, err := TerminalSessionsMode(kind, true, false); err != nil {
		reason := err.Error()
		return SessionOffer{Enabled: false, Reason: &reason}
	}
	return SessionOffer{Enabled: true}
}

// TerminalSessionsAttemptKind is the kind an agent attempt is recorded under.
// Lock: none (pure).
func TerminalSessionsAttemptKind(run Run, attempt Attempt, headless *string) TerminalSessionKind {
	return TerminalSessionKind{
		Case: TerminalSessionKindAttempt, IsOpen: attempt.IsOpen(),
		IsPaused: run.Pause != nil && run.Pause.BlocksProgress(), Headless: headless,
	}
}
