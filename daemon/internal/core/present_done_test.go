// Tests of how a Done run is shown next to a Shipped one: its location, its attempt panel, its caption
// and its status group.

package core

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// doneRunWithPull is a Done run of the default belt that opened pull request 12.
func doneRunWithPull(t *testing.T, state RunState) Run {
	t.Helper()
	run := insightsTestRun(t, 4, insightsNow.Add(-time.Hour))
	run.State = state
	run.PullRequests = []PullRequest{{Repo: "a/b", Number: 12}}
	return run
}

// TestDoneAndShippedAreShownApart checks the status line, the caption and the group of each.
func TestDoneAndShippedAreShownApart(t *testing.T) {
	t.Parallel()
	done := doneRunWithPull(t, RunStateDone)
	shipped := doneRunWithPull(t, RunStateShipped)
	if got := StatusLocation(done, false, Pause{}).Text; got != "Done · #12" {
		t.Errorf("done location = %q", got)
	}
	if got := StatusLocation(shipped, false, Pause{}).Text; got != "Shipped · #12" {
		t.Errorf("shipped location = %q", got)
	}
	if got := RunPresentationCaption(done, nil); got != "Done as b#12" {
		t.Errorf("done caption = %q", got)
	}
	if got := RunPresentationCaption(shipped, nil); got != "Shipped as b#12" {
		t.Errorf("shipped caption = %q", got)
	}
	if got := GroupBy(GroupByStatus).Group(done).Value; got != "Done" {
		t.Errorf("done group = %q", got)
	}
	if got := GroupBy(GroupByStatus).Group(shipped).Value; got != "Shipped" {
		t.Errorf("shipped group = %q", got)
	}
	if !slices.Contains(groupByStatusOrder, "Done") {
		t.Errorf("status order %v lacks Done", groupByStatusOrder)
	}
}

// TestDoneAndShippedAreNamedWithoutAPullRequest checks that a run with no pull request still says
// how it ended, in its status line and its caption, rather than "Finished" for both.
func TestDoneAndShippedAreNamedWithoutAPullRequest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		state RunState
		word  string
	}{{RunStateDone, "Done"}, {RunStateShipped, "Shipped"}} {
		run := doneRunWithPull(t, c.state)
		run.PullRequests = nil
		if got := StatusLocation(run, false, Pause{}).Text; got != c.word {
			t.Errorf("%s location = %q, want %q", c.state, got, c.word)
		}
		if got := RunPresentationCaption(run, nil); got != c.word {
			t.Errorf("%s caption = %q, want %q", c.state, got, c.word)
		}
	}
}

// TestDoneAndShippedOfferTheSameCommands checks that a Done run and a Shipped run enable and refuse
// exactly the same commands. Their refusal reasons name the ending, so only the enabled state and
// whether a reason is given are compared.
func TestDoneAndShippedOfferTheSameCommands(t *testing.T) {
	t.Parallel()
	done := RunActionContextDefault(presState("done"))
	shipped := RunActionContextDefault(presState("shipped"))
	for _, command := range AllRunCommand {
		d, s := presAction(t, command, done), presAction(t, command, shipped)
		if d.Enabled != s.Enabled || (d.Reason == nil) != (s.Reason == nil) {
			t.Errorf("%s: done enabled %t reason %v, shipped enabled %t reason %v", command, d.Enabled, d.Reason, s.Enabled, s.Reason)
		}
	}
}

// TestDoneCannotContinueAndHasTheSameTerminalCommands checks the attempt panel refuses Continue for a
// Done run as it does for a Shipped one, and that neither run has a current attempt.
func TestDoneCannotContinueAndHasTheSameTerminalCommands(t *testing.T) {
	t.Parallel()
	done := doneRunWithPull(t, RunStateDone)
	shipped := doneRunWithPull(t, RunStateShipped)
	attempt := NewAttempt(AttemptArgs{StageID: done.StageID, Number: 1, StartedAt: jsonx.Date{Time: insightsNow}, Folder: "f"})
	if got := AttemptPanelCanContinue(done, attempt, true); got == nil || *got != "the run is done" {
		t.Errorf("done continue = %v", got)
	}
	if got := AttemptPanelCanContinue(shipped, attempt, true); got == nil || *got != "the run is shipped" {
		t.Errorf("shipped continue = %v", got)
	}
	if AttemptPanelCurrentID(done) != nil || AttemptPanelCurrentID(shipped) != nil {
		t.Errorf("a finished run has no current attempt")
	}
	if !reflect.DeepEqual(runAvailabilityFinished(done), "This run is done.") {
		t.Errorf("done availability = %q", runAvailabilityFinished(done))
	}
}

// TestOverviewShippedCountsOnlyShippedRuns checks that the shipped-today count, the weekly chart and
// the median use Shipped runs alone, so a Done review is left out of all three.
func TestOverviewShippedCountsOnlyShippedRuns(t *testing.T) {
	t.Parallel()
	now := ovNow
	shipped := ovRunAt("shipped", "issues", RunStateShipped, now.Add(-2*time.Hour))
	review := ovRunAt("review", "reviews", RunStateDone, now.Add(-time.Hour))
	summary := overviewSummaryShipped([]Run{shipped, review}, now)
	total := 0
	for _, count := range summary.Weeks {
		total += count
	}
	if total != 1 {
		t.Errorf("weekly shipped total = %d, want the one shipped run", total)
	}
	only := overviewSummaryShipped([]Run{review}, now)
	if only.Latest != 0 || only.Median != "—" {
		t.Errorf("done only: latest %d median %q, want no shipped runs", only.Latest, only.Median)
	}
}

// TestALegacyShippedRunStaysShippedAndCounts checks that a run saved as shipped, for a pull request
// review, loads as Shipped and keeps counting in the historical shipping figures.
func TestALegacyShippedRunStaysShippedAndCounts(t *testing.T) {
	t.Parallel()
	run := doneRunWithPull(t, RunStateShipped)
	run.Subject = Subject{Kind: SubjectKindPullRequest, Repo: "a/b", Number: 12, Title: "Review", IsOpen: true}
	run.UpdatedAt = jsonx.Date{Time: insightsNow.Add(-time.Hour)}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "shipping")
	legacy, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var back Run
	if err := json.Unmarshal(legacy, &back); err != nil {
		t.Fatal(err)
	}
	if back.State != RunStateShipped || back.Shipping {
		t.Fatalf("legacy shipped run read as %q shipping %t", back.State, back.Shipping)
	}
	if back.Subject.Kind != SubjectKindPullRequest {
		t.Fatalf("legacy review read as %q", back.Subject.Kind)
	}
	summary := overviewSummaryShipped([]Run{back}, insightsNow)
	total := 0
	for _, week := range summary.Weeks {
		total += week
	}
	if summary.Median == "—" || total != 1 {
		t.Errorf("a legacy shipped review is missing from the shipped figures: median %q, weekly total %d", summary.Median, total)
	}
}
