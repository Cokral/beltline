package coordinator

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// BeltContext is what the belt editor needs beside a draft, computed by the
// daemon: the actions the draft may take, its warnings, the placeholder help for
// one stage, and the menus, outcome labels, schedule summaries and policy options.
type BeltContext struct {
	Actions      []core.BeltAction                             `json:"actions"`
	Warnings     []string                                      `json:"warnings"`
	Placeholders *core.PlaceholderHelp                         `json:"placeholders,omitzero"`
	Routes       []core.BeltContextPresentationRouteItem       `json:"routes"`
	Stages       []core.BeltContextPresentationStageItem       `json:"stages"`
	Outcomes     []core.BeltContextPresentationOutcomePreview  `json:"outcomes"`
	Schedules    []core.BeltContextPresentationSchedulePreview `json:"schedules"`
	Policies     []core.BeltContextPresentationPolicyOption    `json:"policies"`
	// PolicySection is the policies the belt's triggers can use, worded for them; absent when none can.
	PolicySection *core.BeltContextPresentationPolicySection `json:"policySection,omitzero"`
	// TriggerForm is the trigger editor's kinds, choices and help.
	TriggerForm *core.TriggerFormPresentation `json:"triggerForm,omitzero"`
	// Stage is the stage form's menus for the stage asked about.
	Stage *core.StageMenu `json:"stage,omitzero"`
	// Notes is the line under the warnings: one while the belt has unsent changes,
	// one for the saved belt as it is. The browser decides which applies.
	Notes *EditorNotes `json:"notes,omitzero"`
	// Agent is the agent version that stage pins, for its inherited brief and the Update offer.
	Agent *PinnedAgent `json:"agent,omitzero"`
	// Line is the stage line: its blocks, fork boxes and where each "+" inserts a stage.
	Line *core.StageLinePresentation `json:"line,omitzero"`
	// OutdatedStages counts the stages whose agent has a newer version, for the Update offer; absent when none.
	OutdatedStages *int `json:"outdatedStages,omitzero"`
	// Task is the task sheet's defaults: the first manual entry and its first repository.
	Task *core.TaskDraftSelection `json:"task,omitzero"`
	// TaskEntries are the entries the task sheet chooses from.
	TaskEntries []core.TaskDraftEntry `json:"taskEntries"`
	// ModelStatus is the stage's model when the runner's allowed list no longer offers it.
	ModelStatus *StageModelStatus `json:"modelStatus,omitzero"`
	// ShadowDifferences, for a shadow draft, are its stages that differ from the live version.
	ShadowDifferences []core.ShadowStageDifference `json:"shadowDifferences,omitzero"`
}

// EditorNotes are the warning notes for a draft, while it is being edited and
// once it is saved as it is.
type EditorNotes struct {
	WhileEditing *string `json:"whileEditing,omitzero"`
	WhenSaved    *string `json:"whenSaved,omitzero"`
}

// StageModelStatus is a stage's model that the picker must mark: not allowed for
// its runner, or allowed but missing from the catalog.
type StageModelStatus struct {
	ID string `json:"id"`
	// Label is the option's label, such as "Model — not allowed".
	Label string `json:"label"`
	// Reason is the reason alone: "not allowed" or "unavailable in catalog".
	Reason string `json:"reason"`
}

// PinnedAgent is the agent version a stage pins, as the stage form shows it.
type PinnedAgent struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	// Name is the agent's name, or its id when that version is missing from the library.
	Name string `json:"name"`
	// Brief is what a stage with no brief of its own runs with; empty when the version is missing.
	Brief string `json:"brief"`
	// Key is the key the stage's output goes by when the stage names none.
	Key *string `json:"key,omitzero"`
	// LatestVersion is the latest version, when it is newer than the pinned one.
	LatestVersion *int `json:"latestVersion,omitzero"`
	Archived      bool `json:"archived"`
	// Missing is true when the library has no file for the pinned version.
	Missing bool `json:"missing"`
}

// BeltContext is the editor's context for a draft. Read only: nothing is saved
// and the draft is the caller's.
// Lock: exported entry point; takes c.mu. Suspends: none. The stage line is read with c.mu held.
func (c *Coordinator) BeltContext(draft core.Belt, stage *string) BeltContext {
	c.Lock()
	defer c.EndTurn()
	notes := c.editorNotesNow(draft)
	line := c.stageLine(draft)
	form := core.TriggerFormPresentationMake(draft)
	task := core.TaskDraftSelectionOf(draft, nil, "", "")
	now := c.clock.Now()
	context := BeltContext{
		Actions:     c.draftActionsNow(draft),
		Warnings:    c.library.Warnings(draft),
		Routes:      core.BeltContextPresentationRoutes(draft),
		Stages:      core.BeltContextPresentationStages(draft),
		Outcomes:    core.BeltContextPresentationOutcomes(draft),
		Schedules:   core.BeltContextPresentationSchedules(draft, &now, nil),
		Policies:    core.BeltContextPresentationPolicies(draft),
		TriggerForm: &form,
		Notes:       &notes,
		Line:        &line,
		Task:        &task,
		TaskEntries: core.TaskDraftManuals(draft),
	}
	if outdated := c.outdatedStageCount(draft); outdated > 0 {
		context.OutdatedStages = &outdated
	}
	context.PolicySection = core.BeltContextPresentationPolicySectionOf(draft)
	if stage != nil {
		help := c.library.PlaceholderHelp(draft, stage)
		context.Placeholders = &help
		context.Stage = core.StageMenuMake(draft, *stage)
		context.Agent = c.pinnedAgentNow(draft, *stage)
		context.ModelStatus = c.stageModelStatusNow(draft, *stage)
	}
	return context
}

