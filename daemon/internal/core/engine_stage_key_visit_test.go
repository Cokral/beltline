package core

import (
	"strings"
	"testing"
)

// TestVerifySendBackKeepsTheApprovedOutput: verify sits after a fork and sends the
// run back into it. The output spec wrote before must still resolve for build:
// Hiding it as an earlier visit would make build find "(no spec.md yet)".
func TestVerifySendBackKeepsTheApprovedOutput(t *testing.T) {
	t.Parallel()
	belt := stageKeyTestForkedBelt()
	belt.Stages[0].Outcomes[0].Then = NewThen(ThenArgs{Action: ThenActionRoute, Routes: []string{"complex"}})
	belt.Stages[1].SetKey(new("spec"))
	belt.Stages[1].SetWrites("spec.md")
	belt.Stages[2].Brief = "Approved {spec}"
	belt.Stages[4].Outcomes = []Outcome{{ID: "back", Name: "mismatch", When: "code mismatch", Tone: ToneNeutral,
		Then: NewThen(ThenArgs{Action: ThenActionGoto, To: []string{"build"}})}}
	run := briefTestSampleRun(belt)
	const folder = "/runs/r"
	context := BriefContext{RunFolder: folder, AttemptFolder: folder + "/attempt", Base: "main", Branch: "b",
		Agents: placeholderTestAgents()}

	briefTestFinish(&run, "ready to implement")
	if run.StageID != "spec" {
		t.Fatalf("stage = %q, want spec", run.StageID)
	}
	briefTestFinish(&run, "complete")
	if run.StageID != "build" {
		t.Fatalf("stage = %q, want build", run.StageID)
	}
	briefTestFinish(&run, "done")
	if run.StageID != "verify" {
		t.Fatalf("stage = %q, want verify", run.StageID)
	}
	briefTestFinish(&run, "mismatch")
	if run.StageID != "build" {
		t.Fatalf("stage = %q, want build after the send-back", run.StageID)
	}
	if got := run.Routes()["size"]; len(got) != 1 || got[0] != "complex" {
		t.Fatalf("route = %v, want [complex]", got)
	}
	build := run.Stage()
	if got, want := briefPlaceholders(run, *build, context)["spec"], folder+"/f/spec.md"; got != want {
		t.Fatalf("spec on the send-back = %q, want %q", got, want)
	}
	// A run saved while visits were recorded still loads and resolves the same.
	saved := strings.Replace(btEncode(t, run), `"attempts":[`, `"forkVisits":{"size":2},"attempts":[`, 1)
	saved = strings.Replace(saved, `"stageId":"spec",`, `"stageId":"spec","visits":{"size":1},`, 1)
	if !strings.Contains(saved, `"visits":{"size":1}`) {
		t.Fatal("the saved run has no attempt visits to ignore")
	}
	loaded := btDecode[Run](t, saved)
	if got, want := briefPlaceholders(loaded, *loaded.Stage(), context)["spec"], folder+"/f/spec.md"; got != want {
		t.Fatalf("spec on a run saved with visits = %q, want %q", got, want)
	}
}
