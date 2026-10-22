package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// TestLegacyActionOnlyKeepsOutcomesThatRouteTheSame checks that an action's
// outcomes tied to a success are rewritten, "failed" is rewritten, and the rest
// (a merge, a close) are dropped, as the legacy loader does.
func TestLegacyActionOnlyKeepsOutcomesThatRouteTheSame(t *testing.T) {
	t.Parallel()
	fields := legacyActionFields{
		Name: "Open the pull request",
		Outcomes: []Outcome{
			{Name: "opened", When: "pullRequestOpened", Tone: ToneGood, Then: Then{Action: ThenActionNext}},
			{Name: "failed", When: "failed", Tone: ToneProblem, Then: Then{Action: ThenActionHold}},
			{Name: "merged", When: "merged", Tone: ToneGood, Then: Then{Action: ThenActionNext}},
		},
	}
	action, err := newLegacyAction("ship", fields)
	if err != nil || action == nil {
		t.Fatalf("ship action = %v, %v", action, err)
	}
	if len(action.outcomes) != 2 {
		t.Fatalf("kept %d outcomes, want 2: %+v", len(action.outcomes), action.outcomes)
	}
	if action.outcomes[0].When != "It did what the brief asks." || action.outcomes[1].When != "It couldn't do it." {
		t.Fatalf("rewritten outcomes = %+v", action.outcomes)
	}
	if action.fallback != FallbackHold {
		t.Fatalf("fallback = %q, want hold", action.fallback)
	}
	if action.brief() != "" {
		t.Fatal("a ship action's brief is its agent's, so it is empty here")
	}
}

// TestLegacyActionIgnoresOtherTypes checks that a type which is not a legacy action is not one.
func TestLegacyActionIgnoresOtherTypes(t *testing.T) {
	t.Parallel()
	action, err := newLegacyAction("agent", legacyActionFields{Name: "Build"})
	if err != nil || action != nil {
		t.Fatalf("non-action type = %v, %v", action, err)
	}
}

// TestLegacyActionBriefs checks the briefs the comment, label and transition
// actions became, with the placeholders they used.
func TestLegacyActionBriefs(t *testing.T) {
	t.Parallel()
	const tick = "`"
	comment := func(text string) string {
		action, err := newLegacyAction("comment", legacyActionFields{Name: "Comment", Brief: new(text)})
		if err != nil || action == nil {
			t.Fatalf("comment action = %v, %v", action, err)
		}
		return action.brief()
	}
	wantComment := "Comment on {issue} with " + tick + "gh" + tick + " or " + tick + "acli" + tick + ". Post this text, where {previous} " +
		"stands for that file's text:\n\nHello {previous}, see {previous}\n\nWrite {output}: the link to your comment."
	if got := comment("  Hello {output}, see {output} \n"); got != wantComment {
		t.Fatalf("comment brief =\n%s\nwant\n%s", got, wantComment)
	}
	wantEmpty := "Comment on {issue} with " + tick + "gh" + tick + " or " + tick + "acli" + tick + ". Post the text in {previous}, " +
		"without its " + tick + "Outcome:" + tick + " line\n\nWrite {output}: the link to your comment."
	if got := comment(""); got != wantEmpty {
		t.Fatalf("empty comment brief =\n%s\nwant\n%s", got, wantEmpty)
	}
	label, err := newLegacyAction("label", legacyActionFields{Name: "Label", Brief: new("bug")})
	if err != nil || label == nil {
		t.Fatalf("label action = %v, %v", label, err)
	}
	wantLabel := "Add the label “bug” to {issue} with " + tick + "gh issue edit --add-label" + tick +
		". Change nothing else.\n\nWrite {output}: the label you added."
	if got := label.brief(); got != wantLabel {
		t.Fatalf("label brief = %q, want %q", got, wantLabel)
	}
	transition, err := newLegacyAction("transition", legacyActionFields{Name: "Move", Brief: new("Done")})
	if err != nil || transition == nil {
		t.Fatalf("transition action = %v, %v", transition, err)
	}
	wantTransition := "Move {issue} to the status “Done” with " + tick + "acli jira workitem transition" + tick +
		". Change nothing else.\n\nWrite {output}: the status it is in now."
	if got := transition.brief(); got != wantTransition {
		t.Fatalf("transition brief = %q, want %q", got, wantTransition)
	}
}

