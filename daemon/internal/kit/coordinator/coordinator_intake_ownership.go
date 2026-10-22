// Which entry owns a run and which belt it checks against: the admission and
// provenance rules the intake uses on every poll.

package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// beltAdmits reports whether a new run may join: false when the belt is paused,
// archived, or gone, including when that changed while a check was reading.
// Lock: runs with c.mu held.
func (i *Intake) beltAdmits(record store.BeltRecord) bool {
	live := i.library.Belt(record.Belt.ID)
	return live != nil && !live.EffectivelyPaused()
}

// owns reports whether run was admitted by entry. A run saved without a source
// takes one from the belt version it started on.
// Lock: runs with c.mu held.
func (i *Intake) owns(run core.Run, entry core.TriggerEntry) bool {
	prov := i.provenance(run)
	return prov != nil && prov.Entry == entry.ID
}

// sourceBelt is the belt the run's source checks against: the saved entry, or the live one.
// Lock: runs with c.mu held.
func (i *Intake) sourceBelt(run core.Run, entry core.TriggerEntry, belt core.Belt) core.Belt {
	snapshot := i.provenance(run)
	if snapshot == nil {
		snapshot = &core.RunSource{Entry: entry.ID, Trigger: entry.Trigger, Repos: entry.Repos}
	}
	return belt.Focusing(core.TriggerEntry{ID: snapshot.Entry, Trigger: snapshot.Trigger, Repos: snapshot.Repos})
}

// provenance returns the entry that owns run, recovered from its belt version
// when the run predates sources, or nil.
// Lock: runs with c.mu held.
func (i *Intake) provenance(run core.Run) *core.RunSource {
	var source *core.RunSource
	if live := i.book.Run(run.ID); live != nil {
		source = live.Source
	}
	if source == nil {
		source = run.Source
	}
	if source != nil {
		return source
	}
	recovered := i.recoverSource(run)
	if recovered == nil {
		return nil
	}
	i.book.update(run.ID, nil, nil, func(r *core.Run) {
		if r.Source == nil {
			value := *recovered
			r.Source = &value
		}
	})
	return recovered
}

// recoverSource returns the single trigger of the belt version the run started
// on, or nil.
// Lock: runs with c.mu held.
func (i *Intake) recoverSource(run core.Run) *core.RunSource {
	belt := i.library.storage.Belt(run.BeltID, run.BeltVersion)
	if belt == nil {
		if record := i.library.Belt(run.BeltID); record != nil {
			belt = &record.Belt
		}
	}
	if belt == nil {
		return nil
	}
	return core.RunSourceOf(*belt, run.Subject.Kind)
}
