// The problems that would reject a belt draft request, from problems(in:)
// and located(in:), the checks that attach them to fields.
// The located list is split into one helper per family of operations; each
// helper records the problems of its requests in order, and the request's
// location is attached to every one of them.

package core

import (
	"fmt"
)

// draftProblems collects the located problems of one request in the order the
// checks run.
type draftProblems struct {
	list []LocatedProblem
	at   *ProblemLocation
}

// needs records message at the request's location unless ok holds.
func (p *draftProblems) needs(ok bool, message string) {
	if !ok {
		p.add(message)
	}
}

// add records message at the request's location.
func (p *draftProblems) add(message string) {
	p.list = append(p.list, LocatedProblem{Message: message, Location: p.at})
}

// BeltDraftEditingLocated is the same problems, each with the stage, fork, route
// or trigger it is about.
func BeltDraftEditingLocated(request BeltDraftRequest) []LocatedProblem {
	p := &draftProblems{at: beltDraftEditingLocation(request)}
	switch request.Op {
	case BeltDraftOperationStageAdd, BeltDraftOperationStageRemove, BeltDraftOperationStageMove, BeltDraftOperationStageUpdate:
		stageDraftProblems(p, request)
	case BeltDraftOperationForkAdd, BeltDraftOperationForkRemove, BeltDraftOperationRouteAdd,
		BeltDraftOperationRouteRemove, BeltDraftOperationRouteUpdate, BeltDraftOperationFeedInto:
		flowDraftProblems(p, request)
	case BeltDraftOperationGotoPick, BeltDraftOperationGotoRoute:
		gotoDraftProblems(p, request)
	case BeltDraftOperationTriggerAdd, BeltDraftOperationTriggerRemove, BeltDraftOperationTriggerMove,
		BeltDraftOperationTriggerUpdate, BeltDraftOperationTriggerKind:
		triggerDraftProblems(p, request)
	case BeltDraftOperationRepositoryAdd, BeltDraftOperationRepositoryRemove:
		repositoryDraftProblems(p, request)
	case BeltDraftOperationPoliciesUpdate:
		p.needs(request.Policies != nil, "policies-update needs policies.")
	default:
		// Unknown operations are problems, so a request with one never applies as a no-op.
		p.add(string(request.Op) + " is not a belt draft operation.")
	}
	return p.list
}

// stageDraftProblems checks stage-add, stage-remove, stage-move and stage-update.
func stageDraftProblems(p *draftProblems, request BeltDraftRequest) {
	d := request.Draft
	switch request.Op {
	case BeltDraftOperationStageAdd:
		if request.Stage == nil {
			p.add("stage-add needs a stage.")
			return
		}
		p.needs(beltDraftEditingValidStageID(request.Stage.ID), "stage-add needs a stage id.")
		p.needs(!draftHasStage(d, request.Stage.ID), fmt.Sprintf("stage-add: a stage with id %s is already on the belt.", request.Stage.ID))
		if request.Index != nil {
			p.needs(*request.Index >= 0 && *request.Index <= len(d.Stages), fmt.Sprintf("stage-add: index %d is outside the stage list.", *request.Index))
		}
		if request.Route != nil {
			p.needs(draftHasRoute(d, *request.Route), fmt.Sprintf("stage-add: route %s is not on the belt.", *request.Route))
		}
	case BeltDraftOperationStageRemove:
		p.needs(draftHasOptional(d, request.StageID, draftHasStage), "stage-remove needs a stage id that is on the belt.")
	case BeltDraftOperationStageMove:
		p.needs(draftHasOptional(d, request.StageID, draftHasStage), "stage-move needs a stage id that is on the belt.")
		forms := 0
		for _, present := range []bool{request.TargetID != nil, request.Step != nil, draftIsTrue(request.Detach) || request.Route != nil} {
			if present {
				forms++
			}
		}
		p.needs(forms == 1, "stage-move needs exactly one of targetId, step, route or detach.")
		if request.TargetID != nil {
			target := *request.TargetID
			p.needs(draftHasStage(d, target), fmt.Sprintf("stage-move: target %s is not on the belt.", target))
			p.needs(request.StageID == nil || target != *request.StageID, "stage-move: a stage cannot move onto itself.")
		}
		if request.Step != nil {
			p.needs(*request.Step != 0, "stage-move: step must not be zero.")
		}
		if request.Route != nil && !draftIsTrue(request.Detach) {
			p.needs(draftHasRoute(d, *request.Route), fmt.Sprintf("stage-move: route %s is not on the belt.", *request.Route))
		}
	case BeltDraftOperationStageUpdate:
		p.needs(request.StageID != nil, "stage-update needs a stage id.")
		if request.StageID != nil {
			p.needs(draftHasStage(d, *request.StageID), fmt.Sprintf("stage-update: stage %s is not on the belt.", *request.StageID))
		}
		p.needs(request.Update != nil || request.Stage != nil, "stage-update needs update fields or a stage.")
		if request.Stage != nil && request.StageID != nil {
			p.needs(request.Stage.ID == *request.StageID, "stage-update: the stage id must match stageId.")
		}
	}
}

