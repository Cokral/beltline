// The split run the run-read tests append to the demo: a fork with the thorough route queued, built at
// now.

package servertest

import (
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// SplitRun is the split run of the contract tests: a manual belt whose run is at the simple route of
// a fork, with the thorough route queued. It is built at now.
func SplitRun(now time.Time) core.Run {
	simpleRoute, thoroughRoute := "simple", "thorough"
	triage := core.NewStage(core.StageArgs{ID: "triage", Type: core.StageTypeAgent, Name: new("Triage"), Outcomes: []core.Outcome{}})
	simple := core.NewStage(core.StageArgs{ID: "simple", Type: core.StageTypeAgent, Name: new("Simple build"),
		Outcomes: []core.Outcome{}, Route: &simpleRoute})
	thorough := core.NewStage(core.StageArgs{ID: "thorough", Type: core.StageTypeAgent, Name: new("Thorough build"),
		Outcomes: []core.Outcome{}, Route: &thoroughRoute})
	fork := core.Fork{ID: "scope", Routes: []core.Route{{ID: "simple", Name: "Simple"}, {ID: "thorough", Name: "Thorough"}}}
	belt := core.Belt{
		ID: "split-view", Name: "Split view", Version: 1, Policies: core.DefaultPolicies(),
		Triggers: []core.TriggerEntry{{ID: "1", Trigger: core.Trigger{Kind: core.TriggerKindManual}, Repos: []string{"acme/read"}}},
		Stages:   []core.Stage{triage, simple, thorough}, Forks: []core.Fork{fork},
	}
	subject := core.Subject{Kind: core.SubjectKindManual, Repo: "acme/read", Key: "split", Title: "Split view", IsOpen: true}
	run := core.NewRun(core.RunArgs{Belt: belt, Subject: subject, Now: now})
	run.ID = "split/view"
	run.StageID = simple.ID
	run.State = core.RunStateRunning
	run.SetRoutes(map[string][]string{fork.ID: {"simple", "thorough"}})
	simpleStage, thoroughStage := simple.ID, thorough.ID
	run.Legs = []core.Leg{
		{ID: "r:simple", Route: &simpleRoute, StageID: &simpleStage, State: core.RunStateRunning},
		{ID: "r:thorough", Route: &thoroughRoute, StageID: &thoroughStage, State: core.RunStateQueued},
	}
	return run
}
