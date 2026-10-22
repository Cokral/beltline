// The fixtures: the issues, pull
// requests and tickets the demo's belts work on, and the epic and its first
// ticket. Every function returns fresh values, so a caller may change them.

package demo

import (
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// fixture is one subject a demo belt works on, with the outcome its triage or
// review agent picks.
type fixture struct {
	// Belt is the id of the belt the subject is queued on.
	Belt    string
	Subject core.Subject
	// Verdict is the outcome its triage or review agent picks.
	Verdict string
}

// sessionKey is the fixture's part of the demo's session names, which the
// verdicts are found by.
func (f fixture) sessionKey() string {
	return f.Belt + "-" + lastPathComponent(f.Subject.Repo) + "-" + f.Subject.SessionPart()
}

// lastPathComponent is the last non-empty piece of a repository name.
func lastPathComponent(repo string) string {
	parts := strings.Split(repo, "/")
	return parts[len(parts)-1]
}

// demoFixtures are the subjects the demo's belts are seeded with, in order.
func demoFixtures() []fixture {
	return []fixture{
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/platform", Number: 139, Title: "Rate limit the public API", Body: "Keys that make too many requests should get 429 with Retry-After. Health checks are never limited.", Labels: []string{"agent", "api"}, IsOpen: true}, Verdict: "ready to spec"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 142, Title: "Export invoices as CSV", Body: "Export downloads the rows that match the current filter. Amounts use two decimals and the invoice currency.", Labels: []string{"agent", "billing"}, IsOpen: true}, Verdict: "ready to spec"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 145, Title: "Show VAT on receipts", Body: "Receipts list VAT per rate below the subtotal.", Labels: []string{"agent", "billing"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/platform", Number: 147, Title: "Retry failed webhook deliveries", Body: "A failed delivery is retried 5 times with exponential backoff.", Labels: []string{"agent", "platform"}, IsOpen: true}, Verdict: "ready to spec"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 151, Title: "Crash when saving an empty draft", Body: "The app crashes.", Labels: []string{"agent", "bug"}, IsOpen: true}, Verdict: "needs info"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 152, Title: "Paginate the customer list", Body: "The list should show 50 customers per page, and search results too.", Labels: []string{"agent", "customers"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 153, Title: "Add a dark mode toggle", Body: "A toggle in settings.", Labels: []string{"agent", "design"}, IsOpen: true}, Verdict: "wait to implement"},
		{Belt: "issues", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 155, Title: "Duplicate a quote", Body: "Duplicate creates a draft copy with today's date and a new number.", Labels: []string{"agent", "quotes"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "security", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/platform", Number: 88, Title: "Token leak in webhook logs", Body: "Webhook logs contain the signing secret.", Labels: []string{"security"}, IsOpen: true}, Verdict: "ready to spec"},
		{Belt: "security", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/platform", Number: 91, Title: "Open redirect after sign-in", Body: "Any return URL is followed after sign-in.", Labels: []string{"security"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "security", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 86, Title: "Weak session cookie flags", Body: "Session cookies lack Secure and SameSite.", Labels: []string{"security"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "security", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 93, Title: "CSRF token missing on the billing form", Body: "The billing form skips the CSRF middleware.", Labels: []string{"security"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "docs", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/docs", Number: 412, Title: "Fix broken links in the API guide", Body: "Several links 404.", Labels: []string{"docs"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "docs", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/docs", Number: 415, Title: "Document webhook retries", Body: "Explain the retry schedule and headers.", Labels: []string{"docs"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "deps", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/web", Number: 301, Title: "Bump next to 15.5", Body: "Minor version.", Labels: []string{"dependencies"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "deps", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/platform", Number: 303, Title: "Bump typescript to 5.9", Body: "Minor version.", Labels: []string{"dependencies"}, IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "reviews", Subject: core.Subject{Kind: core.SubjectKindPullRequest, Repo: "acme/web", Number: 160, Title: "Move checkout to the new payments API", Body: "Replaces the legacy charge call with PaymentIntents.", URL: "https://github.com/acme/web/pull/160", IsOpen: true, Base: new("main"), Occurrence: new("9001"), Reviewer: new("you")}, Verdict: "no blocking findings"},
		{Belt: "reviews", Subject: core.Subject{Kind: core.SubjectKindPullRequest, Repo: "acme/platform", Number: 162, Title: "Trace requests across services", Body: "Adds a trace id header and logs it.", URL: "https://github.com/acme/platform/pull/162", IsOpen: true, Base: new("main"), Occurrence: new("9002"), Reviewer: new("you")}, Verdict: "blocking findings"},
		{Belt: "jira", Subject: core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-31", Title: "Refund part of a payment", Body: "Support can refund any amount up to what was paid. The receipt shows the refund.", Labels: []string{"agent"}, URL: "https://acme.atlassian.net/browse/PAY-31", IsOpen: true}, Verdict: "ready to spec"},
		{Belt: "jira", Subject: core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-34", Title: "Show failed charges on the dashboard", Body: "A card on the dashboard lists charges that failed in the last 7 days.", Labels: []string{"agent"}, URL: "https://acme.atlassian.net/browse/PAY-34", IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "epics", Subject: demoEpic(), Verdict: ""},
		{Belt: "jira", Subject: core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-42", Title: "Move invoice numbering into the invoicing service", Body: "Part of PAY-40. The service hands out invoice numbers; billing asks it for one.", Labels: []string{"agent", "base:beltline/PAY-40"}, URL: "https://acme.atlassian.net/browse/PAY-42", IsOpen: true, Parent: new("PAY-40")}, Verdict: "ready to implement"},
		{Belt: "jira", Subject: core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-43", Title: "Send invoice emails from the invoicing service", Body: "Part of PAY-40.", Labels: []string{"agent", "base:beltline/PAY-40"}, URL: "https://acme.atlassian.net/browse/PAY-43", IsOpen: true, Parent: new("PAY-40"), Blockers: []string{"PAY-42"}}, Verdict: "ready to implement"},
		{Belt: "tasks", Subject: core.Subject{Kind: core.SubjectKindManual, Repo: "acme/web", Key: "1", Title: "Tidy the settings page copy", Body: "Use sentence case and shorter labels throughout Settings.", IsOpen: true}, Verdict: "ready to implement"},
		{Belt: "nested", Subject: core.Subject{Kind: core.SubjectKindIssue, Repo: "acme/billing", Number: 180, Title: "Allow a fork inside a route", Body: "A route can split again, and the routes meet at the right level.", Labels: []string{"agent"}, IsOpen: true}, Verdict: "ready to spec"},
	}
}

// demoEpic is the epic the epic belt is working through, a wave in.
func demoEpic() core.Subject {
	return core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-40", Title: "Split invoicing into its own service",
		Body:   "Invoicing runs inside billing today. Move it into a service of its own that billing calls.",
		Labels: []string{"epic"}, URL: "https://acme.atlassian.net/browse/PAY-40", IsOpen: true}
}

// demoDoneChild is the epic's first ticket, already done.
func demoDoneChild() core.Subject {
	return core.Subject{Kind: core.SubjectKindJira, Repo: "acme/billing", Key: "PAY-41", Title: "Create the invoicing service skeleton",
		Labels: []string{"agent", "base:beltline/PAY-40"}, IsOpen: false, Parent: new("PAY-40")}
}