// TestASavedShipActionLoadsAsTheShipAgent
func TestASavedShipActionLoadsAsTheShipAgent(t *testing.T) {
	t.Parallel()
	stage := btDecode[Stage](t, `{"id":"ship","type":"ship","name":"Ship","outcomes":[
		{"id":"a","name":"opened","when":"pullRequestOpened","tone":"good","then":{"do":"done"}},
		{"id":"b","name":"merged","when":"pullRequestMerged","tone":"good","then":{"do":"done"}},
		{"id":"c","name":"failed","when":"failed","tone":"problem","then":{"do":"hold"}}]}`)
	if stage.Type != StageTypeAgent {
		t.Errorf("type = %q, want agent", stage.Type)
	}
	if stage.Agent == nil || *stage.Agent != (AgentRef{ID: "ship", Version: 1}) {
		t.Errorf("agent = %+v, want ship version 1", stage.Agent)
	}
	if stage.Workspace != WorkspaceWorktree {
		t.Errorf("workspace = %q, want worktree", stage.Workspace)
	}
	if stage.Writes() != "pull-request.md" {
		t.Errorf("writes = %q, want pull-request.md", stage.Writes())
	}
	if got := outcomeNames(stage.Outcomes); !slices.Equal(got, []string{"opened", "failed"}) {
		t.Errorf("outcomes = %v, want [opened failed]", got)
	}
	if d := testDiffEmpty(NewThen(ThenArgs{Action: ThenActionDone}), stage.Outcomes[0].Then); d != "" {
		t.Errorf("then differs:\n%s", d)
	}
}

// TestASavedCommentLoadsAsACustomAgentWithItsText
func TestASavedCommentLoadsAsACustomAgentWithItsText(t *testing.T) {
	t.Parallel()
	stage := btDecode[Stage](t, `{"id":"comment","type":"comment","name":"Comment","brief":"Found: {output}","outcomes":[
		{"id":"a","name":"posted","when":"commentPosted","tone":"good","then":{"do":"next"}}]}`)
	if stage.Agent == nil || *stage.Agent != (AgentRef{ID: "custom", Version: 1}) {
		t.Errorf("agent = %+v, want custom version 1", stage.Agent)
	}
	if stage.Workspace != WorkspaceReadOnly {
		t.Errorf("workspace = %q, want readOnly", stage.Workspace)
	}
	if !strings.Contains(stage.Brief, "Found: {previous}") {
		t.Errorf("brief = %q, want the output placeholder renamed to previous", stage.Brief)
	}
	if strings.Contains(stage.Brief, "{output}:") && !strings.HasSuffix(stage.Brief, "Write {output}: the link to your comment.") {
		t.Errorf("brief = %q keeps the output placeholder", stage.Brief)
	}
	if len(stage.Outcomes) == 0 || stage.Outcomes[0].When != "It did what the brief asks." {
		t.Errorf("first outcome = %+v", stage.Outcomes)
	}
}

// TestARunAtAnActionStageLoadsHeld checks that a run at a legacy action stage loads held.
// The run is testModelRun with the custom agent stage.
func TestARunAtAnActionStageLoadsHeld(t *testing.T) {
	t.Parallel()
	custom := NewStage(StageArgs{ID: "custom", Type: StageTypeAgent, Name: new("Custom agent"), Agent: &AgentRef{ID: "custom", Version: 1}, Outcomes: []Outcome{btOutcome("done")}})
	run := testModelRun(custom)
	// As an action stage left it: running, with an attempt no session belongs to.
	run.Attempts = []Attempt{NewAttempt(AttemptArgs{StageID: run.StageID, Number: 1, StartedAt: jsonx.Date{Time: testT0}, Folder: "01-custom-1"})}
	run.State = RunStateRunning
	loaded := btDecode[Run](t, btEncode(t, run))
	if loaded.State != RunStateHeld {
		t.Errorf("state = %s, want held", loaded.State)
	}
	if loaded.Reason == nil || *loaded.Reason != "Custom agent is an agent stage now" {
		t.Errorf("reason = %v", loaded.Reason)
	}
	if len(loaded.Attempts) != 1 || loaded.Attempts[0].EndedAt == nil {
		t.Errorf("attempts = %+v, want the open one ended", loaded.Attempts)
	}
}

// outcomeNames lists the names of outcomes, in order.
func outcomeNames(outcomes []Outcome) []string {
	names := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		names = append(names, o.Name)
	}
	return names
}

// TestLegacyActionDecodesStrictKeys checks the keys a saved action stage reads
// with decode (its id, type, name and outcomes) and that its fallback must be a case.
