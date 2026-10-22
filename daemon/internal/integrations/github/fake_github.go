// An in-memory GitHub for tests and demo mode. It records outward calls and review-request
// lookups. Its methods are grouped by area across fake_github_*.go.

package github

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// FakeCallKind names one outward call a FakeGitHub recorded.
type FakeCallKind string

const (
	FakeCallLatestReviewRequest  FakeCallKind = "latestReviewRequest"
	FakeCallCommitsBehind        FakeCallKind = "commitsBehind"
	FakeCallComment              FakeCallKind = "comment"
	FakeCallCommentOnPullRequest FakeCallKind = "commentOnPullRequest"
)

// FakeCall is one recorded call. Only the fields its kind uses are set.
type FakeCall struct {
	Kind     FakeCallKind
	Repo     string
	Number   int
	Body     string
	Reviewer string
	Base     string
	Head     string
}

// FakeReviewRequest is a pull request that asks for a review, with the request id.
type FakeReviewRequest struct {
	Pull    core.Subject
	Request string
}

// FakePullHead is a pull request the fake already has, with the branch it was opened from.
type FakePullHead struct {
	Pull core.PullRequest
	Head string
}

// reviewEntry is a pull request's latest review requests, keyed by lower-cased reviewer.
type reviewEntry struct {
	pull     core.Subject
	requests map[string]string
}

// FakeGitHub is safe for concurrent use. Hooks run without the lock held.
type FakeGitHub struct {
	mu sync.Mutex

	issues        map[string]core.Subject
	reviews       map[string]*reviewEntry
	currentLogin  string
	loginCount    int
	failingReview map[string]bool
	pulls         map[string]core.PullRequest
	heads         map[string]string
	pullsBehind   map[string]int
	calls         []FakeCall
	issueComments map[string][]core.TicketStateComment
	// BeforeLatestReview is awaited at the start of LatestReviewRequest, so a test can
	// overlap a check with erasure.
	BeforeLatestReview func(ctx context.Context)
	// BeforeIssueList is awaited at the start of Issues, so a test can pause a belt
	// while the list is in flight.
	BeforeIssueList func(ctx context.Context)
	// failing holds the repositories whose issues and review requests can't be listed.
	failing map[string]bool
	// failingIssues holds single issues whose read should fail, so Resume can stop on reconciliation.
	failingIssues map[string]bool
	// pullsHeld is set by HoldPulls until ReleasePulls; reads then wait on gate.
	pullsHeld    bool
	gate         chan struct{}
	waitingPulls int
	// opensPulls is the state a branch lookup with no pull request opens one in. Nil opens none.
	opensPulls *core.PullRequestState
	nextPull   int
	ahead      map[string]int
	missing    map[string]bool
	lookups    int
}

// NewFakeGitHub is FakeGitHub(issues:reviews:pulls:opensPulls:).
func NewFakeGitHub(issues []core.Subject, reviews []FakeReviewRequest, pulls []FakePullHead, opensPulls *core.PullRequestState) *FakeGitHub {
	f := &FakeGitHub{
		issues: map[string]core.Subject{}, reviews: map[string]*reviewEntry{}, currentLogin: "you",
		failingReview: map[string]bool{}, pulls: map[string]core.PullRequest{}, heads: map[string]string{},
		pullsBehind: map[string]int{}, issueComments: map[string][]core.TicketStateComment{},
		failing: map[string]bool{}, failingIssues: map[string]bool{}, ahead: map[string]int{}, missing: map[string]bool{},
		opensPulls: opensPulls, nextPull: 200,
	}
	for _, issue := range issues {
		f.issues[fakeKey(issue.Repo, issue.Number)] = issue.Clone()
	}
	for _, p := range pulls {
		pull := clonePull(withRepository(p.Pull))
		key := fakeKey(pull.Repo, pull.Number)
		f.pulls[key] = pull
		f.heads[key] = p.Head
		f.nextPull = max(f.nextPull, pull.Number)
	}
	for _, r := range reviews {
		f.requestLocked(r.Pull, lowerOr(r.Pull.Reviewer, "you"), r.Request)
	}
	return f
}

// lowerOr lowercases *s, or returns def when s is nil.
func lowerOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return strings.ToLower(*s)
}

// fakeKey is "<repo>#<n>", the key of a pull request or issue.
func fakeKey(repo string, number int) string { return repo + "#" + strconv.Itoa(number) }

// withRepository fills in a pull request's repository from its URL when it has none.
func withRepository(pull core.PullRequest) core.PullRequest {
	if pull.Repo == "" && pull.URL != "" {
		if path := urlPath(pull.URL); path != "" {
			var parts []string
			for _, p := range strings.Split(path, "/") {
				if p != "" {
					parts = append(parts, p)
				}
			}
			if len(parts) > 2 {
				parts = parts[:2]
			}
			pull.Repo = strings.Join(parts, "/")
		}
	}
	return pull
}

// urlPath is the path of an absolute URL, after the scheme and host; "" when it has none.
func urlPath(raw string) string {
	rest := raw
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			return rest[j:]
		}
		return ""
	}
	return rest
}

// SetLogin sets the signed-in login.
func (f *FakeGitHub) SetLogin(login string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentLogin = login
}

// LoginCount is how many times Login was called.
func (f *FakeGitHub) LoginCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCount
}

// SetFailing makes every read of repo fail (or not).
func (f *FakeGitHub) SetFailing(repo string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fail {
		f.failing[repo] = true
	} else {
		delete(f.failing, repo)
	}
}

// Calls returns the recorded calls so far, in order.
func (f *FakeGitHub) Calls() []FakeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeCall(nil), f.calls...)
}

// DefaultBranch is always "main".
func (f *FakeGitHub) DefaultBranch(_ context.Context, _ string) (string, error) { return "main", nil }

// Login is the current login; it counts the calls.
func (f *FakeGitHub) Login(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginCount++
	return f.currentLogin, nil
}

var _ GitHub = (*FakeGitHub)(nil)