// ShadowDifferences are the stages a shadow draft adds, changes or removes against the live
// version of its belt; empty when the belt is not saved.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) ShadowDifferences(draft core.Belt) []core.ShadowStageDifference {
	c.Lock()
	defer c.EndTurn()
	record := c.library.Belt(draft.ID)
	if record == nil {
		return []core.ShadowStageDifference{}
	}
	return core.ShadowDifferences(record.Belt, draft)
}

// EditorNotes is the editor's note under the warnings, for the draft and for the
// saved version it was opened from.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) EditorNotes(draft core.Belt) EditorNotes {
	c.Lock()
	defer c.EndTurn()
	return c.editorNotesNow(draft)
}

// editorNotesNow is EditorNotes with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) editorNotesNow(draft core.Belt) EditorNotes {
	draftWarnings := c.library.Warnings(draft)
	var savedWarnings []string
	paused := false
	if record := c.library.Belt(draft.ID); record != nil {
		savedWarnings = c.library.Warnings(record.Belt)
		paused = record.Paused
	}
	return EditorNotes{
		WhileEditing: core.BeltEditorWarningNote(draftWarnings, savedWarnings, true, paused),
		WhenSaved:    core.BeltEditorWarningNote(draftWarnings, savedWarnings, false, paused),
	}
}

// StageModelStatus returns the model a stage asks for when the runner's allowed
// list does not offer it; nil when it is offered.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) StageModelStatus(draft core.Belt, stageID string) *StageModelStatus {
	c.Lock()
	defer c.EndTurn()
	return c.stageModelStatusNow(draft, stageID)
}

// stageModelStatusNow is StageModelStatus with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) stageModelStatusNow(draft core.Belt, stageID string) *StageModelStatus {
	stage := beltStageNamed(draft, stageID)
	if stage == nil || stage.Kind() != core.StageKindAgent || stage.Model == "" {
		return nil
	}
	runner := core.BackendPi
	if stage.Runner != nil {
		runner = *stage.Runner
	} else if stage.Agent != nil {
		if agent := c.library.Agent(stage.Agent); agent != nil {
			runner = agent.Runner
		}
	}
	for _, choice := range c.settings.AllowedChoices(runner, "", c.env) {
		if choice.ID == stage.Model {
			return nil
		}
	}
	var listed *core.ModelChoice
	if snapshot, ok := c.settings.ModelCatalogs[string(runner)]; ok && snapshot.IsUsable(runner, runner.Executable(c.env)) {
		for i := range snapshot.Models {
			if snapshot.Models[i].ID == stage.Model {
				listed = &snapshot.Models[i]
				break
			}
		}
	}
	reason := "unavailable in catalog"
	name := stage.Model
	if listed != nil {
		reason = "not allowed"
		name = listed.DisplayLabel()
	}
	return &StageModelStatus{ID: stage.Model, Label: name + " — " + reason, Reason: reason}
}

// PinnedAgent returns the agent the stage pins, with its brief and whether a
// newer version is in the library.
// Lock: exported entry point; takes c.mu.
func (c *Coordinator) PinnedAgent(draft core.Belt, stageID string) *PinnedAgent {
	c.Lock()
	defer c.EndTurn()
	return c.pinnedAgentNow(draft, stageID)
}

// pinnedAgentNow is PinnedAgent with c.mu held.
// Lock: runs with c.mu held.
func (c *Coordinator) pinnedAgentNow(draft core.Belt, stageID string) *PinnedAgent {
	stage := beltStageNamed(draft, stageID)
	if stage == nil || stage.Kind() != core.StageKindAgent || stage.Agent == nil {
		return nil
	}
	ref := *stage.Agent
	agent := c.library.Agent(&ref)
	latest := c.library.LatestAgent(ref.ID)
	pinned := &PinnedAgent{ID: ref.ID, Version: ref.Version, Name: ref.ID, Missing: agent == nil}
	if agent != nil {
		pinned.Name = agent.Name
		pinned.Brief = agent.Brief
		if key := core.StageKeysLegacyName(*stage, map[core.AgentRef]core.Agent{ref: *agent}); key != "" {
			pinned.Key = &key
		}
	}
	if latest != nil {
		pinned.Archived = latest.Archived
		if latest.Agent.Version > ref.Version {
			version := latest.Agent.Version
			pinned.LatestVersion = &version
		}
	}
	return pinned
}

// beltStageNamed is the stage of belt with id, or nil.
// Lock: none (pure).
func beltStageNamed(belt core.Belt, id string) *core.Stage {
	for i := range belt.Stages {
		if belt.Stages[i].ID == id {
			return &belt.Stages[i]
		}
	}
	return nil
}
