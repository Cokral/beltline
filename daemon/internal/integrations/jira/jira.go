// The Jira protocol and AcliJira, which reads and writes tickets through the Atlassian CLI (acli),
// which keeps its own sign-in.

package jira

import (
	"context"
	"errors"
	"strings"
	"sync"

	"encoding/json"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// Jira is everything the app asks of Jira. AcliJira talks to acli; tests and demo
// mode use FakeJira.
type Jira interface {
	// Search returns the tickets that match jql. Their Repo is not set.
	Search(ctx context.Context, jql string) ([]core.Subject, error)
	Ticket(ctx context.Context, key string) (core.Subject, error)
	Comment(ctx context.Context, key, body string) error
	// TicketState is the status, comments and the tickets under a ticket, for a ticket watch.
	TicketState(ctx context.Context, key string) (core.TicketState, error)
}

// AcliJira reads and writes Jira through acli.
type AcliJira struct {
	Shell shell.Runner

	mu sync.Mutex
	// site is the site acli is signed in to, such as example.atlassian.net, once read.
	site *string
}

// NewAcliJira is AcliJira(shell:). A nil runner is the process runner.
func NewAcliJira(sh shell.Runner) *AcliJira {
	if sh == nil {
		sh = shell.ProcessRunner{}
	}
	return &AcliJira{Shell: sh}
}

var _ Jira = (*AcliJira)(nil)

const (
	acliFields = "summary,description,labels,status,parent,issuelinks"
	// acliSearchFields is what a search may ask for: acli's search refuses parent and issuelinks.
	acliSearchFields = "key,summary"
)

// acli runs acli and returns its stdout, or a Failure naming the command.
func (j *AcliJira) acli(ctx context.Context, arguments []string) (string, error) {
	result := shell.Bounded(ctx, j.Shell, "acli", arguments, "", nil)
	if !result.OK() {
		if result.Status == 127 {
			return "", core.NewFailure("acli is not installed: brew install acli")
		}
		prefix := arguments
		if len(prefix) > 3 {
			prefix = prefix[:3]
		}
		return "", core.NewFailure("acli " + strings.Join(prefix, " ") + ": " + result.Message())
	}
	return result.Stdout, nil
}

// siteName is the signed-in site, for links to tickets. Empty when acli doesn't say,
// or when its lookup was stopped: a stopped lookup is not cached, so the next one asks again.
func (j *AcliJira) siteName(ctx context.Context) string {
	j.mu.Lock()
	cached := j.site
	j.mu.Unlock()
	if cached != nil {
		return *cached
	}
	result := shell.Bounded(ctx, j.Shell, "acli", []string{"jira", "auth", "status"}, "", nil)
	if result.IsStopped() {
		return ""
	}
	name := ""
	for _, line := range strings.Split(result.Stdout+"\n"+result.Stderr, "\n") {
		if line == "" {
			continue
		}
		if trimmed := strings.Trim(line, " \t"); strings.HasPrefix(trimmed, "Site:") {
			name = strings.Trim(trimmed[5:], " \t")
			break
		}
	}
	if result.OK() {
		j.mu.Lock()
		j.site = &name
		j.mu.Unlock()
	}
	return name
}

// Search lists the tickets that match jql, each read on its own for its epic and
// blockers, which the search can't give.
func (j *AcliJira) Search(ctx context.Context, jql string) ([]core.Subject, error) {
	text, err := j.acli(ctx, []string{"jira", "workitem", "search", "--jql", jql, "--fields", acliSearchFields, "--limit", "100", "--json"})
	if err != nil {
		return nil, err
	}
	answer, err := decodeAcliJSON([]byte(text))
	if err != nil {
		return nil, err
	}
	list := objs(answer)
	if list == nil {
		return nil, core.NewFailure("Could not read acli's answer")
	}
	tickets := []core.Subject{}
	for _, item := range list {
		key, ok := str(item["key"])
		if !ok {
			continue
		}
		ticket, err := j.Ticket(ctx, key)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, nil
}

// Ticket reads one ticket.
func (j *AcliJira) Ticket(ctx context.Context, key string) (core.Subject, error) {
	text, err := j.acli(ctx, []string{"jira", "workitem", "view", key, "--fields", acliFields, "--json"})
	if err != nil {
		return core.Subject{}, err
	}
	object, ok, err := parseObject(text)
	if err != nil {
		return core.Subject{}, err
	}
	if ok {
		if ticket, ok := AcliJiraTicket(object, j.siteName(ctx)); ok {
			return ticket, nil
		}
	}
	return core.Subject{}, core.NewFailure("Could not read " + key + " from acli")
}

// Comment adds a comment to a ticket.
func (j *AcliJira) Comment(ctx context.Context, key, body string) error {
	_, err := j.acli(ctx, []string{"jira", "workitem", "comment", "create", "--key", key, "--body", body})
	return err
}

// TicketState reads a ticket's status and comments, and the tickets under it.
func (j *AcliJira) TicketState(ctx context.Context, key string) (core.TicketState, error) {
	text, err := j.acli(ctx, []string{"jira", "workitem", "view", key, "--fields", "status,comment", "--json"})
	if err != nil {
		return core.TicketState{}, err
	}
	object, ok, err := parseObject(text)
	if err != nil {
		return core.TicketState{}, err
	}
	if !ok {
		return core.TicketState{}, core.NewFailure("Could not read " + key + " from acli")
	}
	list, err := j.acli(ctx, []string{"jira", "workitem", "search", "--jql", "parent = " + key, "--fields", "summary,status,labels", "--limit", "200", "--json"})
	if err != nil {
		return core.TicketState{}, err
	}
	answer, err := decodeAcliJSON([]byte(list))
	if err != nil {
		return core.TicketState{}, err
	}
	children := objs(answer)
	if children == nil {
		children = []map[string]any{}
	}
	return AcliJiraState(object, children), nil
}

// parseObject reads text as a JSON object, as `try JSONSerialization.jsonObject(...)
// as? [String: Any]` does: text that is not JSON is the parse error, and any other
// top-level value is not an object (ok is false).
func parseObject(text string) (map[string]any, bool, error) {
	v, err := decodeAcliJSON([]byte(text))
	if err != nil {
		return nil, false, err
	}
	m := obj(v)
	return m, m != nil, nil
}

// decodeAcliJSON reads acli's answer as one JSON object or array. The decoder allows
// no top-level fragments, so a top-level
// scalar (5, null, "x") is the same parse error as text that is not JSON.
func decodeAcliJSON(data []byte) (any, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errors.New("The given data was not valid JSON.") //nolint:staticcheck // ST1005: the wording is kept as the acli and gh parse errors have always read.
	}
	switch v.(type) {
	case map[string]any, []any:
		return v, nil
	}
	return nil, errors.New("The given data was not valid JSON.") //nolint:staticcheck // ST1005: the wording is kept as the acli and gh parse errors have always read.
}

// AcliJiraState is the status and comments of a ticket, and the tickets under it,
// from Jira's JSON.
func AcliJiraState(object map[string]any, children []map[string]any) core.TicketState {
	fields := obj(object["fields"])
	if fields == nil {
		fields = map[string]any{}
	}
	comments := []core.TicketStateComment{}
	var rawComments []map[string]any
	if commentField := obj(fields["comment"]); commentField != nil {
		rawComments = objs(commentField["comments"])
	}
	for _, comment := range rawComments {
		id, ok := str(comment["id"])
		if !ok {
			continue
		}
		author := "someone"
		if a := obj(comment["author"]); a != nil {
			author = strOr(a["displayName"], "someone")
		}
		comments = append(comments, core.TicketStateComment{ID: id, Author: author, Body: ADFText(comment["body"])})
	}
	name, done := acliJiraStatus(fields)
	kids := []core.TicketStateChild{}
	for _, child := range children {
		key, ok := str(child["key"])
		if !ok {
			continue
		}
		childFields := obj(child["fields"])
		if childFields == nil {
			continue
		}
		childName, childDone := acliJiraStatus(childFields)
		kids = append(kids, core.TicketStateChild{
			Ref: key, Title: strOr(childFields["summary"], key), Status: childName,
			IsOpen: !childDone, Labels: labelsOf(childFields),
		})
	}
	return core.TicketState{Status: name, IsOpen: !done, Comments: comments, Children: kids}
}

// labelsOf is the fields' labels as strings, or an empty list.
func labelsOf(fields map[string]any) []string {
	if labels := strs(fields["labels"]); labels != nil {
		return labels
	}
	return []string{}
}

// acliJiraStatus is a status's name, and whether it is in the done category.
func acliJiraStatus(fields map[string]any) (string, bool) {
	status := obj(fields["status"])
	category := ""
	if c := obj(status["statusCategory"]); c != nil {
		category = strOr(c["key"], "")
	}
	return strOr(status["name"], ""), category == "done"
}

// acliJiraBlockers are the open tickets that block this one: links of the Blocks
// type whose other end is the inward issue, the one that "blocks" it.
func acliJiraBlockers(fields map[string]any) []string {
	out := []string{}
	for _, link := range objs(fields["issuelinks"]) {
		linkType := obj(link["type"])
		name := strings.ToLower(strOr(linkType["name"], ""))
		inward := strings.ToLower(strOr(linkType["inward"], ""))
		blocks := name == "blocks" || strings.Contains(inward, "blocked by")
		if !blocks {
			continue
		}
		inwardIssue := obj(link["inwardIssue"])
		if inwardIssue == nil {
			continue
		}
		key, ok := str(inwardIssue["key"])
		if !ok {
			continue
		}
		if _, done := acliJiraStatus(obj(inwardIssue["fields"])); done {
			continue
		}
		out = append(out, key)
	}
	return out
}

// AcliJiraTicket is a ticket from Jira's JSON. It is open until its status is in the
// done category. ok is false when the key or fields are missing.
func AcliJiraTicket(object map[string]any, site string) (core.Subject, bool) {
	key, ok := str(object["key"])
	fields := obj(object["fields"])
	if !ok || fields == nil {
		return core.Subject{}, false
	}
	category := ""
	if status := obj(fields["status"]); status != nil {
		if c := obj(status["statusCategory"]); c != nil {
			category = strOr(c["key"], "")
		}
	}
	var parent *string
	if p := obj(fields["parent"]); p != nil {
		if k, ok := str(p["key"]); ok {
			parent = &k
		}
	}
	url := ""
	if site != "" {
		url = "https://" + site + "/browse/" + key
	}
	return core.Subject{
		Kind: core.SubjectKindJira, Repo: "", Key: key, Title: strOr(fields["summary"], key),
		Body: ADFText(fields["description"]), Labels: labelsOf(fields), URL: url, IsOpen: category != "done",
		Parent: parent, Blockers: acliJiraBlockers(fields), Assignees: []string{},
	}, true
}
