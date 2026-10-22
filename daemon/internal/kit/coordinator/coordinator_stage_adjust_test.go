// Coordinator tests of stage adjustment: the editor's brief and placeholders for a pinned agent, the
// run and stage entries, adjusting a stage's model, effort, brief and backend in a run's history, a
// running or past attempt kept intact while the next one takes the adjustment, and an answer that
// keeps the held attempt's follow-up prompt.

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// adjustmentRunner is a simulated runner that keeps the specs it starts and
// the sessions it stops.
type adjustmentRunner struct {
	*runner.SimulatedRunner
	mu     sync.Mutex
	starts []runner.SessionSpec
	stops  []string
}

// newH5m4AdjustmentRunner builds the recording runner over a simulated runner that runs nothing long.
func newH5m4AdjustmentRunner(script *harnessScript) *adjustmentRunner {
	return &adjustmentRunner{SimulatedRunner: runner.NewSimulatedRunner(
		func(runner.SessionSpec) float64 { return 10_000 },
		func() time.Time { return testkit.ContractTime }, script.next)}
}

// Start records the spec, then starts the session.
func (r *adjustmentRunner) Start(ctx context.Context, spec runner.SessionSpec) error {
	r.mu.Lock()
	r.starts = append(r.starts, spec)
	r.mu.Unlock()
	return r.SimulatedRunner.Start(ctx, spec)
}

// Stop records the stopped session, then stops it.
func (r *adjustmentRunner) Stop(ctx context.Context, session string) {
	r.mu.Lock()
	r.stops = append(r.stops, session)
	r.mu.Unlock()
	r.SimulatedRunner.Stop(ctx, session)
}

// Starts returns the specs started so far.
func (r *adjustmentRunner) Starts() []runner.SessionSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]runner.SessionSpec(nil), r.starts...)
}

// Stops returns the sessions stopped so far.
func (r *adjustmentRunner) Stops() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.stops...)
}

// adjustFiles is every file under root, by path, for the no-write checks.
func adjustFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	return coordTree(t, root)
}

// addRunAt adds a run of the first belt on issue number, with the harness clock.
func addRunAt(c *Coordinator, belt core.Belt, number int, title string) core.Run {
	return addRun(c, belt, core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: number, Title: title})
}

// TestStageEntriesUseResolvedKeysAndGroupNamesInStageOrder checks resolved keys and group names in stage order,
// on the default belt's stages (the demo belt has no Go fixture here).
func TestStageEntriesUseResolvedKeysAndGroupNamesInStageOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	stages := c.Belts()[0].Belt.Stages
	keyOf := func(stage core.Stage) *string { return c.Key(stage) }
	entries := core.StageAdjustEntries(stages, keyOf, func(core.Stage) string { return "" }, false)
	if len(entries) == 0 || entries[0].Key != "triage" || !reflect.DeepEqual(entries[0].Labels, []string{"Triage"}) {
		t.Fatalf("first entry = %+v, want triage named Triage", entries)
	}
	for _, entry := range entries {
		for _, label := range entry.Labels {
			if label == "Merge" || label == "Approve" {
				t.Fatalf("entry %+v offers a non-agent stage", entry)
			}
		}
	}
	if !reflect.DeepEqual(core.StageAdjustFixed, core.BriefFixedPlaceholders) {
		t.Fatal("the fixed placeholders differ from the brief's")
	}
	// Even a permissive key provider cannot turn watches and approvals into outputs.
	var nonAgent []core.Stage
	for _, stage := range stages {
		if stage.Kind() != core.StageKindAgent {
			nonAgent = append(nonAgent, stage)
		}
	}
	if got := core.StageAdjustEntries(nonAgent, func(stage core.Stage) *string { return &stage.ID }, func(core.Stage) string { return "" }, false); len(got) != 0 {
		t.Fatalf("non-agent entries = %+v, want none", got)
	}
}

