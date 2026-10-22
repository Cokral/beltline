package api

import (
	"encoding/json"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// RunSnapshot is one run as the API returns it: every field of the saved run
// plus the commands the daemon would accept now. The wire shape keeps the
// run's own fields at the top level, so an existing client reads the same
// object with one new `actions` array.
type RunSnapshot struct {
	Run     core.Run
	Actions []core.RunAction
}

// NewRunSnapshot builds the value from its arguments; a nil actions list
// becomes the empty list the default gives.
func NewRunSnapshot(run core.Run, actions []core.RunAction) RunSnapshot {
	if actions == nil {
		actions = []core.RunAction{}
	}
	return RunSnapshot{Run: run, Actions: actions}
}

// MarshalJSON writes the run's fields at the top level, then `actions`.
func (s RunSnapshot) MarshalJSON() ([]byte, error) {
	actions := s.Actions
	if actions == nil {
		actions = []core.RunAction{}
	}
	return jsonx.Marshal(struct {
		core.Run
		Actions []core.RunAction `json:"actions"`
	}{s.Run, actions})
}

// UnmarshalJSON reads the run from the same object, and `actions` defaults to
// the empty list when absent or null.
func (s *RunSnapshot) UnmarshalJSON(data []byte) error {
	if err := s.Run.UnmarshalJSON(data); err != nil {
		return err
	}
	var w struct {
		Actions []core.RunAction `json:"actions"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	s.Actions = w.Actions
	if s.Actions == nil {
		s.Actions = []core.RunAction{}
	}
	return nil
}

// RunsResponse is the body of GET /api/v1/runs.
type RunsResponse struct {
	Runs []RunSnapshot `json:"runs"`
}

// RunResponse is the body of a run detail or run command response.
type RunResponse struct {
	Run RunSnapshot `json:"run"`
}
