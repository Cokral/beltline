package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// staleContinue is an attempt off its stage's panel whose Continue session is alive, named
// by the ids a later pass finds it again with.
type staleContinue struct {
	runID, attemptID string
}

// stopStaleContinues stops the Continue sessions of attempts that no longer
// need one, rebuilt from the persisted attempts, including after a restart.
// Most attempts are stale, and a Continue session exists for few of them, so the pass asks the
// runner once which sessions are alive, when any attempt is stale, and only then names the stale
// attempts whose Continue session is among them, from the runs as they are after the query.
// Those are read again and ended, so a pass over many old attempts makes one tmux query per
// tick rather than one per attempt, and builds no name for an attempt without a Continue.
// Lock: runs with c.mu held. Suspends: runner.LiveSessionNames, runner.endContinuing.
func (s *Sessions) stopStaleContinues(ctx context.Context) {
	if !s.anyStaleAttempt(nil) {
		return
	}
	var live map[string]bool
	s.c.Suspend(func() { live = s.runner.LiveSessionNames(ctx) })
	continuing := map[string]bool{}
	for name, alive := range live {
		if session, ok := runner.TmuxContinuedSession(name); ok && alive {
			continuing[session] = true
		}
	}
	if len(continuing) == 0 {
		return
	}
	// EndContinuing does nothing when the Continue session is not running, so an attempt
	// whose Continue session is not in the list needs no call.
	var stale []staleContinue
	s.anyStaleAttempt(func(run *core.Run, attempt *core.Attempt) {
		if continuing[*attempt.Session] {
			stale = append(stale, staleContinue{runID: run.ID, attemptID: attempt.ID})
		}
	})
	for _, candidate := range stale {
		s.endStaleContinue(ctx, candidate)
	}
}

// anyStaleAttempt reports whether any run the pass looks after has an attempt with a session
// that is not its stage's current one, and calls each, when given, with every such attempt.
// It reads the book in place and neither copies a run nor suspends.
// Lock: runs with c.mu held.
func (s *Sessions) anyStaleAttempt(each func(run *core.Run, attempt *core.Attempt)) bool {
	found := false
	for i := range s.book.runs {
		run := &s.book.runs[i]
		if s.skipsRun(*run) {
			continue
		}
		current := core.AttemptPanelCurrentID(*run)
		for j := range run.Attempts {
			attempt := &run.Attempts[j]
			if attempt.Session == nil || (current != nil && *current == attempt.ID) {
				continue
			}
			if each == nil {
				return true
			}
			found = true
			each(run, attempt)
		}
	}
	return found
}

// endStaleContinue ends the Continue session of one stale attempt, if the attempt is still stale
// and has a spec when the pass reaches it: the runs may have moved on while earlier stops ran.
// Lock: runs with c.mu held. Suspends: runner.endContinuing.
func (s *Sessions) endStaleContinue(ctx context.Context, candidate staleContinue) {
	run := s.book.Run(candidate.runID)
	if run == nil || s.skipsRun(*run) {
		return
	}
	for _, attempt := range run.Attempts {
		if attempt.ID != candidate.attemptID {
			continue
		}
		if core.AttemptPanelBelongsToCurrentStage(*run, attempt) {
			return
		}
		if spec := s.spec(*run, attempt, ""); spec != nil {
			end := *spec
			s.c.Suspend(func() { s.runner.EndContinuing(ctx, end) })
		}
		return
	}
}

// check watches every running agent session: it reads its status, records the
// output, usage and identity, and finishes what has ended.
// Lock: runs with c.mu held. Suspends: runner.status, workspaces.fingerprint,
// workspaces.restore, runner.reply, runner.finish, runner.usage, runner.usageIdentity.
// Spawns: none.
func (s *Sessions) check(ctx context.Context) {
	running := func(run core.Run) bool { return runMayHaveLegIn(run, core.RunStateRunning) }
	for _, run := range runBookSnapshotWhere(s.book, running) {
		if !s.book.may(run, workAdvance) {
			continue
		}
		for _, leg := range run.WorkingLegs() {
			stage := leg.Stage(run)
			if leg.State != core.RunStateRunning || stage == nil || stage.Kind() != core.StageKindAgent {
				continue
			}
			attempt := s.attempt(run, leg)
			if attempt == nil {
				continue
			}
			spec := s.spec(run, *attempt, "")
			if spec == nil || s.busy(run, leg) {
				continue
			}
			if core.EngineStaleInterruptedTurn(run, &leg.ID) {
				continue
			}
			s.checkLeg(ctx, run, leg, *stage, *attempt, *spec)
		}
	}
}

