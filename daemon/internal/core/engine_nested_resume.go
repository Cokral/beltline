package core

import (
	"slices"
	"strconv"
	"time"
)

// engineResumeDepartures
func engineResumeDepartures(run *Run, now time.Time) []Effect {
	var effects []Effect
	legs := append([]Leg(nil), run.Legs...)
	for _, leg := range legs {
		if leg.Depart == nil {
			continue
		}
		pending := false
		for _, id := range leg.DepartJoins {
			index := run.JoinIndexOf(id)
			if index < 0 || !run.Joins[index].Joined {
				pending = true
				break
			}
		}
		if pending {
			continue
		}
		target := *leg.Depart
		// The first split's parent that has not joined yet.
		var parent *string
		var candidates []string
		for _, id := range leg.DepartJoins {
			if index := run.JoinIndexOf(id); index >= 0 && run.Joins[index].Parent != nil {
				candidates = append(candidates, *run.Joins[index].Parent)
			}
		}
		for _, candidate := range candidates {
			index := run.JoinIndexOf(candidate)
			if index < 0 || !run.Joins[index].Joined {
				parent = new(candidate)
				break
			}
		}
		engineSet(run, new(leg.ID), func(l *Leg) {
			l.Depart = nil
			l.DepartJoins = []string{}
			l.Join = CopyPointer(parent)
			l.State = RunStateQueued
		})
		if parent != nil {
			if index := run.JoinIndexOf(*parent); index >= 0 && !slices.Contains(run.Joins[index].Legs, leg.ID) {
				run.Joins[index].Legs = append(run.Joins[index].Legs, leg.ID)
			}
		}
		if index := run.StageIndexOf(target); index >= 0 {
			engineNestedClear(run, leg.ID, index)
		}
		effects = append(effects, engineEnter(run, target, new(leg.ID), CopyPointer(leg.Note), now)...)
		if leg.Replays != nil {
			replays := *leg.Replays
			engineSet(run, new(leg.ID), func(l *Leg) { l.Replays = new(replays) })
		}
	}
	return effects
}

// engineBubbled
func engineBubbled(layout BeltLayout, forkID string) *BeltLayoutStep {
	fork := layout.Fork(forkID)
	if fork == nil {
		return nil
	}
	step := layout.Resume(*fork)
	if step.Stage != nil || fork.Parent == nil {
		return &step
	}
	outer := layout.ForkWithRoute(*fork.Parent)
	if outer == nil {
		return &step
	}
	return engineBubbled(layout, outer.ID)
}

// engineJoinContext
func engineJoinContext(join Join, layout BeltLayout, into *string, run Run) string {
	var names []string
	for _, id := range join.Routes {
		if route := layout.Route(id); route != nil {
			names = append(names, route.Name)
		}
	}
	var list string
	if len(names) == 1 {
		list = names[0] + " route"
	} else {
		last := ""
		dropped := []string{}
		if len(names) > 0 {
			last = names[len(names)-1]
			dropped = names[:len(names)-1]
		}
		list = joinStrings(dropped, ", ") + " and " + last + " routes"
	}
	destination := "the run's branch"
	if into != nil && !SamePointee(into, run.Branch) {
		destination = *into
	}
	return "the " + list + " into " + destination
}

// engineFreshJoin
func engineFreshJoin(run Run, fork string) string {
	n := len(run.Joins) + 1
	for {
		id := "j:" + fork + ":" + strconv.Itoa(n)
		taken := false
		for i := range run.Joins {
			if run.Joins[i].ID == id {
				taken = true
				break
			}
		}
		if !taken {
			return id
		}
		n++
	}
}

// engineFreshLeg
func engineFreshLeg(run Run, route string) string {
	base := "r:" + route
	if !run.HasLeg(base) {
		return base
	}
	n := 2
	for run.HasLeg(base + "#" + strconv.Itoa(n)) {
		n++
	}
	return base + "#" + strconv.Itoa(n)
}
