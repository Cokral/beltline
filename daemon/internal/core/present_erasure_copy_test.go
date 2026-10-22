// Tests of the run erasure confirmation: it names the run and says what erasure does.

package core

import (
	"strings"
	"testing"
)

// TestConfirmationNamesTheRunAndSaysWhatErasureDoes
func TestConfirmationNamesTheRunAndSaysWhatErasureDoes(t *testing.T) {
	t.Parallel()
	contains := func(text, part string) {
		t.Helper()
		if !strings.Contains(text, part) {
			t.Fatalf("%q does not contain %q", text, part)
		}
	}
	issue := engineSampleRun()
	issue.BeltName = "Issues"
	issueText := ErasureCopyMessage(issue)
	contains(issueText, "#142 Export invoices as CSV on the Issues belt")
	contains(issueText, "Saved history, outputs, artifacts, AFK decisions, drafts and local checkouts will be permanently removed.")
	contains(issueText, "Eligible polled work may return on the next check.")
	contains(issueText, "External work is unchanged.")
	if strings.Contains(issueText, "does not replay") {
		t.Fatal("polled work is told the task is not replayed")
	}

	review := NewRun(RunArgs{Belt: testKindBelt(TriggerKindReviews), Subject: pullSubject(), Now: engineT0})
	review.BeltName = "Review requests"
	reviewText := ErasureCopyMessage(review)
	contains(reviewText, "#57 Move checkout on the Review requests belt")
	contains(reviewText, "Eligible polled work may return on the next check.")

	ticket := Subject{Kind: SubjectKindJira, Repo: "acme/billing", Key: "PAY-31", Title: "Refund part of a payment",
		Body: "Up to what was paid.", URL: "https://acme.atlassian.net/browse/PAY-31", IsOpen: true}
	jira := NewRun(RunArgs{Belt: testKindBelt(TriggerKindJira), Subject: ticket, Now: engineT0})
	jira.BeltName = "Jira tickets"
	contains(ErasureCopyMessage(jira), "PAY-31 Refund part of a payment on the Jira tickets belt")

	manual := NewRun(RunArgs{Belt: testKindBelt(TriggerKindManual),
		Subject: Subject{Kind: SubjectKindManual, Repo: "acme/billing", Key: "3", Title: "Export invoices as CSV", IsOpen: true}, Now: engineT0})
	manual.BeltName = "Manual"
	manualText := ErasureCopyMessage(manual)
	contains(manualText, "T3 Export invoices as CSV on the Manual belt")
	contains(manualText, "Erasure does not replay the task.")
	if strings.Contains(manualText, "may return") {
		t.Fatal("a manual task is told it may return")
	}

	scheduled := NewRun(RunArgs{Belt: testKindBelt(TriggerKindSchedule),
		Subject: Subject{Kind: SubjectKindScheduled, Repo: "acme/platform", Key: "2026-09-29", Title: "Sweep flaky tests", IsOpen: true}, Now: engineT0})
	scheduled.BeltName = "Schedule"
	scheduledText := ErasureCopyMessage(scheduled)
	contains(scheduledText, "09-29 Sweep flaky tests on the Schedule belt")
	contains(scheduledText, "Erasure does not replay the task.")
	contains(scheduledText, "External work is unchanged.")
}
