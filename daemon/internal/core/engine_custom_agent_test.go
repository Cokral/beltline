// Fixtures for the writer, the checker and customBelt, shared by the placeholder,
// agent update and custom agent tests.

package core

// customTestWriter and customTestChecker are the writer and
// checker agents, which the rendering tests use.
func customTestWriter() Agent {
	return NewAgent(AgentArgs{ID: "writer", Name: "Writer", Key: new("draft"), Workspace: new(WorkspaceWorktree),
		Outcomes: []AgentOutcome{{Name: "done", When: "It's written.", Tone: ToneGood, Then: NewThen(ThenArgs{Action: ThenActionNext})}},
		Brief:    "Write {draft}."})
}

func customTestChecker() Agent {
	return NewAgent(AgentArgs{ID: "checker", Name: "Checker", Key: new("check"), Workspace: new(WorkspaceWorktreeReadOnly),
		Outcomes: []AgentOutcome{
			{Name: "fine", When: "It holds up.", Tone: ToneGood, Then: NewThen(ThenArgs{Action: ThenActionNext})},
			{Name: "redo", When: "It doesn't.", Tone: ToneProblem, Then: NewThen(ThenArgs{Action: ThenActionGoto, WithOutput: true, Max: new(1)}), To: []string{"writer"}},
		},
		Brief: "Check {draft} and write {check}."})
}

// customTestBelt is an issues belt of acme/billing: the two
// agents, then a watch.
func customTestBelt(writer, checker Agent) Belt {
	var stages []Stage
	stages = append(stages, DefaultsStageForAgent(writer, stages, nil, stages, TriggerKindIssues))
	stages = append(stages, DefaultsStageForAgent(checker, stages, nil, stages, TriggerKindIssues))
	stages = append(stages, DefaultsStage(StageTypeWatch, stages, nil, stages))
	return NewBeltFromTrigger(BeltFromTriggerArgs{ID: "custom", Name: "Custom", Trigger: Trigger{Kind: TriggerKindIssues, Issues: &IssueFilter{Label: "agent", Assignee: TriggerAssigneeAnyone, Poll: 60}},
		Repos: []string{"acme/billing"}, Stages: stages})
}