// TestSavesOnlyThreeFieldsAndHistoryAndSurvivesRestart
func TestSavesOnlyThreeFieldsAndHistoryAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, thinking := range []core.Thinking{core.ThinkingXHigh, core.ThinkingMax} {
		t.Run(string(thinking), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			c := h.Coord()
			belt := c.Belts()[0].Belt
			original := addRunAt(c, belt, 1, "One")
			other := addRunAt(c, belt, 2, "Two")
			belts := adjustFiles(t, c.Paths().Belts())
			agents := adjustFiles(t, c.Paths().Agents())
			id := original.StageID
			change, err := c.AdjustStage(original.ID, id, new("custom/model"), &thinking, new("Run-only brief"), nil)
			if err != nil || change == nil {
				t.Fatalf("adjust = %+v, %v", change, err)
			}
			if !reflect.DeepEqual(change.Fields, []core.AdjustmentField{core.AdjustmentFieldModel, core.AdjustmentFieldThinking, core.AdjustmentFieldBrief}) {
				t.Fatalf("fields = %v, want model, thinking and brief", change.Fields)
			}
			expected := *c.Run(original.ID)
			expected.ModelAdmission = c.Run(original.ID).ModelAdmission
			expected.Stages[0].Model = "custom/model"
			expected.Stages[0].Thinking = &thinking
			expected.Stages[0].Brief = "Run-only brief"
			expected.Adjustments = []core.Adjustment{*change}
			if got := c.Run(original.ID); !coordSameJSON(t, *got, expected) {
				t.Fatal("the saved run is not the adjusted run")
			}
			if got := c.Run(other.ID); got == nil || !reflect.DeepEqual(*got, other) {
				t.Fatal("the other run changed")
			}
			if got := c.Belt(belt.ID); got == nil || !coordSameJSON(t, got.Belt, belt) {
				t.Fatal("the belt changed")
			}
			if !reflect.DeepEqual(adjustFiles(t, c.Paths().Belts()), belts) || !reflect.DeepEqual(adjustFiles(t, c.Paths().Agents()), agents) {
				t.Fatal("the library files changed")
			}
			restarted := h.RestartWith(idleRunner())
			if got := restarted.Run(original.ID); got == nil || !coordSameJSON(t, *got, expected) {
				t.Fatal("the adjusted run did not survive a restart")
			}
			for _, saved := range h.storage.LoadRuns() {
				if saved.ID == original.ID && !coordSameJSON(t, saved, expected) {
					t.Fatal("the saved file does not match")
				}
			}
			if n := len(adjustmentsFor(expected.Adjustments, "spec")); n != 0 {
				t.Fatal("an adjustment landed on another stage")
			}
			again, err := c.AdjustStage(original.ID, id, new("custom/model"), &thinking, new("Run-only brief"), nil)
			if err != nil || again != nil {
				t.Fatalf("an unchanged adjust = %+v, %v; want nothing", again, err)
			}
			agent := c.Agent(expected.Stages[0].Agent)
			if agent == nil {
				t.Fatal("no pinned agent")
			}
			reset, err := c.AdjustStage(original.ID, id, &agent.Model, nil, &agent.Brief, nil)
			if err != nil || reset == nil || !reflect.DeepEqual(reset.Fields, []core.AdjustmentField{core.AdjustmentFieldModel, core.AdjustmentFieldThinking, core.AdjustmentFieldBrief}) {
				t.Fatalf("reset = %+v, %v", reset, err)
			}
			saved := c.Run(original.ID)
			if saved.Stages[0].Model != agent.Model || saved.Stages[0].Thinking != nil || saved.Stages[0].Brief != "" || len(saved.Adjustments) != 2 {
				t.Fatalf("after the reset = %+v", saved.Stages[0])
			}
			onlyModel, err := c.AdjustStage(original.ID, id, new("another"), nil, &agent.Brief, nil)
			if err != nil || onlyModel == nil || !reflect.DeepEqual(onlyModel.Fields, []core.AdjustmentField{core.AdjustmentFieldModel}) {
				t.Fatalf("only model = %+v, %v", onlyModel, err)
			}
			if n := len(h.github.Calls()); n != 0 {
				t.Fatal("adjusting called GitHub")
			}
			if n := len(h.jira.Calls()); n != 0 {
				t.Fatal("adjusting called Jira")
			}
			c.Lock()
			c.book.update(original.ID, nil, nil, func(r *core.Run) { r.State = core.RunStateShipped })
			c.EndTurn()
			if _, err := c.AdjustStage(original.ID, id, new(""), nil, new(""), nil); err == nil {
				t.Fatal("a shipped run accepted an adjustment")
			}
			if _, err := c.AdjustStage(other.ID, "approve", new(""), nil, new(""), nil); err == nil {
				t.Fatal("a gate accepted an adjustment")
			}
			if _, err := c.AdjustStage(other.ID, "missing", new(""), nil, new(""), nil); err == nil {
				t.Fatal("an unknown stage accepted an adjustment")
			}
			if _, err := c.AdjustStage("missing", id, new(""), nil, new(""), nil); err == nil {
				t.Fatal("an unknown run accepted an adjustment")
			}
		})
	}
}

