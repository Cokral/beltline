// The operation switch of apply, split into one helper per
// family of operations.
// family of operations. Each helper edits the draft it is given in place; the
// request has already passed the location checks when these run.

package core

import "fmt"

// beltDraftEditingApply applies the request, without the cleanup
// BeltDraftEditingApplyResult does after it.
func beltDraftEditingApply(request BeltDraftRequest) (Belt, *TriggerStash, error) {
	// An unknown operation is an error before anything else: it is not a
	// ValidationFailure, and it must never apply as a no-op.
	if !enumKnown(request.Op, AllBeltDraftOperation) {
		return Belt{}, nil, fmt.Errorf("%s is not a belt draft operation", request.Op)
	}
	problems := BeltDraftEditingLocated(request)
	if len(problems) > 0 {
		return Belt{}, nil, ValidationFailureLocated(problems)
	}
	draft := request.Draft.Clone()
	stash := applyBeltDraftOperation(&draft, request)
	return draft, stash, nil
}

// applyBeltDraftOperation edits draft for one operation and returns the trigger
// stash the operation leaves, nil for every operation but trigger-kind.
func applyBeltDraftOperation(draft *Belt, request BeltDraftRequest) *TriggerStash {
	switch request.Op {
	case BeltDraftOperationStageAdd, BeltDraftOperationStageRemove, BeltDraftOperationStageMove, BeltDraftOperationStageUpdate:
		applyStageDraftOperation(draft, request)
	case BeltDraftOperationForkAdd, BeltDraftOperationForkRemove, BeltDraftOperationRouteAdd,
		BeltDraftOperationRouteRemove, BeltDraftOperationRouteUpdate, BeltDraftOperationFeedInto,
		BeltDraftOperationGotoPick, BeltDraftOperationGotoRoute:
		applyFlowDraftOperation(draft, request)
	case BeltDraftOperationTriggerAdd, BeltDraftOperationTriggerRemove, BeltDraftOperationTriggerMove,
		BeltDraftOperationTriggerUpdate, BeltDraftOperationRepositoryAdd, BeltDraftOperationRepositoryRemove:
		applyTriggerDraftOperation(draft, request)
	case BeltDraftOperationTriggerKind:
		return applyTriggerKindDraftOperation(draft, request)
	case BeltDraftOperationPoliciesUpdate:
		if request.Policies != nil {
			draft.Policies = *request.Policies
		}
	}
	return nil
}

// applyStageDraftOperation adds, removes, moves and updates stages.
func applyStageDraftOperation(draft *Belt, request BeltDraftRequest) {
	switch request.Op {
	case BeltDraftOperationStageAdd:
		if request.Stage == nil {
			return
		}
		stage := (*request.Stage).Clone()
		stage.Route = CopyPointer(request.Route)
		at := ValueOr(request.Index, len(draft.Stages))
		draft.Insert(stage, min(max(at, 0), len(draft.Stages)))
	case BeltDraftOperationStageRemove:
		draft.Remove(*request.StageID)
	case BeltDraftOperationStageMove:
		switch {
		case request.TargetID != nil:
			draft.MoveOnto(*request.StageID, *request.TargetID)
		case request.Step != nil:
			draft.Shift(*request.StageID, *request.Step)
		case draftIsTrue(request.Detach):
			draft.MoveTo(*request.StageID, nil)
		default:
			draft.MoveTo(*request.StageID, request.Route)
		}
	case BeltDraftOperationStageUpdate:
		index := -1
		if request.StageID != nil {
			index = draft.stageIndex(*request.StageID)
		}
		if index < 0 {
			return
		}
		if request.Stage != nil {
			stage := (*request.Stage).Clone()
			stage.ID = *request.StageID
			stage.Route = CopyPointer(draft.Stages[index].Route)
			if stage.Kind() == StageKindWatch {
				if wt := stage.WatchTarget(); wt != nil && *wt == WatchTargetPullRequest {
					stage.Watch = nil
				}
			}
			stage.Slots = BeltClampedSlots(stage.Slots)
			stage.Outcomes = BeltClampedOutcomes(stage.Outcomes)
			draft.Stages[index] = stage
		} else if request.Update != nil {
			draft.apply(*request.Update, index)
		}
	}
}

