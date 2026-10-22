// The run page's projection: its header facts, cost, links, watch notes and
// stages. The stage facts and inline spans are in api_run_views_facts.go, the
// map, Markdown blocks and attempt output and artifacts in api_run_views_map.go.
package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
)

// RunViewResponse is the complete daemon-owned run page projection. Navigation
// and selected attempts are intentionally left to each client.
//
// Map, Boxes and Metro are sent only for their look (omitted otherwise);
// HeldAnswer, FirstVerdict and PauseCopy are omitted when nil.
type RunViewResponse struct {
	Run core.Run `json:"run"`
	// AvailableWidth is the width the page has for the belt's drawing.
	AvailableWidth float64 `json:"availableWidth"`
	// Look is how the belt is drawn: `boxes`, `machines` or `metro` (BeltLook).
	Look string `json:"look"`
	// Map is the belt as machines on a track. Sent only for the `machines` look.
	Map *RunMapWire `json:"map,omitzero"`
	// Boxes is the belt as stage boxes. Sent only for the `boxes` look.
	Boxes *core.RunBoxes `json:"boxes,omitzero"`
	// Metro is the belt as a metro line. Sent only for the `metro` look.
	Metro    *core.RunMetro `json:"metro,omitzero"`
	Metadata string         `json:"metadata"`
	// Heading is the tag over the title: the belt's name and the run's repository, short.
	Heading string `json:"heading"`
	// Reference is the subject's reference: `#139`, or a ticket key.
	Reference    string  `json:"reference"`
	FirstVerdict *string `json:"firstVerdict,omitzero"`
	// Shadow is the chip of a run on a shadow version, such as "Shadow v2".
	Shadow      *string     `json:"shadow,omitzero"`
	PauseCopy   *string     `json:"pauseCopy,omitzero"`
	ErasureCopy string      `json:"erasureCopy"`
	Cost        RunCostWire `json:"cost"`
	// SessionCount is how many attempts ran an agent session: the Sessions button's count.
	SessionCount        int                `json:"sessionCount"`
	Links               []RunLinkWire      `json:"links"`
	Worktrees           []RunWorktreeWire  `json:"worktrees"`
	WorktreeAction      string             `json:"worktreeAction"`
	WorktreeUnavailable string             `json:"worktreeUnavailable"`
	WatchNotes          []RunWatchNoteWire `json:"watchNotes"`
	Stages              []RunStageWire     `json:"stages"`
	// HeldAnswer is a held run's in-session answer button, its help and what happens next.
	HeldAnswer *core.HeldAnswerCopy `json:"heldAnswer,omitzero"`
	Legs       []RunLegWire         `json:"legs"`
	Actions    []core.RunAction     `json:"actions"`
	// Afk is the run's AFK button: its mode, whether that means AFK mode now,
	// the mode a click moves to, and its help.
	Afk RunAfkWire `json:"afk"`
	// Active is false once the run has shipped or been dropped: the page then
	// hides its live controls (AFK, Pause, Send to, Abort and Drop).
	Active bool `json:"active"`
	// QuickActions are the shortcut buttons the page shows beside its menu, in order:
	// Start now on a queued run and Retry when a saved attempt can be retried.
	QuickActions []core.RunCommand `json:"quickActions"`
	// Menu is the Run actions menu's rows, in order, each with the record it is enabled from.
	Menu []core.RunMenuRow `json:"menu"`
}