// coordSameJSON reports whether a and b have the same wire form.
func coordSameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, errA := jsonx.Marshal(a)
	y, errB := jsonx.Marshal(b)
	if errA != nil || errB != nil {
		t.Fatalf("encode: %v, %v", errA, errB)
	}
	return string(x) == string(y)
}

// adjustmentsFor is the adjustments that name stage.
func adjustmentsFor(adjustments []core.Adjustment, stage string) []core.Adjustment {
	var out []core.Adjustment
	for _, adjustment := range adjustments {
		if adjustment.StageID == stage {
			out = append(out, adjustment)
		}
	}
	return out
}

// TestRunningAndPastAttemptsStayIntactAndNextAttemptUsesAdjustment
func TestRunningAndPastAttemptsStayIntactAndNextAttemptUsesAdjustment(t *testing.T) {
	t.Parallel()
	for _, thinking := range []core.Thinking{core.ThinkingXHigh, core.ThinkingMax} {
		t.Run(string(thinking), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			adjustRunner := newH5m4AdjustmentRunner(h.Script)
			c := h.RestartWith(adjustRunner)
			libraryBelt := c.Belts()[0].Belt
			pinned := c.Agent(libraryBelt.Stages[0].Agent)
			belt := libraryBelt.Clone()
			belt.Stages[0].Model = "old"
			belt.Stages[0].Brief = "Old prompt"
			c.Lock()
			run := c.book.add(core.NewRun(core.RunArgs{Belt: belt, Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 1, Title: "Test"}, Now: testkit.ContractTime}))
			c.sessions.start(bgCtx, run.ID, nil)
			c.EndTurn()
			before := *c.Run(run.ID)
			first := *before.CurrentAttempt()
			starts := adjustRunner.Starts()
			if len(starts) == 0 {
				t.Fatal("no session started")
			}
			spec := starts[0]
			briefFile := filepath.Join(spec.AttemptFolder, "brief.md")
			writeText(t, spec.Output, "Historical output")
			attemptsBefore, _ := jsonx.Marshal(before.Attempts)
			codex := core.BackendCodex
			if _, err := c.AdjustStage(run.ID, first.StageID, new("new"), &thinking, new("New prompt"), &codex); err != nil {
				t.Fatalf("adjust: %v", err)
			}
			adjusted := *c.Run(run.ID)
			if attemptsAfter, _ := jsonx.Marshal(adjusted.Attempts); string(attemptsAfter) != string(attemptsBefore) {
				t.Fatal("the attempts changed")
			}
			if adjusted.State != before.State || adjusted.StageID != before.StageID || !reflect.DeepEqual(adjusted.Routes(), before.Routes()) {
				t.Fatal("the run's state, stage or routes changed")
			}
			if got := c.Output(adjusted, first); got == nil || *got != "Historical output" {
				t.Fatalf("output = %v, want the historical output", got)
			}
			if data, _ := os.ReadFile(briefFile); string(data) != spec.Prompt {
				t.Fatal("the running brief changed")
			}
			if !reflect.DeepEqual(adjustRunner.Starts(), starts) {
				t.Fatal("the running session was restarted")
			}
			if len(adjustRunner.Stops()) != 0 {
				t.Fatal("the running session was stopped")
			}
			if !adjustRunner.IsRunning(spec.Name) {
				t.Fatal("the running session is not running")
			}
			firstSpec := c.Spec(adjusted, first, "")
			if firstSpec == nil || firstSpec.Backend != spec.Backend || firstSpec.Model != "old" || firstSpec.Thinking != nil {
				t.Fatalf("spec of the running attempt = %+v, want the original backend, model old and no effort", firstSpec)
			}
			if adjusted.Stages[0].Runner == nil || *adjusted.Stages[0].Runner != core.BackendCodex {
				t.Fatal("the stage's backend did not take the adjustment")
			}
			if n := len(h.github.Calls()); n != 0 {
				t.Fatal("adjusting called GitHub")
			}
			if n := len(h.jira.Calls()); n != 0 {
				t.Fatal("adjusting called Jira")
			}
			// Move past it, then adjust that historical stage without losing its output.
			c.Lock()
			c.book.update(run.ID, nil, nil, func(r *core.Run) {
				r.ChangeCurrentAttempt(func(a *core.Attempt) {
					a.EndedAt = &jsonx.Date{Time: testkit.ContractTime}
					outcome := "ready to spec"
					a.Outcome = &outcome
					detail := "Past note"
					a.Detail = &detail
				})
				r.StageID = "spec"
				r.State = core.RunStateQueued
			})
			c.EndTurn()
			past := *c.Run(run.ID)
			if _, err := c.AdjustStage(run.ID, first.StageID, new("newest"), &thinking, new("Newest prompt"), &codex); err != nil {
				t.Fatalf("adjust past: %v", err)
			}
			if got := c.Run(run.ID); got == nil || !reflect.DeepEqual(got.Attempts, past.Attempts) || got.StageID != "spec" {
				t.Fatal("the past attempts or the stage changed")
			}
			if got := c.Output(*c.Run(run.ID), first); got == nil || *got != "Historical output" {
				t.Fatal("the historical output was lost")
			}
			if !coordSameJSON(t, c.Belt(libraryBelt.ID).Belt, libraryBelt) {
				t.Fatal("the library belt changed")
			}
			if !reflect.DeepEqual(c.Agent(adjusted.Stages[0].Agent), pinned) {
				t.Fatal("the pinned agent changed")
			}
			c.Lock()
			c.book.update(run.ID, nil, nil, func(r *core.Run) {
				r.StageID = first.StageID
				r.State = core.RunStateQueued
				replays := first.ID
				r.Replays = &replays
			})
			c.EndTurn()
			restarted := h.RestartWith(adjustRunner)
			c2 := restarted
			c2.Lock()
			c2.sessions.start(bgCtx, run.ID, nil)
			c2.EndTurn()
			all := adjustRunner.Starts()
			next := all[len(all)-1]
			if next.Backend != core.BackendCodex || next.Model != "newest" || next.Thinking == nil || *next.Thinking != thinking {
				t.Fatalf("next = backend %s model %s thinking %v, want codex, newest, %s", next.Backend, next.Model, next.Thinking, thinking)
			}
			args := runner.NewCodexAdapter(nil).Arguments(next.SessionID, next.Name, next.Model, next.Thinking)
			if !slices.Contains(args, "model_reasoning_effort="+string(thinking)) {
				t.Fatalf("codex arguments = %v, want the effort %s", args, thinking)
			}
			if !strings.Contains(next.Prompt, "Newest prompt") || !strings.Contains(next.Prompt, "What this stage already had") || strings.Contains(next.Prompt, "Old prompt") {
				t.Fatal("the next prompt does not use the adjusted brief")
			}
			if next.Resume {
				t.Fatal("the next attempt resumes")
			}
		})
	}
}

