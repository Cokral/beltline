package core

import "testing"

// TestStageSendWordsCountTheAgentStagesClosedAttempts checks the Send sheet of an agent stage: the earlier
// attempts are the closed ones, singular at one, and a parked run offers Unpark instead of running again.
func TestStageSendWordsCountTheAgentStagesClosedAttempts(t *testing.T) {
	t.Parallel()
	run := nfDefaultRun()
	stage := run.Stages[0]

	words := StageSendWordsOf(stage, run)
	if words.Earlier == nil || *words.Earlier != "Include "+stage.Name+"'s 0 earlier attempts" {
		t.Fatalf("earlier = %v, want zero attempts", words.Earlier)
	}
	if words.Note == nil || *words.Note != stage.Name+" starts a new session with its brief and current model and thinking level." {
		t.Errorf("note = %v", words.Note)
	}
	if words.Again != "Run this stage again." {
		t.Errorf("again = %q", words.Again)
	}

	nfFinish(&run, "ready to implement")
	words = StageSendWordsOf(stage, run)
	if words.Earlier == nil || *words.Earlier != "Include "+stage.Name+"'s 1 earlier attempt" {
		t.Errorf("earlier after one attempt = %v", words.Earlier)
	}

	run.State = RunStateParked
	if again := StageSendWordsOf(stage, run).Again; again != "Unpark at this stage." {
		t.Errorf("parked again = %q", again)
	}
}

// TestStageSendWordsOmitTheAgentLinesForAGate checks that a gate's Send sheet has only the again line.
func TestStageSendWordsOmitTheAgentLinesForAGate(t *testing.T) {
	t.Parallel()
	run := nfDefaultRun()
	var gate *Stage
	for i := range run.Stages {
		if run.Stages[i].Kind() == StageKindGate {
			gate = &run.Stages[i]
			break
		}
	}
	if gate == nil {
		t.Fatal("default belt has no gate stage")
	}
	words := StageSendWordsOf(*gate, run)
	if words.Earlier != nil || words.Note != nil || words.Again != "Run this stage again." {
		t.Errorf("gate send = %+v", words)
	}
}
