package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// TerminalSessionRecord is a terminal target recorded by one coordinator. Names are compared exactly.
type TerminalSessionRecord struct {
	Name string
	Kind core.TerminalSessionKind
}

// terminalSessionsFind returns the one terminal record named exactly name, or nil. It walks the
// runs, the assistant and the AFK decisions in order and works out a kind, with its headless note,
// only for the record named name, so a terminal or sidebar read does not build every record.
// Lock: none (pure); headless is called for the matching attempt only.
func terminalSessionsFind(name string, runs []core.Run, assistantSession string, decisions []core.AfkDecision,
	headless func(run core.Run, attempt core.Attempt) *string) *TerminalSessionRecord {
	continued, isContinue := runner.TmuxContinuedSession(name)
	for i := range runs {
		run := &runs[i]
		for j := range run.Attempts {
			attempt := &run.Attempts[j]
			if attempt.Session == nil {
				continue
			}
			session := *attempt.Session
			opens := session == name
			continues := isContinue && session == continued && !attempt.IsOpen()
			if !opens && !continues {
				continue
			}
			if !run.HasAgentStage(attempt.StageID) {
				continue
			}
			if opens {
				return &TerminalSessionRecord{Name: name, Kind: core.TerminalSessionsAttemptKind(*run, *attempt, headless(*run, *attempt))}
			}
			return &TerminalSessionRecord{Name: name, Kind: core.TerminalSessionKind{Case: core.TerminalSessionKindContinuation}}
		}
	}
	if assistantSession == name {
		return &TerminalSessionRecord{Name: name, Kind: core.TerminalSessionKind{Case: core.TerminalSessionKindAssistant}}
	}
	for _, decision := range decisions {
		if decision.Session != nil && *decision.Session == name {
			return &TerminalSessionRecord{Name: name, Kind: core.TerminalSessionKind{Case: core.TerminalSessionKindAfkDecision}}
		}
	}
	return nil
}

// TerminalModeForSession resolves a terminal against only this coordinator's
// saved data and applies the current shared read/write policy.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) TerminalModeForSession(session string, takeOver bool, ignoresPause bool) (*core.TerminalSessionMode, error) {
	c.Lock()
	defer c.EndTurn()
	return c.terminalModeLocked(session, takeOver, ignoresPause)
}

// terminalModeLocked is TerminalModeForSession with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) terminalModeLocked(session string, takeOver bool, ignoresPause bool) (*core.TerminalSessionMode, error) {
	record := terminalSessionsFind(session, c.book.runs, c.assistant.session, c.autopilot.decisions, c.headlessNote)
	if record == nil {
		return nil, nil
	}
	mode, err := core.TerminalSessionsMode(record.Kind, takeOver, ignoresPause)
	if err != nil {
		return nil, err
	}
	return &mode, nil
}

// HeadlessNote is the note for an attempt launched headless, which prints
// nothing until it ends, so has no live terminal to take over; nil when it was
// launched interactively and draws as it runs. Unknown backends count as headless
// with a generic note.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) HeadlessNote(run core.Run, attempt core.Attempt) *string {
	c.Lock()
	defer c.EndTurn()
	return c.headlessNote(run, attempt)
}

// headlessNote is HeadlessNote with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) headlessNote(run core.Run, attempt core.Attempt) *string {
	const noTerminal = "This session has no live terminal to take over."
	stage := findStage(run, attempt.StageID)
	var settings *core.SessionSettings
	if stage != nil {
		settings = attemptSettingsHistorical(run, attempt, *stage)
	}
	if settings == nil || settings.Runner == nil {
		text := noTerminal
		return &text
	}
	if attempt.Interactive(*settings.Runner) {
		return nil
	}
	return runner.AdapterFor(*settings.Runner).RunningNote()
}

// TakeOverOffer is Take over for an agent attempt, under the policy the daemon
// applies to the attach.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) TakeOverOffer(run core.Run, attempt core.Attempt) SessionOffer {
	c.Lock()
	defer c.EndTurn()
	return c.takeOverOfferLocked(run, attempt)
}

// takeOverOfferLocked is TakeOverOffer with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) takeOverOfferLocked(run core.Run, attempt core.Attempt) SessionOffer {
	return core.TerminalSessionsTakeOverOffer(core.TerminalSessionsAttemptKind(run, attempt, c.headlessNote(run, attempt)))
}

// TerminalModeForAttempt is the sidebar's terminal mode for an attempt, under the
// same policy as the daemon.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) TerminalModeForAttempt(run core.Run, attempt core.Attempt, takeOver bool) *core.TerminalSessionMode {
	c.Lock()
	defer c.EndTurn()
	if attempt.Session == nil {
		return nil
	}
	session := *attempt.Session
	if !attempt.IsOpen() {
		session = runner.TmuxContinueName(session)
	}
	mode, err := c.terminalModeLocked(session, takeOver, false)
	if err != nil {
		return nil
	}
	return mode
}
