// Coordinator tests of agent library edits, which need the agent resources.

package coordinator

import (
	"strconv"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestRunnerChangeKeepsModelAndThinking: switching a draft's runner keeps its model and thinking.
func TestRunnerChangeKeepsModelAndThinking(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	agent := c.LatestAgent("triage").Agent
	agent.Runner = core.BackendPi
	agent.Model = "some-model-the-new-runner-lacks"
	high := core.ThinkingHigh
	agent.Thinking = &high
	switched := c.SwitchAgentRunnerDraft(agent, core.BackendClaude, nil)
	if switched.Runner != core.BackendClaude {
		t.Errorf("runner = %q", switched.Runner)
	}
	if switched.Model != "some-model-the-new-runner-lacks" {
		t.Errorf("model = %q", switched.Model)
	}
	if switched.Thinking == nil || *switched.Thinking != core.ThinkingHigh {
		t.Errorf("thinking = %v", switched.Thinking)
	}
}

// TestStatusLineSaysWhatSaveWouldDo: the editor's status line says what saving would do for a new agent, the latest
// version, and unsaved changes, and the draft's presentation carries the same line.
func TestStatusLineSaysWhatSaveWouldDo(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	latest := c.LatestAgent("triage").Agent
	if got := core.AgentStatusLine(c.NewAgentDraft(), nil); got != "A new agent. Belts can use it once you save it." {
		t.Errorf("new agent status = %q", got)
	}
	if got := core.AgentStatusLine(latest, &latest); got != "This is the latest version, v"+strconv.Itoa(latest.Version)+"." {
		t.Errorf("latest status = %q", got)
	}
	edited := latest
	edited.Brief = "changed"
	want := "Unsaved changes. Saving makes v" + strconv.Itoa(latest.Version+1) + "; belts keep the version they use until you move them."
	if got := core.AgentStatusLine(edited, &latest); got != want {
		t.Errorf("edited status = %q, want %q", got, want)
	}
	if got := c.DraftAgentResource(edited).Presentation.Status; got != core.AgentStatusLine(edited, &latest) {
		t.Errorf("the draft's status = %q", got)
	}
}

// TestDraftPresentationWarnsAboutAVersionOfAnUnknownAgent: a draft that names an agent the library does not have
// is warned about by name.
func TestDraftPresentationWarnsAboutAVersionOfAnUnknownAgent(t *testing.T) {
	t.Parallel()
	h := newLibraryHarness(t, liveTestBelt("command", "Command belt", "acme/app"))
	c := h.Coord()
	agent := c.LatestAgent("triage").Agent
	agent.ID = "ghost-agent"
	for _, warning := range c.DraftAgentResource(agent).Presentation.Warnings {
		if strings.Contains(warning, "ghost-agent") {
			return
		}
	}
	t.Errorf("no warning names ghost-agent: %v", c.DraftAgentResource(agent).Presentation.Warnings)
}
