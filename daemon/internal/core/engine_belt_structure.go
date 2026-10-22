package core

import "fmt"

// BeltStructureRefusals are the problems that stop the belt being written at all.
// Nothing is saved when any exists. Only what cannot be written to disk or keyed
// by the stores is a rejection: a stage with no id has no folder, key or runs to
// attach to.
func BeltStructureRefusals(belt Belt) []LocatedProblem {
	var out []LocatedProblem
	for _, stage := range belt.Stages {
		if validationBlank(stage.ID, false) {
			out = append(out, LocatedProblem{
				Message:  "A stage needs an id, so the belt can't be saved with one missing.",
				Location: new(ProblemLocationStage(stage.ID)),
			})
		}
	}
	return append(out, RepositoryBaseProblems(belt)...)
}

// BeltStructureLocated lists every structural problem with the stage, fork, route,
// trigger, repository or outcome it is about. A warned belt saves and is paused.
func BeltStructureLocated(belt Belt) []LocatedProblem {
	var problems []LocatedProblem
	add := func(message string, location *ProblemLocation) {
		problems = append(problems, LocatedProblem{Message: message, Location: location})
	}

	stageIDs := map[string]struct{}{}
	for _, stage := range belt.Stages {
		if validationBlank(stage.ID, false) {
			add("A stage needs an id.", new(ProblemLocationStage(stage.ID)))
		} else if _, dup := stageIDs[stage.ID]; dup {
			add(fmt.Sprintf("Stage id %s is used by more than one stage.", stage.ID), new(ProblemLocationStage(stage.ID)))
		} else {
			stageIDs[stage.ID] = struct{}{}
		}
	}

	forkIDs := map[string]struct{}{}
	routeIDs := map[string]struct{}{}
	for _, fork := range belt.Forks {
		if _, dup := forkIDs[fork.ID]; dup {
			add(fmt.Sprintf("Fork id %s is used more than once.", fork.ID), new(ProblemLocationFork(fork.ID)))
		} else {
			forkIDs[fork.ID] = struct{}{}
		}
		for _, route := range fork.Routes {
			if _, dup := routeIDs[route.ID]; dup {
				add(fmt.Sprintf("Route id %s is used more than once.", route.ID), new(ProblemLocationRoute(route.ID)))
			} else {
				routeIDs[route.ID] = struct{}{}
			}
		}
	}

	entryIDs := map[string]struct{}{}
	for _, entry := range belt.Triggers {
		if _, dup := entryIDs[entry.ID]; dup {
			add(fmt.Sprintf("Trigger id %s is used more than once.", entry.ID), new(ProblemLocationTrigger(entry.ID)))
		} else {
			entryIDs[entry.ID] = struct{}{}
		}
		repos := map[string]struct{}{}
		for _, repo := range entry.Repos {
			if _, dup := repos[repo]; dup {
				add(fmt.Sprintf("Repository %s is listed more than once.", repo), new(ProblemLocationRepository(entry.ID, repo)))
			} else {
				repos[repo] = struct{}{}
			}
		}
	}

	for _, stage := range belt.Stages {
		if stage.Route != nil {
			if _, ok := routeIDs[*stage.Route]; !ok {
				add(fmt.Sprintf("Stage %s is on route %s, which is not on the belt.", stage.Name, *stage.Route),
					new(ProblemLocation{Kind: "stage", ID: new(stage.ID), Field: new("route")}))
			}
		}
		for _, outcome := range stage.Outcomes {
			location := ProblemLocation{Kind: "outcome", ID: new(stage.ID), Field: new(outcome.ID)}
			for _, target := range outcome.Then.To {
				if _, ok := stageIDs[target]; !ok {
					add(fmt.Sprintf("Outcome %s goes to stage %s, which is not on the belt.", outcome.Name, target), new(location))
				}
			}
			for _, route := range outcome.Then.Routes {
				if _, ok := routeIDs[route]; !ok {
					add(fmt.Sprintf("Outcome %s takes route %s, which is not on the belt.", outcome.Name, route), new(location))
				}
			}
		}
	}
	return append(problems, RepositoryBaseProblems(belt)...)
}
