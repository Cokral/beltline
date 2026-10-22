// The data the demo gives its fake GitHub, its fake Jira and its simulated runner. GitHub and Jira
// build the fakes from it; the simulated runner's plan is runner.RunnerPlan.

package demo

import (
	"slices"
	"strings"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/github"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/jira"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
)

// GitHub is the fake GitHub the demo's services describe: its issues, review
// requests and pull requests, and the state a branch's new pull request starts in.
func (s Services) GitHub() *github.FakeGitHub {
	reviews := make([]github.FakeReviewRequest, 0, len(s.Reviews))
	for _, r := range s.Reviews {
		reviews = append(reviews, github.FakeReviewRequest{Pull: r.Subject, Request: r.Occurrence})
	}
	pulls := make([]github.FakePullHead, 0, len(s.Pulls))
	for _, p := range s.Pulls {
		pulls = append(pulls, github.FakePullHead{Pull: p.Pull, Head: p.Head})
	}
	opens := s.OpensPulls
	return github.NewFakeGitHub(s.Issues, reviews, pulls, &opens)
}

// Jira is the fake Jira the demo's services describe: its tickets, statuses and
// the query filter that makes only epics match the epic belt's query.
func (s Services) Jira() *jira.FakeJira {
	return jira.NewFakeJira(s.Tickets, s.Statuses, s.TicketMatches)
}

// newSetup is an opened demo: its storage, its services, and the simulated
// runner and folder workspaces its coordinator is given. The runner's clock is
// the real one (the current time).
func newSetup(storage store.Storage, services Services) Setup {
	root := storage.Paths.Root
	return Setup{
		Storage:    storage,
		Services:   services,
		LibraryURL: func(repository string) string { return libraryRepoURL(root, repository) },
		Runner:     services.Runner.SimulatedRunner(time.Now),
		Workspaces: runner.FolderWorkspaces{},
	}
}

// ReviewRequest is a review request the fake GitHub holds: a pull request and
// the occurrence it was requested under.
type ReviewRequest struct {
	Subject    core.Subject
	Occurrence string
}

// PullRequestHead is a pull request the fake GitHub already has, with the branch
// it was opened from.
type PullRequestHead struct {
	Pull core.PullRequest
	Head string
}

// Services is everything the demo's fakes are given, read from the seeded
// folder (Open) or from the seed just written (Make). Nothing else in the demo
// talks to the fakes.
type Services struct {
	// Issues are the issues of every belt.
	Issues []core.Subject
	// Reviews are the review requests.
	Reviews []ReviewRequest
	// Pulls are the pull requests, each with its head branch.
	Pulls []PullRequestHead
	// OpensPulls is the state a pull request the fake opens starts in.
	OpensPulls core.PullRequestState
	// Tickets are the Jira tickets, the epic's first ticket included.
	Tickets []core.Subject
	// Statuses are the ticket statuses by key.
	Statuses map[string]string
	// TicketMatches says whether a ticket matches a query. Epics match the epic
	// belt's query, and only they do.
	TicketMatches func(jql string, ticket core.Subject) bool
	// Runner is what the simulated sessions do.
	Runner runner.RunnerPlan
}

// services is the data the fakes and the runner get for a demo whose pull
// requests are pulls.
func services(pulls []PullRequestHead) Services {
	var issues []core.Subject
	var reviews []ReviewRequest
	var tickets []core.Subject
	verdicts := map[string]string{}
	for _, f := range demoFixtures() {
		verdicts[f.sessionKey()] = f.Verdict
		switch f.Subject.Kind {
		case core.SubjectKindIssue:
			issues = append(issues, f.Subject)
		case core.SubjectKindPullRequest:
			occurrence := core.ValueOr(f.Subject.Occurrence, "0")
			reviews = append(reviews, ReviewRequest{Subject: f.Subject, Occurrence: occurrence})
		case core.SubjectKindJira:
			tickets = append(tickets, f.Subject)
		}
	}
	// The epic's first ticket is one more ticket, already done.
	tickets = append(tickets, demoDoneChild())
	return Services{
		Issues:     issues,
		Reviews:    reviews,
		Pulls:      pulls,
		OpensPulls: core.PullRequestStateOpen,
		Tickets:    tickets,
		Statuses:   map[string]string{"PAY-40": "In Progress", "PAY-41": "Done", "PAY-42": "In Progress"},
		// Epics match the epic belt's query, and only they do.
		TicketMatches: func(jql string, ticket core.Subject) bool {
			return strings.Contains(jql, "Epic") == slices.Contains(ticket.Labels, "epic")
		},
		Runner: runner.NewRunnerPlan(verdicts),
	}
}
