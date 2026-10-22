package coordinator

import (
	"context"
	"path/filepath"
	"slices"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// Sessions are the agent stages: it starts sessions within each stage's slots,
// watches them, and reports what they wrote to the run book. A part of
// Coordinator whose methods run with c.mu held; each await is a c.Suspend.
type Sessions struct {
	// c is the coordinator the sessions belong to (the back pointer).
	c          *Coordinator
	book       *RunBook
	library    *Library
	runner     runner.AgentRunner
	workspaces runner.Workspaces
	github     github.GitHub
	paths      store.Paths
	clock      Clock
	// currentAdmission reports the allowed models now.
	currentAdmission func() core.ModelAdmission
	// random draws between a stage's equally busy models; tests give it a seed.
	random core.PoolRandom
	// globallyPaused: while the app's one global pause is on, no session is
	// started, even by an explicit Start now.
	globallyPaused bool
	// fingerprints are read-only folders' state when their session started.
	fingerprints map[string]string
	// bases resolves each leg's base branch.
	bases *Bases
	// continuing holds the runs whose Continue launch is in `has-session` or
	// `new-session`. Erasure waits for these.
	continuing map[string]struct{}
}

// newSessions builds the sessions part.
// Lock: runs during New, before the coordinator is shared.
func newSessions(c *Coordinator, book *RunBook, library *Library, runner runner.AgentRunner,
	workspaces runner.Workspaces, github github.GitHub, paths store.Paths, clock Clock) Sessions {
	bases := newBases(c, github)
	return Sessions{
		c: c, book: book, library: library, runner: runner, workspaces: workspaces, github: github,
		paths: paths, clock: clock,
		currentAdmission: func() core.ModelAdmission { return c.currentModelAdmission() },
		random:           core.PoolRandomSystem(),
		fingerprints:     map[string]string{}, bases: &bases, continuing: map[string]struct{}{},
	}
}

// spec returns the session spec of an agent attempt, rebuilt from the run. Each
// leg of a split run works in its own folder, on its own branch.
// Lock: runs with c.mu held.
func (s *Sessions) spec(run core.Run, attempt core.Attempt, prompt string) *runner.SessionSpec {
	stage := findStage(run, attempt.StageID)
	if stage == nil || attempt.Session == nil {
		return nil
	}
	session := *attempt.Session
	leg := run.LegByID(attempt.Leg)
	settings := attemptSettingsHistorical(run, attempt, *stage)
	if settings == nil {
		return nil
	}
	backend := core.ValueOr(settings.Runner, core.StageAdjustBackend(*stage))
	folder := s.paths.AttemptFolder(run, attempt)
	model := settings.Model
	if run.ModelAdmission != nil {
		model = run.ModelAdmission.RunnerModel(settings.Model, settings.Thinking, backend)
	}
	sessionID := core.ValueOr(attempt.SessionID, session)
	var legID *string
	if leg != nil {
		legID = &leg.ID
	}
	var agentID *string
	if stage.Agent != nil {
		id := stage.Agent.ID
		agentID = &id
	}
	logMark, usageMark := 0, 0
	if attempt.LogMark != nil {
		logMark = *attempt.LogMark
	}
	if attempt.UsageMark != nil {
		usageMark = *attempt.UsageMark
	}
	completionToken := core.ValueOf(attempt.CompletionToken)
	spec := runner.NewSessionSpec(runner.SessionSpecArgs{
		Name: session, SessionID: sessionID, Backend: backend, Model: model, Thinking: settings.Thinking, Prompt: prompt,
		Directory:     runner.WorkspaceCheckoutPath(s.paths, run, sessionsCheckout(run, *stage), legID),
		AttemptFolder: folder, Output: filepath.Join(folder, stage.Writes()), Outcomes: stageOutcomeNames(*stage), AgentID: agentID,
		Resume: attempt.Continues != nil, AgentName: attempt.AgentSessionName(), LogMark: logMark, UsageMark: usageMark,
		UsageCheckpoint: attempt.UsageCheckpoint, Interactive: attempt.Interactive(backend), CompletionToken: completionToken,
	})
	return &spec
}

// stageOutcomeNames are the names of a stage's outcomes, in order.
// Lock: none (pure).
func stageOutcomeNames(stage core.Stage) []string {
	names := make([]string, 0, len(stage.Outcomes))
	for _, outcome := range stage.Outcomes {
		names = append(names, outcome.Name)
	}
	return names
}

// attempt returns the open attempt of a leg at its stage, or nil.
// Lock: runs with c.mu held.
func (s *Sessions) attempt(run core.Run, leg core.Leg) *core.Attempt {
	stageID := core.ValueOr(leg.StageID, run.StageID)
	want := core.AttemptKey(stageID, &leg.ID)
	for index := len(run.Attempts) - 1; index >= 0; index-- {
		attempt := run.Attempts[index]
		if core.AttemptKey(attempt.StageID, attempt.Leg) == want && attempt.IsOpen() {
			found := attempt.Clone()
			return &found
		}
	}
	return nil
}

// skipsRun reports whether a run's sessions are not checked, started or
// continued: the book bars checking it.
// Lock: runs with c.mu held.
func (s *Sessions) skipsRun(run core.Run) bool {
	return !s.book.may(run, workCheck)
}

// busy reports whether run or the leg is busy with a whole-run operation: work on
// the whole run keeps every leg waiting.
// Lock: runs with c.mu held.
func (s *Sessions) busy(run core.Run, leg core.Leg) bool {
	if _, ok := s.book.busy[run.ID]; ok {
		return true
	}
	_, ok := s.book.busy[run.ID+"#"+leg.ID]
	return ok
}

// mark records that run or its leg is busy.
// Lock: runs with c.mu held.
func (s *Sessions) mark(run core.Run, leg core.Leg, busy bool) {
	key := run.ID + "#" + leg.ID
	if busy {
		s.book.busy[key] = struct{}{}
	} else {
		delete(s.book.busy, key)
	}
}

// legsAtStage counts the legs that hold a slot of stageID on the belt, across its runs.
// Lock: runs with c.mu held.
func (s *Sessions) legsAtStage(beltID, stageID string) int {
	count := 0
	for _, other := range s.book.runs {
		if other.BeltID != beltID {
			continue
		}
		for _, working := range other.WorkingLegs() {
			if working.StageID != nil && *working.StageID == stageID && core.EngineOccupiesSlot(other, working) {
				count++
			}
		}
	}
	return count
}

// startQueued starts every queued agent stage that has a free slot.
// Lock: runs with c.mu held. Suspends: start for each run.
func (s *Sessions) startQueued(ctx context.Context) {
	queued := func(run core.Run) bool { return runMayHaveLegIn(run, core.RunStateQueued) }
	for _, run := range runBookSnapshotWhere(s.book, queued) {
		if !s.book.may(run, workAdvance) {
			continue
		}
		// A route whose branches are still being merged waits for the merge.
		// Another route of the same run keeps working.
		for _, working := range run.WorkingLegs() {
			id := working.ID
			if len(run.MergeBranches) > 0 && (run.MergeLeg == nil || *run.MergeLeg == id) {
				continue
			}
			if mergeWaitingOn(run, id) {
				continue
			}
			// Read the leg again: an earlier one may have started by now.
			fresh := s.book.Run(run.ID)
			if fresh == nil {
				continue
			}
			leg := fresh.LegByID(&id)
			if leg == nil || leg.State != core.RunStateQueued {
				continue
			}
			stage := leg.Stage(run)
			if stage == nil || stage.Kind() != core.StageKindAgent {
				continue
			}
			if live := s.library.Belt(run.BeltID); live != nil && live.EffectivelyPaused() {
				continue
			}
			// A stage's slots count legs, so one run taking two routes of
			// the same stage needs room for both.
			if s.legsAtStage(run.BeltID, stage.ID) >= max(stage.Slots, 1) {
				continue
			}
			s.start(ctx, run.ID, &id)
		}
	}
}

// mergeWaitingOn reports whether a join's merge is waiting on leg id.
// Lock: none (pure).
func mergeWaitingOn(run core.Run, id string) bool {
	return slices.ContainsFunc(run.MergesWaiting, func(merge core.Merge) bool {
		return merge.Leg != nil && *merge.Leg == id
	})
}

// startNow starts a queued run now, past its stage's slots and a paused belt.
// Lock: runs with c.mu held. Suspends: start.
func (s *Sessions) startNow(ctx context.Context, id string) {
	if s.globallyPaused {
		return
	}
	run := s.book.Run(id)
	if run == nil || (run.Pause != nil && run.Pause.BlocksProgress()) || len(run.MergeBranches) > 0 {
		return
	}
	var queued *core.Leg
	for _, leg := range run.WorkingLegs() {
		if leg.State == core.RunStateQueued {
			found := leg
			queued = &found
			break
		}
	}
	if queued == nil {
		return
	}
	if stage := queued.Stage(*run); stage == nil || stage.Kind() != core.StageKindAgent {
		return
	}
	legID := queued.ID
	s.start(ctx, id, &legID)
}

// runBookSnapshot is a copy of the book's live runs, for loops that suspend while they read
// it: the loop iterates a copy of `book.runs`, and erasure may reuse the slice's array.
// Lock: runs with c.mu held.
func runBookSnapshot(book *RunBook) []core.Run {
	out := make([]core.Run, 0, len(book.runs))
	for _, run := range book.runs {
		out = append(out, run.Clone())
	}
	return out
}

// runBookSnapshotWhere is runBookSnapshot for the runs keep accepts. keep reads the runs
// as they are now, so it is evaluated at the snapshot's instant, and a run it drops is one
// the pass would skip: keep must accept every run the pass acts on, and may accept more.
// Each tick runs several passes over the book, most of which act on a handful of runs, and
// a clone is the costly part, so only the runs a pass can act on are cloned.
// Lock: runs with c.mu held.
func runBookSnapshotWhere(book *RunBook, keep func(core.Run) bool) []core.Run {
	out := make([]core.Run, 0)
	for _, run := range book.runs {
		if keep(run) {
			out = append(out, run.Clone())
		}
	}
	return out
}

// runBookCopiesWhere is a copy of the runs keep accepts, as they are now, for a pass that
// suspends while it reads them: the loop iterates a copy of `book.runs`. Unlike
// runBookSnapshotWhere it copies each run's fields and shares their slices and pointers with
// the book, as a copy of the slice does, so a pass that reads its runs, and changes them only
// through the book, pays for neither the whole book nor a deep copy. keep is evaluated as for
// runBookSnapshotWhere.
// Lock: runs with c.mu held.
func runBookCopiesWhere(book *RunBook, keep func(core.Run) bool) []core.Run {
	var out []core.Run
	for i := range book.runs {
		if keep(book.runs[i]) {
			out = append(out, book.runs[i])
		}
	}
	return out
}

// runMayHaveLegIn reports whether run is in state or has a leg in it. Every leg WorkingLegs
// returns is one of run's legs, or the run itself when it is not split, so this holds for
// every run with a working leg in state.
// Lock: none (pure).
func runMayHaveLegIn(run core.Run, state core.RunState) bool {
	if run.State == state {
		return true
	}
	return slices.ContainsFunc(run.Legs, func(leg core.Leg) bool { return leg.State == state })
}

// runHasQueuedContinuation reports whether an attempt of run has a Continue queued.
// Lock: none (pure).
func runHasQueuedContinuation(run core.Run) bool {
	return slices.ContainsFunc(run.Attempts, func(attempt core.Attempt) bool {
		return attempt.Interruption != nil && attempt.Interruption.Continuation != nil &&
			attempt.Interruption.Continuation.Status == core.AttemptInterruptionContinuationStatusQueued
	})
}