// NewRunViewResponse ports `RunViewResponse.init(_ snapshot: CoordinatorRunViewSnapshot)`.
func NewRunViewResponse(snapshot coordinator.CoordinatorRunViewSnapshot) RunViewResponse {
	out := RunViewResponse{
		Run:            snapshot.Run,
		AvailableWidth: snapshot.AvailableWidth,
		Look:           string(snapshot.Look),
		Boxes:          snapshot.Boxes,
		Metadata:       snapshot.Presentation.Metadata,
		Reference:      snapshot.Run.Subject.Ref(),
		FirstVerdict:   snapshot.Presentation.FirstVerdict,
		Shadow:         core.ShadowChip(snapshot.Run),
		PauseCopy:      snapshot.Presentation.PauseCopy,
		ErasureCopy:    snapshot.Presentation.ErasureCopy,
		Cost:           NewRunCostWire(snapshot.Presentation.Spend, snapshot.Presentation.CostLine),
		SessionCount:   snapshot.Presentation.SessionCount,
		Links:          []RunLinkWire{},
		Worktrees:      []RunWorktreeWire{},
		WorktreeAction: runner.WorktreeTargetsAction,
		// WorktreeUnavailable is the reason when a run has no worktree to open.
		WorktreeUnavailable: runner.WorktreeTargetsUnavailable,
		WatchNotes:          []RunWatchNoteWire{},
		Stages:              []RunStageWire{},
		HeldAnswer:          core.HeldAnswerCopyForRun(snapshot.Run),
		Legs:                []RunLegWire{},
		Actions:             snapshot.Presentation.Actions,
		Afk:                 NewRunAfkWire(snapshot.Run, snapshot.AfkGlobal),
		Active:              snapshot.Run.State.IsActive(),
		QuickActions:        core.RunQuickActions(snapshot.Run, snapshot.Presentation.Actions),
		Menu:                core.RunMenu(snapshot.Run, snapshot.Presentation.Actions),
	}
	if snapshot.Look == core.BeltLookMachines {
		mapWire := NewRunMapWire(snapshot)
		out.Map = &mapWire
	}
	if snapshot.Look == core.BeltLookMetro {
		metro := RunMetroWire(snapshot)
		out.Metro = &metro
	}
	out.Heading = core.RunPresentationHeading(snapshot.Run, snapshot.AmbiguousRepositories)
	for _, link := range snapshot.Presentation.Links {
		out.Links = append(out.Links, NewRunLinkWire(link))
	}
	for _, target := range snapshot.Worktrees {
		out.Worktrees = append(out.Worktrees, NewRunWorktreeWire(target))
	}
	for _, note := range snapshot.Presentation.WatchNotes {
		out.WatchNotes = append(out.WatchNotes, NewRunWatchNoteWire(note))
	}
	for _, panel := range snapshot.Presentation.Stages {
		var adjust *RunStageAdjustWire
		if help, ok := snapshot.AdjustHelp[panel.ID]; ok {
			wire := NewRunStageAdjustWire(help)
			adjust = &wire
		}
		var facts *RunStageFactsWire
		if panelFacts, ok := snapshot.PanelFacts[panel.ID]; ok {
			wire := NewRunStageFactsWire(panelFacts)
			facts = &wire
		}
		out.Stages = append(out.Stages, NewRunStageWire(panel, adjust, facts))
	}
	for _, leg := range snapshot.Presentation.Legs {
		out.Legs = append(out.Legs, NewRunLegWire(leg))
	}
	return out
}

// RunAfkWire is the run's AFK button.
type RunAfkWire struct {
	Mode  string `json:"mode"`
	Label string `json:"label"`
	On    bool   `json:"on"`
	Next  string `json:"next"`
	Help  string `json:"help"`
}

// NewRunAfkWire ports `RunAfkWire.init(run:global:)`; core.RunAfkButtonHelp writes the help.
func NewRunAfkWire(run core.Run, global bool) RunAfkWire {
	mode := run.Afk()
	on := run.AfkEffective(global)
	return RunAfkWire{
		Mode:  string(mode),
		Label: mode.Label(),
		On:    on,
		Next:  string(mode.Next()),
		Help:  core.RunAfkButtonHelp(mode, on),
	}
}

// RunWorktreeWire is one existing worktree the daemon can open in an editor.
// Detail is its full path.
type RunWorktreeWire struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// NewRunWorktreeWire ports `RunWorktreeWire.init(_ target: WorktreeTargets.Target)`.
func NewRunWorktreeWire(target runner.WorktreeTarget) RunWorktreeWire {
	return RunWorktreeWire{ID: target.ID, Title: target.Title, Detail: target.Detail()}
}

// RunCostWire is the run's cost. Nanos and Line are omitted when nil.
type RunCostWire struct {
	Nanos               *int64  `json:"nanos,omitzero"`
	Estimated           bool    `json:"estimated"`
	Partial             bool    `json:"partial"`
	EndedAgents         int     `json:"endedAgents"`
	LiveAgents          bool    `json:"liveAgents"`
	Line                *string `json:"line,omitzero"`
	EstimateExplanation string  `json:"estimateExplanation"`
}

// NewRunCostWire ports `RunCostWire.init(_ spend: Spend, line:)`.
func NewRunCostWire(spend core.Spend, line *string) RunCostWire {
	out := RunCostWire{
		Estimated:           spend.Estimated,
		Partial:             spend.Partial,
		EndedAgents:         spend.EndedAgents,
		LiveAgents:          spend.LiveAgents,
		Line:                line,
		EstimateExplanation: core.CostEstimateExplanation,
	}
	if spend.Nanos != nil {
		nanos := int64(*spend.Nanos)
		out.Nanos = &nanos
	}
	return out
}

// RunLinkWire is one link the run page offers.
type RunLinkWire struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Reference string `json:"reference"`
	Detail    string `json:"detail"`
	URL       string `json:"url"`
}

