// Coordinator tests of Continue sessions: an interactive session that resumes a finished agent attempt.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// coordContinue is the continue-session fixture: a run held at triage after
// "needs info", set to state, on a simulated runner whose sessions never end.
type coordContinue struct {
	h   *harness
	run core.Run
	sim *runner.SimulatedRunner
}

// newH5m2Continue builds the fixture. The coordinator is the harness's.
func newH5m2Continue(t *testing.T, state core.RunState) coordContinue {
	t.Helper()
	run := coordSampleRun(defaultBelt())
	coordFinish(&run, new("needs info"))
	run.State = state
	var sim *runner.SimulatedRunner
	h := newHarness(t, harnessOptions{
		Issues: []core.Subject{},
		Prepare: func(storage store.Storage) {
			if err := storage.SaveRun(run); err != nil {
				t.Fatalf("save run: %v", err)
			}
		},
		Runner: func(*harnessScript) runner.AgentRunner {
			// A constant clock: the session never reaches its 10000 seconds.
			sim = runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 10000 },
				func() time.Time { return testkit.ContractTime }, nil)
			return sim
		},
	})
	stored := h.Coord().Run(run.ID)
	if stored == nil {
		t.Fatal("the fixture run was not loaded")
	}
	return coordContinue{h: h, run: *stored, sim: sim}
}

// open starts the Continue session of the run's first attempt on the simulated runner, as
//
//	// open(coordinator, run:, runner:) does, and returns its spec.
func (f coordContinue) open(t *testing.T, c *Coordinator, run core.Run, sim *runner.SimulatedRunner) runner.SessionSpec {
	t.Helper()
	spec := c.Spec(run, run.Attempts[0], "")
	if spec == nil {
		t.Fatal("no spec for the first attempt")
	}
	continuing := *spec
	continuing.Name = runner.TmuxContinueName(spec.Name)
	if err := sim.Start(context.Background(), continuing); err != nil {
		t.Fatalf("start continue: %v", err)
	}
	return continuing
}

// TestSameStageKeepsContinueUntilResumedElsewhere: a continue session lives while the run waits at its stage, and ends once it resumes elsewhere.
func TestSameStageKeepsContinueUntilResumedElsewhere(t *testing.T) {
	t.Parallel()
	for _, state := range []core.RunState{core.RunStateHeld, core.RunStateParked} {
		t.Run(string(state), func(t *testing.T) {
			f := newH5m2Continue(t, state)
			c := f.h.Coord()
			continuing := f.open(t, c, f.run, f.sim)
			f.h.Tick()
			if !f.sim.IsRunning(continuing.Name) {
				t.Fatal("the continue session stopped while the run waited at its stage")
			}
			c.Resume(f.run.ID, new("spec"), nil, false, nil)
			f.h.Tick()
			if f.sim.IsRunning(continuing.Name) {
				t.Error("the continue session outlived the resume to another stage")
			}
		})
	}
}

