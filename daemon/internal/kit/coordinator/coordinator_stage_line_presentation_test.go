// Golden check for the belt editor's stage line: the line of belt-context.json, built from its draft.
// The agents list is left out of the comparison, because the harness library holds only the built-in
// agents.

package coordinator

import (
	"os"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/testkit"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// contractDraft is the draft of the contract tests: approval, two agent stages on the routes of
// the fork "split" (fast, slow), and a verifier, on an issues trigger with label agent.
func contractDraft() core.Belt {
	stage := func(id string, typ core.StageType, route *string, agent *core.AgentRef, key *string, outcomes []core.Outcome) core.Stage {
		var runner *core.Backend
		if agent != nil {
			pi := core.BackendPi
			runner = &pi
		}
		writes := id + ".md"
		name := id
		readOnly := core.WorkspaceReadOnly
		return core.NewStage(core.StageArgs{ID: id, Type: typ, Name: &name, Agent: agent, Key: key, Runner: runner,
			Model: "openrouter/stealth/space-bunny-alpha", Workspace: &readOnly, Writes: &writes, Outcomes: outcomes, Route: route})
	}
	outcome := func(id, name, when string, tone core.Tone, action core.ThenAction) core.Outcome {
		return core.Outcome{ID: id, Name: name, When: when, Tone: tone, Then: core.NewThen(core.ThenArgs{Action: action})}
	}
	fast, slow := "fast", "slow"
	approve := stage("approve", core.StageTypeApprove, nil, nil, nil, []core.Outcome{
		outcome("approve-ok", "approved", "Approve", core.ToneGood, core.ThenActionNext),
		outcome("approve-park", "parked", "Park", core.ToneNeutral, core.ThenActionPark),
	})
	approve.Name = "Approval"
	build := stage("build", core.StageTypeAgent, &fast, &core.AgentRef{ID: "build", Version: 1}, new("build"), []core.Outcome{
		outcome("build-done", "done", "The work is done", core.ToneGood, core.ThenActionNext),
		outcome("build-hold", "needs me", "Anything else", core.ToneYou, core.ThenActionHold),
	})
	build.Name = "Build"
	check := stage("check", core.StageTypeAgent, &slow, &core.AgentRef{ID: "verify", Version: 1}, new("check"), []core.Outcome{
		outcome("check-done", "done", "The work is done", core.ToneGood, core.ThenActionNext),
		outcome("check-hold", "needs me", "Anything else", core.ToneYou, core.ThenActionHold),
	})
	check.Name = "Check"
	verify := stage("verify", core.StageTypeAgent, nil, &core.AgentRef{ID: "verify", Version: 1}, new("verify"), []core.Outcome{
		outcome("verify-done", "done", "The work is done", core.ToneGood, core.ThenActionNext),
		outcome("verify-hold", "needs me", "Anything else", core.ToneYou, core.ThenActionHold),
	})
	verify.Name = "Verify"
	issues := core.DefaultIssueFilter()
	issues.Label = "agent"
	return core.Belt{
		ID: "contract-draft", Name: "Contract draft",
		Triggers: []core.TriggerEntry{{ID: "1", Trigger: core.Trigger{Kind: core.TriggerKindIssues, Issues: new(issues)}, Repos: []string{"acme/app"}}},
		Stages:   []core.Stage{approve, build, check, verify},
		Forks:    []core.Fork{{ID: "split", Routes: []core.Route{{ID: "fast", Name: "Fast"}, {ID: "slow", Name: "Slow"}}}},
	}
}

// TestGoldenBeltContextLineMatchesTheContractDraft checks the stage line of belt-context.json, without its
// agents, against the coordinator's stage line of the contract draft.
func TestGoldenBeltContextLineMatchesTheContractDraft(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{})
	var line core.StageLinePresentation
	h.Turn(func() { line = h.Coord().stageLine(contractDraft()) })

	raw, err := os.ReadFile(testkit.Testdata(t, "golden", "belt-context.json"))
	if err != nil {
		t.Fatalf("golden: %v", err)
	}
	goldenLine := stageLineMember(t, raw, "line")
	encoded, err := jsonx.Marshal(line)
	if err != nil {
		t.Fatalf("marshal line: %v", err)
	}
	for _, key := range []string{"trigger", "items", "addEnd", "watchAllowed"} {
		stageLineSame(t, "line "+key, stageLineMember(t, encoded, key), stageLineMember(t, goldenLine, key))
	}
}

// stageLineMember is the raw JSON of key in the object raw; it fails the test when the key is missing.
func stageLineMember(t *testing.T, raw []byte, key string) []byte {
	t.Helper()
	members, err := jsonx.Members(raw)
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	value, ok := members[key]
	if !ok {
		t.Fatalf("no key %q", key)
	}
	return []byte(value)
}

// stageLineSame fails the test when the two JSON values differ after canonicalisation.
func stageLineSame(t *testing.T, what string, got, want []byte) {
	t.Helper()
	gotCanonical, err := jsonx.Canonical(got)
	if err != nil {
		t.Fatalf("%s: canonical got: %v", what, err)
	}
	wantCanonical, err := jsonx.Canonical(want)
	if err != nil {
		t.Fatalf("%s: canonical want: %v", what, err)
	}
	if string(gotCanonical) != string(wantCanonical) {
		t.Errorf("%s differs\n got: %s\nwant: %s", what, gotCanonical, wantCanonical)
	}
}
