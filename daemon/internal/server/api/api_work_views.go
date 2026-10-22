package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// WorkResponse is `GET /api/v1/views/work`: the global summary and the selected
// groups. All rows travel unfolded; folding stays a client preference.
type WorkResponse struct {
	Summary core.RunCounts `json:"summary"`
	// Caption is the global counts line, which no query changes.
	Caption string          `json:"caption"`
	Stage   *core.WorkStage `json:"stage,omitzero"`
	Belts   []core.WorkBelt `json:"belts"`
	// MainEmptyMessage is the main table's empty copy, present only when it has no row.
	MainEmptyMessage *string         `json:"mainEmptyMessage,omitzero"`
	MainGroups       []WorkGroupWire `json:"mainGroups"`
	WatchingGroups   []WorkGroupWire `json:"watchingGroups"`
	// ShadowGroups are the runs on a shadow version, a section of their own.
	ShadowGroups []WorkGroupWire `json:"shadowGroups"`
	// ShownRows counts the rows of every section; WatchingRows and ShadowRows count one section's alone.
	ShownRows    int                  `json:"shownRows"`
	WatchingRows int                  `json:"watchingRows"`
	ShadowRows   int                  `json:"shadowRows"`
	Waiting      []WaitingSubjectWire `json:"waiting"`
}

// NewWorkResponse builds the work response from its arguments.
func NewWorkResponse(view core.WorkView) WorkResponse {
	out := WorkResponse{
		Summary:          view.Summary,
		Caption:          view.Caption(),
		Stage:            view.Stage,
		Belts:            view.Belts,
		MainEmptyMessage: view.MainEmptyMessage,
		MainGroups:       []WorkGroupWire{},
		WatchingGroups:   []WorkGroupWire{},
		ShadowGroups:     []WorkGroupWire{},
		ShownRows:        view.ShownRowCount(),
		WatchingRows:     view.WatchingRowCount(),
		ShadowRows:       view.ShadowRowCount(),
		Waiting:          []WaitingSubjectWire{},
	}
	for _, group := range view.MainGroups {
		out.MainGroups = append(out.MainGroups, NewWorkGroupWire(group))
	}
	for _, group := range view.WatchingGroups {
		out.WatchingGroups = append(out.WatchingGroups, NewWorkGroupWire(group))
	}
	for _, group := range view.ShadowGroups {
		out.ShadowGroups = append(out.ShadowGroups, NewWorkGroupWire(group))
	}
	for _, waiting := range view.Waiting {
		out.Waiting = append(out.Waiting, NewWaitingSubjectWire(waiting))
	}
	return out
}

// WorkGroupWire is one group of work rows.
type WorkGroupWire struct {
	Dimension string `json:"dimension"`
	Value     string `json:"value"`
	Title     string `json:"title"`
	// Label is the header text, with a repository's disambiguated short name.
	Label string        `json:"label"`
	Count int           `json:"count"`
	Rows  []WorkRowWire `json:"rows"`
}

// NewWorkGroupWire builds the work group wire from its arguments.
func NewWorkGroupWire(group core.WorkGroupPresentation) WorkGroupWire {
	out := WorkGroupWire{
		Dimension: string(group.Key.Dimension),
		Value:     group.Key.Value,
		Title:     group.Title,
		Label:     group.Label,
		Count:     group.Count,
		Rows:      []WorkRowWire{},
	}
	for _, row := range group.Rows {
		out.Rows = append(out.Rows, NewWorkRowWire(row))
	}
	return out
}

// WorkLocationWire is where a work row's run is.
type WorkLocationWire struct {
	Text  string `json:"text"`
	Look  string `json:"look"`
	Glyph string `json:"glyph"`
}

// NewWorkLocationWire builds the work location wire from its arguments.
func NewWorkLocationWire(where core.StatusWhere) WorkLocationWire {
	return WorkLocationWire{Text: where.Text, Look: string(where.Look), Glyph: where.Glyph}
}

// WorkRunWire is the run a Work row carries: the fields the row and its link read. The
// whole run is the runs resource's, and every row of a page sends it, so a row sends
// only these; the Work page reads nothing else of it.
type WorkRunWire struct {
	ID        string           `json:"id"`
	BeltID    string           `json:"beltId"`
	BeltName  string           `json:"beltName"`
	StageID   string           `json:"stageId"`
	State     core.RunState    `json:"state"`
	Issue     WorkRunIssueWire `json:"issue"`
	UpdatedAt jsonx.Date       `json:"updatedAt"`
}

// WorkRunIssueWire is the subject a Work row names: its title.
type WorkRunIssueWire struct {
	Title string `json:"title"`
}

// NewWorkRunWire is the row's run view of run.
func NewWorkRunWire(run core.WorkRowRun) WorkRunWire {
	return WorkRunWire{
		ID: run.ID, BeltID: run.BeltID, BeltName: run.BeltName, StageID: run.StageID, State: run.State,
		Issue: WorkRunIssueWire{Title: run.Title}, UpdatedAt: run.UpdatedAt,
	}
}

// WorkRowWire is one row of the Work table.
type WorkRowWire struct {
	Run        WorkRunWire `json:"run"`
	Reference  string      `json:"reference"`
	Repository string      `json:"repository"`
	// UpdatedAgo is the Updated column's text, from the daemon's clock.
	UpdatedAgo  string           `json:"updatedAgo"`
	Progress    float64          `json:"progress"`
	Location    WorkLocationWire `json:"location"`
	PausedByYou bool             `json:"pausedByYou"`
	PauseWord   *string          `json:"pauseWord,omitzero"`
	Chip        *string          `json:"chip,omitzero"`
	NeedsYou    bool             `json:"needsYou"`
	// OpenStage is the stage a row opens at: a decision's stage, or nil to follow the run.
	OpenStage *string `json:"openStage,omitzero"`
	// Shadow is the chip of a run on a shadow version, such as "Shadow v2".
	Shadow *string `json:"shadow,omitzero"`
}

// NewWorkRowWire builds the value from its arguments; OpenStage
// comes from `WorkRowPresentation.openStage(for:)`.
func NewWorkRowWire(row core.WorkRowPresentation) WorkRowWire {
	return WorkRowWire{
		Run:         NewWorkRunWire(row.Run),
		Reference:   row.Reference,
		Repository:  row.Repository,
		UpdatedAgo:  row.UpdatedAgo,
		Progress:    row.Progress,
		Location:    NewWorkLocationWire(row.Location),
		PausedByYou: row.PausedByYou,
		PauseWord:   row.PauseWord,
		Chip:        row.Chip,
		NeedsYou:    row.NeedsYou,
		OpenStage:   row.OpenStage,
		Shadow:      row.Shadow,
	}
}
