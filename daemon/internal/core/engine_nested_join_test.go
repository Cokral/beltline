package core

import "testing"

// nestedRunAt is a run over the nested fixture with the given legs and routes,
// on the line before wave and with the run's own branch.
func nestedRunAt(legs []Leg, routes map[string][]string) Run {
	belt := nestedFixtureBelt(false, true, true)
	run := Run{ID: "run", Stages: belt.Stages, Forks: belt.Forks, Legs: legs, Branch: new("run-branch")}
	run.SetRoutes(routes)
	return run
}

// TestEngineNestedClearLeavesAForkAnotherLegIsInside: a fork another leg still
// works inside keeps its choice; one nobody is in is forgotten.
func TestEngineNestedClearLeavesAForkAnotherLegIsInside(t *testing.T) {
	t.Parallel()
	legs := []Leg{
		{ID: "r:build", State: RunStateRunning, StageID: new("assess")},
		{ID: "r:quick", State: RunStateRunning, StageID: new("quick-build")},
	}
	run := nestedRunAt(legs, map[string][]string{"wave": {"build"}, "pace": {"quick"}})
	engineNestedClear(&run, "r:quick", 0)
	routes := run.Routes()
	if _, ok := routes["wave"]; !ok {
		t.Fatalf("wave forgotten while r:build is inside it: %v", routes)
	}
	if _, ok := routes["pace"]; ok {
		t.Fatalf("pace kept though only r:quick, the leg asking, was inside it: %v", routes)
	}
}

// TestEngineNestedWaitsOnlyOutsideTheSplitTheLegIsIn: a stage outside the leg's
// open split waits for the split; one inside does not.
func TestEngineNestedWaitsOnlyOutsideTheSplitTheLegIsIn(t *testing.T) {
	t.Parallel()
	join := Join{ID: "j:pace:1", ForkID: "pace", Routes: []string{"quick", "thorough"}, Legs: []string{"r:quick"}}
	run := nestedRunAt([]Leg{{ID: "r:quick", State: RunStateRunning, StageID: new("quick-build"), Join: new(join.ID)}}, nil)
	run.Joins = []Join{join}
	if !engineNestedWaits(run, "verify", "r:quick") {
		t.Fatal("verify is outside pace, so the leg must wait")
	}
	if engineNestedWaits(run, "thorough-spec", "r:quick") {
		t.Fatal("thorough-spec is inside pace, so the leg must not wait")
	}
	run.Joins[0].Joined = true
	if engineNestedWaits(run, "verify", "r:quick") {
		t.Fatal("a joined split has nothing to wait for")
	}
}

// TestEngineJoinReadyNeedsEveryLegFinished: a join is ready when each of its legs
// has finished, or when it has only child joins that are all joined.
func TestEngineJoinReadyNeedsEveryLegFinished(t *testing.T) {
	t.Parallel()
	join := Join{ID: "j:pace:1", ForkID: "pace", Legs: []string{"r:quick", "r:thorough"}}
	run := nestedRunAt([]Leg{
		{ID: "r:quick", State: RunStateShipped},
		{ID: "r:thorough", State: RunStateRunning, StageID: new("thorough-build")},
	}, nil)
	run.Joins = []Join{join}
	if engineJoinReady(run, join) {
		t.Fatal("a running leg must hold the join")
	}
	run.Legs[1].State = RunStateDropped
	if !engineJoinReady(run, join) {
		t.Fatal("finished legs must let the join go")
	}
	empty := Join{ID: "j:empty", ForkID: "pace"}
	if engineJoinReady(run, empty) {
		t.Fatal("a join with no legs and no children is not ready")
	}
	parent := Join{ID: "j:outer", ForkID: "wave"}
	child := Join{ID: "j:inner", ForkID: "pace", Parent: new(parent.ID), Joined: false}
	run.Joins = []Join{parent, child}
	if engineJoinReady(run, parent) {
		t.Fatal("an unjoined child join must hold its parent")
	}
	run.Joins[1].Joined = true
	if !engineJoinReady(run, parent) {
		t.Fatal("a parent with only joined children and no legs of its own is ready")
	}
}

