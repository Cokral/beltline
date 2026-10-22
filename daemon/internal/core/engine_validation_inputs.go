// The inputs each stage needs,
// the trigger checks, and the walk over the routes of each fork.

package core

import (
	"fmt"
	"slices"
	"strings"
)

// validationInputs is the checks on what each stage needs, once per trigger: that
// entry provides it, or an earlier stage makes it; after a fork, a stage on every
// route. An input one entry provides does not satisfy another.
func validationInputs(belt Belt) []string {
	several := len(belt.Triggers) > 1
	var warnings []string
	for index, entry := range belt.Triggers {
		prefix := ""
		if several {
			prefix = fmt.Sprintf("Trigger %d (%s): ", index+1, entry.Trigger.Kind.Title())
		}
		for _, w := range validationInputWarnings(belt, entry.Trigger.Kind.Provides()) {
			warnings = append(warnings, prefix+w)
		}
	}
	return warnings
}

// validationInputWarnings is the input warnings for belt given the inputs that
// available provides at the start of the line.
func validationInputWarnings(belt Belt, available map[Input]struct{}) []string {
	var warnings []string
	// What some routes of a fork made and others didn't, with the ones that didn't.
	partly := map[Input][]string{}
	// A stage only gives the stages after it what it makes when it has what it needs.
	pass := func(stage Stage, available *map[Input]struct{}, path []string, lacking map[Input][]string) {
		needs := stage.Needs()
		var unmet []Input
		for _, input := range AllInput {
			_, needed := needs[input]
			_, have := (*available)[input]
			if needed && !have {
				unmet = append(unmet, input)
			}
		}
		for _, input := range unmet {
			if missing, ok := lacking[input]; ok {
				where := stage.Name
				if len(path) > 0 {
					where = stage.Name + " on " + strings.Join(path, " / ")
				}
				verb := "don't"
				if len(missing) == 1 {
					verb = "doesn't"
				}
				warnings = append(warnings, where+" needs "+input.Label()+", which the "+validationNames(missing)+" "+verb+" make.")
			} else {
				warnings = append(warnings, validationMissing(input, stage))
			}
		}
		if len(unmet) == 0 {
			for input := range stage.Produces() {
				(*available)[input] = struct{}{}
			}
		}
	}
	layout := belt.Layout()
	validationWalk(layout.Segments(), available, nil, &partly, layout.Joins, pass)
	return warnings
}

// validationTriggerWarnings is each entry's own checks. An empty list can sit in a
// draft, and cannot be saved.
func validationTriggerWarnings(belt Belt) []string {
	if len(belt.Triggers) == 0 {
		return []string{"Add at least one trigger"}
	}
	var warnings []string
	ids := make([]string, len(belt.Triggers))
	for i, e := range belt.Triggers {
		ids[i] = e.ID
	}
	if validationHasDuplicate(ids) {
		warnings = append(warnings, "Two triggers have the same id.")
	}
	several := len(belt.Triggers) > 1
	for index, entry := range belt.Triggers {
		prefix := ""
		if several {
			prefix = fmt.Sprintf("Trigger %d (%s): ", index+1, entry.Trigger.Kind.Title())
		}
		for _, w := range ValidationSourceWarnings(entry.Trigger, entry.Repos) {
			warnings = append(warnings, prefix+w)
		}
		if poll := entry.Trigger.Poll(); poll != nil && !validationPollOffered(*poll) {
			warnings = append(warnings, prefix+"The check interval isn't one the editor offers. Use 30 seconds, 1 minute, 5 minutes or 15 minutes.")
		}
		seen := map[string]struct{}{}
		for _, repo := range entry.Repos {
			if _, dup := seen[repo]; dup {
				warnings = append(warnings, prefix+"Repository "+repo+" is listed more than once.")
			}
			seen[repo] = struct{}{}
		}
	}
	return warnings
}

// validationPollOffered reports whether seconds is one of the editor's check intervals.
func validationPollOffered(seconds int) bool {
	return slices.Contains(TriggerPollChoices, seconds)
}

// validationWalk is what every route of each fork makes, walking forks nested
// inside a route.
// partly is shared across the recursion.
func validationWalk(segments []BeltLayoutSegment, available map[Input]struct{}, path []string, partly *map[Input][]string,
	joins func(route string) bool, pass func(stage Stage, available *map[Input]struct{}, path []string, lacking map[Input][]string)) map[Input]struct{} {
	available = CopySet(available)
	for _, segment := range segments {
		switch segment.Kind {
		case BeltLayoutSegmentStage:
			pass(*segment.Stage, &available, path, *partly)
		case BeltLayoutSegmentFork:
			fork := *segment.Fork
			n := min(len(fork.Routes), len(segment.Routes))
			type end struct {
				route Route
				set   map[Input]struct{}
			}
			ends := make([]end, n)
			for i := 0; i < n; i++ {
				route := fork.Routes[i]
				ends[i] = end{route: route, set: validationWalk(segment.Routes[i], available, validationAppendPath(path, route.Name), partly, joins, pass)}
			}
			// Only a route that joins the line again hands on what it made.
			var joining []end
			for _, e := range ends {
				if joins(e.route.ID) {
					joining = append(joining, e)
				}
			}
			counted := joining
			if len(joining) == 0 {
				counted = ends
			}
			everywhere := available
			if len(counted) > 0 {
				everywhere = counted[0].set
				for _, e := range counted[1:] {
					everywhere = intersectInputs(everywhere, e.set)
				}
			}
			union := map[Input]struct{}{}
			for _, e := range counted {
				for input := range e.set {
					union[input] = struct{}{}
				}
			}
			for input := range union {
				if _, all := everywhere[input]; all {
					continue
				}
				var lacking []string
				for _, e := range counted {
					if _, has := e.set[input]; !has {
						if len(path) == 0 {
							lacking = append(lacking, e.route.Name)
						} else {
							lacking = append(lacking, strings.Join(validationAppendPath(path, e.route.Name), " / "))
						}
					}
				}
				(*partly)[input] = lacking
			}
			available = everywhere
		}
	}
	return available
}

// validationAppendPath is path with name added at its end, in a new slice.
func validationAppendPath(path []string, name string) []string {
	out := make([]string, 0, len(path)+1)
	out = append(out, path...)
	return append(out, name)
}

// validationNames is "the Simple route", or "the Quick and Simple routes".
func validationNames(routes []string) string {
	if len(routes) == 1 {
		return routes[0] + " route"
	}
	last := ""
	if len(routes) > 0 {
		last = routes[len(routes)-1]
	}
	head := ""
	if len(routes) > 1 {
		head = strings.Join(routes[:len(routes)-1], ", ")
	}
	return head + " and " + last + " routes"
}

// validationMissing is why stage can't have input where it is.
func validationMissing(input Input, stage Stage) string {
	switch input {
	case InputPullRequest:
		return stage.Name + " comes before any stage that changes code, so there is no pull request to watch."
	case InputBaseBranch:
		return stage.Name + " changes code, but a review works on someone else's pull request. Give it a read-only checkout."
	case InputBranch:
		return stage.Name + " reads the worktree before any stage changes code, so there is nothing to look at yet."
	case InputTicket:
		return stage.Name + " watches the issue or ticket, but this belt's runs aren't about one."
	default:
		panic("validationMissing: unknown input")
	}
}
