// Coordinator tests of a stage's model pool, run through the coordinator.

package coordinator

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// poolAdmission is the admission the pool tests run under: pi allows a and b.
func poolAdmission() core.ModelAdmission {
	return core.NewModelAdmission(map[string][]core.ModelChoice{
		"pi": {{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	}, nil, nil)
}

// coordPooledBelt is the default belt, its first stage on pi with
// model a and an alternate b, six slots.
func coordPooledBelt(c *Coordinator) core.Belt {
	pi := core.BackendPi
	belt := c.Belts()[0].Belt
	belt.Stages[0].Runner = &pi
	belt.Stages[0].Model = "a"
	belt.Stages[0].Thinking = nil
	belt.Stages[0].Alternates = []core.SessionSettings{{Model: "b", Runner: &pi}}
	belt.Stages[0].Slots = 6
	return belt
}

// poolRun is add(c, belt, number:): a pooled run with the pool admission, saved.
func poolRun(c *Coordinator, belt core.Belt, number int) core.Run {
	run := core.NewRun(core.RunArgs{
		Belt:    belt,
		Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: number, Title: fmt.Sprintf("Issue %d", number)},
		Now:     sessionSampleT0,
	})
	admission := poolAdmission()
	run.ModelAdmission = &admission
	return coordAdd(c, run)
}

// TestFreshSessionsSpreadAcrossThePool: fresh sessions take the pool's settings the fewest sessions use.
func TestFreshSessionsSpreadAcrossThePool(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	sim := newH5m2StartsRunner(h)
	c := h.RestartWith(sim)
	h.Turn(func() { c.sessions.random = core.NewPoolRandom(3) })
	belt := coordPooledBelt(c)
	var models []string
	for number := 1; number <= 4; number++ {
		run := poolRun(c, belt, number)
		coordStart(c, run.ID)
		current := c.Run(run.ID)
		if current == nil || current.CurrentAttempt() == nil {
			t.Fatalf("run %d has no attempt after its start", number)
		}
		attempt := current.CurrentAttempt()
		starts := sim.Starts()
		if len(starts) == 0 {
			t.Fatalf("run %d was not started", number)
		}
		started := starts[len(starts)-1]
		if attempt.Settings == nil || attempt.Settings.Model != started.Model {
			t.Fatalf("run %d: attempt model %+v, started %q", number, attempt.Settings, started.Model)
		}
		if attempt.Settings.Runner == nil || *attempt.Settings.Runner != core.BackendPi {
			t.Fatalf("run %d: attempt runner %+v, want pi", number, attempt.Settings.Runner)
		}
		models = append(models, started.Model)
	}
	// Each second session evens the count out, whichever the tie drew.
	if models[0] == models[1] {
		t.Errorf("the first two sessions share model %q", models[0])
	}
	if models[2] == models[3] {
		t.Errorf("the last two sessions share model %q", models[2])
	}
	count := 0
	for _, model := range models {
		if model == "a" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("model a runs %d times, want 2 (%v)", count, models)
	}
}

// TestARunComingBackKeepsItsModel: a run that comes back to the stage keeps the settings it had there.
func TestARunComingBackKeepsItsModel(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.RestartWith(newH5m2StartsRunner(h))
	h.Turn(func() { c.sessions.random = core.NewPoolRandom(5) })
	belt := coordPooledBelt(c)
	// Three other runs at the stage: one model has two of them.
	for number := 2; number <= 4; number++ {
		coordStart(c, poolRun(c, belt, number).ID)
	}
	var busy []string
	for _, run := range c.Runs() {
		if current := run.CurrentAttempt(); current != nil && current.Settings != nil {
			busy = append(busy, current.Settings.Model)
		}
	}
	if len(busy) != 3 {
		t.Fatalf("%d runs are busy, want 3 (%v)", len(busy), busy)
	}
	busier, quieter := "b", "a"
	if countOutcome(busy, "a") == 2 {
		busier, quieter = "a", "b"
	}
	run := poolRun(c, belt, 1)
	stage := run.Stages[0]
	var got *core.SessionSettings
	locked(c, func() { got = c.sessions.pooledSettings(run, stage, "main") })
	if got == nil || got.Model != quieter {
		t.Fatalf("fresh pick = %+v, want %s", got, quieter)
	}
	earlier := core.NewAttempt(core.AttemptArgs{StageID: stage.ID, Number: 1, StartedAt: jsonx.Date{Time: sessionSampleT0}, Folder: "f"})
	ended := jsonx.Date{Time: sessionSampleT0}
	earlier.EndedAt = &ended
	pi := core.BackendPi
	earlier.Settings = &core.SessionSettings{Model: busier, Runner: &pi}
	run.Attempts = []core.Attempt{earlier}
	locked(c, func() { got = c.sessions.pooledSettings(run, stage, "main") })
	if got == nil || got.Model != busier {
		t.Fatalf("returning pick = %+v, want %s", got, busier)
	}
}

// TestAdjustingTheStagePinsTheRunToThatModel: an adjustment to one pool model makes it the stage's only model for the run.
func TestAdjustingTheStagePinsTheRunToThatModel(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	run := poolRun(c, coordPooledBelt(c), 1)
	pi := core.BackendPi
	if _, err := c.AdjustStage(run.ID, run.Stages[0].ID, new("b"), nil, nil, &pi); err != nil {
		t.Fatalf("adjust: %v", err)
	}
	saved := c.Run(run.ID)
	if saved == nil {
		t.Fatal("the run is gone")
	}
	if len(saved.Stages[0].Alternates) != 0 {
		t.Errorf("alternates = %+v, want none", saved.Stages[0].Alternates)
	}
	want := []core.SessionSettings{{Model: "b", Runner: &pi}}
	if !reflect.DeepEqual(saved.Stages[0].Pool(), want) {
		t.Errorf("pool = %+v, want %+v", saved.Stages[0].Pool(), want)
	}
}
