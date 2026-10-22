//
// LegacyAction's members are unexported. Its initialiser reads a Stage's own keys from the stage's JSON object: that is
// legacyActionFields, decoded by the caller.

package core

import "strings"

// legacyActionSucceeded are the events the action's outcomes were tied to that
// meant it worked.
var legacyActionSucceeded = map[string]struct{}{
	"pullRequestOpened": {}, "commentPosted": {}, "labelAdded": {}, "reviewSubmitted": {}, "ticketMoved": {},
}

// legacyActionFields are the keys of a stage object an action stage reads.
type legacyActionFields struct {
	Name     string    `json:"name"`
	Brief    *string   `json:"brief,omitzero"`
	Outcomes []Outcome `json:"outcomes"`
	Fallback *Fallback `json:"fallback,omitzero"`
}

// legacyAction is an action stage saved before agents made every outward call:
// opening a pull request, a comment, a label, a review or a ticket move. It
// loads as an agent stage that does the same, with outcomes that route the same way.
type legacyAction struct {
	// typ is the old stage type: ship, comment, label, submitReview or transition.
	typ string
	// name is the stage's name.
	name string
	// text is the comment's text, the label, or the status.
	text string
	// outcomes are the outcomes kept, with the events the action's outcomes were tied to rewritten.
	outcomes []Outcome
	fallback Fallback
}

// newLegacyAction reads an action stage from its typ and fields. It returns nil,
// with no error, when typ is not one of the legacy action types.
// Outcomes tied to a merge or a close (not in
// legacyActionSucceeded, "failed" or the events above) are dropped.
func newLegacyAction(typ string, fields legacyActionFields) (*legacyAction, error) {
	switch typ {
	case "ship", "comment", "label", "submitReview", "transition":
	default:
		return nil, nil
	}
	var text string
	if fields.Brief != nil {
		text = strings.TrimSpace(*fields.Brief)
	}
	// Outcomes tied to a merge or a close waited on the pull request; a watch stage does that now.
	var outcomes []Outcome
	for _, outcome := range fields.Outcomes {
		if _, ok := legacyActionSucceeded[outcome.When]; ok {
			outcome.When = "It did what the brief asks."
		} else if outcome.When == "failed" {
			outcome.When = "It couldn't do it."
		} else {
			continue
		}
		outcomes = append(outcomes, outcome)
	}
	fallback := ValueOr(fields.Fallback, FallbackHold)
	return &legacyAction{typ: typ, name: fields.Name, text: text, outcomes: outcomes, fallback: fallback}, nil
}

// stage is the agent stage this action becomes, with the id given.
func (a legacyAction) stage(id string) Stage {
	agent, writes := "custom", "output.md"
	switch a.typ {
	case "ship":
		agent, writes = "ship", "pull-request.md"
	case "submitReview":
		agent, writes = "post-review", "posted-review.md"
	}
	workspace := WorkspaceReadOnly
	if a.typ == "ship" {
		workspace = WorkspaceWorktree
	}
	name := a.name
	runner := BackendPi
	slots := 2
	fallback := a.fallback
	return NewStage(StageArgs{
		ID:        id,
		Type:      StageTypeAgent,
		Name:      &name,
		Agent:     &AgentRef{ID: agent, Version: 1},
		Runner:    &runner,
		Model:     DefaultsModel,
		Workspace: &workspace,
		Writes:    &writes,
		Slots:     &slots,
		Brief:     a.brief(),
		Outcomes:  a.outcomes,
		Fallback:  &fallback,
	})
}

// brief is the brief of a comment, label or ticket move; the others use their
// agent's, so it is empty for them.
func (a legacyAction) brief() string {
	const tick = "`"
	switch a.typ {
	case "comment":
		// `{output}` meant the previous agent's output, which is `{previous}` now.
		text := strings.ReplaceAll(a.text, "{output}", "{previous}")
		body := "this text, where {previous} stands for that file's text:\n\n" + text
		if text == "" {
			body = "the text in {previous}, without its " + tick + "Outcome:" + tick + " line"
		}
		return "Comment on {issue} with " + tick + "gh" + tick + " or " + tick + "acli" + tick + ". Post " + body +
			"\n\nWrite {output}: the link to your comment."
	case "label":
		return "Add the label “" + a.text + "” to {issue} with " + tick + "gh issue edit --add-label" + tick +
			". Change nothing else.\n\nWrite {output}: the label you added."
	case "transition":
		return "Move {issue} to the status “" + a.text + "” with " + tick + "acli jira workitem transition" + tick +
			". Change nothing else.\n\nWrite {output}: the status it is in now."
	}
	return ""
}
