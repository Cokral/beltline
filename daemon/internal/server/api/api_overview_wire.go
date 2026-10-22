package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// OverviewResponse is `GET /api/v1/views/overview`: the Summary, the Floor in
// the belt look Settings picks, and the global counts. Every figure, order and
// coordinate is computed by Core; the browser draws what arrives and computes
// none of it.
type OverviewResponse struct {
	Summary core.RunCounts `json:"summary"`
	// Caption is the global counts line the Overview's greeting carries.
	Caption string `json:"caption"`
	// Greeting is "Good morning." and its kin, for the daemon's clock and calendar.
	Greeting string `json:"greeting"`
	// Look is how the Floor is drawn: the belt look Settings holds.
	Look  core.BeltLook       `json:"look"`
	Today OverviewSummaryWire `json:"today"`
	Floor OverviewFloorWire   `json:"floor"`
	// MetroLayout is the metro's drawing, sent only when the Floor is the metro.
	MetroLayout *MetroLayoutWire `json:"metroLayout,omitzero"`
	// Runs is every active run the Overview names, ordered by id.
	Runs []OverviewRunWire `json:"runs"`
}

// NewOverviewResponse builds the overview response from its arguments.
func NewOverviewResponse(view core.OverviewView) OverviewResponse {
	out := OverviewResponse{
		Summary:  view.Summary,
		Caption:  view.Today.Caption,
		Greeting: view.Greeting,
		Look:     view.Look,
		Today:    NewOverviewSummaryWire(view.Today),
		Floor:    NewOverviewFloorWire(view.Floor, view.Look, view.FactoryLayout),
		Runs:     []OverviewRunWire{},
	}
	if view.MetroLayout != nil {
		metro := NewMetroLayoutWire(*view.MetroLayout)
		out.MetroLayout = &metro
	}
	for _, run := range view.OrderedRuns() {
		out.Runs = append(out.Runs, NewOverviewRunWire(run))
	}
	return out
}

// OverviewRunWire is a run as the Overview names it, wherever it appears.
type OverviewRunWire struct {
	ID         string           `json:"id"`
	Ref        string           `json:"ref"`
	Title      string           `json:"title"`
	StageName  string           `json:"stageName"`
	Repository string           `json:"repository"`
	BeltID     string           `json:"beltId"`
	BeltName   string           `json:"beltName"`
	Location   WorkLocationWire `json:"location"`
	NeedsYou   bool             `json:"needsYou"`
	// OpenStage is the stage a click opens the run at, when a person is needed.
	OpenStage *string `json:"openStage,omitzero"`
}

// NewOverviewRunWire builds the overview run wire from its arguments.
func NewOverviewRunWire(run core.OverviewRun) OverviewRunWire {
	return OverviewRunWire{
		ID:         run.ID,
		Ref:        run.Ref,
		Title:      run.Title,
		StageName:  run.StageName,
		Repository: run.Repository,
		BeltID:     run.BeltID,
		BeltName:   run.BeltName,
		Location:   NewWorkLocationWire(run.Location),
		NeedsYou:   run.NeedsYou,
		OpenStage:  run.OpenStage,
	}
}

// OverviewSummaryWire is the Summary's figures and rows, each already limited
// and ordered.
type OverviewSummaryWire struct {
	Spend      OverviewSummaryWireSpend    `json:"spend"`
	Sessions   OverviewSummaryWireSessions `json:"sessions"`
	Running    OverviewSummaryWireRunning  `json:"running"`
	Shipped    OverviewSummaryWireShipped  `json:"shipped"`
	InProgress OverviewSummaryWireRows     `json:"inProgress"`
	NeedsYou   OverviewSummaryWireRows     `json:"needsYou"`
}

// OverviewSummaryWireSpend is the spend figure of the Summary.
type OverviewSummaryWireSpend struct {
	Amount  string  `json:"amount"`
	Caption string  `json:"caption"`
	Note    *string `json:"note,omitzero"`
}

// OverviewSummaryWireSessions is the sessions heat map of the Summary.
type OverviewSummaryWireSessions struct {
	Today   int `json:"today"`
	Running int `json:"running"`
	// Start is the first day's midnight, the axis's start.
	Start jsonx.Date `json:"start"`
	// StartLabel is the first day's axis label, such as "SEP 27".
	StartLabel string `json:"startLabel"`
	// Shades is the shade level (0 to 4) of each cell, 56 in drawing order: 14
	// day columns of 4 six-hour rows.
	Shades         []int  `json:"shades"`
	RunningCaption string `json:"runningCaption"`
}

// OverviewSummaryWireRunning is the running post of the Summary.
type OverviewSummaryWireRunning struct {
	Count int `json:"count"`
	// RunIDs holds up to four run ids, in the order the post lists them.
	RunIDs    []string `json:"runIds"`
	Remaining int      `json:"remaining"`
	Empty     string   `json:"empty"`
	// Busy is "N AGENTS BUSY", the post's foot caption.
	Busy string `json:"busy"`
}

