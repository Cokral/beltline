// Coordinator tests of the usage a session's interval reports across a reload and across legs.

package coordinator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// coordWriteOutcome writes the outcome into the
// open attempt's output file, as the agent would.
func coordWriteOutcome(t *testing.T, c *Coordinator, outcome string) {
	t.Helper()
	runs := c.Runs()
	if len(runs) == 0 {
		t.Fatal("no run")
	}
	run := runs[0]
	var attempt *core.Attempt
	for index := range run.Attempts {
		if run.Attempts[index].IsOpen() {
			attempt = &run.Attempts[index]
		}
	}
	if attempt == nil {
		t.Fatal("no open attempt")
	}
	stage := findStage(run, attempt.StageID)
	if stage == nil {
		t.Fatal("the attempt's stage is gone")
	}
	folder := c.Paths().AttemptFolder(run, *attempt)
	writeText(t, filepath.Join(folder, stage.Writes()), "Outcome: "+outcome+"\n")
}

// TestIntervalsSurviveReloadAndASecondCompletion: a session's usage interval survives a reload and a second completion.
func TestIntervalsSurviveReloadAndASecondCompletion(t *testing.T) {
	t.Parallel()
	for _, reports := range []bool{true, false} {
		t.Run(map[bool]string{true: "reports", false: "silent"}[reports], func(t *testing.T) {
			belt := defaultBelt()
			belt.Policies.CommentOnHold = false
			h := newHarness(t, harnessOptions{Belts: []core.Belt{belt}, Issues: []core.Subject{}})
			boundary := newBoundaryRunner(h, t, reports)
			coord := h.RestartWith(boundary)
			run := coordAdd(coord, core.NewRun(core.RunArgs{Belt: belt, Subject: harnessIssue(3, "Bounds"), Now: sessionSampleT0}))
			coordStart(coord, run.ID)
			boundary.bump(40)
			reloaded := h.RestartWith(boundary)
			coordWriteOutcome(t, reloaded, "needs info")
			locked(reloaded, func() { reloaded.sessions.check(context.Background()) })
			locked(reloaded, func() { reloaded.sessions.check(context.Background()) })
			runs := reloaded.Runs()
			if len(runs) == 0 || len(runs[0].Attempts) == 0 {
				t.Fatal("the reloaded run has no attempt")
			}
			first := runs[0].Attempts[0]
			if first.Outcome == nil || *first.Outcome != "needs info" {
				t.Fatalf("outcome = %v, want needs info", first.Outcome)
			}
			if reports {
				if first.Usage == nil || first.Usage.CostNanos == nil || *first.Usage.CostNanos != 40 {
					t.Errorf("usage = %+v, want cost 40", first.Usage)
				}
			} else if first.Usage != nil && first.Usage.CostNanos != nil {
				t.Errorf("usage = %+v, want none", first.Usage)
			}
			boundary.bump(15)
			locked(reloaded, func() {
				continueWith := "continue"
				reloaded.book.apply(run.ID, core.Event{Kind: core.EventKindAnswered, Answer: &continueWith}, nil, nil, nil)
			})
			locked(reloaded, func() { reloaded.sessions.startQueued(context.Background()) })
			boundary.bump(25)
			coordWriteOutcome(t, reloaded, "ready to spec")
			locked(reloaded, func() { reloaded.sessions.check(context.Background()) })
			locked(reloaded, func() { reloaded.sessions.check(context.Background()) })
			saved := h.storage.LoadRuns()
			if len(saved) == 0 || len(saved[0].Attempts) < 2 {
				t.Fatalf("saved attempts = %+v, want two", saved)
			}
			outcomes := []string{*saved[0].Attempts[0].Outcome, *saved[0].Attempts[1].Outcome}
			if outcomes[0] != "needs info" || outcomes[1] != "ready to spec" {
				t.Errorf("outcomes = %v, want [needs info ready to spec]", outcomes)
			}
			for index, want := range []*int{new(40), new(25)} {
				got := saved[0].Attempts[index].Usage
				if !reports {
					if got != nil && got.CostNanos != nil {
						t.Errorf("attempt %d cost = %d, want none", index, *got.CostNanos)
					}
					continue
				}
				if got == nil || got.CostNanos == nil || *got.CostNanos != *want {
					t.Errorf("attempt %d usage = %+v, want cost %d", index, got, *want)
				}
			}
			if saved[0].Attempts[1].SessionID == nil || saved[0].Attempts[0].SessionID == nil ||
				*saved[0].Attempts[1].SessionID != *first.SessionID {
				t.Errorf("the second attempt does not share the first session")
			}
		})
	}
}