// NewRunLinkWire ports `RunLinkWire.init(_ target: RunLinks.Target)`.
func NewRunLinkWire(target core.RunLinksTarget) RunLinkWire {
	return RunLinkWire{ID: target.ID, Title: target.Title, Reference: target.Reference, Detail: target.Detail(), URL: target.URL}
}

// RunWatchNoteWire is one watch note on the run page.
type RunWatchNoteWire struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
}

// NewRunWatchNoteWire ports `RunWatchNoteWire.init(_ note: RunPresentation.WatchNote)`.
func NewRunWatchNoteWire(note core.RunPresentationWatchNote) RunWatchNoteWire {
	return RunWatchNoteWire{ID: note.ID, Reference: note.Reference, Note: note.Note}
}

// RunStageWire is one stage panel of the run page. Optional fields are omitted
// when nil; Adjust and Facts are set from the snapshot's per-stage help.
type RunStageWire struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Number int    `json:"number"`
	Kind   string `json:"kind"`
	// Step is the panel's step word: done, running, waiting, queued, held,
	// parked, dropped, redo, notStarted, skipped.
	Step             string                `json:"step"`
	Status           RunStatusWire         `json:"status"`
	CompactStatus    RunStatusWire         `json:"compactStatus"`
	DefaultAttempt   *string               `json:"defaultAttempt,omitzero"`
	Attempts         []RunAttemptPanelWire `json:"attempts"`
	EmptyMessage     *string               `json:"emptyMessage,omitzero"`
	LiveWatchSummary *string               `json:"liveWatchSummary,omitzero"`
	Machine          string                `json:"machine"`
	Tint             string                `json:"tint"`
	Worker           *string               `json:"worker,omitzero"`
	// Adjust is an agent stage's Adjust stage help; absent for approvals and watches.
	Adjust *RunStageAdjustWire `json:"adjust,omitzero"`
	// Facts are the panel's head chips, attempt lines and an approval's document.
	Facts *RunStageFactsWire `json:"facts,omitzero"`
}

// NewRunStageWire ports `RunStageWire.init(_ panel: RunPresentation.StagePanel, adjust:facts:)`.
func NewRunStageWire(panel core.RunPresentationStagePanel, adjust *RunStageAdjustWire, facts *RunStageFactsWire) RunStageWire {
	out := RunStageWire{
		ID:               panel.ID,
		Name:             panel.Name,
		Number:           panel.Number,
		Kind:             string(panel.Kind),
		Step:             runStageStepWire(panel.Step),
		Status:           NewRunStatusWire(panel.Status),
		CompactStatus:    NewRunStatusWire(panel.CompactStatus),
		DefaultAttempt:   panel.DefaultAttempt,
		Attempts:         []RunAttemptPanelWire{},
		EmptyMessage:     panel.EmptyMessage,
		LiveWatchSummary: panel.LiveWatchSummary,
		Machine:          string(panel.Machine),
		Tint:             string(panel.Tint),
		Worker:           panel.Worker,
		Adjust:           adjust,
		Facts:            facts,
	}
	for _, attempt := range panel.Attempts {
		out.Attempts = append(out.Attempts, NewRunAttemptPanelWire(attempt))
	}
	return out
}

// runStageStepWire is the step word a stage panel shows: todo is written as notStarted.
func runStageStepWire(step core.StatusStep) string {
	if step == core.StatusStepTodo {
		return "notStarted"
	}
	return string(step)
}

// RunStatusWire is a stage status: its text, colour look and machine face.
type RunStatusWire struct {
	Text string `json:"text"`
	Look string `json:"look"`
	Face string `json:"face"`
}

// NewRunStatusWire ports `RunStatusWire.init(_ status: StageStatus)`.
func NewRunStatusWire(status core.StageStatus) RunStatusWire {
	return RunStatusWire{Text: status.Text, Look: string(status.Look), Face: string(status.Face)}
}

// RunStageAdjustWire is a stage's Adjust sheet: its help, the current settings
// and the runners the sheet offers. AgentBrief, Thinking and RepairNote are
// omitted when nil.
type RunStageAdjustWire struct {
	Placeholders core.PlaceholderHelp `json:"placeholders"`
	Note         string               `json:"note"`
	AgentBrief   *string              `json:"agentBrief,omitzero"`
	Runner       string               `json:"runner"`
	Model        string               `json:"model"`
	Thinking     *string              `json:"thinking,omitzero"`
	Brief        string               `json:"brief"`
	// Runners is every runner the sheet offers, with the models and effort levels each takes in this run.
	Runners []RunAdjustRunnerWire `json:"runners"`
	// RepairNote is shown under the model list of a legacy blank stage.
	RepairNote *string `json:"repairNote,omitzero"`
	// FreshSessionNote is shown under the sheet's fields: the changes apply to the next fresh session.
	FreshSessionNote string `json:"freshSessionNote"`
}

