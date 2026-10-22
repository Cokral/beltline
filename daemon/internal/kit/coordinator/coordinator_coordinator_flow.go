// The run and belt commands
// the server calls: decisions, sends, holds, AFK, starts, pauses and saves.

package coordinator

import (
	"context"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// DesktopSessionAvailable confirms a daemon-owned session is available before a Ghostty attach.
// Lock: exported entry point; takes c.mu. Suspends: runner.conversationAlive.
func (c *Coordinator) DesktopSessionAvailable(ctx context.Context, name string) bool {
	c.Lock()
	defer c.EndTurn()
	alive := false
	c.Suspend(func() { alive = c.runner.ConversationAlive(ctx, name) })
	return alive
}

// Decide records a decision on a waiting approval and kicks.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) Decide(id, outcome string, note *string) {
	c.Lock()
	defer c.EndTurn()
	run := c.book.Run(id)
	if run == nil || run.Parking != nil || (run.Pause != nil && run.Pause.BlocksProgress()) {
		return
	}
	c.book.apply(id, core.Event{Kind: core.EventKindDecided, Outcome: outcome, Note: note}, nil, nil, nil)
	c.kick()
}

// Resume sends a run's route to stage, or where it stopped. Send to
// works on any unfinished run: a pause keeps the destination from starting, and a
// park ends. Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) Resume(id string, stage, note *string, earlier bool, leg *string) {
	c.Lock()
	defer c.EndTurn()
	message := trimmedOrNil(note)
	run := c.book.Run(id)
	if run == nil || !run.State.IsActive() {
		return
	}
	stageID := core.ValueOr(stage, run.StageID)
	target := findStage(*run, stageID)
	if target == nil {
		return
	}
	event := core.EventResume(stage, message, leg)
	event.Earlier = earlier
	c.book.apply(id, event, nil, nil, nil)
	if message != nil {
		runID := id
		c.book.note("Sent to "+target.Name+": "+*message, &runID)
	}
	c.kick()
}

// Answer continues a held agent stage's session with your answer, or with
// what you told it in the session when answer is nil.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) Answer(id string, answer *string) {
	c.Lock()
	defer c.EndTurn()
	run := c.book.Run(id)
	if run == nil || run.Parking != nil || (run.Pause != nil && run.Pause.BlocksProgress()) {
		return
	}
	c.book.apply(id, core.Event{Kind: core.EventKindAnswered, Answer: trimmedOrNil(answer)}, nil, nil, nil)
	c.kick()
}

// StartNow starts a queued agent stage now, even without a free slot or while
// the belt is paused.
// Lock: exported entry point; takes c.mu. Suspends: sessions.startNow.
func (c *Coordinator) StartNow(ctx context.Context, id string) {
	c.Lock()
	defer c.EndTurn()
	c.startNow(ctx, id)
}

// SetAfk turns global AFK on or off. Runs that follow it go with it.
// Lock: exported entry point; takes c.mu. Suspends: autopilot.cancelTurnedOff.
// Spawns: kick.
func (c *Coordinator) SetAfk(ctx context.Context, enabled bool) {
	c.Lock()
	defer c.EndTurn()
	c.changeSettings(func(s *store.Settings) { s.Afk.Enabled = enabled })
	if enabled {
		c.book.note("Global AFK is on: the "+core.AfkName+" decides for runs that follow it", nil)
	} else {
		c.book.note("Global AFK is off: runs that follow it wait for you", nil)
	}
	c.autopilot.cancelTurnedOff(ctx)
	c.kick()
}

// AfkDefault reports whether global AFK is on, which a run set to follow it goes with.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) AfkDefault(run core.Run) bool {
	c.Lock()
	defer c.EndTurn()
	return c.settings.Afk.Enabled
}

// SetRunAfk saves an active run's AFK mode, then lets it take effect.
// Lock: exported entry point; takes c.mu. Suspends: autopilot.cancelTurnedOff.
// Spawns: kick.
func (c *Coordinator) SetRunAfk(ctx context.Context, id string, mode core.AfkMode) {
	c.Lock()
	defer c.EndTurn()
	run := c.book.Run(id)
	if run == nil || !run.State.IsActive() {
		return
	}
	saved := c.book.update(id, nil, nil, func(r *core.Run) {
		r.SetAfk(mode)
		r.UpdatedAt = jsonx.Date{Time: c.clock.Now()}
	})
	if !saved {
		return
	}
	c.autopilot.cancelTurnedOff(ctx)
	c.kick()
}

// ReviewAfk marks the AFK decisions as looked over, or all of them when ids is nil.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ReviewAfk(ids map[string]struct{}) {
	c.Lock()
	defer c.EndTurn()
	c.autopilot.review(ids)
}

// Abort holds every abortable route, or only leg. Spawns: kick.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) Abort(id string, leg *string) {
	c.Lock()
	defer c.EndTurn()
	c.book.apply(id, core.Event{Kind: core.EventKindAbort, Leg: leg}, nil, nil, nil)
	c.kick()
}

// Drop ends the whole run, or only leg: "dropped by you" is why.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) Drop(id string, leg *string) {
	c.Lock()
	defer c.EndTurn()
	c.book.apply(id, core.Event{Kind: core.EventKindDrop, Reason: "dropped by you", Leg: leg}, nil, nil, nil)
}

// StartTask starts a task you wrote on a manual belt, in one of its repositories.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) StartTask(beltID, repo, title, body string, entry *string) (core.Run, error) {
	c.Lock()
	defer c.EndTurn()
	run, err := c.intake.startTask(beltID, repo, title, body, entry)
	if err != nil {
		return core.Run{}, err
	}
	c.kick()
	return run, nil
}

// SetPaused pauses or resumes a belt, and kicks on resume.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) SetPaused(beltID string, paused bool) error {
	c.Lock()
	defer c.EndTurn()
	if err := c.library.SetPaused(beltID, paused); err != nil {
		return err
	}
	if !paused {
		c.kick()
	}
	return nil
}

// SetBeltAfk saves a new version of the belt with only its AFK start changed; nil when unchanged.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) SetBeltAfk(beltID string, afk core.AfkMode) (*core.Belt, error) {
	c.Lock()
	defer c.EndTurn()
	record := c.library.Belt(beltID)
	if record == nil || record.Belt.Afk == afk {
		return nil, nil
	}
	belt := record.Belt.Clone()
	belt.Afk = afk
	saved, err := c.saveBelt(belt)
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// SaveBelt saves a belt as a new version. New runs use it; runs in progress keep theirs.
// Lock: exported entry point; takes c.mu. Spawns: kick.
func (c *Coordinator) SaveBelt(belt core.Belt) (core.Belt, error) {
	c.Lock()
	defer c.EndTurn()
	return c.saveBelt(belt)
}