// flowDraftProblems checks the fork, route and feed-into operations.
func flowDraftProblems(p *draftProblems, request BeltDraftRequest) {
	d := request.Draft
	switch request.Op {
	case BeltDraftOperationForkAdd:
		p.needs(draftHasOptional(d, request.StageID, draftHasStage), "fork-add needs a stage id that is on the belt.")
	case BeltDraftOperationForkRemove:
		p.needs(draftHasOptional(d, request.ForkID, draftHasFork), "fork-remove needs a fork id that is on the belt.")
	case BeltDraftOperationRouteAdd:
		p.needs(draftHasOptional(d, request.ForkID, draftHasFork), "route-add needs a fork id that is on the belt.")
	case BeltDraftOperationRouteRemove:
		p.needs(draftHasOptional(d, request.RouteID, draftHasRoute), "route-remove needs a route id that is on the belt.")
	case BeltDraftOperationRouteUpdate:
		p.needs(draftHasOptional(d, request.RouteID, draftHasRoute), "route-update needs a route id that is on the belt.")
		p.needs(request.RouteName != nil, "route-update needs a name.")
	case BeltDraftOperationFeedInto:
		p.needs(request.Stage != nil, "feed-into needs a stage.")
		if request.Stage != nil {
			p.needs(beltDraftEditingValidStageID(request.Stage.ID), "feed-into needs a stage id.")
			p.needs(!draftHasStage(d, request.Stage.ID), fmt.Sprintf("feed-into: a stage with id %s is already on the belt.", request.Stage.ID))
		}
		p.needs(draftHasOptional(d, request.StageID, draftHasStage), "feed-into needs a stageId that is on the belt.")
	}
}

// gotoDraftProblems checks goto-pick and goto-route, which need an outcome of
// the stage they are set on.
func gotoDraftProblems(p *draftProblems, request BeltDraftRequest) {
	d := request.Draft
	op := string(request.Op)
	outcome := false
	if request.StageID != nil && request.OutcomeID != nil {
		if s := draftStage(d, *request.StageID); s != nil {
			for _, o := range s.Outcomes {
				if o.ID == *request.OutcomeID {
					outcome = true
				}
			}
		}
	}
	p.needs(draftHasOptional(d, request.StageID, draftHasStage), op+" needs a stage id that is on the belt.")
	p.needs(outcome, op+" needs an outcome id that is on the stage.")
	if request.GotoTarget != nil && *request.GotoTarget != "" {
		p.needs(draftHasStage(d, *request.GotoTarget), fmt.Sprintf("%s: stage %s is not on the belt.", op, *request.GotoTarget))
	}
	if request.Op == BeltDraftOperationGotoRoute {
		p.needs(draftHasOptional(d, request.RouteID, draftHasRoute), "goto-route needs a route id that is on the belt.")
	}
}

