package core

import "slices"

// validationForks is the checks on the forks: each has two routes or more with
// names, and stages that sit together, and every route but the first is taken by
// some outcome.
func validationForks(belt Belt) []string {
	layout := belt.Layout()
	warnings := validationForkIDWarnings(belt)
	for _, stage := range belt.Stages {
		if stage.Route != nil && layout.Place(stage.ID) == nil {
			warnings = append(warnings, stage.Name+" is on a route that isn't on this belt anymore.")
		}
	}
	warnings = append(warnings, validationForkNesting(belt, layout)...)
	taken := validationTakenRoutes(belt)
	for _, fork := range belt.Forks {
		warnings = append(warnings, validationForkWarnings(belt, layout, fork, taken)...)
	}
	return warnings
}

// validationForkIDWarnings reports a fork id or a route id that two forks or routes
// of the belt share.
func validationForkIDWarnings(belt Belt) []string {
	var warnings []string
	forkIDs := make([]string, len(belt.Forks))
	for i, f := range belt.Forks {
		forkIDs[i] = f.ID
	}
	if validationHasDuplicate(forkIDs) {
		warnings = append(warnings, "Two forks have the same id.")
	}
	var routeIDs []string
	for _, f := range belt.Forks {
		for _, r := range f.Routes {
			routeIDs = append(routeIDs, r.ID)
		}
	}
	if validationHasDuplicate(routeIDs) {
		warnings = append(warnings, "Two routes have the same id.")
	}
	return warnings
}

// validationForkNesting reports a fork whose parent route isn't on the belt, and a
// fork that contains a fork containing it.
func validationForkNesting(belt Belt, layout BeltLayout) []string {
	var warnings []string
	for _, fork := range belt.Forks {
		chain := []string{fork.ID}
		parent := fork.Parent
		for parent != nil {
			outer := layout.ForkWithRoute(*parent)
			if outer == nil {
				warnings = append(warnings, "A fork sits on the "+*parent+" route, which isn't on this belt.")
				break
			}
			if slices.Contains(chain, outer.ID) {
				warnings = append(warnings, "Forks contain each other in a circle.")
				break
			}
			chain = append(chain, outer.ID)
			parent = outer.Parent
		}
	}
	return warnings
}

// validationTakenRoutes is the set of route ids some outcome of the belt takes.
func validationTakenRoutes(belt Belt) map[string]struct{} {
	taken := map[string]struct{}{}
	for _, stage := range belt.Stages {
		for _, outcome := range stage.Outcomes {
			if outcome.Then.Action == ThenActionRoute {
				for _, r := range outcome.Then.Routes {
					taken[r] = struct{}{}
				}
			}
		}
	}
	return taken
}

// validationForkWarnings are the checks on one fork: its routes, the stages it
// groups and how the stage before it reaches it. A fork with no stages is reported
// alone, since there is nothing else to check.
func validationForkWarnings(belt Belt, layout BeltLayout, fork Fork, taken map[string]struct{}) []string {
	routeNames := make([]string, len(fork.Routes))
	for i, r := range fork.Routes {
		routeNames[i] = r.Name
	}
	warnings := validationForkRouteWarnings(fork, routeNames)
	span := layout.Span(fork)
	if span == nil {
		return append(warnings, "The fork with the "+validationNames(routeNames)+" has no stages. Add one, or remove the fork.")
	}
	warnings = append(warnings, validationForkOrderWarnings(belt, layout, fork, *span, routeNames)...)
	return append(warnings, validationForkEntryWarnings(layout, fork, routeNames, taken)...)
}

// validationForkRouteWarnings are the checks on a fork's routes: there are at least
// two, each has a name and no two share one, and the first does not feed in.
func validationForkRouteWarnings(fork Fork, routeNames []string) []string {
	var warnings []string
	if len(fork.Routes) < 2 {
		warnings = append(warnings, "A fork needs at least two routes.")
	}
	names := make([]string, len(fork.Routes))
	for i, r := range fork.Routes {
		names[i] = outcomeNormalize(r.Name)
	}
	if slices.Contains(names, "") {
		warnings = append(warnings, "A route has no name.")
	}
	if validationHasDuplicate(names) {
		warnings = append(warnings, "Two routes of one fork have the same name.")
	}
	if len(fork.Routes) > 0 && fork.Routes[0].Feeds {
		warnings = append(warnings, "The "+fork.Routes[0].Name+" route is a fork's first route, so it can't feed in.")
	}
	return warnings
}

// validationForkOrderWarnings reports a fork whose stages in its span are not the
// stages of its routes, route after route: each route's stages stay together.
func validationForkOrderWarnings(belt Belt, layout BeltLayout, fork Fork, span IntRange, routeNames []string) []string {
	var grouped []string
	for _, r := range fork.Routes {
		for _, s := range layout.StagesWithin(r.ID) {
			grouped = append(grouped, s.ID)
		}
	}
	inSpan := belt.Stages[span.Lower : span.Upper+1]
	var spanIDs []string
	for _, s := range inSpan {
		spanIDs = append(spanIDs, s.ID)
	}
	if !slices.Equal(spanIDs, grouped) {
		return []string{"The stages of the " + validationNames(routeNames) + " are out of order. Keep each route's stages together."}
	}
	return nil
}

// validationForkEntryWarnings are the checks on how a fork is entered: a fork that
// starts its route has no stage before it to pick a route, and otherwise each open
// route after the first needs an outcome that takes it or a feed into the fork.
func validationForkEntryWarnings(layout BeltLayout, fork Fork, routeNames []string, taken map[string]struct{}) []string {
	var warnings []string
	pieces := layout.Pieces(fork.Parent)
	var before *Stage
	for i, piece := range pieces {
		if piece.Fork != nil && piece.Fork.ID == fork.ID {
			if i > 0 {
				before = pieces[i-1].Stage
			}
			break
		}
	}
	if before == nil && fork.Parent != nil {
		first := "first"
		if len(fork.Routes) > 0 {
			first = fork.Routes[0].Name
		}
		warnings = append(warnings, "The fork with the "+validationNames(routeNames)+" starts its route, so nothing can pick a route: every run takes the "+first+" route.")
	} else if before != nil {
		for _, route := range fork.Routes[min(1, len(fork.Routes)):] {
			_, isTaken := taken[route.ID]
			if !isTaken && layout.Opens(route.ID) {
				warnings = append(warnings, "Nothing takes the "+route.Name+" route. Give the stage before the fork an outcome that takes it, or make it feed in.")
			}
		}
	}
	return warnings
}
