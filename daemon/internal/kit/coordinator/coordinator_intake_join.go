// The intake's joins: a trigger's subject starts a run, or joins the run already on it.

package coordinator

import (
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// join starts a run on subject from entry.
// Lock: runs with c.mu held.
func (i *Intake) join(record store.BeltRecord, subject core.Subject, entry *core.TriggerEntry) (core.Run, error) {
	id := core.RunID(record.Belt.ID, subject)
	name := record.Belt.Name
	if i.book.isErasing(id) {
		return core.Run{}, core.NewFailure("Cannot start " + name + ": a run for " + subject.Ref() + " is still being erased.")
	}
	if i.book.Run(id) != nil {
		return core.Run{}, core.NewFailure("Cannot start " + name + ": a run for " + subject.Ref() + " already exists.")
	}
	if warnings := i.library.Warnings(record.Belt); len(warnings) > 0 {
		return core.Run{}, core.NewFailure("Cannot start " + name + ": resolve its warnings before starting a run. " + strings.Join(warnings, " "))
	}
	// During a shadow test the run starts on the version with fewer of the test's runs.
	belt, shadow := i.library.startDefinition(record, i.book.runs)
	snapshot := i.admission()
	var errs []string
	for _, stage := range belt.Stages {
		if stage.Kind() != core.StageKindAgent {
			continue
		}
		for _, settings := range stage.Pool() {
			var warning *string
			if i.currentModelWarning != nil {
				warning = i.currentModelWarning(settings, stage.Name)
			}
			if warning == nil {
				backend := core.ValueOr(settings.Runner, core.BackendPi)
				warning = core.ModelPolicyWarning(settings.Model, backend, stage.Name, snapshot, nil)
			}
			if warning != nil {
				errs = append(errs, *warning)
			}
		}
	}
	if len(errs) > 0 {
		return core.Run{}, core.NewFailure("Cannot start " + name + ": " + strings.Join(errs, " "))
	}
	definition := core.StageKeysExplicit(belt, i.library.pinnedAgents(belt))
	run := core.NewRun(core.RunArgs{Belt: definition, Subject: subject, Now: i.clock.Now(), Entry: entry})
	run.Shadow = shadow
	run.Agents = i.library.snapshots(run)
	run.ModelAdmission = &snapshot
	saved := i.book.add(run)
	if i.book.Run(saved.ID) == nil {
		return core.Run{}, core.NewFailure("Cannot start " + name + ": could not persist model admission. Check storage permissions.")
	}
	runID := saved.ID
	i.book.note(subject.Ref()+" "+subject.Title+" joined "+name, &runID)
	return saved, nil
}

// activeRun returns the run on this belt still working on the same issue, pull
// request or ticket, or nil.
// Lock: runs with c.mu held.
func (i *Intake) activeRun(beltID string, subject core.Subject) *core.Run {
	about := subject.About()
	if about == nil {
		return nil
	}
	for _, run := range i.book.runs {
		if run.BeltID != beltID || !run.State.IsActive() {
			continue
		}
		runAbout := run.Subject.About()
		if runAbout == nil || *runAbout != *about {
			continue
		}
		if subject.Kind == core.SubjectKindPullRequest && !optionalEqualFold(run.Subject.Reviewer, subject.Reviewer) {
			continue
		}
		found := run.Clone()
		return &found
	}
	return nil
}

// optionalEqualFold reports whether two optional texts are the same ignoring case, nil only equal to nil.
// Lock: none (pure).
func optionalEqualFold(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return strings.EqualFold(*a, *b)
}
