// The stage menu's option lists: where a stage sits on its fork and the routes it can move to,
// the workspaces, watch targets and actions it can pick, and the goto targets a stage can name.
// The menu types and StageMenuMake are in present_stage_menu.go.

package core

import "slices"

// stageMenuCanShift reports whether stageID can move by step on belt.
func stageMenuCanShift(belt Belt, stageID string, step int) bool {
	moved := belt.Clone()
	moved.Shift(stageID, step)
	return !slices.EqualFunc(moved.Stages, belt.Stages, func(a, b Stage) bool { return a.ID == b.ID })
}

// stageMenuPlacement is where stage sits on its fork; nil for a stage that is not on one.
func stageMenuPlacement(stage Stage, belt Belt) *StageMenuPlacement {
	if stage.Route == nil {
		return nil
	}
	layout := belt.Layout()
	var fork *Fork
	if way := layout.Place(stage.ID); way != nil {
		forkCopy := way.Fork
		fork = &forkCopy
	}
	off := "None: on the line after the fork"
	if fork != nil && fork.Parent != nil {
		if route := layout.Route(*fork.Parent); route != nil {
			off = "After this fork, on " + route.Name
		}
	}
	routes := []StageMenuOption{}
	if fork != nil {
		for _, route := range fork.Routes {
			routes = append(routes, StageMenuOptionNew(route.ID, route.Name))
		}
	}
	return &StageMenuPlacement{Off: off, Routes: routes}
}

// stageMenuWorkspaces are the workspaces the stage can use on belt.
func stageMenuWorkspaces(stage Stage, belt Belt) []StageMenuOption {
	inputs := belt.MenuInputs()
	var options []Workspace
	for _, workspace := range AllWorkspace {
		if InputsSubset(workspace.Needs(), inputs) {
			options = append(options, workspace)
		}
	}
	if !slices.Contains(options, stage.Workspace) {
		options = append(options, stage.Workspace)
	}
	list := []StageMenuOption{}
	for _, workspace := range options {
		list = append(list, StageMenuOptionNew(string(workspace), workspace.Label()))
	}
	return list
}

// stageMenuWatchTargets are the watch targets belt offers for stage.
func stageMenuWatchTargets(stage Stage, belt Belt) []StageMenuOption {
	inputs := belt.MenuInputs()
	var options []WatchTarget
	for _, target := range AllWatchTarget {
		switch target {
		case WatchTargetTicket:
			if _, ok := inputs[InputTicket]; ok {
				options = append(options, target)
			}
		case WatchTargetPullRequest:
			if _, ok := inputs[InputPullRequest]; ok {
				options = append(options, target)
			}
		}
	}
	current := WatchTargetPullRequest
	if target := stage.WatchTarget(); target != nil {
		current = *target
	}
	if !slices.Contains(options, current) {
		options = append(options, current)
	}
	list := []StageMenuOption{}
	for _, target := range options {
		list = append(list, StageMenuOptionNew(string(target), target.Label()))
	}
	return list
}

// stageMenuActions are the outcome actions for stage on belt; taking a route is offered
// once the stage has a fork right after it, or when an outcome already takes one.
func stageMenuActions(stage Stage, belt Belt) []StageMenuOption {
	routes := len(belt.Layout().RoutesAfter(stage.ID)) > 0
	for _, outcome := range stage.Outcomes {
		if outcome.Then.Action == ThenActionRoute {
			routes = true
		}
	}
	list := []StageMenuOption{}
	for _, action := range AllThenAction {
		if action != ThenActionRoute || routes {
			list = append(list, StageMenuOptionNew(string(action), action.Label()))
		}
	}
	return list
}

// stageMenuGotoTargets is every stage a `goto` can name, with the counterparts a pick adds
// on each other route of a fork it comes back into, and the stages each of those routes
// can take instead. A route that feeds in is never asked for.
func stageMenuGotoTargets(stage Stage, belt Belt) []StageMenuGotoTarget {
	layout := belt.Layout()
	source := map[string]struct{}{}
	for _, way := range layout.Ancestry(stage.ID) {
		source[way.Fork.ID] = struct{}{}
	}
	targets := []StageMenuGotoTarget{}
	for _, target := range belt.Stages {
		if target.ID == stage.ID {
			continue
		}
		var others []Route
		for _, way := range layout.Ancestry(target.ID) {
			if _, taken := source[way.Fork.ID]; taken || way.Route.Feeds {
				continue
			}
			for _, route := range way.Fork.Routes {
				if route.ID != way.Route.ID && !route.Feeds && len(layout.StagesWithin(route.ID)) > 0 {
					others = append(others, route)
				}
			}
		}
		gotoRoutes := []StageMenuGotoRoute{}
		for _, route := range others {
			path := stageMenuRoutePath(route.ID, layout)
			stages := []StageMenuOption{}
			for _, within := range layout.StagesWithin(route.ID) {
				stages = append(stages, StageMenuOptionNew(within.ID, path+": "+within.Name))
			}
			gotoRoutes = append(gotoRoutes, StageMenuGotoRoute{Route: StageMenuOptionNew(route.ID, path), Stages: stages})
		}
		targets = append(targets, StageMenuGotoTarget{
			ID:           target.ID,
			Label:        stageMenuLabel(target, layout),
			Counterparts: layout.Counterparts(target.ID, stage.ID),
			Others:       gotoRoutes,
		})
	}
	return targets
}
