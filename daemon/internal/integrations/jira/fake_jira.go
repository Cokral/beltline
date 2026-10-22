// An in-memory Jira for tests and demo mode. A ticket matches every query until it is done, and it
// records every outward call.

package jira

import (
	"context"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/robbertmanders/beltline/daemon/internal/core"
)

// FakeJiraCall is one comment a FakeJira recorded.
type FakeJiraCall struct {
	Key  string
	Body string
}

// FakeJira is safe for concurrent use.
type FakeJira struct {
	mu sync.Mutex

	tickets  map[string]core.Subject
	matching []string
	calls    []FakeJiraCall
	searches []string
	// statuses are the status names by key; a ticket's children are the tickets whose parent is its key.
	statuses map[string]string
	comments map[string][]core.TicketStateComment
	// filter says which listed tickets a query returns; every one without it.
	filter func(jql string, ticket core.Subject) bool
}

// NewFakeJira is FakeJira(tickets:statuses:filter:). The listed tickets match the query at first.
func NewFakeJira(tickets []core.Subject, statuses map[string]string, filter func(jql string, ticket core.Subject) bool) *FakeJira {
	f := &FakeJira{
		tickets: map[string]core.Subject{}, statuses: map[string]string{}, comments: map[string][]core.TicketStateComment{},
		filter: filter,
	}
	for _, t := range tickets {
		f.tickets[t.Key] = t
		f.matching = append(f.matching, t.Key)
	}
	maps.Copy(f.statuses, statuses)
	return f
}

var _ Jira = (*FakeJira)(nil)

// Put adds a ticket, or replaces it, and lists it as matching.
func (f *FakeJira) Put(ticket core.Subject) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets[ticket.Key] = ticket
	if !slices.Contains(f.matching, ticket.Key) {
		f.matching = append(f.matching, ticket.Key)
	}
}

// Unmatch means the ticket no longer matches the query, but stays as it is.
func (f *FakeJira) Unmatch(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.matching[:0]
	for _, k := range f.matching {
		if k != key {
			kept = append(kept, k)
		}
	}
	f.matching = kept
}

// Calls returns the recorded comments, in order.
func (f *FakeJira) Calls() []FakeJiraCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeJiraCall(nil), f.calls...)
}

// Searches returns the queries searched so far, in order.
func (f *FakeJira) Searches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.searches...)
}

// Search returns the matching tickets that the filter accepts for jql.
func (f *FakeJira) Search(_ context.Context, jql string) ([]core.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, jql)
	out := []core.Subject{}
	for _, key := range f.matching {
		t, ok := f.tickets[key]
		if !ok {
			continue
		}
		if f.filter == nil || f.filter(jql, t) {
			out = append(out, t)
		}
	}
	return out, nil
}

// Ticket reads one ticket.
func (f *FakeJira) Ticket(_ context.Context, key string) (core.Subject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[key]
	if !ok {
		return core.Subject{}, core.NewFailure("no ticket " + key)
	}
	return t, nil
}

// Comment records a comment on a ticket.
func (f *FakeJira) Comment(_ context.Context, key, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, FakeJiraCall{Key: key, Body: body})
	return nil
}

// SetStatus sets a ticket's status; a done status closes it.
func (f *FakeJira) SetStatus(key, status string, done bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[key] = status
	if t, ok := f.tickets[key]; ok {
		t.IsOpen = !done
		f.tickets[key] = t
	}
}

// AddComment adds a comment to a ticket.
func (f *FakeJira) AddComment(key, id, author, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[key] = append(f.comments[key], core.TicketStateComment{ID: id, Author: author, Body: body})
}

// TicketState reads a ticket's status, comments and the tickets under it.
func (f *FakeJira) TicketState(_ context.Context, key string) (core.TicketState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ticket, ok := f.tickets[key]
	if !ok {
		return core.TicketState{}, core.NewFailure("no ticket " + key)
	}
	var kids []core.Subject
	for _, t := range f.tickets {
		if t.Parent != nil && *t.Parent == key {
			kids = append(kids, t)
		}
	}
	sort.Slice(kids, func(i, j int) bool { return kids[i].Key < kids[j].Key })
	children := []core.TicketStateChild{}
	for _, kid := range kids {
		children = append(children, core.TicketStateChild{
			Ref: kid.Key, Title: kid.Title, Status: f.status(kid), IsOpen: kid.IsOpen, Labels: kid.Labels,
		})
	}
	comments := append([]core.TicketStateComment{}, f.comments[key]...)
	return core.TicketState{Status: f.status(ticket), IsOpen: ticket.IsOpen, Comments: comments, Children: children}, nil
}

// status is a ticket's status name: the set one, else "To Do" or "Done" by its openness.
func (f *FakeJira) status(t core.Subject) string {
	if s, ok := f.statuses[t.Key]; ok {
		return s
	}
	if t.IsOpen {
		return "To Do"
	}
	return "Done"
}
