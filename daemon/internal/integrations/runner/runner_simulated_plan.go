// The RunnerPlan that gives the simulated runner its duration and pick functions. It lives here, not in
// kit/demo, so demo mode keeps no runner logic.

package runner

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// RunnerPlan is how long the demo's simulated sessions run and what their
// triage, review and verify agents pick. SimulatedRunner takes its duration and
// pick from these methods.
type RunnerPlan struct {
	verdicts   map[string]string
	mismatches *mismatchOnce
}

// NewRunnerPlan is the demo plan for a map of verdicts by session key (the
// session name's `bl-<key>-` prefix). The mismatch is offered once per issue.
func NewRunnerPlan(verdicts map[string]string) RunnerPlan {
	copied := make(map[string]string, len(verdicts))
	maps.Copy(copied, verdicts)
	return RunnerPlan{verdicts: copied, mismatches: &mismatchOnce{seen: map[string]struct{}{}}}
}

// Duration is how many seconds an agent's session runs in the demo, by agent id.
func (p RunnerPlan) Duration(agentID string) float64 {
	switch agentID {
	case "build":
		return 16
	case "spec":
		return 12
	case "verify":
		return 10
	default:
		return 8
	}
}

// Pick is the outcome a session picks, or nil to leave the choice to the agent.
// Triage and review pick their fixture's verdict, found by the session's name.
// The first verify of #142 finds a mismatch, so the demo shows a trip back to
// Build.
func (p RunnerPlan) Pick(sessionName, agentID string) *string {
	if agentID == "triage" || agentID == "review" {
		if key, ok := p.verdictKey(sessionName); ok {
			verdict := p.verdicts[key]
			return &verdict
		}
	}
	if agentID == "verify" && strings.Contains(sessionName, "-142-") && p.mismatches.first("142") {
		mismatch := "code mismatch"
		return &mismatch
	}
	return nil
}

// SimulatedRunner is the demo's simulated runner: its durations and picks come
// from this plan, by the spec's agent id and session name.
func (p RunnerPlan) SimulatedRunner(clock func() time.Time) *SimulatedRunner {
	agentOf := func(spec SessionSpec) string {
		if spec.AgentID != nil {
			return *spec.AgentID
		}
		return ""
	}
	return NewSimulatedRunner(
		func(spec SessionSpec) float64 { return p.Duration(agentOf(spec)) },
		clock,
		func(spec SessionSpec) *string { return p.Pick(spec.Name, agentOf(spec)) },
	)
}

// verdictKey is the first verdict key (sorted, so the choice never depends on
// map order) that the session name starts with, as "bl-<key>-".
func (p RunnerPlan) verdictKey(sessionName string) (string, bool) {
	keys := slices.Sorted(maps.Keys(p.verdicts))
	for _, key := range keys {
		if strings.HasPrefix(sessionName, "bl-"+key+"-") {
			return key, true
		}
	}
	return "", false
}

// mismatchOnce remembers which issues already had their one demo mismatch.
type mismatchOnce struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// first reports whether key is new, and records it.
func (m *mismatchOnce) first(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.seen[key]; ok {
		return false
	}
	m.seen[key] = struct{}{}
	return true
}
