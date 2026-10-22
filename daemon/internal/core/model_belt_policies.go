// Policies: the run policies a belt
// has and a run keeps.

package core

import (
	"encoding/json"
)

// Policies are a belt's run policies. A run keeps the policies it started with.
//
// Decoding defaults every policy to true when its key is missing.
type Policies struct {
	// RetriageOnEdit: edited before any stage changes code, start again at the first stage.
	RetriageOnEdit bool `json:"retriageOnEdit"`
	// StopOnClose: closed, done, or no longer asked for, stop the run, unless it
	// has reached a watch stage.
	StopOnClose bool `json:"stopOnClose"`
	// CommentOnHold: comment on the issue, pull request or ticket when a stage
	// holds the run.
	CommentOnHold bool `json:"commentOnHold"`
	// WaitForBlockers: leave an issue or ticket alone while an issue or ticket
	// that blocks it is still open.
	WaitForBlockers bool `json:"waitForBlockers"`
}

// DefaultPolicies returns the policies with every policy on.
func DefaultPolicies() Policies {
	return Policies{RetriageOnEdit: true, StopOnClose: true, CommentOnHold: true, WaitForBlockers: true}
}

// UnmarshalJSON reads the policies over the defaults, so a missing one stays on.
func (p *Policies) UnmarshalJSON(data []byte) error {
	type plain Policies
	*p = DefaultPolicies()
	return json.Unmarshal(data, (*plain)(p))
}
