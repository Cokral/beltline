// Tests of the model library policy: an agent with a model that is not allowed is never saved, and a
// belt with one is saved and paused with a warning. Restoring the permission clears the warnings
// without rewriting the saved versions. A permission change while a run is prepared is seen on the
// next poll, and a belt that cannot start runs says so once across polls.

package coordinator

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// preparingSource is one subject on the scope, whose preparation runs prepare first.
type preparingSource struct {
	sourceDefaults
	prepareAction func() error
}

// scopes lists the belt's repositories.
func (s preparingSource) scopes(belt core.Belt) []string { return belt.Repos() }

// place names a scope by itself.
func (s preparingSource) place(scope string, _ core.Belt) string { return scope }

// list returns one labelled subject on the scope.
func (s preparingSource) list(_ context.Context, scope string, _ core.Belt) ([]core.Subject, error) {
	return []core.Subject{{Kind: core.SubjectKindIssue, Repo: scope, Number: 7, Title: "Preparing", Labels: []string{"agent"}, IsOpen: true}}, nil
}

// watches reports no watch.
func (s preparingSource) watches(core.Run, string) bool { return false }

// prepare runs the test's preparation, then returns the subject.
func (s preparingSource) prepare(_ context.Context, subject core.Subject, _ core.Belt) (core.Subject, error) {
	if err := s.prepareAction(); err != nil {
		return core.Subject{}, err
	}
	return subject, nil
}

// missing reports the run's subject as changed.
func (s preparingSource) missing(_ context.Context, run core.Run, _ core.Belt) (Fate, error) {
	return Fate{Kind: FateChanged, Subject: run.Subject}, nil
}

// TestInvalidAgentAndLaterBeltStageNeverCreateVersions: an agent with a model that is not allowed is refused and
// keeps its version; a belt stage with such a model is saved as the next version, paused, with the warning; a stage
// with an empty model is saved too, with the runner's default warning.
func TestInvalidAgentAndLaterBeltStageNeverCreateVersions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	agent := c.Agents()[0].Agent
	before := agent.Version
	agent.Model = "outside"
	if _, err := c.SaveAgent(agent); err == nil {
		t.Fatalf("an agent with a model that is not allowed was saved")
	}
	if latest := c.LatestAgent(agent.ID); latest == nil || latest.Agent.Version != before {
		t.Fatalf("the agent's version changed after a refused save")
	}
	belt := c.Belts()[0].Belt
	version := belt.Version
	last := len(belt.Stages) - 1
	belt.Stages[last].Model = "outside"
	outside, err := c.SaveBelt(belt)
	if err != nil {
		t.Fatalf("save belt with a model outside the allowance: %v", err)
	}
	if outside.Version != version+1 || outside.Stages[last].Model != "outside" {
		t.Fatalf("saved belt = v%d model %q, want v%d outside", outside.Version, outside.Stages[last].Model, version+1)
	}
	if record := c.Belt(belt.ID); record == nil || !record.Paused {
		t.Fatalf("the belt is not paused after the save")
	}
	if !coordAnyContains(c.Warnings(outside), "outside", "is not allowed") {
		t.Fatalf("warnings = %v, want the model is not allowed", c.Warnings(outside))
	}
	belt = outside
	belt.Stages[last].Model = ""
	empty, err := c.SaveBelt(belt)
	if err != nil {
		t.Fatalf("save belt with an empty model: %v", err)
	}
	if empty.Version != outside.Version+1 || empty.Stages[last].Model != "" {
		t.Fatalf("saved belt = v%d model %q", empty.Version, empty.Stages[last].Model)
	}
	if record := c.Belt(belt.ID); record == nil || !record.Paused {
		t.Fatalf("the belt is not paused after the empty-model save")
	}
	if !coordAnyContains(c.Warnings(empty), "The runner's default") {
		t.Fatalf("warnings = %v, want the runner's default", c.Warnings(empty))
	}
}

// coordAnyContains reports whether one of the list holds every one of the parts.
func coordAnyContains(list []string, parts ...string) bool {
	for _, item := range list {
		all := !slices.ContainsFunc(parts, func(part string) bool { return !strings.Contains(item, part) })
		if all {
			return true
		}
	}
	return false
}