// OverviewSummaryWireShipped is the shipped figure and sparkline of the Summary.
type OverviewSummaryWireShipped struct {
	Weeks  []int  `json:"weeks"`
	Latest int    `json:"latest"`
	Median string `json:"median"`
	// Chart is the sparkline's drawing in its 300 × 150 space.
	Chart OverviewSummaryWireChart `json:"chart"`
}

// OverviewSummaryWireChart is the sparkline's drawing.
type OverviewSummaryWireChart struct {
	Line        string  `json:"line"`
	Area        string  `json:"area"`
	DotX        float64 `json:"dotX"`
	DotY        float64 `json:"dotY"`
	StartLabel  string  `json:"startLabel"`
	EndLabel    string  `json:"endLabel"`
	LatestLabel string  `json:"latestLabel"`
}

// OverviewSummaryWireRow is one row of a Summary list.
type OverviewSummaryWireRow struct {
	RunID     string  `json:"runId"`
	Glyph     string  `json:"glyph"`
	Look      string  `json:"look"`
	Title     string  `json:"title"`
	Chip      string  `json:"chip"`
	OpenStage *string `json:"openStage,omitzero"`
}

// OverviewSummaryWireRows is a Summary list of rows with its count and empty copy.
type OverviewSummaryWireRows struct {
	Count int                      `json:"count"`
	Rows  []OverviewSummaryWireRow `json:"rows"`
	Empty string                   `json:"empty"`
}

// NewOverviewSummaryWire builds the overview summary wire from its arguments.
// The empty copy and the busy caption come from the OverviewSummary constants.
func NewOverviewSummaryWire(summary core.OverviewSummary) OverviewSummaryWire {
	out := OverviewSummaryWire{
		Spend: OverviewSummaryWireSpend{
			Amount:  summary.Spend.Amount,
			Caption: summary.Spend.Caption,
			Note:    summary.Spend.Note,
		},
		Sessions: OverviewSummaryWireSessions{
			Today:          summary.Sessions.Today,
			Running:        summary.Sessions.Running,
			Start:          jsonx.Date{Time: summary.Sessions.Start},
			StartLabel:     summary.Sessions.StartLabel,
			Shades:         summary.Sessions.Shades,
			RunningCaption: summary.Sessions.RunningCaption,
		},
		Running: OverviewSummaryWireRunning{
			Count:     summary.Running.Count,
			RunIDs:    summary.Running.RunIDs,
			Remaining: summary.Running.Remaining,
			Empty:     core.OverviewSummaryEmptyRunning,
			Busy:      core.OverviewSummaryBusyCaption(summary.Running.Count),
		},
		Shipped: OverviewSummaryWireShipped{
			Weeks:  summary.Shipped.Weeks,
			Latest: summary.Shipped.Latest,
			Median: summary.Shipped.Median,
			Chart:  NewOverviewSummaryWireChart(summary.Shipped.Chart),
		},
		InProgress: OverviewSummaryWireRows{
			Count: summary.InProgress.Count,
			Rows:  []OverviewSummaryWireRow{},
			Empty: core.OverviewSummaryEmptyInProgress,
		},
		NeedsYou: OverviewSummaryWireRows{
			Count: summary.NeedsYou.Count,
			Rows:  []OverviewSummaryWireRow{},
			Empty: core.OverviewSummaryEmptyNeedsYou,
		},
	}
	for _, row := range summary.InProgress.Rows {
		out.InProgress.Rows = append(out.InProgress.Rows, NewOverviewSummaryWireRow(row))
	}
	for _, row := range summary.NeedsYou.Rows {
		out.NeedsYou.Rows = append(out.NeedsYou.Rows, NewOverviewSummaryWireRow(row))
	}
	return out
}

// NewOverviewSummaryWireChart builds the overview summary wire chart from its arguments.
func NewOverviewSummaryWireChart(chart core.OverviewSummaryChart) OverviewSummaryWireChart {
	return OverviewSummaryWireChart{
		Line:        chart.Line,
		Area:        chart.Area,
		DotX:        chart.DotX,
		DotY:        chart.DotY,
		StartLabel:  chart.StartLabel,
		EndLabel:    chart.EndLabel,
		LatestLabel: chart.LatestLabel,
	}
}

// NewOverviewSummaryWireRow builds the overview summary wire row from its arguments.
func NewOverviewSummaryWireRow(row core.OverviewSummaryRow) OverviewSummaryWireRow {
	return OverviewSummaryWireRow{
		RunID:     row.RunID,
		Glyph:     row.Glyph,
		Look:      string(row.Look),
		Title:     row.Title,
		Chip:      row.Chip,
		OpenStage: row.OpenStage,
	}
}
