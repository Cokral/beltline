// The belt's flow checks:
// goto and route; the forks are in engine_validation_forks.go, the inputs,
// triggers and route walk in engine_validation_inputs.go.

package core

import (
	"fmt"
	"slices"
	"strings"
)

// validationGoto is the checks on a goto: its stages are on the belt, and one on a
// fork stays on its route, or, coming back into the fork, names a stage on every
// nonempty route of every fork it re-enters. Leaving a nested route for a stage
// outside it is allowed.
func validationGoto(outcome Outcome, from Stage, at int, layout BeltLayout) []string {
	what := fmt.Sprintf("%s: “%s”", from.Name, outcome.Name)
	if problem := validationGotoProblem(what, outcome.Then.To, from, layout); problem != "" {
		return []string{problem}
	}
	var warnings []string
	source := layout.Ancestry(from.ID)
	for _, fork := range layout.Forks {
		warnings = append(warnings, validationGotoFork(what, outcome.Then.To, fork, source, at, layout)...)
	}
	return warnings
}

// validationGotoProblem is the error for the stages a goto names, or "" when they
// are fine: it names none, names the stage it is on, names a stage no longer on
// the belt, or names several without each being a stage on a fork's route.
func validationGotoProblem(what string, targets []string, from Stage, layout BeltLayout) string {
	if len(targets) == 0 {
		return what + " goes to a stage, but names none."
	}
	if len(targets) == 1 && targets[0] == from.ID {
		return what + " goes to itself. Use “Run again” instead."
	}
	for _, id := range targets {
		if !validationHasStage(layout.Stages, id) {
			return what + " goes to a stage that isn't on this belt anymore."
		}
	}
	if len(targets) > 1 {
		for _, id := range targets {
			if layout.Place(id) == nil {
				return what + " goes to more than one stage. Only the routes of a fork can each have their own."
			}
		}
	}
	return ""
}

// validationGotoRoute is a route of a fork that one of a goto's stages is on.
type validationGotoRoute struct {
	route Route
	id    string
}

// validationGotoFork is the warnings a goto gives for one fork: its stages on the
// fork's routes, a jump from one route into another, and the routes a re-entry
// leaves without a stage.
func validationGotoFork(what string, targets []string, fork Fork, source []BeltLayoutWay, at int, layout BeltLayout) []string {
	var onFork []validationGotoRoute
	for _, id := range targets {
		for _, way := range layout.Ancestry(id) {
			if way.Fork.ID == fork.ID {
				onFork = append(onFork, validationGotoRoute{route: way.Route, id: id})
				break
			}
		}
	}
	span := layout.Span(fork)
	if len(onFork) == 0 || span == nil {
		return nil
	}
	// A route only jumps reach can be gone to from anywhere, alone.
	allClosed := !slices.ContainsFunc(onFork, func(r validationGotoRoute) bool {
		return layout.Opens(r.route.ID)
	})
	if allClosed {
		return nil
	}
	if here, ok := nestedAlong(source, fork.ID); ok {
		for _, r := range onFork {
			if r.route.ID != here.Route.ID && layout.Opens(r.route.ID) {
				return []string{what + " goes from the " + here.Route.Name + " route into another route."}
			}
		}
		return nil
	}
	var warnings []string
	routes := make([]Route, len(onFork))
	for i, r := range onFork {
		routes[i] = r.route
	}
	if repeated := validationGotoRepeated(routes, layout); repeated != "" {
		warnings = append(warnings, what+" names two stages on the "+repeated+".")
	}
	if missing := validationGotoMissing(fork, routes, layout); len(missing) > 0 && at > span.Upper {
		warnings = append(warnings, what+" goes back into a fork, but names no stage on the "+validationNames(missing)+".")
	}
	return warnings
}

// validationGotoRepeated is the label of the first route that two of routes name, or "".
func validationGotoRepeated(routes []Route, layout BeltLayout) string {
	for _, route := range routes {
		count := 0
		for _, other := range routes {
			if other.ID == route.ID {
				count++
			}
		}
		if count > 1 {
			return validationLabel(route, layout)
		}
	}
	return ""
}

// validationGotoMissing is the labels of the fork's open routes that hold stages
// but are not among routes.
func validationGotoMissing(fork Fork, routes []Route, layout BeltLayout) []string {
	var missing []string
	for _, route := range fork.Routes {
		if len(layout.StagesWithin(route.ID)) == 0 || !layout.Opens(route.ID) {
			continue
		}
		named := slices.ContainsFunc(routes, func(r Route) bool { return r.ID == route.ID })
		if !named {
			missing = append(missing, validationLabel(route, layout))
		}
	}
	return missing
}

// validationHasStage reports whether a stage with id is in stages.
func validationHasStage(stages []Stage, id string) bool {
	return slices.ContainsFunc(stages, func(s Stage) bool { return s.ID == id })
}

// validationLabel is a route's name, with the routes outside it when a fork is
// nested, so two forks can share a name.
func validationLabel(route Route, layout BeltLayout) string {
	fork := layout.ForkWithRoute(route.ID)
	if fork == nil {
		return route.Name
	}
	names := []string{route.Name}
	parent := fork.Parent
	seen := map[string]struct{}{fork.ID: {}}
	for parent != nil {
		outer := layout.ForkWithRoute(*parent)
		if outer == nil {
			break
		}
		if _, dup := seen[outer.ID]; dup {
			break
		}
		seen[outer.ID] = struct{}{}
		name, found := "", false
		for _, r := range outer.Routes {
			if r.ID == *parent {
				name, found = r.Name, true
				break
			}
		}
		if !found {
			break
		}
		names = append([]string{name}, names...)
		parent = outer.Parent
	}
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names, " / ")
}

// validationRoute is the checks on taking a route: the route is on the belt, in
// the fork that comes right after the stage.
func validationRoute(outcome Outcome, from Stage, layout BeltLayout) []string {
	what := fmt.Sprintf("%s: “%s”", from.Name, outcome.Name)
	routes := outcome.Then.Routes
	known := len(routes) > 0
	for _, id := range routes {
		if layout.ForkWithRoute(id) == nil {
			known = false
		}
	}
	if !known {
		return []string{what + " takes a route that isn't on this belt anymore."}
	}
	for _, id := range routes {
		if r := layout.Route(id); r != nil && r.Feeds {
			return []string{what + " takes the " + r.Name + " route, which feeds in. Go to one of its stages instead."}
		}
	}
	var next *string
	if fork := layout.ForkAfter(from.ID); fork != nil {
		id := fork.ID
		next = &id
	}
	for _, id := range routes {
		fork := layout.ForkWithRoute(id)
		if fork == nil {
			return []string{what + " takes a route of a fork that doesn't come right after " + from.Name + "."}
		}
		if layout.Span(*fork) != nil && (next == nil || fork.ID != *next) {
			return []string{what + " takes a route of a fork that doesn't come right after " + from.Name + "."}
		}
	}
	return nil
}

// validationHasDuplicate reports whether values has a repeated element.
func validationHasDuplicate(values []string) bool {
	seen := map[string]struct{}{}
	for _, v := range values {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}
