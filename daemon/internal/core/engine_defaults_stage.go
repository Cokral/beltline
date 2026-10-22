// The stages a new install
// adds one at a time, updates a stage to a newer agent, and the comparisons
// those need. The agents and outcomes are in engine_defaults.go.

package core

import (
	"slices"
	"strconv"
)

// DefaultsStage is a new approval or watch stage between before and later, with
// an id unique among existing.
func DefaultsStage(typ StageType, before, later, existing []Stage) Stage {
	return DefaultsStageAt(typ, DefaultsPlace{Before: before, Later: later}, existing)
}

// DefaultsStageAt is a new approval or watch stage at place.
func DefaultsStageAt(typ StageType, place DefaultsPlace, existing []Stage) Stage {
	return NewStage(StageArgs{ID: defaultsUniqueID(string(typ), existing), Type: typ, Outcomes: DefaultsOutcomesForAt(typ, place), Route: place.Route})
}

// DefaultsStageForAgent is a new stage with agent between before and later on a
// belt of kind, with an id unique among existing. Callers pass TriggerKindIssues
// for the default kind.
func DefaultsStageForAgent(agent Agent, before, later, existing []Stage, kind TriggerKind) Stage {
	return DefaultsStageForAgentAt(agent, DefaultsPlace{Before: before, Later: later}, existing, kind)
}

// DefaultsStageForAgentAt is DefaultsStageForAgent at place.
func DefaultsStageForAgentAt(agent Agent, place DefaultsPlace, existing []Stage, kind TriggerKind) Stage {
	key := StageKeysSuggestion(agent)
	name := agent.Name
	runner := agent.Runner
	workspace := agent.Workspace
	slots := agent.Slots
	ref := agent.Ref()
	stage := NewStage(StageArgs{
		ID: defaultsUniqueID(agent.ID, existing), Type: StageTypeAgent, Name: &name, Agent: &ref, Key: &key,
		Runner: &runner, Model: agent.Model, Thinking: agent.Thinking, Alternates: agent.Alternates,
		Workspace: &workspace, Slots: &slots, Outcomes: DefaultsOutcomesForAgentAt(agent, place), Route: place.Route,
	})
	if _, ok := kind.Provides()[InputBaseBranch]; !ok {
		// A review reads the pull request as checked out, and there is no worktree of ours.
		if stage.Workspace == WorkspaceWorktreeReadOnly {
			stage.Workspace = WorkspaceReadOnly
		}
		// Nothing on a review belt changes code, so a go-to with no stage to go to moves on to the review.
		for index, outcome := range agent.Outcomes {
			if outcome.Then.Action == ThenActionGoto && stage.Outcomes[index].Then.Action == ThenActionHold {
				stage.Outcomes[index].Then = NewThen(ThenArgs{Action: ThenActionNext})
			}
		}
	}
	return stage
}

// DefaultsUpdateAt is stage, between place's before and later stages, moved from
// version old of its agent to newAgent (a nil old moves only the pin).
//
// The brief and look follow the pin; each setting still as old set it takes
// newAgent's value, and what you changed on the stage stays. The stage's
// placeholder key stays too: a missing key is the old agent's key, not the new one's.
func DefaultsUpdateAt(stage Stage, old *Agent, newAgent Agent, place DefaultsPlace, kind TriggerKind) Stage {
	updated := stage.Clone()
	// Only a stage that never had a key takes the old agent's legacy name.
	// An explicitly empty key stays invalid rather than being restored.
	if updated.Key == nil && old != nil {
		key := old.Key
		updated.SetKey(&key)
	}
	ref := newAgent.Ref()
	updated.Agent = &ref
	if old == nil {
		return updated
	}
	was := DefaultsStageForAgentAt(*old, place, nil, kind)
	now := DefaultsStageForAgentAt(newAgent, place, nil, kind)
	if stage.Name == was.Name {
		updated.Name = now.Name
	}
	if SamePointee(stage.Runner, was.Runner) {
		updated.Runner = now.Runner
	}
	if stage.Model == was.Model {
		updated.Model = now.Model
	}
	if SamePointee(stage.Thinking, was.Thinking) {
		updated.Thinking = now.Thinking
	}
	if slices.EqualFunc(stage.Alternates, was.Alternates, modelPoolSame) {
		updated.Alternates = now.Alternates
	}
	if stage.Workspace == was.Workspace {
		updated.Workspace = now.Workspace
	}
	if stage.Slots == was.Slots {
		updated.Slots = now.Slots
	}
	if defaultsSame(stage.Outcomes, was.Outcomes) {
		updated.Outcomes = now.Outcomes
	}
	return updated
}

// defaultsSame reports whether two lists of outcomes say the same, whatever their
// ids.
func defaultsSame(a, b []Outcome) bool {
	return slices.EqualFunc(a, b, func(x, y Outcome) bool {
		return x.Name == y.Name && x.When == y.When && x.Tone == y.Tone && defaultsThenEqual(x.Then, y.Then)
	})
}

// defaultsThenEqual compares two Thens: the same action, targets,
// routes, output flag, limit and comment flag. Nil and empty lists are equal.
func defaultsThenEqual(a, b Then) bool {
	if a.Action != b.Action || a.WithOutput != b.WithOutput || a.Comment != b.Comment {
		return false
	}
	return slices.Equal(a.To, b.To) && slices.Equal(a.Routes, b.Routes) && SamePointee(a.Max, b.Max)
}

// defaultsUniqueID is base, or base with the first free "-n" suffix (from 2) among
// the existing stages' ids.
func defaultsUniqueID(base string, existing []Stage) string {
	id := base
	n := 2
	for validationHasStage(existing, id) {
		id = base + "-" + strconv.Itoa(n)
		n++
	}
	return id
}