// legCheck is one agent session being checked. It keeps the run's generation and
// operation from when the check began, so a turn that has since moved on stops it.
type legCheck struct {
	sessions   *Sessions
	run        core.Run
	leg        core.Leg
	stage      core.Stage
	attempt    core.Attempt
	spec       runner.SessionSpec
	generation int
	operation  int
}

// checkLeg finishes one running agent session that has ended. It is the body of the check loop.
// Lock: runs with c.mu held. Suspends: as check.
func (s *Sessions) checkLeg(ctx context.Context, run core.Run, leg core.Leg, stage core.Stage, attempt core.Attempt, spec runner.SessionSpec) {
	check := &legCheck{sessions: s, run: run, leg: leg, stage: stage, attempt: attempt, spec: spec, generation: run.Generation, operation: run.Operation}
	var status runner.SessionStatus
	s.c.Suspend(func() { status = s.runner.Status(ctx, spec) })
	if !check.current() {
		return
	}
	if status.Kind == runner.SessionStatusRunning {
		return
	}
	s.mark(run, leg, true)
	defer s.mark(run, leg, false)
	output := readSessionOutput(spec.Output)
	detail := statusDetail(status)
	if check.readOnlyChanged(ctx) {
		output = nil
		detail = new("the session changed its read-only checkout")
	}
	output = check.completeOutcome(ctx, output)
	if !check.current() {
		return
	}
	delete(s.fingerprints, check.fingerprintKey())
	s.c.Suspend(func() { s.runner.Finish(ctx, spec) })
	if !check.current() {
		return
	}
	detail = exitDetail(status, spec, detail)
	if output != nil && status.Kind == runner.SessionStatusFinished {
		detail = nil
	}
	usage, identity := check.readUsage(ctx)
	if !check.current() {
		return
	}
	if current := s.book.Run(run.ID); current == nil || current.Parking != nil {
		return
	}
	check.recordFinished(output, detail, usage, identity)
}

// readSessionOutput is the session's output file, or nil when it is missing or blank.
// Lock: none (pure apart from the read). Suspends: none.
func readSessionOutput(path string) *string {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	value := string(text)
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

// statusDetail says why a session that did not finish cleanly ended, from its status alone.
// Lock: none (pure).
func statusDetail(status runner.SessionStatus) *string {
	switch {
	case status.Kind == runner.SessionStatusFinished && status.ExitCode != nil && *status.ExitCode != 0:
		return new("the agent exited with code " + strconv.Itoa(int(*status.ExitCode)))
	case status.Kind == runner.SessionStatusGone:
		return new("the session ended before the agent finished")
	case status.Kind == runner.SessionStatusFailed:
		return new("the agent stopped: " + status.Message)
	}
	return nil
}

// exitDetail adds the last lines of the session log to the detail of a session that
// failed or exited with an error. Lock: none. Suspends: none.
func exitDetail(status runner.SessionStatus, spec runner.SessionSpec, detail *string) *string {
	exitedEarly := status.Kind == runner.SessionStatusFailed && spec.Interactive
	exitedWithError := status.Kind == runner.SessionStatusFinished && status.ExitCode != nil && *status.ExitCode != 0
	if !exitedEarly && !exitedWithError {
		return detail
	}
	// Headless runners, and interactive ones that exit before handing back,
	// print why they failed; keep the last lines.
	transcript, _ := os.ReadFile(filepath.Join(spec.AttemptFolder, "session.log"))
	message := lastTranscriptLines(string(transcript), 5)
	if message == "" {
		return detail
	}
	if exitedEarly {
		// An interface draws more than a headless turn prints: its end says why.
		chars := core.Characters(message)
		if len(chars) > 500 {
			message = "…" + strings.Join(chars[len(chars)-500:], "")
		}
		return new(*detail + ": " + message)
	}
	return new("the agent exited with code " + strconv.Itoa(int(*status.ExitCode)) + ": " + core.PrefixCharacters(message, 500))
}

// lastTranscriptLines is the last n non-blank lines of a session log, joined by newlines.
// Lock: none (pure).
func lastTranscriptLines(text string, n int) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return strings.Join(kept, "\n")
}

