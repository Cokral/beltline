package core

import (
	"slices"
	"time"
)

// engineNestedWaits is true when stage is outside the split this leg is still
// working, so the stage waits until that split joins or the jump's siblings
// finish.
func engineNestedWaits(run Run, stage string, leg string) bool {
	joinID := nestedJoinOf(nestedLegOf(run, leg))
	if joinID == nil {
		return false
	}
	index := -1
	for i := range run.Joins {
		if run.Joins[i].ID == *joinID && !run.Joins[i].Joined {
			index = i
			break
		}
	}
	if index < 0 {
		return false
	}
	join := run.Joins[index]
	return !slices.ContainsFunc(run.Layout().Ancestry(stage), func(way BeltLayoutWay) bool {
		return way.Fork.ID == join.ForkID
	})
}

// engineNestedClear forgets choices for forks this leg will meet again, and leaves
// a fork another leg is still inside.
func engineNestedClear(run *Run, leg string, landing int) {
	layout := run.Layout()
	for _, fork := range run.Forks {
		span := layout.Span(fork)
		if span == nil || span.Lower <= landing {
			continue
		}
		occupied := false
		for _, other := range run.Legs {
			if other.ID == leg || other.IsFinished() || other.StageID == nil {
				continue
			}
			for _, way := range layout.Ancestry(*other.StageID) {
				if way.Fork.ID == fork.ID {
					occupied = true
					break
				}
			}
			if occupied {
				break
			}
		}
		if !occupied {
			routes := nestedRoutes(run)
			delete(routes, fork.ID)
			run.SetRoutes(routes)
		}
	}
}

// engineNestedSettle
func engineNestedSettle(run *Run, now time.Time) []Effect {
	var effects []Effect
	moved := true
	for moved {
		moved = false
		for index := range run.Joins {
			if run.Joins[index].Joined || !engineJoinReady(*run, run.Joins[index]) {
				continue
			}
			if run.Joins[index].Escaped {
				run.Joins[index].Joined = true
				engineDropJoinLegs(run, run.Joins[index].Legs)
				effects = append(effects, engineResumeDepartures(run, now)...)
			} else {
				effects = append(effects, engineNestedJoin(run, index, now)...)
			}
			moved = true
			break
		}
	}
	return effects
}

// engineJoinReady
func engineJoinReady(run Run, join Join) bool {
	var children []Join
	for _, other := range run.Joins {
		if other.Parent != nil && *other.Parent == join.ID {
			children = append(children, other)
		}
	}
	for _, child := range children {
		if !child.Joined {
			return false
		}
	}
	var legs []Leg
	for _, id := range join.Legs {
		if leg := nestedLegOf(run, id); leg != nil {
			legs = append(legs, *leg)
		}
	}
	// A split whose every route split again has no legs of its own, only children.
	if len(legs) == 0 && len(children) == 0 {
		return false
	}
	return !slices.ContainsFunc(legs, func(leg Leg) bool { return !leg.IsFinished() })
}

// engineDropJoinLegs forgets the legs a join has taken in, except the ones still
// departing, which keep running.
func engineDropJoinLegs(run *Run, legIDs []string) {
	ids := map[string]struct{}{}
	for _, id := range legIDs {
		ids[id] = struct{}{}
	}
	var kept []Leg
	for _, leg := range run.Legs {
		_, in := ids[leg.ID]
		if in && leg.Depart == nil {
			continue
		}
		kept = append(kept, leg)
	}
	run.Legs = kept
}

// engineJoinBranches are the branches a join merges: each leg's branch, except the
// one it merges into.
func engineJoinBranches(legs []Leg, into *string) []string {
	var branches []string
	for _, leg := range legs {
		if leg.Branch == nil {
			continue
		}
		if into != nil && *leg.Branch == *into {
			continue
		}
		branches = append(branches, *leg.Branch)
	}
	return branches
}

// engineJoinCarrier is the leg id that carries on after a join. It is the checkout
// that already has the branch being merged into, since a new folder would try to
// check that branch out a second time, unless another leg is still using it.
func engineJoinCarrier(run Run, join Join) string {
	reused := ValueOr(join.Checkout, "main")
	if run.HasLeg(reused) {
		return engineFreshLeg(run, "join-"+join.ForkID)
	}
	return reused
}

