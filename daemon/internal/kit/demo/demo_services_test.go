package demo

import (
	"context"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// TestRunnerPlanPicksVerdictsAndOneMismatch: triage picks its fixture's verdict
// by session name, verify finds #142's mismatch once, and sessions take their
// durations by agent.
func TestRunnerPlanPicksVerdictsAndOneMismatch(t *testing.T) {
	t.Parallel()
	plan := services(nil).Runner
	verdict := plan.Pick("bl-issues-web-152-triage", "triage")
	if verdict == nil || *verdict != "ready to implement" {
		t.Errorf("triage verdict = %v, want ready to implement", verdict)
	}
	if got := plan.Pick("bl-issues-web-152-build", "build"); got != nil {
		t.Errorf("build picked %q, want nothing", *got)
	}
	if got := plan.Pick("bl-issues-billing-142-verify", "verify"); got == nil || *got != "code mismatch" {
		t.Errorf("first verify of #142 = %v, want code mismatch", got)
	}
	if got := plan.Pick("bl-issues-billing-142-verify", "verify"); got != nil {
		t.Errorf("second verify of #142 picked %q, want nothing", *got)
	}
	if plan.Duration("build") != 16 || plan.Duration("spec") != 12 || plan.Duration("verify") != 10 || plan.Duration("ship") != 8 {
		t.Errorf("durations differ from the demo's")
	}
}

// TestTicketMatchesOnlyEpicsForTheEpicQuery: the fake Jira answers a query with
// "Epic" only with the epic, other queries only with the other tickets, and the
// epic's first ticket is the last ticket, already done.
func TestTicketMatchesOnlyEpicsForTheEpicQuery(t *testing.T) {
	t.Parallel()
	svc := services(nil)
	epic := demoEpic()
	var payment core.Subject
	for _, f := range demoFixtures() {
		if f.Subject.Key == "PAY-42" {
			payment = f.Subject
		}
	}
	if !svc.TicketMatches("project = PAY AND issuetype = Epic", epic) {
		t.Errorf("the epic query does not match the epic")
	}
	if svc.TicketMatches("project = PAY AND issuetype = Epic", payment) {
		t.Errorf("the epic query matches %s", payment.Key)
	}
	if svc.TicketMatches("project = PAY AND labels = agent", epic) {
		t.Errorf("a ticket query matches the epic")
	}
	if last := svc.Tickets[len(svc.Tickets)-1]; last.Key != "PAY-41" || last.IsOpen {
		t.Errorf("last ticket = %s open=%v, want the done PAY-41", last.Key, last.IsOpen)
	}
	if svc.OpensPulls != core.PullRequestStateOpen {
		t.Errorf("opened pull requests start %s", svc.OpensPulls)
	}
}

// TestServicesFeedTheFakes: the demo's fake GitHub lists its open issues and
// review requests, and its fake Jira matches only epics for the epic query.
func TestServicesFeedTheFakes(t *testing.T) {
	t.Parallel()
	svc := services(nil)
	ctx := context.Background()
	gh := svc.GitHub()
	if len(svc.Issues) == 0 {
		t.Fatal("the demo has no issues")
	}
	issue := svc.Issues[0]
	for _, label := range issue.Labels {
		listed, err := gh.Issues(ctx, issue.Repo, label, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, l := range listed {
			found = found || l.Number == issue.Number
		}
		if issue.IsOpen && !found {
			t.Errorf("issue %s#%d with label %s is not listed", issue.Repo, issue.Number, label)
		}
	}
	jiraFake := svc.Jira()
	epics, err := jiraFake.Search(ctx, "project = PAY AND issuetype = Epic")
	if err != nil {
		t.Fatal(err)
	}
	if len(epics) != 1 || epics[0].Key != demoEpic().Key {
		t.Errorf("epic query returned %d tickets", len(epics))
	}
}
