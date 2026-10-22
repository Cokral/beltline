package api

import (
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// StateSettings is the settings subset the state resource carries.
type StateSettings struct {
	Login         string `json:"login"`
	Notifications bool   `json:"notifications"`
	AfkEnabled    bool   `json:"afkEnabled"`
}

// StateNotice is one notice in the state resource. The encoder writes
// `runId` as an explicit null when there is none (custom encode(to:)).
type StateNotice struct {
	ID    string     `json:"id"`
	Date  jsonx.Date `json:"date"`
	Text  string     `json:"text"`
	RunID *string    `json:"runId"`
}

// NewStateNotice builds the state notice from its arguments.
func NewStateNotice(id string, date jsonx.Date, text string, runID *string) StateNotice {
	return StateNotice{ID: id, Date: date, Text: text, RunID: core.CopyPointer(runID)}
}

// StateAction is one global control in the state resource. `reason` is
// written as an explicit null when absent; `help` is omitted.
type StateAction struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Enabled bool    `json:"enabled"`
	Reason  *string `json:"reason"`
	Help    *string `json:"help,omitzero"`
}

// NewStateAction builds the state action from its arguments.
func NewStateAction(action coordinator.GlobalAction) StateAction {
	return StateAction{
		ID:      action.ID,
		Label:   action.Label,
		Enabled: action.Enabled,
		Reason:  core.CopyPointer(action.Reason),
		Help:    core.CopyPointer(action.Help),
	}
}

// StateNavigation is the navigation's counts and the library summary: whether any library
// repository is configured, and whether the library changed since the backup repository.
// `changedSinceBackup` is an explicit null without a backup repository.
type StateNavigation struct {
	Belts      int          `json:"belts"`
	Agents     int          `json:"agents"`
	AfkToCheck int          `json:"afkToCheck"`
	Library    StateLibrary `json:"library"`
}

// StateLibrary is the navigation's view of the library repositories.
type StateLibrary struct {
	Offered            bool  `json:"offered"`
	ChangedSinceBackup *bool `json:"changedSinceBackup"`
}

// NewStateNavigation builds the state navigation from its arguments.
func NewStateNavigation(summary coordinator.NavigationSummary) StateNavigation {
	return StateNavigation{
		Belts:      summary.Belts,
		Agents:     summary.Agents,
		AfkToCheck: summary.AfkToCheck,
		Library: StateLibrary{
			Offered:            summary.Library.Offered,
			ChangedSinceBackup: core.CopyPointer(summary.Library.ChangedSinceBackup),
		},
	}
}

// StateResponse is the body of GET /api/v1/state. `lastCheck` is written as an
// explicit null when no check has run.
type StateResponse struct {
	GloballyPaused bool            `json:"globallyPaused"`
	Checking       bool            `json:"checking"`
	LastCheck      *jsonx.Date     `json:"lastCheck"`
	Counts         core.RunCounts  `json:"counts"`
	Notices        []StateNotice   `json:"notices"`
	Settings       StateSettings   `json:"settings"`
	Actions        []StateAction   `json:"actions"`
	Navigation     StateNavigation `json:"navigation"`
	ClockLabel     string          `json:"clockLabel"`
	ClockTooltip   string          `json:"clockTooltip"`
}

// NewStateResponse builds the state response from its arguments.
func NewStateResponse(snapshot coordinator.CoordinatorStateSnapshot) StateResponse {
	var lastCheck *jsonx.Date
	if snapshot.LastCheck != nil {
		lastCheck = &jsonx.Date{Time: *snapshot.LastCheck}
	}
	notices := make([]StateNotice, 0, len(snapshot.Notices))
	for _, notice := range snapshot.Notices {
		notices = append(notices, NewStateNotice(notice.ID, notice.Date, notice.Text, notice.RunID))
	}
	actions := make([]StateAction, 0, len(snapshot.Actions))
	for _, action := range snapshot.Actions {
		actions = append(actions, NewStateAction(action))
	}
	return StateResponse{
		GloballyPaused: snapshot.GloballyPaused,
		Checking:       snapshot.Checking,
		LastCheck:      lastCheck,
		Counts:         snapshot.Counts,
		Notices:        notices,
		Settings:       StateSettings{Login: snapshot.Login, Notifications: snapshot.Notifications, AfkEnabled: snapshot.AfkEnabled},
		Actions:        actions,
		Navigation:     NewStateNavigation(snapshot.Navigation),
		ClockLabel:     snapshot.ClockLabel,
		ClockTooltip:   snapshot.ClockTooltip,
	}
}
