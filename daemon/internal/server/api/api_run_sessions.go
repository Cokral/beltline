package api

import "github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"

// RunSessionsResponse is the Run page's Sessions read. The browser draws
// these facts and offers, and never infers permission from a session name.
type RunSessionsResponse struct {
	RunID        string               `json:"runId"`
	Rows         []SessionRowWire     `json:"rows"`
	Selection    SessionSelectionWire `json:"selection"`
	EmptyMessage *string              `json:"emptyMessage,omitzero"`
	Terminal     *SessionTerminalWire `json:"terminal,omitzero"`
	Log          *string              `json:"log,omitzero"`
	Usage        *SessionUsageWire    `json:"usage,omitzero"`
	TakeOver     *SessionOfferWire    `json:"takeOver,omitzero"`
	Ghostty      SessionOfferWire     `json:"ghostty"`
	// ContinueSession is the Continue offer; omitted when there is none.
	ContinueSession *SessionOfferWire `json:"continueSession,omitzero"`
}

// NewRunSessionsResponse builds the run sessions response from its arguments.
func NewRunSessionsResponse(view coordinator.SessionsView) RunSessionsResponse {
	out := RunSessionsResponse{
		RunID:        view.RunID,
		Rows:         make([]SessionRowWire, 0, len(view.Rows)),
		Selection:    NewSessionSelectionWire(view.Selection),
		EmptyMessage: view.EmptyMessage,
		Log:          view.Log,
		Ghostty:      NewSessionOfferWire(view.Ghostty),
	}
	for _, row := range view.Rows {
		out.Rows = append(out.Rows, NewSessionRowWire(row))
	}
	if view.Terminal != nil {
		terminal := NewSessionTerminalWire(*view.Terminal)
		out.Terminal = &terminal
	}
	if view.Usage != nil {
		usage := NewSessionUsageWire(*view.Usage)
		out.Usage = &usage
	}
	if view.TakeOver != nil {
		takeOver := NewSessionOfferWire(*view.TakeOver)
		out.TakeOver = &takeOver
	}
	if view.ContinueSession != nil {
		continueSession := NewSessionOfferWire(*view.ContinueSession)
		out.ContinueSession = &continueSession
	}
	return out
}

// SessionRowWire is one row of the Sessions sidebar.
type SessionRowWire struct {
	AttemptID string `json:"attemptId"`
	StageID   string `json:"stageId"`
	Title     string `json:"title"`
	Caption   string `json:"caption"`
	// Status is the row's status as its raw string.
	Status   string `json:"status"`
	Session  string `json:"session"`
	Selected bool   `json:"selected"`
}

// NewSessionRowWire builds the session row wire from its arguments.
func NewSessionRowWire(row coordinator.SessionSidebarProjectionRow) SessionRowWire {
	return SessionRowWire{
		AttemptID: row.AttemptID,
		StageID:   row.StageID,
		Title:     row.Title,
		Caption:   row.Caption,
		Status:    string(row.Status),
		Session:   row.Session,
		Selected:  row.Selected,
	}
}

// SessionSelectionWire is the attempt the sidebar selects.
type SessionSelectionWire struct {
	StageID   string  `json:"stageId"`
	AttemptID *string `json:"attemptId,omitzero"`
	Fallback  *string `json:"fallback,omitzero"`
}

// NewSessionSelectionWire builds the session selection wire from its arguments.
func NewSessionSelectionWire(selection coordinator.SessionSidebarProjectionSelection) SessionSelectionWire {
	return SessionSelectionWire{StageID: selection.StageID, AttemptID: selection.AttemptID, Fallback: selection.Fallback}
}

// SessionTerminalWire is the session the terminal can attach.
type SessionTerminalWire struct {
	Target    string `json:"target"`
	ReadOnly  bool   `json:"readOnly"`
	Continued bool   `json:"continued"`
}

// NewSessionTerminalWire builds the session terminal wire from its arguments.
func NewSessionTerminalWire(terminal coordinator.SessionsViewTerminal) SessionTerminalWire {
	return SessionTerminalWire{Target: terminal.Target, ReadOnly: terminal.ReadOnly, Continued: terminal.Continued}
}

// SessionUsageWire is the token and estimate detail of an attempt.
type SessionUsageWire struct {
	Tokens   *string `json:"tokens,omitzero"`
	Estimate *string `json:"estimate,omitzero"`
}

// NewSessionUsageWire builds the session usage wire from its arguments.
func NewSessionUsageWire(usage coordinator.SessionUsage) SessionUsageWire {
	return SessionUsageWire{Tokens: usage.Tokens, Estimate: usage.Estimate}
}

// SessionOfferWire is an offer the browser can enable or explain.
type SessionOfferWire struct {
	Enabled bool    `json:"enabled"`
	Reason  *string `json:"reason,omitzero"`
}

// NewSessionOfferWire builds the session offer wire from its arguments.
func NewSessionOfferWire(offer coordinator.SessionOffer) SessionOfferWire {
	return SessionOfferWire{Enabled: offer.Enabled, Reason: offer.Reason}
}
