package core

import "testing"

// TestUpgradeRunGivesOldStagesKeysThatKeepTheirFiles checks a run from before
// stage keys takes each agent's key when it names the file the stage wrote,
// the file's own name otherwise, and no key when that name is a fixed one;
// and that the agents it had no copy of are copied in.
func TestUpgradeRunGivesOldStagesKeysThatKeepTheirFiles(t *testing.T) {
	t.Parallel()
	spec := btStageAgent("spec", nil)
	spec.SetWrites("spec.md")
	draft := btStageAgent("draft", nil)
	draft.SetWrites("notes.md")
	custom := btStageAgent("custom", nil)
	custom.SetWrites("output.md")
	agents := map[AgentRef]Agent{}
	for _, id := range []string{"spec", "draft", "custom"} {
		agents[AgentRef{ID: id, Version: 1}] = Agent{ID: id, Version: 1, Name: id, Key: id, Outcomes: []AgentOutcome{}}
	}
	run := Run{Stages: []Stage{spec, draft, custom, btApproval()}, Agents: []AgentSnapshot{}}
	upgraded := UpgradeRun(run, agents, nil)
	if !upgraded.UsesStageKeys() || run.UsesStageKeys() {
		t.Fatalf("references %v, the original's %v", upgraded.References, run.References)
	}
	want := []struct {
		key, writes string
	}{{"spec", "spec.md"}, {"notes", "notes.md"}, {"", "output.md"}}
	for index, w := range want {
		stage := upgraded.Stages[index]
		if StageKeysChosen(stage) != w.key || stage.Writes() != w.writes {
			t.Errorf("%s: key %q writes %q, want %q and %q", stage.ID, StageKeysChosen(stage), stage.Writes(), w.key, w.writes)
		}
	}
	if len(upgraded.Agents) != 3 || upgraded.Agents[0].Ref() != (AgentRef{ID: "spec", Version: 1}) {
		t.Fatalf("agents %+v", upgraded.Agents)
	}
}

// TestUpgradeRunLeavesAKeyedRunsStagesAndTakesItsSource checks a run started
// with stage keys keeps them, and a run without a source takes the entry of
// the belt it started on that finds its subject.
func TestUpgradeRunLeavesAKeyedRunsStagesAndTakesItsSource(t *testing.T) {
	t.Parallel()
	stage := btStageAgent("build", nil)
	run := Run{Stages: []Stage{stage}, Agents: []AgentSnapshot{}, References: new(StageKeysVersion),
		Subject: Subject{Kind: SubjectKindPullRequest}}
	belt := btBelt(stage)
	belt.Triggers = []TriggerEntry{btEntry("1", btIssues(), "a/b"), btEntry("2", TriggerKindReviews.Fresh(), "c/d")}
	upgraded := UpgradeRun(run, map[AgentRef]Agent{}, &belt)
	if upgraded.Stages[0].Key != nil {
		t.Fatalf("key %v", *upgraded.Stages[0].Key)
	}
	if upgraded.Source == nil || upgraded.Source.Entry != "2" || upgraded.Source.Repos[0] != "c/d" {
		t.Fatalf("source %+v", upgraded.Source)
	}
	if again := UpgradeRun(upgraded, map[AgentRef]Agent{}, nil); again.Source.Entry != "2" {
		t.Fatalf("a run with a source took another: %+v", again.Source)
	}
}
