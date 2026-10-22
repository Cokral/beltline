// The issue and ticket side of FakeGitHub: issues, comments, blockers and ticket state.

package github

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// Put adds an issue, or replaces it.
func (f *FakeGitHub) Put(issue core.Subject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues[fakeKey(issue.Repo, issue.Number)] = issue.Clone()
}

// FailIssueRead makes the read of one issue fail (or not).
func (f *FakeGitHub) FailIssueRead(repo string, number int, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(repo, number)
	if fail {
		f.failingIssues[key] = true
	} else {
		delete(f.failingIssues, key)
	}
}

// Issues lists the open issues in repo with label, assigned to assignee when it is set.
func (f *FakeGitHub) Issues(ctx context.Context, repo, label string, assignee *string) ([]core.Subject, error) {
	if hook := f.BeforeIssueList; hook != nil {
		hook(ctx)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing[repo] {
		return nil, core.NewFailure("gh issue list: could not reach " + repo)
	}
	out := []core.Subject{}
	for _, issue := range f.issues {
		if issue.Repo != repo || !issue.IsOpen || !slices.Contains(issue.Labels, label) {
			continue
		}
		if assignee != nil && *assignee != "" && *assignee != "@me" && !slices.Contains(issue.Assignees, *assignee) {
			continue
		}
		out = append(out, issue.Clone())
	}
	sortSubjects(out)
	return out, nil
}

// sortSubjects orders subjects by number, then repository, so ties never depend on map order.
func sortSubjects(list []core.Subject) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Number != list[j].Number {
			return list[i].Number < list[j].Number
		}
		return list[i].Repo < list[j].Repo
	})
}

// Issue reads one issue.
func (f *FakeGitHub) Issue(_ context.Context, repo string, number int) (core.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(repo, number)
	if f.failingIssues[key] {
		return core.Subject{}, core.NewFailure("could not read " + repo + "#" + strconv.Itoa(number))
	}
	issue, ok := f.issues[key]
	if !ok {
		return core.Subject{}, core.NewFailure("no issue " + repo + "#" + strconv.Itoa(number))
	}
	return issue.Clone(), nil
}

// Comment records a comment on an issue.
func (f *FakeGitHub) Comment(_ context.Context, repo string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeCall{Kind: FakeCallComment, Repo: repo, Number: number, Body: body})
	return nil
}

// Blockers are an issue's open blockers: its refs ("#n" in its repository) that are not closed.
func (f *FakeGitHub) Blockers(_ context.Context, repo string, number int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, ref := range f.issues[fakeKey(repo, number)].Blockers {
		n, err := strconv.Atoi(strings.TrimPrefix(ref, "#"))
		if !strings.HasPrefix(ref, "#") || err != nil {
			out = append(out, ref)
			continue
		}
		blocker, ok := f.issues[fakeKey(repo, n)]
		if !ok || blocker.IsOpen {
			out = append(out, ref)
		}
	}
	return out, nil
}

// AddIssueComment records a comment on an issue, for ticket watches.
func (f *FakeGitHub) AddIssueComment(repo string, number int, id, author, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(repo, number)
	f.issueComments[key] = append(f.issueComments[key], core.TicketStateComment{ID: id, Author: author, Body: body})
}

// IssueState reads an issue's state, comments and sub-issues. Its sub-issues are the
// issues in its repository whose parent is "#<n>".
func (f *FakeGitHub) IssueState(_ context.Context, repo string, number int) (core.TicketState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeKey(repo, number)
	issue, ok := f.issues[key]
	if !ok {
		return core.TicketState{}, core.NewFailure("no issue " + key)
	}
	var kids []core.Subject
	for _, candidate := range f.issues {
		if candidate.Repo == repo && candidate.Parent != nil && *candidate.Parent == "#"+strconv.Itoa(number) {
			kids = append(kids, candidate.Clone())
		}
	}
	sortSubjects(kids)
	children := []core.TicketStateChild{}
	for _, kid := range kids {
		status := "closed"
		if kid.IsOpen {
			status = "open"
		}
		children = append(children, core.TicketStateChild{
			Ref: "#" + strconv.Itoa(kid.Number), Title: kid.Title, Status: status, IsOpen: kid.IsOpen, Labels: kid.Labels,
		})
	}
	status := "closed"
	if issue.IsOpen {
		status = "open"
	}
	comments := append([]core.TicketStateComment{}, f.issueComments[key]...)
	return core.TicketState{Status: status, IsOpen: issue.IsOpen, Comments: comments, Children: children}, nil
}