// applyFlowDraftOperation changes forks, routes, feeds and the goto targets of
// outcomes.
func applyFlowDraftOperation(draft *Belt, request BeltDraftRequest) {
	switch request.Op {
	case BeltDraftOperationForkAdd:
		draft.ForkAround(*request.StageID)
	case BeltDraftOperationForkRemove:
		draft.Dissolve(*request.ForkID)
	case BeltDraftOperationRouteAdd:
		draft.AddRoute(*request.ForkID, draftIsTrue(request.Feeds))
	case BeltDraftOperationRouteRemove:
		draft.RemoveRoute(*request.RouteID)
	case BeltDraftOperationRouteUpdate:
		if request.RouteID != nil {
			for fi := range draft.Forks {
				if ri := forkRouteIndex(draft.Forks[fi], *request.RouteID); ri >= 0 {
					draft.Forks[fi].Routes[ri].Name = ValueOf(request.RouteName)
					break
				}
			}
		}
	case BeltDraftOperationFeedInto:
		if request.Stage == nil || request.StageID == nil {
			return
		}
		draft.FeedInto(*request.StageID, (*request.Stage).Clone())
	case BeltDraftOperationGotoPick:
		draft.PickGoto(ValueOf(request.GotoTarget), *request.OutcomeID, *request.StageID)
	case BeltDraftOperationGotoRoute:
		draft.SetGotoRoute(ValueOf(request.GotoTarget), *request.RouteID, *request.OutcomeID, *request.StageID)
	}
}

// applyTriggerDraftOperation adds, removes, moves and updates trigger entries,
// and changes their repositories.
func applyTriggerDraftOperation(draft *Belt, request BeltDraftRequest) {
	switch request.Op {
	case BeltDraftOperationTriggerAdd:
		var entry TriggerEntry
		if request.Entry != nil {
			entry = *request.Entry
			entry.Repos = cloneStrings(entry.Repos)
		} else {
			filter := DefaultIssueFilter()
			entry = TriggerEntry{ID: draft.NextTriggerID(), Trigger: Trigger{Kind: TriggerKindIssues, Issues: &filter}, Repos: []string{}}
		}
		if entry.ID == "" {
			entry.ID = draft.NextTriggerID()
		}
		entry.Trigger = BeltClampedTrigger(entry.Trigger)
		draft.Triggers = append(draft.Triggers, entry)
	case BeltDraftOperationTriggerRemove:
		draft.RemoveTrigger(*request.EntryID)
	case BeltDraftOperationTriggerMove:
		draft.MoveTrigger(*request.EntryID, *request.Offset)
	case BeltDraftOperationTriggerUpdate:
		draft.UpdateTrigger(*request.EntryID, func(entry *TriggerEntry) {
			if request.Trigger != nil {
				entry.Trigger = BeltClampedTrigger(*request.Trigger)
			}
			if request.Entry != nil {
				repos := make([]string, 0, len(request.Entry.Repos))
				for _, r := range request.Entry.Repos {
					if n := RepositoryInputNormalize(r); n != nil {
						repos = append(repos, *n)
					} else {
						repos = append(repos, r)
					}
				}
				entry.Repos = repos
			}
			if request.Entry != nil && request.Entry.ID != "" {
				entry.ID = request.Entry.ID
			}
		})
	case BeltDraftOperationRepositoryAdd:
		draft.AddRepository(ValueOf(request.Repository), *request.EntryID)
	case BeltDraftOperationRepositoryRemove:
		draft.RemoveRepository(ValueOf(request.Repository), *request.EntryID)
	}
}

// applyTriggerKindDraftOperation switches an entry's trigger kind and returns the
// stash the client keeps for the entry.
func applyTriggerKindDraftOperation(draft *Belt, request BeltDraftRequest) *TriggerStash {
	// Picking the kind an entry already has changes nothing, and the stash comes back as it was sent.
	if request.Kind != nil && request.EntryID != nil {
		index := draftTriggerIndex(*draft, *request.EntryID)
		if index < 0 || draft.Triggers[index].Trigger.Kind != *request.Kind {
			next := TriggerStash{Entry: *request.EntryID}
			if request.Stash != nil && request.Stash.Entry == *request.EntryID {
				next = (*request.Stash).Clone()
			}
			draft.SwitchTriggerKind(*request.EntryID, *request.Kind, &next)
			return &next
		}
	}
	var kept *TriggerStash
	if request.Stash != nil {
		c := request.Stash.Clone()
		kept = &c
	}
	return kept
}