// TestRestorePermissionClearsErrorsWithoutRewritingVersions: refusing a model blocks the agent's save and warns on
// the belt; restoring the permission clears the warnings and leaves the saved versions and the pause as they are.
func TestRestorePermissionClearsErrorsWithoutRewritingVersions(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	belt := c.Belts()[0].Belt
	agent := c.Agents()[0].Agent
	if err := c.SetModelAllowed(agent.Model, agent.Runner, false); err != nil {
		t.Fatalf("refuse model: %v", err)
	}
	if len(c.ModelWarnings(agent)) == 0 {
		t.Fatalf("the agent shows no model warning")
	}
	if len(c.Warnings(belt)) == 0 {
		t.Fatalf("the belt shows no warning")
	}
	if _, err := c.SaveAgent(agent); err == nil {
		t.Fatalf("the agent was saved with a refused model")
	}
	saved, err := c.SaveBelt(belt)
	if err != nil {
		t.Fatalf("save belt: %v", err)
	}
	if saved.Version != belt.Version+1 {
		t.Fatalf("saved version = %d, want %d", saved.Version, belt.Version+1)
	}
	if record := c.Belt(belt.ID); record == nil || !record.Paused {
		t.Fatalf("the belt is not paused")
	}
	if len(c.Warnings(saved)) == 0 {
		t.Fatalf("the saved belt shows no warning")
	}
	if err := c.SetModelAllowed(agent.Model, agent.Runner, true); err != nil {
		t.Fatalf("allow model: %v", err)
	}
	if len(c.ModelWarnings(agent)) != 0 || len(c.Warnings(saved)) != 0 {
		t.Fatalf("restoring the permission left warnings")
	}
	if latest := c.LatestAgent(agent.ID); latest == nil || !reflect.DeepEqual(latest.Agent, agent) {
		t.Fatalf("the agent's saved version changed")
	}
	if record := c.Belt(belt.ID); record == nil || !reflect.DeepEqual(record.Belt, saved) || !record.Paused {
		t.Fatalf("the saved belt or its pause changed")
	}
	if err := c.SetPaused(belt.ID, false); err != nil {
		t.Fatalf("unpause: %v", err)
	}
	if record := c.Belt(belt.ID); record == nil || record.Paused {
		t.Fatalf("the belt is still paused after unpausing")
	}
}

// TestPermissionChangeDuringPreparationIsObservedAndNextPollRetries: a permission removed while a subject is being
// prepared stops the run with a notice, and the next poll after the permission returns starts it with the admission
// it was given, which is saved too.
func TestPermissionChangeDuringPreparationIsObservedAndNextPollRetries(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	record := c.Belts()[0]
	model := record.Belt.Stages[0].Model
	ctx := context.Background()
	c.Lock()
	c.intake.pollFirstEntry(ctx, record, preparingSource{prepareAction: func() error {
		return c.SetModelAllowed(model, core.BackendPi, false)
	}}, "acme/billing")
	c.EndTurn()
	if len(c.Runs()) != 0 {
		t.Fatalf("a run started with a refused model")
	}
	if !noticesContain(c, "Cannot start", "Triage") {
		t.Fatalf("notices = %v, want the cannot-start notice", noticeTexts(c))
	}
	if err := c.SetModelAllowed(model, core.BackendPi, true); err != nil {
		t.Fatalf("allow again: %v", err)
	}
	c.Lock()
	c.intake.pollFirstEntry(ctx, record, preparingSource{prepareAction: func() error { return nil }}, "acme/billing")
	c.EndTurn()
	runs := c.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if admission := runs[0].ModelAdmission; admission == nil || !admission.Allows(model, core.BackendPi) {
		t.Fatalf("the run's admission does not allow %s", model)
	}
}

// noticesContain reports whether a notice holds every part.
func noticesContain(c *Coordinator, parts ...string) bool {
	return coordAnyContains(noticeTexts(c), parts...)
}

// TestABeltThatCannotStartRunsSaysSoOnceAcrossPolls: a belt whose model is refused notes the refusal once across
// repeated polls, and after the permission returns and the belt starts again, it has no refusal left.
func TestABeltThatCannotStartRunsSaysSoOnceAcrossPolls(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	c := h.Coord()
	record := c.Belts()[0]
	model := record.Belt.Stages[0].Model
	ctx := context.Background()
	if err := c.SetModelAllowed(model, core.BackendPi, false); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	for range 3 {
		c.Lock()
		c.intake.pollFirstEntry(ctx, record, preparingSource{prepareAction: func() error { return nil }}, "acme/billing")
		c.EndTurn()
	}
	count := 0
	for _, text := range noticeTexts(c) {
		if strings.Contains(text, "Cannot start") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("cannot-start notices = %d, want 1", count)
	}
	if err := c.SetModelAllowed(model, core.BackendPi, true); err != nil {
		t.Fatalf("allow: %v", err)
	}
	c.Lock()
	c.intake.pollFirstEntry(ctx, record, preparingSource{prepareAction: func() error { return nil }}, "acme/billing")
	refused := len(c.intake.refused)
	c.EndTurn()
	if refused != 0 {
		t.Fatalf("refusals after the belt started again = %d, want 0", refused)
	}
}

// pollFirstEntry checks the belt's first entry.
// Lock: runs with c.mu held. Suspends: as poll.
func (i *Intake) pollFirstEntry(ctx context.Context, record store.BeltRecord, source Source, scope string) {
	entry := core.TriggerEntry{ID: core.TriggerEntryLegacyID, Trigger: record.Belt.Trigger(), Repos: record.Belt.Repos()}
	if len(record.Belt.Triggers) > 0 {
		entry = record.Belt.Triggers[0]
	}
	i.poll(ctx, record, entry, source, scope)
}

// noticeTexts returns the texts of the notices, in order.
func noticeTexts(c *Coordinator) []string {
	var texts []string
	for _, notice := range c.Notices() {
		texts = append(texts, notice.Text)
	}
	return texts
}
