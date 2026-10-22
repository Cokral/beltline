// Pins the Sessions sidebar's reason and empty-list copy in present_session_copy.go: the
// fallback names the stage and says whether it runs an agent, and the empty copy names
// the first agent stage.

package core

import "testing"

// sessionCopyRun is a run at its approve gate with an approve stage and an agent stage.
func sessionCopyRun() Run {
	approveName := "Approve"
	buildName := "Build"
	return Run{ID: "run", StageID: "approve", Stages: []Stage{
		NewStage(StageArgs{ID: "approve", Type: StageTypeApprove, Name: &approveName, Outcomes: []Outcome{}}),
		NewStage(StageArgs{ID: "build", Type: StageTypeAgent, Name: &buildName, Agent: &AgentRef{ID: "build", Version: 1}, Outcomes: []Outcome{}}),
	}}
}

// TestSessionFallbackCopyNamesTheStageAndItsKind: a gate says it runs no agent, an
// agent stage without a session says it has none yet, and an unknown stage falls back
// to its id.
func TestSessionFallbackCopyNamesTheStageAndItsKind(t *testing.T) {
	run := sessionCopyRun()
	if got := SessionFallbackCopy(run, "approve"); got != "Approve runs no agent. Showing the latest session." {
		t.Fatalf("gate copy = %q", got)
	}
	if got := SessionFallbackCopy(run, "build"); got != "Build has no session yet. Showing the latest session." {
		t.Fatalf("agent copy = %q", got)
	}
	if got := SessionFallbackCopy(run, "gone"); got != "gone runs no agent. Showing the latest session." {
		t.Fatalf("missing stage copy = %q", got)
	}
}

// TestSessionEmptyCopyNamesTheFirstAgentStage: the empty copy names the first agent
// stage, and falls back to "an agent" when the run has none.
func TestSessionEmptyCopyNamesTheFirstAgentStage(t *testing.T) {
	want := "No sessions yet. The first one starts when Build picks this issue up."
	if got := SessionEmptyCopy(sessionCopyRun()); got != want {
		t.Fatalf("empty copy = %q, want %q", got, want)
	}
	gate := Run{ID: "run", StageID: "approve", Stages: sessionCopyRun().Stages[:1]}
	if got := SessionEmptyCopy(gate); got != "No sessions yet. The first one starts when an agent picks this issue up." {
		t.Fatalf("gate-only empty copy = %q", got)
	}
}