// TestEngineFreshJoinAndLegSkipTakenIds: new ids never repeat one the run holds.
func TestEngineFreshJoinAndLegSkipTakenIds(t *testing.T) {
	t.Parallel()
	run := nestedRunAt(nil, nil)
	if got := engineFreshJoin(run, "pace"); got != "j:pace:1" {
		t.Fatalf("fresh join = %s, want j:pace:1", got)
	}
	run.Joins = []Join{{ID: "j:pace:1"}}
	if got := engineFreshJoin(run, "pace"); got != "j:pace:2" {
		t.Fatalf("fresh join after one = %s, want j:pace:2", got)
	}
	run.Joins = []Join{{ID: "j:pace:2"}}
	if got := engineFreshJoin(run, "pace"); got != "j:pace:3" {
		t.Fatalf("fresh join when :2 is taken = %s, want j:pace:3", got)
	}

	run.Legs = []Leg{{ID: "r:quick"}}
	if got := engineFreshLeg(run, "quick"); got != "r:quick#2" {
		t.Fatalf("fresh leg = %s, want r:quick#2", got)
	}
	run.Legs = []Leg{{ID: "r:quick"}, {ID: "r:quick#2"}}
	if got := engineFreshLeg(run, "quick"); got != "r:quick#3" {
		t.Fatalf("fresh leg when #2 is taken = %s, want r:quick#3", got)
	}
	if got := engineFreshLeg(run, "thorough"); got != "r:thorough" {
		t.Fatalf("fresh leg for a free route = %s, want r:thorough", got)
	}
}

// TestEngineJoinContextNamesTheRoutesAndTheBranch: the merge context names the
// routes and where they merge.
func TestEngineJoinContextNamesTheRoutesAndTheBranch(t *testing.T) {
	t.Parallel()
	run := nestedRunAt(nil, nil)
	layout := run.Layout()
	two := Join{ID: "j", Routes: []string{"quick", "thorough"}}
	if got := engineJoinContext(two, layout, nil, run); got != "the Quick and Thorough routes into the run's branch" {
		t.Fatalf("context = %q", got)
	}
	if got := engineJoinContext(two, layout, new("run-branch"), run); got != "the Quick and Thorough routes into the run's branch" {
		t.Fatalf("context into the run's own branch = %q", got)
	}
	if got := engineJoinContext(two, layout, new("build-branch"), run); got != "the Quick and Thorough routes into build-branch" {
		t.Fatalf("context into a route branch = %q", got)
	}
	one := Join{ID: "j", Routes: []string{"thorough"}}
	if got := engineJoinContext(one, layout, nil, run); got != "the Thorough route into the run's branch" {
		t.Fatalf("context for one route = %q", got)
	}
}

// TestEngineBubbledClimbsOutOfAForkWithNothingAfterIt: a fork that ends its route
// hands the next stage to the fork around it.
func TestEngineBubbledClimbsOutOfAForkWithNothingAfterIt(t *testing.T) {
	t.Parallel()
	layout := nestedFixtureBelt(false, true, true).Layout()
	step := engineBubbled(layout, "pace")
	if step == nil || step.Stage == nil || *step.Stage != "verify" {
		t.Fatalf("pace resumes at %v, want verify", step)
	}
	noVerify := nestedFixtureBelt(false, false, true).Layout()
	step = engineBubbled(noVerify, "pace")
	if step == nil || step.Stage == nil || *step.Stage != "ship" {
		t.Fatalf("pace with no verify resumes at %v, want ship", step)
	}
	if engineBubbled(layout, "missing") != nil {
		t.Fatal("an unknown fork has no step")
	}
}

// TestEngineMergeQueuesBehindTheMergeUnderWay: one merge runs at a time; the next
// one starts when the one under way is over.
func TestEngineMergeQueuesBehindTheMergeUnderWay(t *testing.T) {
	t.Parallel()
	run := nestedRunAt(nil, nil)
	first := engineMerge(&run, Merge{Branches: []string{"a"}, Into: new("run-branch")})
	if len(first) != 1 || first[0].Kind != EffectKindMergeLegs || len(first[0].Legs) != 1 || first[0].Legs[0] != "a" {
		t.Fatalf("first merge effects = %+v, want one mergeLegs for a", first)
	}
	second := engineMerge(&run, Merge{Branches: []string{"b"}})
	if len(second) != 0 || len(run.MergesWaiting) != 1 {
		t.Fatalf("second merge effects = %+v, waiting = %d; want none queued", second, len(run.MergesWaiting))
	}
	if got := engineNextMerge(&run); len(got) != 0 || len(run.MergesWaiting) != 1 {
		t.Fatalf("next merge started while the first is under way: effects %+v, waiting %d", got, len(run.MergesWaiting))
	}
	run.MergeBranches = nil
	next := engineNextMerge(&run)
	if len(next) != 1 || len(next[0].Legs) != 1 || next[0].Legs[0] != "b" {
		t.Fatalf("next merge effects = %+v, want one mergeLegs for b", next)
	}
	if len(run.MergesWaiting) != 0 || run.MergeInto != nil {
		t.Fatalf("after next merge: waiting = %d, into = %v", len(run.MergesWaiting), run.MergeInto)
	}
}