// TestConcurrentLegsKeepSeparateSessions: two legs running at once keep their own sessions and their own usage.
func TestConcurrentLegsKeepSeparateSessions(t *testing.T) {
	t.Parallel()
	belt := defaultBelt()
	run := core.NewRun(core.RunArgs{Belt: belt, Subject: harnessIssue(4, "Legs"), Now: sessionSampleT0})
	complexRoute, simpleRoute := "complex", "simple"
	triage, spec := "triage", "spec"
	run.Legs = []core.Leg{
		{ID: "r:complex", Route: &complexRoute, StageID: &triage, State: core.RunStateRunning},
		{ID: "r:simple", Route: &simpleRoute, StageID: &spec, State: core.RunStateRunning},
	}
	run.State = core.RunStateRunning
	open := func(stage, leg, session string) core.Attempt {
		legID := leg
		attempt := core.NewAttempt(core.AttemptArgs{
			StageID: stage, Number: 1, StartedAt: jsonx.Date{Time: sessionSampleT0},
			Folder: stage + "-" + leg, Leg: &legID,
		})
		name := "bl-" + session
		attempt.Session = &name
		id := session
		attempt.SessionID = &id
		pi := core.BackendPi
		attempt.Settings = &core.SessionSettings{Model: "m", Runner: &pi}
		zero := core.TokenTotalsZero
		attempt.UsageCheckpoint = &core.UsageCheckpoint{SessionID: &id, Totals: &zero}
		return attempt
	}
	run.Attempts = []core.Attempt{open("triage", "r:complex", "leg-a"), open("spec", "r:simple", "leg-b")}
	h := newHarness(t, harnessOptions{
		Issues: []core.Subject{},
		Prepare: func(storage store.Storage) {
			if err := storage.SaveRun(run); err != nil {
				t.Fatalf("save run: %v", err)
			}
		},
	})
	boundary := newBoundaryRunner(h, t, true)
	coordinator := h.RestartWith(boundary)
	boundary.set("leg-a", 10)
	boundary.set("leg-b", 30)
	var savedRun *core.Run
	for _, candidate := range coordinator.Runs() {
		if candidate.Subject.Number == 4 {
			copied := candidate
			savedRun = &copied
		}
	}
	if savedRun == nil {
		t.Fatal("the run with number 4 is not loaded")
	}
	for _, attempt := range savedRun.Attempts {
		stage := findStage(*savedRun, attempt.StageID)
		if stage == nil {
			t.Fatal("an attempt's stage is gone")
		}
		folder := coordinator.Paths().AttemptFolder(*savedRun, attempt)
		writeText(t, filepath.Join(folder, stage.Writes()), "Outcome: ready to spec\n")
	}
	locked(coordinator, func() { coordinator.sessions.check(context.Background()) })
	var saved *core.Run
	for _, stored := range h.storage.LoadRuns() {
		if stored.Subject.Number == 4 {
			copied := stored
			saved = &copied
		}
	}
	if saved == nil || len(saved.Attempts) != 2 {
		t.Fatalf("saved run = %+v, want two attempts", saved)
	}
	if got := saved.Attempts[0].Usage; got == nil || got.CostNanos == nil || *got.CostNanos != 10 {
		t.Errorf("leg a usage = %+v, want cost 10", got)
	}
	if got := saved.Attempts[1].Usage; got == nil || got.CostNanos == nil || *got.CostNanos != 30 {
		t.Errorf("leg b usage = %+v, want cost 30", got)
	}
	if saved.Attempts[0].SessionID == nil || *saved.Attempts[0].SessionID != "leg-a" ||
		saved.Attempts[1].SessionID == nil || *saved.Attempts[1].SessionID != "leg-b" {
		t.Errorf("session ids = %v, %v, want leg-a and leg-b", saved.Attempts[0].SessionID, saved.Attempts[1].SessionID)
	}
}