// engineJoinLanding is where the carrying leg goes once its join has merged: on to
// the join's stage after it, back out to the parent split, on to the stage the
// fork bubbles to, or done when no leg is left.
func engineJoinLanding(run *Run, join Join, id string, layout BeltLayout, now time.Time) []Effect {
	if join.AfterStage != nil {
		return engineEnter(run, *join.AfterStage, new(id), nil, now)
	}
	if join.Parent != nil {
		var outside *string
		if at := run.JoinIndexOf(*join.Parent); at >= 0 {
			if step := engineBubbled(layout, run.Joins[at].ForkID); step != nil {
				outside = CopyPointer(step.Stage)
			}
		}
		engineSet(run, new(id), func(l *Leg) {
			l.State = RunStateDone
			l.StageID = outside
		})
		return nil
	}
	if step := engineBubbled(layout, join.ForkID); step != nil && step.Stage != nil {
		return engineEnter(run, *step.Stage, new(id), nil, now)
	}
	engineSet(run, new(id), func(l *Leg) {
		l.State = RunStateDone
		l.StageID = nil
	})
	if engineAllFinished(run.Legs) {
		return engineComplete(run)
	}
	return nil
}

// engineNestedJoin
func engineNestedJoin(run *Run, index int, now time.Time) []Effect {
	join := run.Joins[index]
	layout := run.Layout()
	var legs []Leg
	for _, id := range join.Legs {
		if leg := nestedLegOf(*run, id); leg != nil {
			legs = append(legs, *leg)
		}
	}
	into := CopyPointer(join.Into)
	if into == nil {
		into = CopyPointer(run.Branch)
	}
	branches := engineJoinBranches(legs, into)
	run.Joins[index].Joined = true
	engineDropJoinLegs(run, join.Legs)
	id := engineJoinCarrier(*run, join)
	leg := Leg{ID: id, Route: nil, StageID: CopyPointer(join.AfterStage), State: RunStateQueued,
		Branch: CopyPointer(into), Join: CopyPointer(join.Parent)}
	if fork := layout.Fork(join.ForkID); fork != nil && fork.Parent != nil {
		leg.Route = CopyPointer(fork.Parent)
	}
	run.Legs = append(run.Legs, leg)
	if join.Parent != nil {
		if at := run.JoinIndexOf(*join.Parent); at >= 0 {
			run.Joins[at].Legs = append(run.Joins[at].Legs, id)
		}
	}
	nestedMergeRoutes(run, join.AfterRoutes)
	var effects []Effect
	effects = append(effects, engineJoinLanding(run, join, id, layout, now)...)
	if len(branches) > 0 {
		effects = append(effects, engineMerge(run, Merge{
			Branches: branches, Into: CopyPointer(into), Checkout: CopyPointer(join.Checkout),
			Leg: new(id), Context: new(engineJoinContext(join, layout, into, *run)),
		})...)
	}
	return effects
}

// engineMerge starts a join's merge, or queues it behind the one under way: a run
// merges one join at a time, each into the branch and checkout it named.
func engineMerge(run *Run, merge Merge) []Effect {
	if len(run.MergeBranches) > 0 {
		run.MergesWaiting = append(run.MergesWaiting, merge)
		return nil
	}
	run.MergeBranches = merge.Branches
	run.MergeInto = merge.Into
	run.MergeCheckout = merge.Checkout
	run.MergeLeg = merge.Leg
	run.MergeContext = merge.Context
	return []Effect{{Kind: EffectKindMergeLegs, Legs: merge.Branches}}
}

// engineNextMerge is the merge under way being over, merged or conflicted: the
// next one starts.
func engineNextMerge(run *Run) []Effect {
	run.MergeInto = nil
	run.MergeCheckout = nil
	run.MergeLeg = nil
	run.MergeContext = nil
	if len(run.MergeBranches) != 0 || len(run.MergesWaiting) == 0 {
		return nil
	}
	next := run.MergesWaiting[0]
	run.MergesWaiting = append([]Merge(nil), run.MergesWaiting[1:]...)
	return engineMerge(run, next)
}
