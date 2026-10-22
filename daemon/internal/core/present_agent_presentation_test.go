package core

import (
	"reflect"
	"testing"
)

// TestOutcomeTargetsListTheOtherLibraryAgentsInOrder checks that an agent's outcomes may name every
// other library agent, archived ones included, in the library's order, and never the agent itself.
func TestOutcomeTargetsListTheOtherLibraryAgentsInOrder(t *testing.T) {
	t.Parallel()
	records := []AgentRecord{
		{Agent: Agent{ID: "spec", Name: "Spec"}},
		{Agent: Agent{ID: "build", Name: "Build"}},
		{Agent: Agent{ID: "review", Name: "Review"}, Archived: true},
	}
	got := AgentOutcomeTargets("build", records)
	want := []AgentOutcomeTarget{{ID: "spec", Label: "Spec"}, {ID: "review", Label: "Review"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets for build = %v, want %v", got, want)
	}
}

// TestOutcomeTargetsEncodeAsAnEmptyListWithNoAgents checks that an empty library gives [] on the wire, not null.
func TestOutcomeTargetsEncodeAsAnEmptyListWithNoAgents(t *testing.T) {
	t.Parallel()
	got := AgentOutcomeTargets("solo", []AgentRecord{{Agent: Agent{ID: "solo", Name: "Solo"}}})
	if got == nil || len(got) != 0 {
		t.Fatalf("targets with only the agent itself = %#v, want an empty non-nil list", got)
	}
}
