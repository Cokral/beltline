package coordinator

import (
	"reflect"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// review marks decisions as looked over, or all of them when ids is nil.
// Lock: part of Coordinator; runs with c.mu held.
func (a *Autopilot) review(ids map[string]struct{}) {
	before := append([]core.AfkDecision(nil), a.decisions...)
	for index := range a.decisions {
		if !a.decisions[index].NeedsReview() {
			continue
		}
		if ids != nil {
			if _, chosen := ids[a.decisions[index].ID]; !chosen {
				continue
			}
		}
		a.decisions[index].Reviewed = true
	}
	if !reflect.DeepEqual(before, a.decisions) {
		a.decisionsRevision++
		a.save()
	}
}

// projection is the AFK change projection: the decision log, encoded at the
// decisions' revision. Lock: runs with c.mu held.
func (a *Autopilot) projection() []byte {
	return a.decisionsMemo.of("afk", a.decisionsRevision, func() []byte {
		return projectionBytes(a.decisions)
	})
}

// add appends a decision and saves the log.
// Lock: runs with c.mu held.
func (a *Autopilot) add(decision core.AfkDecision) {
	a.decisions = append(a.decisions, decision)
	a.decisionsRevision++
	a.save()
}

// change edits the decision with id and saves the log.
// Lock: runs with c.mu held.
func (a *Autopilot) change(id string, change func(decision *core.AfkDecision)) {
	for index := range a.decisions {
		if a.decisions[index].ID == id {
			change(&a.decisions[index])
			a.decisionsRevision++
			a.save()
			return
		}
	}
}

// removeDecisions drops every decision for runID and saves the log. The
// in-memory list changes only after the save succeeds.
// Lock: runs with c.mu held.
func (a *Autopilot) removeDecisions(runID string) error {
	var kept []core.AfkDecision
	for _, decision := range a.decisions {
		if decision.RunID != runID {
			kept = append(kept, decision)
		}
	}
	if err := a.storage.SaveAfkDecisions(kept); err != nil {
		return err
	}
	a.decisions = kept
	a.decisionsRevision++
	return nil
}

// save writes the decision log to disk.
// Lock: runs with c.mu held.
func (a *Autopilot) save() {
	if err := a.storage.SaveAfkDecisions(a.decisions); err != nil {
		a.book.note("Could not save the "+core.AfkName+"'s decisions: "+err.Error(), nil)
	}
}

// recordOverrule writes the overrule onto the decision. The in-memory log changes
// only after the save succeeds.
// Lock: runs with c.mu held.
func (a *Autopilot) recordOverrule(overrule core.AfkOverrule, id string) error {
	index := -1
	for i := range a.decisions {
		if a.decisions[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return core.NewFailure(core.AfkDecisionGone)
	}
	if current := a.decisions[index].Overrule; current != nil && reflect.DeepEqual(*current, overrule) {
		return nil
	}
	next := append([]core.AfkDecision(nil), a.decisions...)
	copied := overrule
	next[index].Overrule = &copied
	if err := a.storage.SaveAfkDecisions(next); err != nil {
		return err
	}
	a.decisions = next
	a.decisionsRevision++
	return nil
}

// overruleRecord returns the overrule recorded on the run's decision, or nil.
// Lock: runs with c.mu held.
func (a *Autopilot) overruleRecord(run core.Run) *core.AfkOverrule {
	if run.Restart == nil || (run.Restart.Phase != core.RestartIntentPhaseApplied && run.Restart.Phase != core.RestartIntentPhaseHeld) {
		return nil
	}
	intent := *run.Restart
	stageName := intent.StageID
	for _, stage := range run.Stages {
		if stage.ID == intent.StageID {
			stageName = stage.Name
			break
		}
	}
	var routeName *string
	if way := run.Layout().Place(intent.StageID); way != nil {
		name := way.Route.Name
		routeName = &name
	}
	phase := core.AfkOverrulePhaseRestarted
	if intent.Phase == core.RestartIntentPhaseHeld {
		phase = core.AfkOverrulePhaseHeld
	}
	return &core.AfkOverrule{
		Comments: intent.Comments, StageID: intent.StageID, StageName: stageName, RouteName: routeName,
		At: intent.At, AttemptID: intent.AttemptID, Phase: phase, Detail: intent.Detail,
	}
}

// newID returns a new lower-case decision id (a UUID, lower-cased).
// drawn from the coordinator's IDs through the back pointer).
// Lock: runs with c.mu held.
func (a *Autopilot) newID() string { return strings.ToLower(a.c.ids.UUID()) }

// decision returns a copy of the decision with id, or nil.
// Lock: runs with c.mu held.
func (a *Autopilot) decision(id string) *core.AfkDecision { return a.Decision(&id) }

// nowDate is the clock's time as the log's date.
// Lock: none (pure).
func (a *Autopilot) nowDate() *jsonx.Date {
	date := jsonx.Date{Time: a.clock.Now()}
	return &date
}
