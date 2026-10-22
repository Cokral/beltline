package core

import (
	"time"
)

// engineNestedGoto is a jump from inside a nested fork. A target still inside the
// same routes is entered. A target on a sibling route of a fork the leg is still
// in is held. A target outside waits for the other selected routes, and does not
// cancel them or merge the branches it left behind.
func engineNestedGoto(run *Run, leg string, outcome Outcome, stage Stage, targets []string, note *string, now time.Time) []Effect {
	layout := run.Layout()
	var along []BeltLayoutWay
	if source := nestedLegOf(*run, leg); source != nil && source.StageID != nil {
		along = layout.Ancestry(*source.StageID)
	}
	to := layout.Target(targets, run.Routes(), along)
	if to == nil {
		if here, way, target, found := nestedFirstConflict(layout, targets, along); found {
			return engineHoldLeg(run, new(leg),
				outcome.Name+": "+stage.Name+" is on the "+here.Route.Name+" route and cannot go to "+target+" on the "+way.Route.Name+" route.",
				nil, now, true)
		}
		var names []string
		for _, id := range targets {
			if named := layout.stageNamed(id); named != nil {
				names = append(names, named.Name)
			}
		}
		var routeNames []string
		for _, way := range along {
			routeNames = append(routeNames, way.Route.Name)
		}
		where := stage.Name
		if path := joinStrings(routeNames, " / "); path != "" {
			where = stage.Name + " on " + path
		}
		return engineHoldLeg(run, new(leg), outcome.Name+": "+where+" cannot go to "+joinStrings(names, ", ")+".", nil, now, true)
	}
	there := layout.Ancestry(*to)
	for _, way := range there {
		if here, ok := nestedAlong(along, way.Fork.ID); ok && here.Route.ID != way.Route.ID {
			target := *to
			if named := layout.stageNamed(*to); named != nil {
				target = named.Name
			}
			return engineHoldLeg(run, new(leg),
				outcome.Name+": "+stage.Name+" is on the "+here.Route.Name+" route and cannot go to "+target+" on the "+way.Route.Name+" route.",
				nil, now, true)
		}
	}
	departs := engineDepart(run, leg, *to)
	effects := engineEnter(run, *to, new(leg), note, now)
	if departs {
		// The leg lands later, when the splits it left finish: keep what it replays for then.
		var replays *string
		if target := nestedStageOf(*run, *to); target != nil && target.Kind() == StageKindAgent {
			if source := engineReplaySource(*run, *to, leg); source != nil {
				replays = new(source.ID)
			}
		}
		engineSet(run, new(leg), func(l *Leg) { l.Replays = replays })
	} else {
		engineReplayClosedAttempt(run, new(leg))
	}
	return effects
}

// nestedFirstConflict is the first target, in order, whose route runs through a
// fork the leg is on a different route of: the leg's route, the target's way, and
// the target's name.
func nestedFirstConflict(layout BeltLayout, targets []string, along []BeltLayoutWay) (BeltLayoutWay, BeltLayoutWay, string, bool) {
	for _, id := range targets {
		for _, way := range layout.Ancestry(id) {
			if here, ok := nestedAlong(along, way.Fork.ID); ok && here.Route.ID != way.Route.ID {
				name := id
				if named := layout.stageNamed(id); named != nil {
					name = named.Name
				}
				return here, way, name, true
			}
		}
	}
	return BeltLayoutWay{}, BeltLayoutWay{}, "", false
}

// nestedAlong is the leg's way through fork, if it is on one.
func nestedAlong(along []BeltLayoutWay, fork string) (BeltLayoutWay, bool) {
	for _, way := range along {
		if way.Fork.ID == fork {
			return way, true
		}
	}
	return BeltLayoutWay{}, false
}

// nestedStageOf is the run's stage with id, or nil.
func nestedStageOf(run Run, id string) *Stage {
	for i := range run.Stages {
		if run.Stages[i].ID == id {
			return &run.Stages[i]
		}
	}
	return nil
}

// joinStrings joins parts with sep.
func joinStrings(parts []string, sep string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += sep
		}
		out += part
	}
	return out
}

// engineDepart is a jump out of splits the leg is still inside: those splits skip
// their join, and the leg waits to enter target until they finish. False when the
// jump leaves no open split, so the leg goes straight there.
func engineDepart(run *Run, leg string, target string) bool {
	layout := run.Layout()
	var along []BeltLayoutWay
	if source := nestedLegOf(*run, leg); source != nil && source.StageID != nil {
		along = layout.Ancestry(*source.StageID)
	}
	there := map[string]struct{}{}
	for _, way := range layout.Ancestry(target) {
		there[way.Fork.ID] = struct{}{}
	}
	leaving := map[string]struct{}{}
	for _, way := range along {
		if _, ok := there[way.Fork.ID]; !ok {
			leaving[way.Fork.ID] = struct{}{}
		}
	}
	// The splits this leg belongs to, from its own outwards.
	var open []string
	next := CopyPointer(nestedJoinOf(nestedLegOf(*run, leg)))
	seen := map[string]struct{}{}
	for next != nil {
		if _, dup := seen[*next]; dup {
			break
		}
		seen[*next] = struct{}{}
		index := run.JoinIndexOf(*next)
		if index < 0 {
			break
		}
		join := run.Joins[index]
		if !join.Joined {
			if _, ok := leaving[join.ForkID]; ok {
				open = append(open, join.ID)
			}
		}
		next = CopyPointer(join.Parent)
	}
	if len(open) == 0 {
		return false
	}
	for _, id := range open {
		if index := run.JoinIndexOf(id); index >= 0 {
			run.Joins[index].Escaped = true
		}
	}
	engineSet(run, new(leg), func(l *Leg) {
		l.Depart = new(target)
		l.DepartJoins = open
	})
	return true
}

// nestedJoinOf is the leg's join, or nil when there is no leg.
func nestedJoinOf(leg *Leg) *string {
	if leg == nil {
		return nil
	}
	return leg.Join
}