// TestDropStopsAndRepeatedTicksChangeNeitherRunNorLogs: a dropped run ends its continue session, and later ticks change nothing.
func TestDropStopsAndRepeatedTicksChangeNeitherRunNorLogs(t *testing.T) {
	t.Parallel()
	f := newH5m2Continue(t, core.RunStateHeld)
	c := f.h.Coord()
	continuing := f.open(t, c, f.run, f.sim)
	original := filepath.Join(c.Paths().AttemptFolder(f.run, f.run.Attempts[0]), "session.log")
	if err := os.WriteFile(original, []byte("original bytes\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c.Drop(f.run.ID, nil)
	f.h.Tick()
	if f.sim.IsRunning(continuing.Name) {
		t.Fatal("the continue session is still running after the drop")
	}
	saved := f.h.storage.LoadRuns()
	continueLog := filepath.Join(c.Paths().AttemptFolder(f.run, f.run.Attempts[0]), "continue.log")
	log, err := os.ReadFile(continueLog)
	if err != nil {
		t.Fatalf("read continue.log: %v", err)
	}
	f.h.Tick()
	f.h.Tick()
	if !coordSame(f.h.storage.LoadRuns(), saved) {
		t.Error("repeated ticks changed the saved runs")
	}
	if got, _ := os.ReadFile(original); string(got) != "original bytes\n" {
		t.Errorf("session.log changed to %q", got)
	}
	if got, _ := os.ReadFile(continueLog); string(got) != string(log) {
		t.Errorf("continue.log changed to %q", got)
	}
	if ended := f.sim.EndedContinues(); len(ended) != 1 || ended[0] != continuing.Name {
		t.Errorf("ended continues = %v, want [%s]", ended, continuing.Name)
	}
}

// TestRestartSweepsPersistedStaleAttempts: a restart ends a continue session its run no longer needs.
func TestRestartSweepsPersistedStaleAttempts(t *testing.T) {
	t.Parallel()
	for _, state := range []core.RunState{core.RunStateShipped, core.RunStateDropped, core.RunStateHeld} {
		t.Run(string(state), func(t *testing.T) {
			f := newH5m2Continue(t, core.RunStateHeld)
			c := f.h.Coord()
			continuing := f.open(t, c, f.run, f.sim)
			moved := f.run
			moved.State = state
			if state == core.RunStateHeld {
				moved.StageID = "spec"
			}
			if err := f.h.storage.SaveRun(moved); err != nil {
				t.Fatalf("save: %v", err)
			}
			// The runner stands for the private tmux server shared across app launches.
			restarted := f.h.RestartWith(f.sim)
			restarted.Tick(context.Background())
			restarted.Idle()
			if f.sim.IsRunning(continuing.Name) {
				t.Error("the stale continue session is still running after the restart")
			}
			if saved := f.h.storage.LoadRuns(); !coordSame(saved, []core.Run{moved}) {
				t.Errorf("saved runs = %+v, want the moved run only", saved)
			}
		})
	}
}

// TestAgentOutcomeReconcilesContinue: the agent's outcome decides whether a continue session stays or the run moves on.
func TestAgentOutcomeReconcilesContinue(t *testing.T) {
	t.Parallel()
	for _, outcome := range []string{"retry", "next", "hold", "park"} {
		t.Run(outcome, func(t *testing.T) {
			var then core.Then
			switch outcome {
			case "retry":
				then = core.NewThen(core.ThenArgs{Action: core.ThenActionRetry})
			case "hold":
				then = core.NewThen(core.ThenArgs{Action: core.ThenActionHold})
			case "park":
				then = core.NewThen(core.ThenArgs{Action: core.ThenActionPark})
			default:
				then = core.NewThen(core.ThenArgs{Action: core.ThenActionGoto, To: []string{"spec"}})
			}
			initial := coordSampleRun(defaultBelt())
			coordFinish(&initial, new("needs info"))
			run := initial
			run.State = core.RunStateRunning
			run.Attempts[0].EndedAt = nil
			run.Stages[0].Outcomes = []core.Outcome{{ID: "outcome-" + outcome, Name: outcome, When: "", Tone: core.ToneNeutral, Then: then}}
			var sim *runner.SimulatedRunner
			h := newHarness(t, harnessOptions{
				Issues: []core.Subject{},
				Prepare: func(storage store.Storage) {
					if err := storage.SaveRun(run); err != nil {
						t.Fatalf("save run: %v", err)
					}
				},
				Runner: func(*harnessScript) runner.AgentRunner {
					pick := outcome
					sim = runner.NewSimulatedRunner(func(runner.SessionSpec) float64 { return 0 },
						func() time.Time { return testkit.ContractTime }, func(runner.SessionSpec) *string { return &pick })
					return sim
				},
			})
			f := coordContinue{h: h, run: run, sim: sim}
			c := h.Coord()
			saved := c.Run(run.ID)
			if saved == nil {
				t.Fatal("the run is gone")
			}
			spec := c.Spec(*saved, saved.Attempts[0], "")
			if spec == nil {
				t.Fatal("no spec for the attempt")
			}
			if err := sim.Start(context.Background(), *spec); err != nil {
				t.Fatalf("start: %v", err)
			}
			continuing := f.open(t, c, *saved, sim)
			h.Tick()
			stays := outcome == "hold" || outcome == "park"
			if sim.IsRunning(continuing.Name) != stays {
				t.Errorf("continue running = %v, want %v", sim.IsRunning(continuing.Name), stays)
			}
			current := c.Run(run.ID)
			if current == nil {
				t.Fatal("the run is gone")
			}
			wantAttempts := 2
			if stays {
				wantAttempts = 1
			}
			if len(current.Attempts) != wantAttempts {
				t.Errorf("attempts = %d, want %d", len(current.Attempts), wantAttempts)
			}
			wantStage := "triage"
			if outcome == "next" {
				wantStage = "spec"
			}
			if current.StageID != wantStage {
				t.Errorf("stage = %s, want %s", current.StageID, wantStage)
			}
			if stays {
				c.Resume(run.ID, new("spec"), nil, false, nil)
				h.Tick()
				if sim.IsRunning(continuing.Name) {
					t.Error("the continue session outlived the resume")
				}
			}
		})
	}
}

// TestAbsentContinueIsANoopAndInvalidStartsGiveReasons: with no checkout a tick changes nothing, and each invalid start says why.
func TestAbsentContinueIsANoopAndInvalidStartsGiveReasons(t *testing.T) {
	t.Parallel()
	f := newH5m2Continue(t, core.RunStateHeld)
	c := f.h.Coord()
	f.h.Tick()
	if saved := f.h.storage.LoadRuns(); !coordSame(saved, []core.Run{f.run}) {
		t.Errorf("a tick changed the run")
	}
	if _, err := c.ContinueSession(context.Background(), f.run.ID, f.run.Attempts[0]); err == nil {
		t.Error("a continuation without a checkout was accepted")
	} else if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("error = %q, want the checkout reason", err.Error())
	}
	for _, state := range []core.RunState{core.RunStateShipped, core.RunStateDropped, core.RunStateHeld} {
		run := f.run
		run.State = state
		if state == core.RunStateHeld {
			run.StageID = "spec"
		}
		if err := f.h.storage.SaveRun(run); err != nil {
			t.Fatalf("save: %v", err)
		}
		restarted := f.h.RestartWith(runner.NewTmuxRunner(nil))
		_, err := restarted.ContinueSession(context.Background(), run.ID, run.Attempts[0])
		if err == nil {
			t.Errorf("%s: a stale attempt was accepted", state)
			continue
		}
		want := string(state)
		if state == core.RunStateHeld {
			want = "left Triage"
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %q, want %q", state, err.Error(), want)
		}
	}
}
