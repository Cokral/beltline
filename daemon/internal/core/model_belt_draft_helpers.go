// Private lookups and character classes the belt draft editing uses on a draft:
// the whitespace and newline class, the stage, route, fork and entry lookups,
// and the locations problems are attached to.

package core

import (
	"slices"
)

// draftIsTrue reports whether an optional bool is true.
func draftIsTrue(b *bool) bool {
	return b != nil && *b
}

// absInt is the absolute value of x.
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// draftHasStage reports whether the belt has a stage with id.
func draftHasStage(b Belt, id string) bool {
	return b.stageIndex(id) >= 0
}

// draftStage is the stage with id, or nil.
func draftStage(b Belt, id string) *Stage {
	if i := b.stageIndex(id); i >= 0 {
		return &b.Stages[i]
	}
	return nil
}

// draftHasRoute reports whether a fork on the belt has a route with id.
func draftHasRoute(b Belt, id string) bool {
	return slices.ContainsFunc(b.Forks, func(f Fork) bool { return forkHasRoute(f, id) })
}

// draftHasFork reports whether the belt has a fork with id.
func draftHasFork(b Belt, id string) bool {
	return slices.ContainsFunc(b.Forks, func(f Fork) bool { return f.ID == id })
}

// draftHasOptional reports whether an optional id is set and names something on
// the belt, as has says. Replaces draftHasStageID, draftHasRouteID, draftHasForkID
// and draftHasEntryID.
func draftHasOptional(b Belt, id *string, has func(Belt, string) bool) bool {
	return id != nil && has(b, *id)
}

// draftTriggerIndex is the position of the trigger entry with id, or -1.
func draftTriggerIndex(b Belt, id string) int {
	for i := range b.Triggers {
		if b.Triggers[i].ID == id {
			return i
		}
	}
	return -1
}

// draftHasTrigger reports whether the belt has a trigger entry with id.
func draftHasTrigger(b Belt, id string) bool {
	return draftTriggerIndex(b, id) >= 0
}

// draftOutcomeIndex is the position of the outcome with id in s, or -1.
func draftOutcomeIndex(s Stage, id string) int {
	for i := range s.Outcomes {
		if s.Outcomes[i].ID == id {
			return i
		}
	}
	return -1
}

// draftAncestryHasRoute reports whether one of the ways names route.
func draftAncestryHasRoute(chain []BeltLayoutWay, route string) bool {
	return slices.ContainsFunc(chain, func(w BeltLayoutWay) bool { return w.Route.ID == route })
}

// draftLocationOf is the location made from an optional id, or nil.
func draftLocationOf(id *string, build func(string) ProblemLocation) *ProblemLocation {
	if id == nil {
		return nil
	}
	return new(build(*id))
}
