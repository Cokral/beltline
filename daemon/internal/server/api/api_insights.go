package api

import "github.com/robbertmanders/beltline/daemon/internal/core"

// InsightsResponse is `GET /api/v1/views/insights`: every belt at a glance when no belt is chosen,
// or one belt's stages with their models, for every version of it or one. Every figure and label is the
// daemon's; the page lays them out.
type InsightsResponse struct {
	Belt               string                       `json:"belt"`
	Version            int                          `json:"version"`
	Versions           []core.InsightsVersionChoice `json:"versions"`
	Period             core.InsightsPeriod          `json:"period"`
	Belts              []core.InsightsBelt          `json:"belts"`
	Periods            []core.InsightsPeriodChoice  `json:"periods"`
	Title              string                       `json:"title"`
	Caption            string                       `json:"caption"`
	LowSampleThreshold int                          `json:"lowSampleThreshold"`
	BeltRows           []core.InsightsBeltRow       `json:"beltRows"`
	Stages             []core.InsightsStage         `json:"stages"`
	// Shadow is the shadow test the query compares, omitted otherwise.
	Shadow *core.InsightsShadow `json:"shadow,omitzero"`
}

// NewInsightsResponse builds the insights response from the core view.
func NewInsightsResponse(view core.InsightsView) InsightsResponse {
	return InsightsResponse{
		Belt: view.Belt, Version: view.Version, Versions: view.Versions, Period: view.Period, Belts: view.Belts, Periods: view.Periods,
		Title: view.Title, Caption: view.Caption, LowSampleThreshold: view.LowSampleThreshold,
		BeltRows: view.BeltRows, Stages: view.Stages, Shadow: view.Shadow,
	}
}

// InsightsEvolutionResponse is `GET /api/v1/views/insights/evolution`: one belt's finished runs over
// its versions or weeks, with every figure, statistic, tick and label computed by the daemon; the page
// draws them.
type InsightsEvolutionResponse struct {
	Belt      string                      `json:"belt"`
	Belts     []core.InsightsBelt         `json:"belts"`
	Title     string                      `json:"title"`
	Caption   string                      `json:"caption"`
	Axis      core.InsightsEvolutionAxis  `json:"axis"`
	Separate  bool                        `json:"separate"`
	Columns   []core.InsightsColumn       `json:"columns"`
	From      int                         `json:"from"`
	Split     string                      `json:"split"`
	Runs      []core.InsightsEvolutionRun `json:"runs"`
	Figures   []core.InsightsFigure       `json:"figures"`
	Time      core.InsightsStack          `json:"time"`
	Touches   core.InsightsStack          `json:"touches"`
	SendsBack core.InsightsGrid           `json:"sendsBack"`
	Cost      core.InsightsStack          `json:"cost"`
	Endings   core.InsightsEndings        `json:"endings"`
}

// NewInsightsEvolutionResponse builds the evolution response from the core view.
func NewInsightsEvolutionResponse(view core.InsightsEvolutionView) InsightsEvolutionResponse {
	return InsightsEvolutionResponse{
		Belt: view.Belt, Belts: view.Belts, Title: view.Title, Caption: view.Caption, Axis: view.Axis, Separate: view.Separate,
		Columns: view.Columns, From: view.From, Split: view.Split, Runs: view.Runs, Figures: view.Figures,
		Time: view.Time, Touches: view.Touches, SendsBack: view.SendsBack, Cost: view.Cost, Endings: view.Endings,
	}
}
