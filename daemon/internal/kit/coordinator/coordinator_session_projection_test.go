// Tests of the Sessions sidebar's shared projection over hand-built sample runs. Pure: no coordinator.

package coordinator

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// sessionProjectionSession names an attempt's session in makeRun.
type sessionProjectionSession struct {
	stage   string
	session string
	open    bool
}

// sessionProjectionRun is a sample run with one attempt per agent stage named in sessions, in the
// order given. Each attempt is finished unless open is set.
func sessionProjectionRun(sessions []sessionProjectionSession) core.Run {
	run := sessionSampleRun()
	run.Attempts = nil
	for index, entry := range sessions {
		attempt := sessionSampleAttempt(entry.stage, index+1, fmt.Sprintf("s%d", index))
		attempt.Session = new(entry.session)
		if !entry.open {
			ended := sessionSampleT0.Add(90 * time.Second)
			endedAt := jsonx.Date{Time: ended}
			attempt.EndedAt = &endedAt
		}
		run.Attempts = append(run.Attempts, attempt)
	}
	return run
}

// sessionProjectionAgents lists the agent stage ids of run, failing the test when there are fewer than three.
func sessionProjectionAgents(t *testing.T, run core.Run) []string {
	t.Helper()
	var agents []string
	for _, stage := range run.Stages {
		if stage.Kind() == core.StageKindAgent {
			agents = append(agents, stage.ID)
		}
	}
	if len(agents) < 3 {
		t.Fatalf("the default belt needs three agent stages, has %d", len(agents))
	}
	return agents
}

func TestRowsFollowStageOrderNotAttemptOrder(t *testing.T) {
	t.Parallel()
	agents := sessionProjectionAgents(t, sessionSampleRun())
	run := sessionProjectionRun([]sessionProjectionSession{
		{stage: agents[2], session: "bl-third"}, {stage: agents[0], session: "bl-first"}, {stage: agents[1], session: "bl-second", open: true},
	})
	rows := SessionSidebarProjectionRows(run, nil, sessionSampleT0)
	if got, want := sessionRowSessions(rows), []string{"bl-first", "bl-second", "bl-third"}; !slices.Equal(got, want) {
		t.Fatalf("sessions = %v, want %v", got, want)
	}
	if got, want := sessionRowStages(rows), []string{agents[0], agents[1], agents[2]}; !slices.Equal(got, want) {
		t.Fatalf("stages = %v, want %v", got, want)
	}
	for _, row := range rows {
		if row.Selected {
			t.Fatalf("no row is selected without an attempt id, got %+v", row)
		}
	}
}

func TestRowsCarryTheDaemonLabelsStatusAndCaption(t *testing.T) {
	t.Parallel()
	base := sessionSampleRun()
	agents := sessionProjectionAgents(t, base)
	run := sessionProjectionRun([]sessionProjectionSession{{stage: agents[0], session: "bl-done"}, {stage: agents[1], session: "bl-live", open: true}})
	selected := run.Attempts[1].ID
	rows := SessionSidebarProjectionRows(run, &selected, sessionSampleT0)
	var stageName string
	for _, stage := range run.Stages {
		if stage.ID == agents[0] {
			stageName = stage.Name
		}
	}
	wantTitle := fmt.Sprintf("%02d ", run.Layout().Number(agents[0])) + stageName + " · attempt 1"
	if rows[0].Title != wantTitle {
		t.Fatalf("title = %q, want %q", rows[0].Title, wantTitle)
	}
	if rows[0].Status != SessionSidebarProjectionDone {
		t.Fatalf("status = %q, want done", rows[0].Status)
	}
	if want := core.CostSession(run.Attempts[0], true, sessionSampleT0); rows[0].Caption != want {
		t.Fatalf("caption = %q, want %q", rows[0].Caption, want)
	}
	if rows[1].Status != SessionSidebarProjectionLive {
		t.Fatalf("status = %q, want live", rows[1].Status)
	}
	if rows[1].Caption != "live" {
		t.Fatalf("caption = %q, want live", rows[1].Caption)
	}
	if rows[0].Selected || !rows[1].Selected {
		t.Fatalf("selected = %v, %v, want false, true", rows[0].Selected, rows[1].Selected)
	}
}

func TestSelectedAttemptIsTheStagesOwnSessionWhenItHasOne(t *testing.T) {
	t.Parallel()
	agents := sessionProjectionAgents(t, sessionSampleRun())
	run := sessionProjectionRun([]sessionProjectionSession{{stage: agents[0], session: "bl-first"}, {stage: agents[1], session: "bl-second"}})
	selection := SessionSidebarProjectionSelectionOf(run, &agents[0], nil)
	if selection.StageID != agents[0] || selection.AttemptID == nil || *selection.AttemptID != run.Attempts[0].ID || selection.Fallback != nil {
		t.Fatalf("selection = %+v, want the stage's own attempt", selection)
	}
	picked := SessionSidebarProjectionSelectionOf(run, &agents[1], &run.Attempts[1].ID)
	if picked.AttemptID == nil || *picked.AttemptID != run.Attempts[1].ID || picked.Fallback != nil {
		t.Fatalf("picked = %+v, want attempt two without a fallback", picked)
	}
}