// NewRunStageAdjustWire ports `RunStageAdjustWire.init(_ help: StageAdjustHelp)`.
func NewRunStageAdjustWire(help coordinator.StageAdjustHelp) RunStageAdjustWire {
	out := RunStageAdjustWire{
		Placeholders:     help.Placeholders,
		Note:             help.Note,
		AgentBrief:       help.AgentBrief,
		Runner:           string(help.Runner),
		Model:            help.Model,
		Brief:            help.Brief,
		Runners:          []RunAdjustRunnerWire{},
		RepairNote:       help.RepairNote,
		FreshSessionNote: core.StageAdjustFreshSessionNote,
	}
	if help.Thinking != nil {
		thinking := string(*help.Thinking)
		out.Thinking = &thinking
	}
	for _, rn := range help.Runners {
		out.Runners = append(out.Runners, NewRunAdjustRunnerWire(rn))
	}
	return out
}

// RunAdjustRunnerWire is one runner row of the Adjust sheet. EmptyNote is shown
// instead of the model list when this run may use no model on the runner.
type RunAdjustRunnerWire struct {
	Runner       string             `json:"runner"`
	Label        string             `json:"label"`
	Models       []core.ModelChoice `json:"models"`
	Efforts      []string           `json:"efforts"`
	ThinkingNote *string            `json:"thinkingNote,omitzero"`
	EmptyNote    *string            `json:"emptyNote,omitzero"`
}

// NewRunAdjustRunnerWire ports `RunAdjustRunnerWire.init(_ runner: StageAdjustRunner)`.
func NewRunAdjustRunnerWire(rn coordinator.StageAdjustRunner) RunAdjustRunnerWire {
	out := RunAdjustRunnerWire{
		Runner:       string(rn.Backend),
		Label:        rn.Backend.Label(),
		Models:       rn.Models,
		Efforts:      []string{},
		ThinkingNote: rn.ThinkingNote,
	}
	for _, effort := range rn.Efforts {
		out.Efforts = append(out.Efforts, string(effort))
	}
	if len(rn.Models) == 0 {
		empty := core.StageAdjustNoModelsAllowed
		out.EmptyNote = &empty
	}
	return out
}

// RunAttemptPanelWire is one attempt row of a stage panel.
type RunAttemptPanelWire struct {
	ID             string  `json:"id"`
	Number         int     `json:"number"`
	Caption        string  `json:"caption"`
	Content        string  `json:"content"`
	RunningAttempt *int    `json:"runningAttempt,omitzero"`
	Outcome        *string `json:"outcome,omitzero"`
	Tone           *string `json:"tone,omitzero"`
	Route          *string `json:"route,omitzero"`
	Note           *string `json:"note,omitzero"`
	Detail         *string `json:"detail,omitzero"`
	IsOpen         bool    `json:"isOpen"`
}

// NewRunAttemptPanelWire ports `RunAttemptPanelWire.init(_ attempt: RunPresentation.AttemptEntry)`.
func NewRunAttemptPanelWire(attempt core.RunPresentationAttemptEntry) RunAttemptPanelWire {
	out := RunAttemptPanelWire{
		ID:             attempt.ID,
		Number:         attempt.Number,
		Caption:        attempt.Caption,
		Content:        string(attempt.Content),
		RunningAttempt: attempt.RunningAttempt,
		Outcome:        attempt.Outcome,
		Route:          attempt.Route,
		Note:           attempt.Note,
		Detail:         attempt.Detail,
		IsOpen:         attempt.IsOpen,
	}
	if attempt.Tone != nil {
		tone := string(*attempt.Tone)
		out.Tone = &tone
	}
	return out
}

// RunLegWire is one leg of a split run.
type RunLegWire struct {
	ID    string  `json:"id"`
	Route *string `json:"route,omitzero"`
	Stage string  `json:"stage"`
	State string  `json:"state"`
	Look  string  `json:"look"`
	Face  string  `json:"face"`
}

// NewRunLegWire ports `RunLegWire.init(_ leg: Status.LegRow)`.
func NewRunLegWire(leg core.StatusLegRow) RunLegWire {
	return RunLegWire{
		ID:    leg.ID,
		Route: leg.Route,
		Stage: leg.Stage,
		State: string(leg.State),
		Look:  string(leg.Look),
		Face:  string(leg.Face),
	}
}