// current reports whether the run still has the generation and operation the check began with.
// Lock: runs with c.mu held. Suspends: none.
func (k *legCheck) current() bool {
	return k.sessions.book.accepts(k.run.ID, k.generation, &k.operation)
}

// fingerprintKey names the read-only checkout's fingerprint for this attempt.
// Lock: none (pure).
func (k *legCheck) fingerprintKey() string {
	return k.attempt.ID + k.run.ID
}

// baseline is the read-only checkout's fingerprint when the session started: the
// attempt's own, else the one this process recorded. Lock: runs with c.mu held.
func (k *legCheck) baseline() *string {
	if k.attempt.ReadOnlyBaseline != nil {
		return k.attempt.ReadOnlyBaseline
	}
	if value, ok := k.sessions.fingerprints[k.fingerprintKey()]; ok {
		return &value
	}
	return nil
}

// readOnlyChanged reports whether a read-only session changed its checkout, and
// restores the checkout when it did. Lock: runs with c.mu held.
// Suspends: workspaces.fingerprint, workspaces.restore.
func (k *legCheck) readOnlyChanged(ctx context.Context) bool {
	before := k.baseline()
	if !k.stage.Workspace.IsReadOnly() || before == nil {
		return false
	}
	var now string
	k.sessions.c.Suspend(func() { now = k.sessions.workspaces.Fingerprint(ctx, k.spec.Directory) })
	if now == *before {
		return false
	}
	k.sessions.c.Suspend(func() { k.sessions.workspaces.Restore(ctx, k.spec.Directory) })
	return true
}

// completeOutcome writes the Outcome line from the agent's reply into the output file
// when the file lacks one, and returns the output to record. Lock: runs with c.mu held.
// Suspends: runner.reply.
func (k *legCheck) completeOutcome(ctx context.Context, output *string) *string {
	// An agent that put its Outcome line in its reply and not the file gets it
	// added to the file, where later stages read it.
	if output == nil || core.OutcomeParserParse(*output, k.stage.Outcomes) != nil {
		return output
	}
	var reply *string
	k.sessions.c.Suspend(func() { reply = k.sessions.runner.Reply(ctx, k.spec) })
	if reply == nil {
		return output
	}
	completed := core.OutcomeParserCompleting(*output, *reply, k.stage.Outcomes)
	if completed == nil || !k.current() {
		return output
	}
	_ = store.WriteFileAtomic(k.spec.Output, []byte(*completed))
	return completed
}

// readUsage reads the session's usage and usage identity.
// Lock: runs with c.mu held. Suspends: runner.usage, runner.usageIdentity.
func (k *legCheck) readUsage(ctx context.Context) (*core.Usage, *string) {
	var usage *core.Usage
	var identity *string
	k.sessions.c.Suspend(func() { usage = k.sessions.runner.Usage(ctx, k.spec) })
	k.sessions.c.Suspend(func() { identity = k.sessions.runner.UsageIdentity(ctx, k.spec) })
	return usage, identity
}

// recordFinished applies the session's end to its run and stores the attempt's usage
// and usage identity. Lock: runs with c.mu held. Spawns: none.
func (k *legCheck) recordFinished(output, detail *string, usage *core.Usage, identity *string) {
	legID := k.leg.ID
	finished := core.Event{Kind: core.EventKindSessionFinished, Output: output, Detail: detail, Leg: &legID}
	if k.attempt.Session != nil {
		finished.Session = *k.attempt.Session
	}
	k.sessions.book.apply(k.run.ID, finished, &k.generation, &k.operation, func(r *core.Run) {
		for index := range r.Attempts {
			if r.Attempts[index].ID != k.attempt.ID {
				continue
			}
			r.Attempts[index].Usage = core.PauseSpendMerge(r.Attempts[index].Usage, usage, core.PauseSpendKindCumulative)
			if identity != nil {
				checkpoint := core.UsageCheckpoint{}
				if existing := r.Attempts[index].UsageCheckpoint; existing != nil {
					checkpoint = *existing
				}
				checkpoint.SessionID = identity
				r.Attempts[index].UsageCheckpoint = &checkpoint
			}
			return
		}
	})
}