// triggerDraftProblems checks trigger-add, trigger-remove, trigger-move,
// trigger-update and trigger-kind.
func triggerDraftProblems(p *draftProblems, request BeltDraftRequest) {
	d := request.Draft
	switch request.Op {
	case BeltDraftOperationTriggerAdd:
		if request.Entry != nil && request.Entry.ID != "" {
			p.needs(!draftHasTrigger(d, request.Entry.ID), fmt.Sprintf("trigger-add: entry %s is already on the belt.", request.Entry.ID))
		}
	case BeltDraftOperationTriggerRemove:
		p.needs(draftHasOptional(d, request.EntryID, draftHasTrigger), "trigger-remove needs an entry id that is on the belt.")
	case BeltDraftOperationTriggerMove:
		id := ValueOf(request.EntryID)
		if request.Offset != nil {
			offset := *request.Offset
			if index := draftTriggerIndex(d, id); index >= 0 {
				p.needs(index+offset >= 0 && index+offset < len(d.Triggers), fmt.Sprintf("trigger-move: %s cannot move %d place(s).", id, offset))
			} else {
				p.add("trigger-move needs an entry id that is on the belt.")
			}
			p.needs(offset != 0, "trigger-move: offset must not be zero.")
		} else {
			p.add("trigger-move needs an offset.")
		}
	case BeltDraftOperationTriggerUpdate:
		p.needs(draftHasOptional(d, request.EntryID, draftHasTrigger), "trigger-update needs an entry id that is on the belt.")
		p.needs(request.Trigger != nil || request.Entry != nil, "trigger-update needs a trigger or an entry.")
	case BeltDraftOperationTriggerKind:
		p.needs(draftHasOptional(d, request.EntryID, draftHasTrigger), "trigger-kind needs an entry id that is on the belt.")
		p.needs(request.Kind != nil, "trigger-kind needs a kind.")
	}
}

// repositoryDraftProblems checks repository-add and repository-remove. A
// one-repository trigger refuses a second repository, located at the trigger.
func repositoryDraftProblems(p *draftProblems, request BeltDraftRequest) {
	d := request.Draft
	p.needs(draftHasOptional(d, request.EntryID, draftHasTrigger), string(request.Op)+" needs an entry id that is on the belt.")
	if request.Op == BeltDraftOperationRepositoryAdd && request.EntryID != nil {
		if index := draftTriggerIndex(d, *request.EntryID); index >= 0 {
			entry := d.Triggers[index]
			if entry.Trigger.Kind.OneRepo() && len(entry.Repos) > 0 {
				// The old editor hid the field: a one-repository trigger keeps the repository it has.
				loc := ProblemLocationTrigger(*request.EntryID)
				p.list = append(p.list, LocatedProblem{Message: "This trigger works in one repository. Remove the current one first.", Location: &loc})
			}
		}
	}
}

// beltDraftEditingLocation is the part of the belt a request is about, for
// attaching its problems to a field, or nil.
func beltDraftEditingLocation(request BeltDraftRequest) *ProblemLocation {
	switch request.Op {
	case BeltDraftOperationStageAdd:
		if request.Stage == nil {
			return nil
		}
		return new(ProblemLocationStage(request.Stage.ID))
	case BeltDraftOperationStageRemove, BeltDraftOperationStageMove, BeltDraftOperationStageUpdate,
		BeltDraftOperationForkAdd, BeltDraftOperationFeedInto:
		return draftLocationOf(request.StageID, ProblemLocationStage)
	case BeltDraftOperationForkRemove, BeltDraftOperationRouteAdd:
		return draftLocationOf(request.ForkID, ProblemLocationFork)
	case BeltDraftOperationRouteRemove, BeltDraftOperationRouteUpdate:
		return draftLocationOf(request.RouteID, ProblemLocationRoute)
	case BeltDraftOperationTriggerAdd:
		if request.Entry == nil {
			return nil
		}
		return new(ProblemLocationTrigger(request.Entry.ID))
	case BeltDraftOperationTriggerRemove, BeltDraftOperationTriggerMove, BeltDraftOperationTriggerUpdate,
		BeltDraftOperationTriggerKind, BeltDraftOperationRepositoryAdd, BeltDraftOperationRepositoryRemove:
		return draftLocationOf(request.EntryID, ProblemLocationTrigger)
	case BeltDraftOperationGotoPick, BeltDraftOperationGotoRoute:
		return draftLocationOf(request.StageID, ProblemLocationStage)
	}
	return nil
}
