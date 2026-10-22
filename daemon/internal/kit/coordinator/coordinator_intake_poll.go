// One place's check of its source: the runs it starts, the runs it tells what
// changed and the runs it asks after.

package coordinator

import (
	"context"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// intakeStamp is a run's generation and operation as they were before a check read its source.
type intakeStamp struct{ generation, operation int }

// intakeStamps maps run IDs to the stamps captured for one belt.
type intakeStamps map[string]intakeStamp

// of returns the run's captured stamp, or its current values for a run the capture did not see.
func (s intakeStamps) of(run core.Run) intakeStamp {
	if st, ok := s[run.ID]; ok {
		return st
	}
	return intakeStamp{generation: run.Generation, operation: run.Operation}
}

// intakePass is one check of one place: the belt focused on the entry it reads,
// the source, the stamps taken before the read, and what the check has answered
// and found waiting so far.
type intakePass struct {
	record   store.BeltRecord
	belt     core.Belt
	entry    core.TriggerEntry
	source   Source
	scope    string
	stamps   intakeStamps
	answered map[string]struct{}
	waiting  []core.WaitingSubject
}

// poll checks one place: it starts runs on new work, tells runs already on the
// belt what changed, and asks after the ones no longer listed.
// Lock: runs with c.mu held. Suspends: source.list, source.blockers,
// source.prepare, source.missing.
func (i *Intake) poll(ctx context.Context, record store.BeltRecord, entry core.TriggerEntry, source Source, scope string) {
	belt := record.Belt.Focusing(entry)
	// Captured before the read, so an edit that was already in flight when
	// the run was parked refreshes the subject and does not retriage it.
	pass := &intakePass{
		record:   record,
		belt:     belt,
		entry:    entry,
		source:   source,
		scope:    scope,
		stamps:   i.stampsOn(belt.ID),
		answered: map[string]struct{}{},
	}
	var listed []core.Subject
	var err error
	i.c.Suspend(func() { listed, err = source.list(ctx, scope, belt) })
	if err != nil {
		i.book.note("Could not check "+source.place(scope, belt)+": "+err.Error(), nil)
		return
	}
	waitingKey := record.Belt.ID + "|" + entry.ID + "|" + scope
	defer func() { i.setWaiting(waitingKey, pass.waiting) }()
	for _, subject := range listed {
		i.pollSubject(ctx, pass, subject)
	}
	i.pollGone(ctx, pass)
}

// stampsOn captures the stamps of every run on beltID.
// Lock: runs with c.mu held.
func (i *Intake) stampsOn(beltID string) intakeStamps {
	stamps := intakeStamps{}
	for _, run := range i.book.runs {
		if run.BeltID == beltID {
			stamps[run.ID] = intakeStamp{generation: run.Generation, operation: run.Operation}
		}
	}
	return stamps
}

// pollSubject handles one listed subject: a run that already answers it is
// told what the source saw, and a subject with no run may start one.
// Lock: runs with c.mu held. Suspends: as startListed.
func (i *Intake) pollSubject(ctx context.Context, pass *intakePass, subject core.Subject) {
	if active := i.activeRun(pass.belt.ID, subject); active != nil {
		// Another entry already owns this subject. It keeps the run.
		if !i.owns(*active, pass.entry) {
			return
		}
		// The run keeps what it was started with, such as the review request it answers.
		subject.Repo = active.Subject.Repo
		subject.Occurrence = active.Subject.Occurrence
		subject.Reviewer = active.Subject.Reviewer
		i.tellListed(pass, subject, *active)
		return
	}
	listedID := core.RunID(pass.record.Belt.ID, subject)
	if i.book.isErasing(listedID) {
		return
	}
	if existing := i.book.Run(listedID); existing != nil {
		if i.owns(*existing, pass.entry) {
			i.tellListed(pass, subject, *existing)
		}
		return
	}
	i.startListed(ctx, pass, subject)
}

// tellListed marks an owned run as answering a listed subject and tells it what its source saw.
// Lock: runs with c.mu held. Spawns: none.
func (i *Intake) tellListed(pass *intakePass, subject core.Subject, run core.Run) {
	pass.answered[run.ID] = struct{}{}
	st := pass.stamps.of(run)
	i.tell(run.ID, pass.source.fate(subject, i.sourceBelt(run, pass.entry, pass.record.Belt)), st.generation, st.operation, false)
}

// startListed starts a run on a listed subject that no run answers yet, or
// records the subject as waiting while a blocker is open.
// Lock: runs with c.mu held. Suspends: source.blockers, source.prepare.
func (i *Intake) startListed(ctx context.Context, pass *intakePass, subject core.Subject) {
	// The record this check started with stays open if pause or archive
	// lands while the list is in flight. Read the live belt.
	if !i.beltAdmits(pass.record) || !pass.source.admits(subject) {
		return
	}
	if pass.belt.Policies.WaitForBlockers {
		var blockers []string
		var err error
		i.c.Suspend(func() { blockers, err = pass.source.blockers(ctx, subject) })
		if err != nil {
			i.book.note("Could not read what blocks "+subject.Ref()+": "+err.Error(), nil)
			return
		}
		if len(blockers) > 0 {
			pass.waiting = append(pass.waiting, core.WaitingSubject{BeltID: pass.belt.ID, Subject: subject, Reason: "waiting on " + strings.Join(blockers, ", ")})
			return
		}
	}
	var prepared core.Subject
	var err error
	i.c.Suspend(func() { prepared, err = pass.source.prepare(ctx, subject, pass.belt) })
	if err != nil {
		i.book.note(err.Error(), nil)
		return
	}
	subject = prepared
	// Already answered, or an erasure of this identity has not finished: only a later check starts another run.
	preparedID := core.RunID(pass.belt.ID, subject)
	if i.book.isErasing(preparedID) || i.book.Run(preparedID) != nil {
		return
	}
	// Blocker and prepare awaits can outlast the check above. A belt
	// paused, archived or newly warned in that gap does not gain a run.
	live := i.library.Belt(pass.belt.ID)
	if live == nil || live.EffectivelyPaused() {
		return
	}
	joined, err := i.join(*live, subject, &pass.entry)
	if err != nil {
		text := err.Error()
		if previous, ok := i.refused[pass.belt.ID]; !ok || previous != text {
			i.book.note(text, nil)
		}
		i.refused[pass.belt.ID] = text
		return
	}
	pass.answered[joined.ID] = struct{}{}
	delete(i.refused, pass.belt.ID)
}

// pollGone asks the source about each run on this entry's list that the list no
// longer shows, unless the run is busy, being erased, or answered by this check.
// Lock: runs with c.mu held. Suspends: source.missing.
func (i *Intake) pollGone(ctx context.Context, pass *intakePass) {
	for _, run := range i.runsSnapshot() {
		if run.BeltID != pass.record.Belt.ID || !i.owns(run, pass.entry) || !run.State.IsActive() || !pass.source.watches(run, pass.scope) {
			continue
		}
		if _, busy := i.book.busy[run.ID]; busy || i.book.isErasing(run.ID) {
			continue
		}
		if _, seen := pass.answered[run.ID]; seen {
			continue
		}
		generation, operation := run.Generation, run.Operation
		runID := run.ID
		sourceBelt := i.sourceBelt(run, pass.entry, pass.record.Belt)
		var fate Fate
		var err error
		i.c.Suspend(func() { fate, err = pass.source.missing(ctx, run, sourceBelt) })
		if err != nil {
			i.book.note("Could not check "+run.Subject.Ref()+": "+err.Error(), &runID)
			continue
		}
		i.tell(run.ID, fate, generation, operation, false)
	}
}