// TestAnsweringKeepsFollowUpPromptRatherThanNewBrief
func TestAnsweringKeepsFollowUpPromptRatherThanNewBrief(t *testing.T) {
	t.Parallel()
	for _, thinking := range []core.Thinking{core.ThinkingXHigh, core.ThinkingMax} {
		t.Run(string(thinking), func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, harnessOptions{})
			adjustRunner := newH5m4AdjustmentRunner(h.Script)
			c := h.RestartWith(adjustRunner)
			answerBelt := c.Belts()[0].Belt
			c.Lock()
			run := c.book.add(core.NewRun(core.RunArgs{Belt: answerBelt, Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 1, Title: "Test"}, Now: testkit.ContractTime}))
			c.sessions.start(bgCtx, run.ID, nil)
			c.book.update(run.ID, nil, nil, func(r *core.Run) {
				r.ChangeCurrentAttempt(func(a *core.Attempt) {
					a.EndedAt = &jsonx.Date{Time: testkit.ContractTime}
					outcome := "needs info"
					a.Outcome = &outcome
				})
				r.State = core.RunStateHeld
			})
			c.EndTurn()
			held := *c.Run(run.ID)
			attempt := held.Answerable()
			if attempt == nil {
				t.Fatal("nothing to answer")
			}
			nextFolder := filepath.Join(c.Paths().RunFolder(held), core.EngineAttemptFolder(held, *held.Stage()))
			answered := held
			note := "The invoices page"
			answered.Note = &note
			context := core.BriefContext{RunFolder: c.Paths().RunFolder(held), AttemptFolder: nextFolder, Base: "main", Branch: core.RunBranch(held), Agents: c.AgentsOf(held)}
			expected := core.BriefFollowUp(answered, *held.Stage(), *attempt, context)
			codex := core.BackendCodex
			high := thinking
			if _, err := c.AdjustStage(run.ID, held.StageID, new("adjusted"), &high, new("DO NOT REPLACE THE HELD PROMPT"), &codex); err != nil {
				t.Fatalf("adjust: %v", err)
			}
			if n := len(adjustRunner.Starts()); n != 1 {
				t.Fatalf("starts = %d, want 1", n)
			}
			if n := len(adjustRunner.Stops()); n != 0 {
				t.Fatalf("stops = %d, want 0", n)
			}
			restarted := h.RestartWith(adjustRunner)
			heldAgain := restarted.Run(run.ID)
			if heldAgain == nil {
				t.Fatal("the held run is gone")
			}
			var stageRunner *core.Backend
			for _, stage := range heldAgain.Stages {
				if stage.ID == held.StageID {
					stageRunner = stage.Runner
				}
			}
			if stageRunner == nil || *stageRunner != core.BackendCodex {
				t.Fatal("the held stage's backend did not take the adjustment")
			}
			if spec := restarted.Spec(*heldAgain, *attempt, ""); spec == nil || attempt.Settings == nil || spec.Backend != *attempt.Settings.Runner || spec.SessionID != core.ValueOf(attempt.SessionID) {
				t.Fatal("the held attempt's spec does not match the attempt")
			}
			restarted.Answer(run.ID, new("The invoices page"))
			// Answer spawns a tick that may start the resumed session; let it settle before looking.
			restarted.Idle()
			restarted.Lock()
			restarted.sessions.start(bgCtx, run.ID, nil)
			restarted.EndTurn()
			restarted.Idle()
			var resumed *runner.SessionSpec
			for _, spec := range adjustRunner.Starts() {
				if spec.Resume {
					s := spec
					resumed = &s
					break
				}
			}
			if resumed == nil {
				t.Fatal("no resumed session started")
			}
			if resumed.SessionID != core.ValueOf(attempt.SessionID) || resumed.Backend != core.BackendPi || !strings.HasPrefix(resumed.Prompt, expected) {
				t.Fatalf("resumed = session %s backend %s, want the held session on pi with the follow-up prompt", resumed.SessionID, resumed.Backend)
			}
			if attempt.Settings != nil && (resumed.Model != attempt.Settings.Model) {
				t.Fatalf("model = %s, want %s", resumed.Model, attempt.Settings.Model)
			}
		})
	}
}