func TestAStageWithoutASessionShowsTheLatestSessionAndSaysSo(t *testing.T) {
	t.Parallel()
	base := sessionSampleRun()
	agents := sessionProjectionAgents(t, base)
	run := sessionProjectionRun([]sessionProjectionSession{{stage: agents[0], session: "bl-first"}, {stage: agents[1], session: "bl-second"}})
	silent := agents[2]
	var name string
	for _, stage := range run.Stages {
		if stage.ID == silent {
			name = stage.Name
		}
	}
	selection := SessionSidebarProjectionSelectionOf(run, &silent, nil)
	if selection.StageID != silent {
		t.Fatalf("stage = %q, want %q", selection.StageID, silent)
	}
	if selection.AttemptID == nil || *selection.AttemptID != run.Attempts[1].ID {
		t.Fatalf("attempt = %v, want the latest", selection.AttemptID)
	}
	want := name + " has no session yet. Showing the latest session."
	if selection.Fallback == nil || *selection.Fallback != want {
		t.Fatalf("fallback = %v, want %q", selection.Fallback, want)
	}
}

func TestANonAgentStageNamesItselfAsRunningNoAgent(t *testing.T) {
	t.Parallel()
	agents := sessionProjectionAgents(t, sessionSampleRun())
	run := sessionProjectionRun([]sessionProjectionSession{{stage: agents[0], session: "bl-first"}})
	var plain core.Stage
	for _, stage := range run.Stages {
		if stage.Kind() != core.StageKindAgent {
			plain = stage
			break
		}
	}
	if plain.ID == "" {
		t.Fatal("the default belt has a stage that runs no agent")
	}
	run.StageID = plain.ID
	selection := SessionSidebarProjectionSelectionOf(run, &plain.ID, nil)
	if selection.AttemptID == nil || *selection.AttemptID != run.Attempts[0].ID {
		t.Fatalf("attempt = %v, want the latest", selection.AttemptID)
	}
	want := plain.Name + " runs no agent. Showing the latest session."
	if selection.Fallback == nil || *selection.Fallback != want {
		t.Fatalf("fallback = %v, want %q", selection.Fallback, want)
	}
}

func TestEmptyRunsHaveNoRowsNoSelectionAndTheEmptyCopy(t *testing.T) {
	t.Parallel()
	run := sessionProjectionRun(nil)
	if rows := SessionSidebarProjectionRows(run, nil, sessionSampleT0); len(rows) != 0 {
		t.Fatalf("rows = %v, want none", rows)
	}
	selection := SessionSidebarProjectionSelectionOf(run, nil, nil)
	if selection.AttemptID != nil || selection.Fallback != nil {
		t.Fatalf("selection = %+v, want none", selection)
	}
	if !strings.HasPrefix(core.SessionEmptyCopy(run), "No sessions yet.") {
		t.Fatalf("empty copy = %q", core.SessionEmptyCopy(run))
	}
}

func TestUsageAppearsOnlyForFinishedAgentAttemptsThatReportedIt(t *testing.T) {
	t.Parallel()
	agents := sessionProjectionAgents(t, sessionSampleRun())
	run := sessionProjectionRun([]sessionProjectionSession{{stage: agents[0], session: "bl-done"}, {stage: agents[1], session: "bl-live", open: true}})
	if usage := SessionSidebarProjectionUsage(run.Attempts[1], run); usage != nil {
		t.Fatalf("an open attempt has usage %+v", usage)
	}
	if usage := SessionSidebarProjectionUsage(run.Attempts[0], run); usage != nil {
		t.Fatalf("an attempt without usage has usage %+v", usage)
	}
	run.Attempts[0].Usage = sessionProjectionUsage()
	usage := SessionSidebarProjectionUsage(run.Attempts[0], run)
	if usage == nil || usage.Tokens == nil {
		t.Fatalf("usage = %+v, want tokens", usage)
	}
	if !strings.HasPrefix(*usage.Tokens, "1,200 in") {
		t.Fatalf("tokens = %q, want the input count first", *usage.Tokens)
	}
	if usage.Estimate != nil {
		t.Fatalf("estimate = %q, want none for a reported cost", *usage.Estimate)
	}
}

// sessionProjectionUsage is a usage with costNanos 2_500_000, input 1200, output 300, totalTokens 1500,
// provenance: .reported, coverage: .complete).
func sessionProjectionUsage() *core.Usage {
	reported := core.CostProvenanceReported
	complete := core.UsageCoverageComplete
	usage := core.NewUsage(core.UsageArgs{
		CostNanos: new(2_500_000), Input: new(1200), Output: new(300), TotalTokens: new(1500),
		Provenance: &reported, Coverage: &complete,
	})
	return &usage
}

// sessionRowSessions, sessionRowStages list the rows' sessions and stages, in order.
func sessionRowSessions(rows []SessionSidebarProjectionRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Session)
	}
	return out
}

func sessionRowStages(rows []SessionSidebarProjectionRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.StageID)
	}
	return out
}
